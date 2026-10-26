//go:build linux

package provisionboot

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

// ProxyConfig is opt-in even for isolated use. Production ports are fixed to
// DHCP/67 and PXE/4011; alternate unprivileged test ports require Development.
// It does not load environment variables or start from an API/worker by default.
type ProxyConfig struct {
	Enabled      bool
	Scope        Scope
	DiscoverPort uint16
	PXEPort      uint16
	ClientPort   uint16
}

func (c ProxyConfig) ports() (uint16, uint16, uint16, error) {
	if !c.Enabled || c.Scope.Validate() != nil {
		return 0, 0, 0, ErrDenied
	}
	discover, pxe, client := c.DiscoverPort, c.PXEPort, c.ClientPort
	if discover == 0 {
		discover = 67
	}
	if pxe == 0 {
		pxe = 4011
	}
	if client == 0 {
		client = 68
	}
	if !c.Scope.Development && (discover != 67 || pxe != 4011 || client != 68) {
		return 0, 0, 0, ErrDenied
	}
	if c.Scope.Development && ((discover != 67 && discover < 1024) || (pxe != 4011 && pxe < 1024) || (client != 68 && client < 1024)) {
		return 0, 0, 0, ErrDenied
	}
	if discover == pxe || discover == client || pxe == client {
		return 0, 0, 0, ErrDenied
	}
	return discover, pxe, client, nil
}
func scopeInterface(scope Scope) (int, error) {
	iface, err := net.InterfaceByName(scope.Interface)
	if err != nil || iface.Flags&net.FlagUp == 0 {
		return 0, ErrDenied
	}
	addresses, err := iface.Addrs()
	if err != nil {
		return 0, ErrDenied
	}
	for _, address := range addresses {
		prefix, e := netip.ParsePrefix(address.String())
		if e == nil && prefix.Addr() == scope.Server && prefix.Masked() == scope.Clients {
			return iface.Index, nil
		}
	}
	return 0, ErrDenied
}
func listenProxy(ctx context.Context, scope Scope, port uint16) (*net.UDPConn, *ipv4.PacketConn, error) {
	return listenScoped(ctx, scope, port, "0.0.0.0")
}
func listenScoped(ctx context.Context, scope Scope, port uint16, address string) (*net.UDPConn, *ipv4.PacketConn, error) {
	listen := net.ListenConfig{Control: func(network, address string, raw syscall.RawConn) error {
		var socketErr error
		err := raw.Control(func(fd uintptr) {
			socketErr = unix.SetsockoptString(int(fd), unix.SOL_SOCKET, unix.SO_BINDTODEVICE, scope.Interface)
			if socketErr == nil {
				socketErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
			}
		})
		if err != nil {
			return err
		}
		return socketErr
	}}
	packet, err := listen.ListenPacket(ctx, "udp4", net.JoinHostPort(address, strconv.Itoa(int(port))))
	if err != nil {
		return nil, nil, err
	}
	udp, ok := packet.(*net.UDPConn)
	if !ok {
		packet.Close()
		return nil, nil, ErrDenied
	}
	udp.SetReadBuffer(128 << 10)
	wire := ipv4.NewPacketConn(udp)
	if err = wire.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true); err != nil {
		udp.Close()
		return nil, nil, err
	}
	return udp, wire, nil
}

// ServeProxy owns only its two freshly opened sockets. Kernel interface binding
// plus receive interface/destination checks constrain broadcast replies to the
// reserved domain. A port collision fails without stopping another DHCP service.
func ServeProxy(ctx context.Context, config ProxyConfig, authority BootAuthority) error {
	discover, pxe, client, err := config.ports()
	if err != nil || authority == nil {
		return ErrDenied
	}
	index, err := scopeInterface(config.Scope)
	if err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	discoveryUDP, discovery, err := listenProxy(run, config.Scope, discover)
	if err != nil {
		return err
	}
	defer discoveryUDP.Close()
	pxeUDP, pxeWire, err := listenProxy(run, config.Scope, pxe)
	if err != nil {
		return err
	}
	defer pxeUDP.Close()
	stop := context.AfterFunc(run, func() { discoveryUDP.Close(); pxeUDP.Close() })
	defer stop()
	proxy := &Proxy{Scope: config.Scope, Authority: authority}
	gate := &packetGate{}
	results := make(chan error, 2)
	go func() { results <- serveProxySocket(run, proxy, discovery, index, 67, client, gate) }()
	go func() { results <- serveProxySocket(run, proxy, pxeWire, index, 4011, client, gate) }()
	first := <-results
	cancel()
	second := <-results
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if first != nil && !errors.Is(first, net.ErrClosed) {
		return first
	}
	return second
}
func serveProxySocket(ctx context.Context, proxy *Proxy, wire *ipv4.PacketConn, index int, logicalPort, clientPort uint16, gate *packetGate) error {
	buffer := make([]byte, 1501)
	for {
		n, control, source, err := wire.ReadFrom(buffer)
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if n > 1500 || control == nil || control.IfIndex != index {
			continue
		}
		dst, ok := netip.AddrFromSlice(control.Dst.To4())
		if !ok || (dst != proxy.Scope.Server && dst != proxy.Scope.Broadcast() && dst != netip.MustParseAddr("255.255.255.255")) {
			continue
		}
		peer, ok := source.(*net.UDPAddr)
		if !ok || peer.Port != int(clientPort) {
			continue
		}
		addr, ok := netip.AddrFromSlice(peer.IP.To4())
		if !ok {
			continue
		}
		parsed, err := ParseClient(buffer[:n])
		if err != nil || !gate.allow(parsed.MAC, time.Now()) {
			continue
		}
		request, done := context.WithTimeout(ctx, 2*time.Second)
		reply, err := proxy.Respond(request, buffer[:n], netip.AddrPortFrom(addr, 68), logicalPort)
		done()
		if err != nil {
			continue
		}
		destination := net.UDPAddrFromAddrPort(netip.AddrPortFrom(reply.Destination.Addr(), clientPort))
		_, err = wire.WriteTo(reply.Packet, &ipv4.ControlMessage{Src: net.IP(proxy.Scope.Server.AsSlice()), IfIndex: index}, destination)
		if err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

// Bound authority work under packet floods. The map is reset each second and
// cannot grow beyond the total allowance, independent of spoofed client MACs.
type packetGate struct {
	mu      sync.Mutex
	second  int64
	total   int
	clients map[string]int
}

func (g *packetGate) allow(mac string, now time.Time) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.second != now.Unix() {
		g.second = now.Unix()
		g.total = 0
		g.clients = map[string]int{}
	}
	if g.total >= 1024 || g.clients[mac] >= 8 {
		return false
	}
	g.total++
	g.clients[mac]++
	return true
}
