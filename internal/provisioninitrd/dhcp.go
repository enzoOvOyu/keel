package provisioninitrd

import (
	"bytes"
	"encoding/binary"
	"net"
	"net/netip"
	"time"
)

// Only direct DHCP on the explicitly approved layer-2 subnet is supported.
// DHCP supplies a temporary address, never installation authority or TLS trust.
type dhcpReply struct {
	kind    byte
	address netip.Addr
	server  netip.Addr
	seconds uint32
}

func dhcpMessage(input Input, xid uint32, kind byte, offer *dhcpReply, elapsed time.Duration) ([]byte, error) {
	mac, err := net.ParseMAC(input.BootMAC)
	if err != nil || len(mac) != 6 || xid == 0 || (kind != 1 && kind != 3 && kind != 4) || ((kind == 3 || kind == 4) && offer == nil) {
		return nil, ErrUnavailable
	}
	b := make([]byte, 240, 320)
	b[0], b[1], b[2] = 1, 1, 6
	binary.BigEndian.PutUint32(b[4:8], xid)
	seconds := elapsed / time.Second
	if seconds < 0 {
		seconds = 0
	}
	if seconds > 65535 {
		seconds = 65535
	}
	binary.BigEndian.PutUint16(b[8:10], uint16(seconds))
	b[10] = 0x80
	copy(b[28:34], mac)
	copy(b[236:240], []byte{99, 130, 83, 99})
	option := func(key byte, value []byte) { b = append(b, key, byte(len(value))); b = append(b, value...) }
	option(53, []byte{kind})
	option(61, append([]byte{1}, mac...))
	option(60, []byte("KeelInitrd"))
	if offer != nil {
		option(50, offer.address.AsSlice())
		option(54, offer.server.AsSlice())
	}
	if kind != 4 {
		option(55, []byte{1, 51, 54})
		option(57, []byte{5, 220})
	}
	b = append(b, 255)
	for len(b) < 300 {
		b = append(b, 0)
	}
	return b, nil
}

func parseDHCP(input Input, xid uint32, source netip.Addr, raw []byte) (*dhcpReply, error) {
	mac, err := net.ParseMAC(input.BootMAC)
	if err != nil || len(mac) != 6 || xid == 0 || len(raw) < 241 || len(raw) > 1500 || raw[0] != 2 || raw[1] != 1 || raw[2] != 6 || raw[3] != 0 || binary.BigEndian.Uint32(raw[4:8]) != xid || !bytes.Equal(raw[28:34], mac) || !bytes.Equal(raw[236:240], []byte{99, 130, 83, 99}) || !bytes.Equal(raw[12:16], make([]byte, 4)) || !bytes.Equal(raw[24:28], make([]byte, 4)) {
		return nil, ErrUnavailable
	}
	options := map[byte][]byte{}
	ended := false
	for at := 240; at < len(raw); {
		key := raw[at]
		at++
		if key == 0 {
			continue
		}
		if key == 255 {
			ended = true
			for _, v := range raw[at:] {
				if v != 0 {
					return nil, ErrUnavailable
				}
			}
			break
		}
		if at >= len(raw) {
			return nil, ErrUnavailable
		}
		n := int(raw[at])
		at++
		if n == 0 || at+n > len(raw) || options[key] != nil || key == 52 || key == 82 {
			return nil, ErrUnavailable
		}
		options[key] = raw[at : at+n]
		at += n
	}
	if !ended || len(options[53]) != 1 || len(options[54]) != 4 || !source.Is4() || !input.Clients.Contains(source) || source == input.Clients.Addr() || source == broadcast(input.Clients) {
		return nil, ErrUnavailable
	}
	r := &dhcpReply{kind: options[53][0], server: netip.AddrFrom4([4]byte(options[54]))}
	if r.server != source || (r.kind != 2 && r.kind != 5 && r.kind != 6) {
		return nil, ErrUnavailable
	}
	if client := options[61]; client != nil && !bytes.Equal(client, append([]byte{1}, mac...)) {
		return nil, ErrUnavailable
	}
	if r.kind == 6 {
		return r, nil
	}
	r.address = netip.AddrFrom4([4]byte(raw[16:20]))
	if !r.address.IsGlobalUnicast() || !input.Clients.Contains(r.address) || r.address == input.Server || r.address == r.server || r.address == input.Clients.Addr() || r.address == broadcast(input.Clients) || len(options[1]) != 4 || len(options[51]) != 4 {
		return nil, ErrUnavailable
	}
	mask := net.CIDRMask(input.Clients.Bits(), 32)
	if !bytes.Equal(options[1], mask) {
		return nil, ErrUnavailable
	}
	r.seconds = binary.BigEndian.Uint32(options[51])
	// Bound the initial read-only session; renewal is required before any future
	// installation session. Never keep using an expired address silently.
	if r.seconds < 300 || r.seconds > 7*24*3600 {
		return nil, ErrUnavailable
	}
	return r, nil
}

func arpProbe(mac net.HardwareAddr, candidate netip.Addr) ([]byte, error) {
	if len(mac) != 6 || !candidate.Is4() {
		return nil, ErrUnavailable
	}
	b := make([]byte, 60)
	for i := 0; i < 6; i++ {
		b[i] = 255
	}
	copy(b[6:12], mac)
	binary.BigEndian.PutUint16(b[12:14], 0x0806)
	binary.BigEndian.PutUint16(b[14:16], 1)
	binary.BigEndian.PutUint16(b[16:18], 0x0800)
	b[18], b[19] = 6, 4
	binary.BigEndian.PutUint16(b[20:22], 1)
	copy(b[22:28], mac)
	copy(b[38:42], candidate.AsSlice())
	return b, nil
}

func arpConflict(raw []byte, own net.HardwareAddr, candidate netip.Addr) bool {
	if len(raw) < 42 || len(own) != 6 || !candidate.Is4() || binary.BigEndian.Uint16(raw[12:14]) != 0x0806 || binary.BigEndian.Uint16(raw[14:16]) != 1 || binary.BigEndian.Uint16(raw[16:18]) != 0x0800 || raw[18] != 6 || raw[19] != 4 || !bytes.Equal(raw[6:12], raw[22:28]) || bytes.Equal(raw[22:28], own) {
		return false
	}
	op := binary.BigEndian.Uint16(raw[20:22])
	if op != 1 && op != 2 {
		return false
	}
	return bytes.Equal(raw[28:32], candidate.AsSlice()) || (op == 1 && bytes.Equal(raw[28:32], []byte{0, 0, 0, 0}) && bytes.Equal(raw[38:42], candidate.AsSlice()))
}
