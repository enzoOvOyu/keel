package fabricboot

// DHCP verification uses an operator-reserved canary, never a device identity.
// Relay mode follows RFC2131: giaddr selects the subnet, replies go to the
// relay listener. Production normally binds UDP/67; tests use isolated ports.
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

type DHCPProbeConfig struct {
	Server         string `json:"server"`
	Listen         string `json:"listen"`
	RelayIP        string `json:"relay_ip"`
	MAC            string `json:"mac"`
	Address        string `json:"address"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}
type DHCPProof struct {
	Option60VCI         string    `json:"option_60_vci,omitempty"`
	Option43Hex         string    `json:"option_43_hex,omitempty"`
	VendorProfileSHA256 string    `json:"vendor_profile_sha256,omitempty"`
	Option143URIs       []string  `json:"option_143_uris,omitempty"`
	Address             string    `json:"address"`
	MAC                 string    `json:"mac"`
	ServerID            string    `json:"server_id"`
	Option239URL        string    `json:"option_239_url"`
	TranscriptSHA256    string    `json:"transcript_sha256"`
	LeaseSeconds        uint32    `json:"lease_seconds"`
	VerifiedAt          time.Time `json:"verified_at"`
}

func (p DHCPProbeConfig) Validate() error {
	server, e := netip.ParseAddrPort(p.Server)
	if e != nil || !server.Addr().Is4() || server.Port() == 0 || server.Addr().IsUnspecified() || server.Addr().IsMulticast() {
		return errors.New("DHCP server must be an explicit IPv4 address and port")
	}
	listen, e := netip.ParseAddrPort(p.Listen)
	if e != nil || !listen.Addr().Is4() || listen.Port() == 0 {
		return errors.New("DHCP relay listener must be an explicit IPv4 address and port")
	}
	relay, e := netip.ParseAddr(p.RelayIP)
	if e != nil || !relay.Is4() || relay.IsUnspecified() || relay.IsMulticast() || listen.Addr() != relay {
		return errors.New("DHCP relay address must equal the local listener address")
	}
	addr, e := netip.ParseAddr(p.Address)
	if e != nil || !addr.Is4() || addr.IsUnspecified() || addr.IsMulticast() || addr == relay {
		return errors.New("DHCP canary needs a dedicated IPv4 address")
	}
	mac, e := net.ParseMAC(p.MAC)
	if e != nil || len(mac) != 6 || mac[0]&1 != 0 || bytes.Equal(mac, make([]byte, 6)) {
		return errors.New("DHCP canary needs a dedicated unicast Ethernet MAC")
	}
	if p.TimeoutSeconds < 0 || p.TimeoutSeconds > 20 {
		return errors.New("DHCP probe timeout must be 1 through 20 seconds")
	}
	return nil
}
func dhcpPacket(xid []byte, mac net.HardwareAddr, relay netip.Addr, kind byte, requested, server []byte, vendor string) []byte {
	b := make([]byte, 240)
	b[0] = 1
	b[1] = 1
	b[2] = 6
	b[3] = 1
	copy(b[4:8], xid)
	r := relay.As4()
	copy(b[24:28], r[:])
	copy(b[28:34], mac)
	copy(b[236:240], []byte{99, 130, 83, 99})
	b = append(b, 53, 1, kind, 61, 7, 1)
	b = append(b, mac...)
	b = append(b, 55, 7, 1, 3, 43, 51, 54, 239, 143)
	if len(vendor) > 0 {
		b = append(b, 60, byte(len(vendor)))
		b = append(b, []byte(vendor)...)
	}
	if requested != nil {
		b = append(b, 50, 4)
		b = append(b, requested...)
	}
	if server != nil {
		b = append(b, 54, 4)
		b = append(b, server...)
	}
	b = append(b, 255)
	for len(b) < 300 {
		b = append(b, 0)
	}
	return b
}
func dhcpOptions(packet []byte, xid []byte, mac net.HardwareAddr, relay netip.Addr) (map[byte][]byte, error) {
	r := relay.As4()
	if len(packet) < 241 || packet[0] != 2 || packet[1] != 1 || packet[2] != 6 || !bytes.Equal(packet[4:8], xid) || !bytes.Equal(packet[28:34], mac) || !bytes.Equal(packet[24:28], r[:]) || !bytes.Equal(packet[236:240], []byte{99, 130, 83, 99}) {
		return nil, errors.New("DHCP reply identity or framing mismatch")
	}
	out := map[byte][]byte{}
	ended := false
	for i := 240; i < len(packet); {
		code := packet[i]
		i++
		if code == 255 {
			ended = true
			break
		}
		if code == 0 {
			continue
		}
		if i >= len(packet) {
			return nil, errors.New("truncated DHCP option")
		}
		n := int(packet[i])
		i++
		if i+n > len(packet) {
			return nil, errors.New("truncated DHCP option value")
		}
		if _, exists := out[code]; exists {
			return nil, errors.New("duplicate DHCP option is unsupported")
		}
		out[code] = append([]byte(nil), packet[i:i+n]...)
		i += n
	}
	if !ended || len(out[53]) != 1 || len(out[54]) != 4 || len(out[51]) != 4 || binary.BigEndian.Uint32(out[51]) == 0 || out[52] != nil {
		return nil, errors.New("DHCP reply is missing required options or uses unsupported overload")
	}
	return out, nil
}
func (p DHCPProbeConfig) Verify239(ctx context.Context, url, vendor string) (*DHCPProof, error) {
	return p.VerifyBootstrap(ctx, url, vendor, nil)
}
func (p DHCPProbeConfig) VerifyBootstrap(ctx context.Context, url, vendor string, uris []string) (*DHCPProof, error) {
	return p.verifyBootstrap(ctx, url, vendor, uris, nil)
}

func (p DHCPProbeConfig) VerifyVendorBootstrap(ctx context.Context, url string, uris []string, profile DHCPVendorProfile) (*DHCPProof, error) {
	if _, err := profile.Payload(); err != nil {
		return nil, err
	}
	return p.verifyBootstrap(ctx, url, profile.VCI, uris, &profile)
}

func (p DHCPProbeConfig) verifyBootstrap(ctx context.Context, url, vendor string, uris []string, profile *DHCPVendorProfile) (*DHCPProof, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	var vendorData []byte
	if profile != nil {
		var err error
		vendorData, err = profile.Payload()
		if err != nil {
			return nil, err
		}
	}
	var redirect []byte
	if len(uris) > 0 {
		var err error
		redirect, err = EncodeSZTPRedirect(uris)
		if err != nil || len(redirect) > 255 {
			return nil, errors.New("invalid expected SZTP redirect")
		}
	}
	if e := p.Validate(); e != nil {
		return nil, e
	}
	if len(url) == 0 || len(url) > 255 || len(vendor) > 128 {
		return nil, errors.New("invalid DHCP expected options")
	}
	remote, e := net.ResolveUDPAddr("udp4", p.Server)
	if e != nil {
		return nil, e
	}
	local, e := net.ResolveUDPAddr("udp4", p.Listen)
	if e != nil {
		return nil, e
	}
	conn, e := net.ListenUDP("udp4", local)
	if e != nil {
		return nil, e
	}
	defer conn.Close()
	timeout := 8 * time.Second
	if p.TimeoutSeconds > 0 {
		timeout = time.Duration(p.TimeoutSeconds) * time.Second
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if e = conn.SetDeadline(deadline); e != nil {
		return nil, e
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	xid := make([]byte, 4)
	if _, e = rand.Read(xid); e != nil {
		return nil, e
	}
	mac, _ := net.ParseMAC(p.MAC)
	relay, _ := netip.ParseAddr(p.RelayIP)
	expected, _ := netip.ParseAddr(p.Address)
	ip := expected.As4()
	transcript := sha256.New()
	exchange := func(packet []byte, kind byte, server []byte) ([]byte, map[byte][]byte, error) {
		if _, err := conn.WriteToUDP(packet, remote); err != nil {
			return nil, nil, err
		}
		transcript.Write(packet)
		buf := make([]byte, 4096)
		for {
			n, peer, err := conn.ReadFromUDP(buf)
			if err != nil {
				return nil, nil, err
			}
			if !peer.IP.Equal(remote.IP) || peer.Port != remote.Port {
				continue
			}
			raw := append([]byte(nil), buf[:n]...)
			options, err := dhcpOptions(raw, xid, mac, relay)
			if err != nil {
				return nil, nil, err
			}
			if err := verifyVendorReply(options, vendorData); err != nil {
				return nil, nil, err
			}
			if len(redirect) > 0 && !bytes.Equal(options[143], redirect) {
				return nil, nil, errors.New("DHCP SZTP redirect does not match the approved bootstrap server")
			}
			if options[53][0] != kind || !bytes.Equal(raw[16:20], ip[:]) || string(options[239]) != url || server != nil && !bytes.Equal(options[54], server) {
				return nil, nil, errors.New("DHCP offer/ack does not match the approved canary, server and bootstrap URL")
			}
			transcript.Write(raw)
			return raw, options, nil
		}
	}
	_, offer, e := exchange(dhcpPacket(xid, mac, relay, 1, nil, nil, vendor), 2, nil)
	if e != nil {
		return nil, e
	}
	_, ack, e := exchange(dhcpPacket(xid, mac, relay, 3, ip[:], offer[54], vendor), 5, offer[54])
	if e != nil {
		return nil, e
	}
	var serverIP [4]byte
	copy(serverIP[:], ack[54])
	proof := &DHCPProof{Option143URIs: append([]string(nil), uris...), Address: p.Address, MAC: mac.String(), ServerID: netip.AddrFrom4(serverIP).String(), Option239URL: url, TranscriptSHA256: hex.EncodeToString(transcript.Sum(nil)), LeaseSeconds: binary.BigEndian.Uint32(ack[51]), VerifiedAt: time.Now().UTC()}
	if profile != nil {
		proof.Option60VCI = vendor
		proof.Option43Hex = hex.EncodeToString(vendorData)
		proof.VendorProfileSHA256 = ManagementPlanDigest(*profile)
	}
	return proof, nil
}
func (s *ProbeSuite) DHCPProbe() *DHCPProbeConfig {
	if s == nil || s.dhcp == nil {
		return nil
	}
	v := *s.dhcp
	return &v
}
