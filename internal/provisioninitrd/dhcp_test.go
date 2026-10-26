package provisioninitrd

import (
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"
)

func dhcpInput() Input {
	return Input{BootMAC: "02:00:00:00:00:07", Server: netip.MustParseAddr("192.0.2.1"), Clients: netip.MustParsePrefix("192.0.2.0/24")}
}
func dhcpFixture(input Input, kind byte) []byte {
	b := make([]byte, 240)
	b[0], b[1], b[2] = 2, 1, 6
	binary.BigEndian.PutUint32(b[4:8], 42)
	copy(b[16:20], []byte{192, 0, 2, 22})
	mac, _ := net.ParseMAC(input.BootMAC)
	copy(b[28:34], mac)
	copy(b[236:240], []byte{99, 130, 83, 99})
	b = append(b, 53, 1, kind, 54, 4, 192, 0, 2, 2, 1, 4, 255, 255, 255, 0, 51, 4, 0, 0, 2, 88, 255)
	return b
}
func TestDHCPDirectLeaseScopeAndBinding(t *testing.T) {
	input := dhcpInput()
	peer := netip.MustParseAddr("192.0.2.2")
	b := dhcpFixture(input, 2)
	r, err := parseDHCP(input, 42, peer, b)
	if err != nil || r.address.String() != "192.0.2.22" || r.seconds != 600 {
		t.Fatalf("valid direct offer: %v", err)
	}
	cases := map[string]func([]byte) []byte{
		"wrong xid":        func(b []byte) []byte { b[7] = 43; return b },
		"wrong mac":        func(b []byte) []byte { b[33]++; return b },
		"relay":            func(b []byte) []byte { b[24] = 192; return b },
		"hops":             func(b []byte) []byte { b[3] = 1; return b },
		"ciaddr":           func(b []byte) []byte { b[12] = 192; return b },
		"proxy no address": func(b []byte) []byte { clear(b[16:20]); return b },
		"other subnet":     func(b []byte) []byte { b[18] = 3; return b },
		"network":          func(b []byte) []byte { b[19] = 0; return b },
		"broadcast":        func(b []byte) []byte { b[19] = 255; return b },
		"boot server":      func(b []byte) []byte { b[19] = 1; return b },
		"dhcp server":      func(b []byte) []byte { b[19] = 2; return b },
		"missing end":      func(b []byte) []byte { return b[:len(b)-1] },
		"nonzero trailing": func(b []byte) []byte { return append(b, 1) },
		"duplicate kind":   func(b []byte) []byte { return append(b[:len(b)-1], 53, 1, 2, 255) },
		"overload":         func(b []byte) []byte { return append(b[:len(b)-1], 52, 1, 1, 255) },
		"relay option":     func(b []byte) []byte { return append(b[:len(b)-1], 82, 1, 1, 255) },
		"client mismatch":  func(b []byte) []byte { return append(b[:len(b)-1], 61, 7, 1, 2, 0, 0, 0, 0, 8, 255) },
		"too short lease":  func(b []byte) []byte { binary.BigEndian.PutUint32(b[len(b)-5:len(b)-1], 299); return b },
		"unbounded lease":  func(b []byte) []byte { binary.BigEndian.PutUint32(b[len(b)-5:len(b)-1], 0xffffffff); return b },
		"wrong mask":       func(b []byte) []byte { b[253] = 0; return b },
		"truncated option": func(b []byte) []byte { return append(b[:len(b)-1], 70, 4, 1) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseDHCP(input, 42, peer, mutate(append([]byte(nil), b...))); err == nil {
				t.Fatal("accepted invalid lease")
			}
		})
	}
	if _, err = parseDHCP(input, 42, netip.MustParseAddr("192.0.2.3"), b); err == nil {
		t.Fatal("server ID differs from direct peer")
	}
	if _, err = parseDHCP(input, 42, peer, dhcpFixture(input, 6)); err != nil {
		t.Fatal("valid NAK must reach selected-server rejection")
	}
}
func TestDHCPClientMessagesUseSameIdentityAndExplicitSelection(t *testing.T) {
	i := dhcpInput()
	offer := &dhcpReply{address: netip.MustParseAddr("192.0.2.22"), server: netip.MustParseAddr("192.0.2.2")}
	for _, kind := range []byte{1, 3, 4} {
		var selected *dhcpReply
		if kind != 1 {
			selected = offer
		}
		b, err := dhcpMessage(i, 42, kind, selected, 5*time.Second)
		if err != nil || len(b) < 300 || b[10] != 0x80 || binary.BigEndian.Uint32(b[4:8]) != 42 || binary.BigEndian.Uint16(b[8:10]) != 5 {
			t.Fatalf("bad client message %d: %v", kind, err)
		}
		if got := net.HardwareAddr(b[28:34]).String(); got != i.BootMAC {
			t.Fatal("wrong BOOTP identity")
		}
	}
	if _, err := dhcpMessage(i, 42, 3, nil, 0); err == nil {
		t.Fatal("request without explicit selection")
	}
	if _, err := dhcpMessage(i, 0, 1, nil, 0); err == nil {
		t.Fatal("zero transaction")
	}
}
func TestARPProbeDetectsExistingOwnerAndCompetingProbe(t *testing.T) {
	own, _ := net.ParseMAC("02:00:00:00:00:07")
	other, _ := net.ParseMAC("02:00:00:00:00:08")
	ip := netip.MustParseAddr("192.0.2.22")
	b, err := arpProbe(other, ip)
	if err != nil {
		t.Fatal(err)
	}
	if !arpConflict(b, own, ip) {
		t.Fatal("competing RFC5227 probe missed")
	}
	if arpConflict(b, other, ip) {
		t.Fatal("own packet counted as conflict")
	}
	copy(b[28:32], ip.AsSlice())
	clear(b[38:42])
	binary.BigEndian.PutUint16(b[20:22], 2)
	if !arpConflict(b, own, ip) {
		t.Fatal("existing owner reply missed")
	}
	b[31]++
	if arpConflict(b, own, ip) {
		t.Fatal("unrelated address")
	}
	if arpConflict(b[:41], own, ip) {
		t.Fatal("truncated packet")
	}
}
