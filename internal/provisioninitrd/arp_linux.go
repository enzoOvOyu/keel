//go:build linux

package provisioninitrd

import (
	"context"
	"crypto/rand"
	"errors"
	"net"
	"net/netip"
	"time"

	"golang.org/x/sys/unix"
)

var errAddressConflict = errors.New("offered address conflicts on selected interface")

func networkShort(v uint16) int {
	// sockaddr_ll/socket protocol fields are network order on both supported
	// little-endian architectures (amd64 and arm64).
	return int(v<<8 | v>>8)
}

// probeAddress implements the RFC 5227 pre-use probe schedule. No address is
// assigned yet; competing claims stop configuration and lead to DHCPDECLINE.
func probeAddress(ctx context.Context, face *net.Interface, candidate netip.Addr) error {
	if RequireInitramfs() != nil || ctx.Err() != nil {
		return ErrUnavailable
	}
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, networkShort(unix.ETH_P_ARP))
	if err != nil {
		return ErrUnavailable
	}
	defer unix.Close(fd)
	address := &unix.SockaddrLinklayer{Protocol: uint16(networkShort(unix.ETH_P_ARP)), Ifindex: face.Index, Halen: 6}
	for i := 0; i < 6; i++ {
		address.Addr[i] = 255
	}
	if unix.Bind(fd, address) != nil {
		return ErrUnavailable
	}
	message, err := arpProbe(face.HardwareAddr, candidate)
	if err != nil {
		return err
	}
	var random [3]byte
	if _, err = rand.Read(random[:]); err != nil {
		return ErrUnavailable
	}
	// Initial wait 0..1s, then three probes 1..2s apart, then wait 2s.
	if err = observeARP(ctx, fd, face, candidate, time.Duration(random[0])*time.Second/255); err != nil {
		return err
	}
	for probe := 0; probe < 3; probe++ {
		if ctx.Err() != nil || unix.Sendto(fd, message, 0, address) != nil {
			return ErrUnavailable
		}
		delay := 2 * time.Second
		if probe < 2 {
			delay = time.Second + time.Duration(random[probe+1])*time.Second/255
		}
		if err = observeARP(ctx, fd, face, candidate, delay); err != nil {
			return err
		}
	}
	return nil
}

func observeARP(ctx context.Context, fd int, face *net.Interface, candidate netip.Addr, duration time.Duration) error {
	until := time.Now().Add(duration)
	buffer := make([]byte, 2048)
	for time.Now().Before(until) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		wait := time.Until(until)
		if wait > 100*time.Millisecond {
			wait = 100 * time.Millisecond
		}
		poll := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(poll, int((wait+time.Millisecond-1)/time.Millisecond))
		if err == unix.EINTR {
			continue
		}
		if err != nil {
			return ErrUnavailable
		}
		if n == 0 {
			continue
		}
		if poll[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return ErrUnavailable
		}
		n, source, err := unix.Recvfrom(fd, buffer, unix.MSG_DONTWAIT)
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err != nil {
			return ErrUnavailable
		}
		link, ok := source.(*unix.SockaddrLinklayer)
		if !ok || link.Ifindex != face.Index || link.Pkttype == unix.PACKET_OUTGOING {
			continue
		}
		if arpConflict(buffer[:n], face.HardwareAddr, candidate) {
			return errAddressConflict
		}
	}
	return nil
}
