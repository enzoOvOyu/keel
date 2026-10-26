//go:build linux

package provisioninitrd

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

type NetworkLease struct {
	Interface string       `json:"interface"`
	Address   netip.Addr   `json:"address"`
	Prefix    netip.Prefix `json:"prefix"`
	Server    netip.Addr   `json:"dhcp_server"`
	ExpiresAt time.Time    `json:"expires_at"`
}

// ConfigureNetwork is a PID1-only adapter. It modifies only the uniquely
// BOM-matched Ethernet interface, which must have no preexisting IPv4 address.
// It does not configure a default route, DNS, another NIC, or a DHCP server.
func ConfigureNetwork(ctx context.Context, input Input) (*NetworkLease, error) {
	if RequireInitramfs() != nil || input.validate(false) != nil || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	face, err := findBootInterface(ctx, input.BootMAC)
	if err != nil || requireUnconfigured(face) != nil {
		return nil, ErrUnavailable
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer unix.Close(fd)
	request, _ := unix.NewIfreq(face.Name)
	if unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, request) != nil {
		return nil, ErrUnavailable
	}
	request.SetUint16(request.Uint16() | unix.IFF_UP)
	if unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, request) != nil {
		return nil, ErrUnavailable
	}
	udp, wire, err := listenDHCP(ctx, face.Name)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer udp.Close()
	stop := context.AfterFunc(ctx, func() { udp.Close() })
	defer stop()
	var random [4]byte
	if _, err = rand.Read(random[:]); err != nil {
		return nil, ErrUnavailable
	}
	xid := binary.BigEndian.Uint32(random[:])
	if xid == 0 {
		return nil, ErrUnavailable
	}
	started := time.Now()
	offer, err := exchangeDHCP(ctx, wire, face, input, xid, 1, nil, started)
	if err != nil {
		return nil, ErrUnavailable
	}
	leaseStarted := time.Now().UTC()
	ack, err := exchangeDHCP(ctx, wire, face, input, xid, 3, offer, started)
	if err != nil {
		return nil, ErrUnavailable
	}
	// Count from the first REQUEST, conservatively including retransmission and
	// ACK latency; receipt time must not extend the server's lease.
	expires := leaseStarted.Add(time.Duration(ack.seconds) * time.Second)
	if err = probeAddress(ctx, face, ack.address); err != nil {
		// RFC 2131: tell the selected server the offered address is in use.
		// A transient probe error also stops; no lease retry or address guess.
		if errors.Is(err, errAddressConflict) {
			if message, e := dhcpMessage(input, xid, 4, ack, time.Since(started)); e == nil {
				sendDHCP(wire, face.Index, message)
			}
		}
		return nil, ErrUnavailable
	}
	current, err := net.InterfaceByIndex(face.Index)
	if err != nil || current.Name != face.Name || current.HardwareAddr.String() != input.BootMAC || requireUnconfigured(current) != nil || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	// Set the address and exact subnet mask. A failure removes the address again;
	// no subsequent download starts on a partially configured interface.
	if request.SetInet4Addr(ack.address.AsSlice()) != nil || unix.IoctlIfreq(fd, unix.SIOCSIFADDR, request) != nil {
		return nil, ErrUnavailable
	}
	if request.SetInet4Addr(net.CIDRMask(input.Clients.Bits(), 32)) != nil || unix.IoctlIfreq(fd, unix.SIOCSIFNETMASK, request) != nil || ctx.Err() != nil {
		request.SetInet4Addr([]byte{0, 0, 0, 0})
		unix.IoctlIfreq(fd, unix.SIOCSIFADDR, request)
		return nil, ErrUnavailable
	}
	return &NetworkLease{Interface: face.Name, Address: ack.address, Prefix: input.Clients, Server: ack.server, ExpiresAt: expires}, nil
}

func findBootInterface(ctx context.Context, mac string) (*net.Interface, error) {
	deadline := time.Now().Add(15 * time.Second)
	for {
		faces, err := net.Interfaces()
		if err != nil {
			return nil, ErrUnavailable
		}
		var found *net.Interface
		for i := range faces {
			f := &faces[i]
			if f.HardwareAddr.String() != mac {
				continue
			}
			if found != nil || !interfacePattern.MatchString(f.Name) || f.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 || f.Flags&net.FlagBroadcast == 0 {
				return nil, ErrUnavailable
			}
			found = f
		}
		if found != nil {
			return found, nil
		}
		if time.Now().After(deadline) {
			return nil, ErrUnavailable
		}
		if waitContext(ctx, 250*time.Millisecond) != nil {
			return nil, ErrUnavailable
		}
	}
}
func requireUnconfigured(face *net.Interface) error {
	addresses, err := face.Addrs()
	if err != nil {
		return ErrUnavailable
	}
	for _, a := range addresses {
		p, e := netip.ParsePrefix(a.String())
		if e != nil || p.Addr().Is4() {
			return ErrUnavailable
		}
	}
	return nil
}
func listenDHCP(ctx context.Context, name string) (*net.UDPConn, *ipv4.PacketConn, error) {
	lc := net.ListenConfig{Control: func(_, _ string, raw syscall.RawConn) error {
		var socketErr error
		err := raw.Control(func(fd uintptr) {
			socketErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, name)
			if socketErr == nil {
				socketErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
			}
		})
		if err != nil {
			return err
		}
		return socketErr
	}}
	packet, err := lc.ListenPacket(ctx, "udp4", "0.0.0.0:68")
	if err != nil {
		return nil, nil, err
	}
	udp, ok := packet.(*net.UDPConn)
	if !ok {
		packet.Close()
		return nil, nil, ErrUnavailable
	}
	wire := ipv4.NewPacketConn(udp)
	if wire.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true) != nil {
		udp.Close()
		return nil, nil, ErrUnavailable
	}
	return udp, wire, nil
}
func sendDHCP(wire *ipv4.PacketConn, index int, message []byte) error {
	if wire.SetWriteDeadline(time.Now().Add(time.Second)) != nil {
		return ErrUnavailable
	}
	_, err := wire.WriteTo(message, &ipv4.ControlMessage{IfIndex: index}, &net.UDPAddr{IP: net.IPv4bcast, Port: 67})
	return err
}
func exchangeDHCP(ctx context.Context, wire *ipv4.PacketConn, face *net.Interface, input Input, xid uint32, kind byte, offer *dhcpReply, started time.Time) (*dhcpReply, error) {
	buffer := make([]byte, 1501)
	for attempt := 0; attempt < 3; attempt++ {
		message, err := dhcpMessage(input, xid, kind, offer, time.Since(started))
		if err != nil || ctx.Err() != nil || sendDHCP(wire, face.Index, message) != nil {
			return nil, ErrUnavailable
		}
		// 4, 8, 16 second exponential retransmission with bounded random jitter.
		var jitter [1]byte
		if _, err = rand.Read(jitter[:]); err != nil {
			return nil, ErrUnavailable
		}
		deadline := time.Now().Add(time.Duration(4<<attempt)*time.Second + time.Duration(jitter[0])*time.Millisecond*4)
		if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
			deadline = limit
		}
		if wire.SetReadDeadline(deadline) != nil {
			return nil, ErrUnavailable
		}
		for ctx.Err() == nil {
			n, cm, source, err := wire.ReadFrom(buffer)
			if err != nil {
				if e, ok := err.(net.Error); ok && e.Timeout() {
					break
				}
				return nil, ErrUnavailable
			}
			peer, ok := source.(*net.UDPAddr)
			if !ok || peer.Port != 67 || cm == nil || cm.IfIndex != face.Index || cm.Dst == nil || !cm.Dst.Equal(net.IPv4bcast) {
				continue
			}
			address, ok := netip.AddrFromSlice(peer.IP)
			if !ok {
				continue
			}
			r, err := parseDHCP(input, xid, address.Unmap(), buffer[:n])
			if err != nil {
				continue
			}
			if kind == 1 && r.kind == 2 {
				return r, nil
			}
			if kind == 3 && r.server == offer.server {
				if r.kind == 6 {
					return nil, ErrUnavailable
				}
				if r.kind == 5 && r.address == offer.address {
					return r, nil
				}
			}
		}
	}
	return nil, ErrUnavailable
}

func waitContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// WithNetworkLease bounds all read-only work by the original lease and withdraws
// the address on completion, expiration, cancellation or a subsequent ARP claim.
// No initial lease can silently survive into a later installation operation.
func WithNetworkLease(ctx context.Context, lease *NetworkLease, work func(context.Context) error) error {
	if RequireInitramfs() != nil || lease == nil || work == nil || !lease.Address.Is4() || !lease.Prefix.IsValid() || !lease.Prefix.Contains(lease.Address) || !interfacePattern.MatchString(lease.Interface) {
		return ErrUnavailable
	}
	face, err := net.InterfaceByName(lease.Interface)
	if err != nil {
		return ErrUnavailable
	}
	defer releaseAddress(face, lease.Address)
	until := lease.ExpiresAt.Add(-10 * time.Second)
	if !until.After(time.Now()) || until.After(time.Now().Add(7*24*time.Hour)) {
		return ErrUnavailable
	}
	ctx, cancel := context.WithDeadline(ctx, until)
	defer cancel()
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, networkShort(unix.ETH_P_ARP))
	if err != nil {
		return ErrUnavailable
	}
	defer unix.Close(fd)
	if unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: uint16(networkShort(unix.ETH_P_ARP)), Ifindex: face.Index}) != nil {
		return ErrUnavailable
	}
	watchDone := make(chan error, 1)
	go func() {
		err := observeARP(ctx, fd, face, lease.Address, time.Until(until))
		if err != nil {
			cancel()
		}
		watchDone <- err
	}()
	err = work(ctx)
	wasCanceled := ctx.Err() != nil
	cancel()
	watchErr := <-watchDone
	if err != nil || wasCanceled || (watchErr != nil && !errors.Is(watchErr, context.Canceled)) {
		return ErrUnavailable
	}
	return nil
}

func releaseAddress(face *net.Interface, address netip.Addr) {
	if RequireInitramfs() != nil {
		return
	}
	current, err := net.InterfaceByIndex(face.Index)
	if err != nil || current.Name != face.Name || current.HardwareAddr.String() != face.HardwareAddr.String() {
		return
	}
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return
	}
	defer unix.Close(fd)
	request, err := unix.NewIfreq(face.Name)
	if err != nil {
		return
	}
	if unix.IoctlIfreq(fd, unix.SIOCGIFADDR, request) != nil {
		return
	}
	assigned, err := request.Inet4Addr()
	if err != nil || !net.IP(assigned).Equal(net.IP(address.AsSlice())) {
		return
	}
	request.SetInet4Addr([]byte{0, 0, 0, 0})
	unix.IoctlIfreq(fd, unix.SIOCSIFADDR, request)
}
