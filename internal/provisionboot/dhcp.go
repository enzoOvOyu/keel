package provisionboot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"time"
)

type Client struct {
	MAC               string
	Arch              string
	Architecture      uint16
	HTTPBoot          bool
	IPXE              bool
	FingerprintSHA256 string
	TransactionID     [4]byte
	Address           netip.Addr
	Broadcast         bool
	options           map[byte][]byte
	packet            []byte
}

// BootAuthority resolves an actual, approved running installation and verifies
// its current exclusive device lease. MAC/architecture/DHCP fingerprints alone
// never authorize registration, completion, callbacks or SSH trust.
type BootAuthority interface {
	LookupBoot(context.Context, Scope, Client) (Grant, error)
	CheckBoot(context.Context, Scope, Grant, Client) error
}
type Proxy struct {
	Scope     Scope
	Authority BootAuthority
}
type Reply struct {
	Packet            []byte
	Destination       netip.AddrPort
	GrantID           string
	FingerprintSHA256 string
	Transport         string
}

// ParseClient accepts only direct Ethernet DHCP Discover/Request. Rejecting
// malformed, duplicated and overloaded options avoids ambiguous authorization.
func ParseClient(packet []byte) (Client, error) {
	var c Client
	if len(packet) < 241 || len(packet) > 1500 || packet[0] != 1 || packet[1] != 1 || packet[2] != 6 || packet[3] != 0 || !bytes.Equal(packet[236:240], []byte{99, 130, 83, 99}) || !bytes.Equal(packet[16:20], make([]byte, 4)) || !bytes.Equal(packet[24:28], make([]byte, 4)) {
		return c, ErrDenied
	}
	c.MAC = net.HardwareAddr(packet[28:34]).String()
	if !unicastMAC(c.MAC) {
		return c, ErrDenied
	}
	copy(c.TransactionID[:], packet[4:8])
	var address [4]byte
	copy(address[:], packet[12:16])
	c.Address = netip.AddrFrom4(address)
	c.Broadcast = binary.BigEndian.Uint16(packet[10:12])&0x8000 != 0
	c.options = map[byte][]byte{}
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
			return c, ErrDenied
		}
		n := int(packet[i])
		i++
		if n == 0 || i+n > len(packet) || c.options[code] != nil {
			return c, ErrDenied
		}
		c.options[code] = append([]byte{}, packet[i:i+n]...)
		i += n
	}
	if !ended || len(c.options[53]) != 1 || (c.options[53][0] != 1 && c.options[53][0] != 3) || c.options[52] != nil || c.options[82] != nil || len(c.options[93]) == 0 || len(c.options[93])%2 != 0 || len(c.options[93]) > 32 {
		return c, ErrDenied
	}
	if id := c.options[61]; len(id) > 0 && (len(id) != 7 || id[0] != 1 || !bytes.Equal(id[1:], packet[28:34])) {
		return c, ErrDenied
	}
	vendor := string(c.options[60])
	if len(vendor) > 128 {
		return c, ErrDenied
	}
	c.IPXE = ipxeClass(c.options[77])
	c.HTTPBoot = vendor == "HTTPClient" || strings.HasPrefix(vendor, "HTTPClient:")
	if !c.IPXE && !c.HTTPBoot && !(vendor == "PXEClient" || strings.HasPrefix(vendor, "PXEClient:")) {
		return c, ErrDenied
	}
	// Preserve the client's architecture preference order. Do not guess EBC (9)
	// as x64; IANA's current registry identifies x64 UEFI as 7.
	for i := 0; i < len(c.options[93]); i += 2 {
		a := binary.BigEndian.Uint16(c.options[93][i : i+2])
		arch := ""
		switch a {
		case 0, 7, 16:
			arch = "amd64"
		case 11, 19:
			arch = "arm64"
		}
		if arch == "" {
			continue
		}
		if c.HTTPBoot && a != 16 && a != 19 {
			continue
		}
		if !c.HTTPBoot && (a == 16 || a == 19) && !c.IPXE {
			continue
		}
		c.Architecture = a
		c.Arch = arch
		break
	}
	if c.Arch == "" {
		return c, ErrDenied
	}
	// Only stable, length-framed client classification enters the auxiliary
	// fingerprint. Transaction IDs, IP leases and secrets do not become identity.
	h := sha256.New()
	h.Write(packet[28:34])
	for _, code := range []byte{60, 77, 93, 94, 97} {
		v := c.options[code]
		h.Write([]byte{code, byte(len(v))})
		h.Write(v)
	}
	c.FingerprintSHA256 = hex.EncodeToString(h.Sum(nil))
	c.packet = append([]byte{}, packet...)
	return c, nil
}
func ipxeClass(raw []byte) bool {
	// iPXE historically sends a raw string instead of RFC3004's length list.
	if bytes.Equal(raw, []byte("iPXE")) {
		return true
	}
	for i := 0; i < len(raw); {
		n := int(raw[i])
		i++
		if n == 0 || i+n > len(raw) {
			return false
		}
		if bytes.Equal(raw[i:i+n], []byte("iPXE")) {
			return i+n == len(raw)
		}
		i += n
	}
	return false
}
func (p *Proxy) Respond(ctx context.Context, packet []byte, peer netip.AddrPort, servicePort uint16) (*Reply, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil || p.Authority == nil || p.Scope.Validate() != nil || (servicePort != 67 && servicePort != 4011) || !peer.IsValid() || !peer.Addr().Is4() || peer.Port() != 68 {
		return nil, ErrDenied
	}
	c, err := ParseClient(packet)
	if err != nil {
		return nil, err
	}
	if peer.Addr().IsUnspecified() {
		if servicePort != 67 || c.options[53][0] != 1 || !c.Address.IsUnspecified() {
			return nil, ErrDenied
		}
	} else if !p.Scope.CheckPeer(peer.Addr()) || (!c.Address.IsUnspecified() && c.Address != peer.Addr()) {
		return nil, ErrDenied
	}
	if !c.Address.IsUnspecified() && !p.Scope.CheckPeer(c.Address) {
		return nil, ErrDenied
	}
	// Normal DHCPREQUEST belongs to the address server and is never answered.
	if c.options[53][0] == 3 {
		if servicePort != 4011 || c.Address.IsUnspecified() || len(c.options[54]) != 4 || !bytes.Equal(c.options[54], p.Scope.Server.AsSlice()) {
			return nil, ErrDenied
		}
	} else if servicePort != 67 {
		return nil, ErrDenied
	}
	grant, err := p.Authority.LookupBoot(ctx, p.Scope, c)
	if err != nil {
		return nil, ErrDenied
	}
	if grant.Validate(p.Scope, time.Now()) != nil || grant.MAC != c.MAC || grant.Arch != c.Arch {
		return nil, ErrDenied
	}
	if err = p.Authority.CheckBoot(ctx, p.Scope, grant, c); err != nil || ctx.Err() != nil || grant.Validate(p.Scope, time.Now()) != nil {
		return nil, ErrDenied
	}
	filename, transport := p.bootFile(grant, c)
	if len(filename) > 255 {
		return nil, ErrDenied
	}
	out := make([]byte, 240)
	out[0] = 2
	out[1] = 1
	out[2] = 6
	copy(out[4:8], c.TransactionID[:])
	copy(out[10:16], packet[10:16])
	copy(out[20:24], p.Scope.Server.AsSlice())
	copy(out[28:44], packet[28:44])
	if len(filename) < 128 {
		copy(out[108:236], filename)
	}
	copy(out[236:240], []byte{99, 130, 83, 99})
	kind := byte(2)
	if c.options[53][0] == 3 {
		kind = 5
	}
	option := func(code byte, value []byte) { out = append(out, code, byte(len(value))); out = append(out, value...) }
	option(53, []byte{kind})
	option(54, p.Scope.Server.AsSlice())
	option(67, []byte(filename))
	option(93, []byte{byte(c.Architecture >> 8), byte(c.Architecture)})
	if c.HTTPBoot && !c.IPXE {
		option(60, []byte("HTTPClient"))
	} else {
		option(60, []byte("PXEClient"))
		option(43, []byte{6, 1, 8, 255})
	}
	out = append(out, 255)
	for len(out) < 300 {
		out = append(out, 0)
	}
	// yiaddr and lease/router/DNS/subnet options are deliberately absent: this
	// proxy supplements the existing address authority and never becomes it.
	destination := netip.AddrPortFrom(p.Scope.Broadcast(), 68)
	if !c.Address.IsUnspecified() && !c.Broadcast {
		destination = netip.AddrPortFrom(c.Address, 68)
	}
	return &Reply{Packet: out, Destination: destination, GrantID: grant.ID, FingerprintSHA256: c.FingerprintSHA256, Transport: transport}, nil
}
func (p *Proxy) bootFile(grant Grant, c Client) (string, string) {
	prefix := "/boot/" + grant.ID + "/"
	if c.IPXE {
		return p.Scope.Origin + prefix + "boot.ipxe", "ipxe"
	}
	if c.HTTPBoot {
		return p.Scope.Origin + prefix + "ipxe.efi", "http"
	}
	if c.Architecture == 0 {
		return prefix[1:] + "undionly.kpxe", "tftp"
	}
	return fmt.Sprintf("%sipxe.efi", prefix[1:]), "tftp"
}
