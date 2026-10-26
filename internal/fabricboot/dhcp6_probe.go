package fabricboot

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"net/netip"
	"time"
)

// DHCP6ProbeConfig is an operator-only relay canary. It never uses a
// device DUID and never infers an address, interface, relay or server identity.
type DHCP6ProbeConfig struct {
	LinkAddress    string `json:"link_address"`
	Server         string `json:"server"`
	Listen         string `json:"listen"`
	DUID           string `json:"duid"`
	ServerDUID     string `json:"server_duid"`
	IAID           uint32 `json:"iaid"`
	Address        string `json:"address"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}
type DHCP6Proof struct {
	LinkAddress      string    `json:"link_address"`
	Address          string    `json:"address"`
	DUID             string    `json:"duid"`
	ServerDUID       string    `json:"server_duid"`
	IAID             uint32    `json:"iaid"`
	Option136URIs    []string  `json:"option_136_uris"`
	TranscriptSHA256 string    `json:"transcript_sha256"`
	PreferredSeconds uint32    `json:"preferred_seconds"`
	ValidSeconds     uint32    `json:"valid_seconds"`
	VerifiedAt       time.Time `json:"verified_at"`
}

func (p DHCP6ProbeConfig) Validate() error {
	server, e := netip.ParseAddrPort(p.Server)
	if e != nil || !server.Addr().Is6() || server.Addr().Is4In6() || server.Addr().IsUnspecified() || server.Addr().IsMulticast() || server.Port() == 0 {
		return errors.New("DHCPv6 server must be an explicit unicast IPv6 endpoint")
	}
	listen, e := netip.ParseAddrPort(p.Listen)
	if e != nil || !listen.Addr().Is6() || listen.Addr().Is4In6() || listen.Addr().IsUnspecified() || listen.Addr().IsMulticast() || listen.Port() == 0 {
		return errors.New("DHCPv6 listener must be an explicit unicast IPv6 endpoint")
	}
	link, e := netip.ParseAddr(p.LinkAddress)
	if e != nil || !link.Is6() || link.Is4In6() || !link.IsGlobalUnicast() || link.Zone() != "" || link.String() != p.LinkAddress {
		return errors.New("explicit relay link address is required")
	}
	if server == listen {
		return errors.New("DHCPv6 listener and server must differ")
	}
	address, e := netip.ParseAddr(p.Address)
	if e != nil || !address.Is6() || address.Is4In6() || !address.IsGlobalUnicast() || address.Zone() != "" || address.String() != p.Address || address == listen.Addr() || address == server.Addr() {
		return errors.New("DHCPv6 canary requires a separate canonical global-unicast or ULA address")
	}
	client, e := decodeDUID(p.DUID)
	if e != nil {
		return e
	}
	serverID, e := decodeDUID(p.ServerDUID)
	if e != nil {
		return e
	}
	if bytes.Equal(client, serverID) {
		return errors.New("DHCPv6 canary and server identities must differ")
	}
	if p.IAID == 0 || p.TimeoutSeconds < 0 || p.TimeoutSeconds > 20 {
		return errors.New("explicit nonzero IAID and timeout up to20 seconds required")
	}
	return nil
}
func dhcp6Option(code uint16, data []byte) []byte {
	out := make([]byte, 4, len(data)+4)
	binary.BigEndian.PutUint16(out, code)
	binary.BigEndian.PutUint16(out[2:], uint16(len(data)))
	return append(out, data...)
}
func dhcp6Options(data []byte) (map[uint16][]byte, error) {
	if len(data) > 65535 {
		return nil, errors.New("DHCPv6 options exceed bounded frame")
	}
	out := map[uint16][]byte{}
	for len(data) > 0 {
		if len(data) < 4 {
			return nil, errors.New("truncated DHCPv6 option")
		}
		code, n := binary.BigEndian.Uint16(data), int(binary.BigEndian.Uint16(data[2:]))
		data = data[4:]
		if n > len(data) {
			return nil, errors.New("truncated DHCPv6 option value")
		}
		if _, exists := out[code]; exists {
			return nil, errors.New("duplicate DHCPv6 option unsupported")
		}
		out[code] = data[:n]
		data = data[n:]
	}
	return out, nil
}
func dhcp6Packet(kind byte, xid [3]byte, duid, serverID, ia []byte) []byte {
	out := []byte{kind, xid[0], xid[1], xid[2]}
	out = append(out, dhcp6Option(1, duid)...)
	if len(serverID) > 0 {
		out = append(out, dhcp6Option(2, serverID)...)
	}
	out = append(out, dhcp6Option(3, ia)...)
	out = append(out, dhcp6Option(6, []byte{0, 136})...)
	return append(out, dhcp6Option(8, []byte{0, 0})...)
}
func dhcp6Status(options map[uint16][]byte) error {
	if raw, ok := options[13]; ok && (len(raw) < 2 || binary.BigEndian.Uint16(raw) != 0) {
		return errors.New("DHCPv6 server or IA reported failure")
	}
	return nil
}
func dhcp6Lease(packet []byte, kind byte, xid [3]byte, p DHCP6ProbeConfig, redirect []byte) ([]byte, uint32, uint32, error) {
	if len(packet) < 4 || packet[0] != kind || !bytes.Equal(packet[1:4], xid[:]) {
		return nil, 0, 0, errors.New("DHCPv6 reply transaction mismatch")
	}
	options, e := dhcp6Options(packet[4:])
	if e != nil {
		return nil, 0, 0, e
	}
	duid, _ := decodeDUID(p.DUID)
	serverID, _ := decodeDUID(p.ServerDUID)
	if !bytes.Equal(options[1], duid) || !bytes.Equal(options[2], serverID) || !bytes.Equal(options[136], redirect) {
		return nil, 0, 0, errors.New("DHCPv6 reply identity or approved redirect mismatch")
	}
	if _, e = DecodeSZTPRedirect(options[136]); e != nil {
		return nil, 0, 0, e
	}
	if e = dhcp6Status(options); e != nil {
		return nil, 0, 0, e
	}
	// Rapid Commit and reconfiguration require separate recovery semantics.
	if _, ok := options[14]; ok {
		return nil, 0, 0, errors.New("Rapid Commit is not supported by this canary")
	}
	ia := options[3]
	if len(ia) < 12 || binary.BigEndian.Uint32(ia) != p.IAID {
		return nil, 0, 0, errors.New("DHCPv6 IAID mismatch")
	}
	t1, t2 := binary.BigEndian.Uint32(ia[4:]), binary.BigEndian.Uint32(ia[8:])
	if t1 > 0 && t2 > 0 && t1 > t2 {
		return nil, 0, 0, errors.New("invalid DHCPv6 renewal timers")
	}
	sub, e := dhcp6Options(ia[12:])
	if e != nil {
		return nil, 0, 0, e
	}
	if e = dhcp6Status(sub); e != nil {
		return nil, 0, 0, e
	}
	raw := sub[5]
	if len(raw) < 24 {
		return nil, 0, 0, errors.New("missing DHCPv6 IA address")
	}
	address, _ := netip.ParseAddr(p.Address)
	a := address.As16()
	if !bytes.Equal(raw[:16], a[:]) {
		return nil, 0, 0, errors.New("DHCPv6 canary address mismatch")
	}
	preferred, valid := binary.BigEndian.Uint32(raw[16:]), binary.BigEndian.Uint32(raw[20:])
	if valid == 0 || preferred > valid || t1 > valid || t2 > valid {
		return nil, 0, 0, errors.New("invalid DHCPv6 lease lifetimes")
	}
	nested, e := dhcp6Options(raw[24:])
	if e != nil {
		return nil, 0, 0, e
	}
	if e = dhcp6Status(nested); e != nil {
		return nil, 0, 0, e
	}
	return ia, preferred, valid, nil
}

// Verify136 performs one relayed Solicit/Advertise and Request/Reply exchange. It does
// not retry a write, use multicast, release customer leases or configure an OS.
func (p DHCP6ProbeConfig) Verify136(ctx context.Context, uris []string) (*DHCP6Proof, error) {
	if e := p.Validate(); e != nil {
		return nil, e
	}
	payload, e := EncodeSZTPv6Option(uris)
	if e != nil {
		return nil, e
	}
	redirect := payload[4:]
	seconds := p.TimeoutSeconds
	if seconds == 0 {
		seconds = 5
	}
	work, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	local, _ := netip.ParseAddrPort(p.Listen)
	remote, _ := netip.ParseAddrPort(p.Server)
	conn, e := net.ListenUDP("udp6", net.UDPAddrFromAddrPort(local))
	if e != nil {
		return nil, errors.New("DHCPv6 canary listener unavailable")
	}
	defer conn.Close()
	stop := context.AfterFunc(work, func() { conn.Close() })
	defer stop()
	deadline, _ := work.Deadline()
	if e = conn.SetDeadline(deadline); e != nil {
		return nil, e
	}
	var solicitID, requestID [3]byte
	if _, e = rand.Read(solicitID[:]); e != nil {
		return nil, e
	}
	if _, e = rand.Read(requestID[:]); e != nil {
		return nil, e
	}
	duid, _ := decodeDUID(p.DUID)
	serverID, _ := decodeDUID(p.ServerDUID)
	ia := make([]byte, 12)
	binary.BigEndian.PutUint32(ia, p.IAID)
	transcript := sha256.New()
	exchange := func(packet []byte, kind byte, xid [3]byte) ([]byte, uint32, uint32, error) {
		if work.Err() != nil {
			return nil, 0, 0, work.Err()
		}
		link, _ := netip.ParseAddr(p.LinkAddress)
		wire := dhcp6Relay(12, link, local.Addr(), packet)
		if _, err := conn.WriteToUDPAddrPort(wire, remote); err != nil {
			return nil, 0, 0, errors.New("DHCPv6 canary send failed")
		}
		transcript.Write(wire)
		buffer := make([]byte, 65536)
		n, peer, err := conn.ReadFromUDPAddrPort(buffer)
		if err != nil {
			return nil, 0, 0, errors.New("DHCPv6 canary reply unavailable")
		}
		if peer != remote {
			return nil, 0, 0, errors.New("DHCPv6 reply from unexpected endpoint")
		}
		if work.Err() != nil {
			return nil, 0, 0, work.Err()
		}
		inner, err := dhcp6RelayReply(buffer[:n], link, local.Addr())
		if err != nil {
			return nil, 0, 0, err
		}
		accepted, preferred, valid, err := dhcp6Lease(inner, kind, xid, p, redirect)
		if err != nil {
			return nil, 0, 0, err
		}
		transcript.Write(buffer[:n])
		return accepted, preferred, valid, nil
	}
	offered, _, _, e := exchange(dhcp6Packet(1, solicitID, duid, nil, ia), 2, solicitID)
	if e != nil {
		return nil, e
	}
	// Advertise renewal timers are hints, not a request to change server policy.
	requested := append([]byte(nil), offered...)
	for i := 4; i < 12; i++ {
		requested[i] = 0
	}
	_, preferred, valid, e := exchange(dhcp6Packet(3, requestID, duid, serverID, requested), 7, requestID)
	if e != nil {
		return nil, e
	}
	return &DHCP6Proof{LinkAddress: p.LinkAddress, Address: p.Address, DUID: p.DUID, ServerDUID: p.ServerDUID, IAID: p.IAID, Option136URIs: append([]string(nil), uris...), TranscriptSHA256: hex.EncodeToString(transcript.Sum(nil)), PreferredSeconds: preferred, ValidSeconds: valid, VerifiedAt: time.Now().UTC()}, nil
}

func dhcp6Relay(kind byte, link, peer netip.Addr, inner []byte) []byte {
	out := make([]byte, 34)
	out[0] = kind
	a, b := link.As16(), peer.WithZone("").As16()
	copy(out[2:18], a[:])
	copy(out[18:34], b[:])
	return append(out, dhcp6Option(9, inner)...)
}
func dhcp6RelayReply(packet []byte, link, peer netip.Addr) ([]byte, error) {
	a, b := link.As16(), peer.WithZone("").As16()
	if len(packet) < 38 || packet[0] != 13 || packet[1] != 0 || !bytes.Equal(packet[2:18], a[:]) || !bytes.Equal(packet[18:34], b[:]) {
		return nil, errors.New("DHCPv6 relay reply binding mismatch")
	}
	options, err := dhcp6Options(packet[34:])
	if err != nil {
		return nil, err
	}
	if len(options[9]) < 4 {
		return nil, errors.New("missing DHCPv6 relay message")
	}
	return options[9], nil
}
