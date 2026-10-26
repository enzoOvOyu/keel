package fabricboot

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"reflect"
	"strings"
	"testing"
	"time"
)

func dhcp6ProbeFixture() DHCP6ProbeConfig {
	return DHCP6ProbeConfig{Server: "[::1]:547", Listen: "[::1]:546", LinkAddress: "2001:db8:1::1", DUID: dhcp6Client, ServerDUID: dhcp6Server, IAID: 42, Address: "2001:db8:1::10", TimeoutSeconds: 2}
}
func dhcp6TestLease(p DHCP6ProbeConfig) []byte {
	ia := make([]byte, 12)
	binary.BigEndian.PutUint32(ia, p.IAID)
	binary.BigEndian.PutUint32(ia[4:], 100)
	binary.BigEndian.PutUint32(ia[8:], 200)
	address, _ := netip.ParseAddr(p.Address)
	value := make([]byte, 24)
	a := address.As16()
	copy(value, a[:])
	binary.BigEndian.PutUint32(value[16:], 300)
	binary.BigEndian.PutUint32(value[20:], 600)
	return append(ia, dhcp6Option(5, value)...)
}
func dhcp6TestReply(p DHCP6ProbeConfig, kind byte, xid [3]byte, uris []string) []byte {
	client, _ := decodeDUID(p.DUID)
	server, _ := decodeDUID(p.ServerDUID)
	raw, _ := EncodeSZTPv6Option(uris)
	out := dhcp6Packet(kind, xid, client, server, dhcp6TestLease(p))
	return append(out, raw...)
}
func TestDHCP6ReplyBindingAndFraming(t *testing.T) {
	p := dhcp6ProbeFixture()
	uris := []string{"https://boot.example"}
	redirect, _ := EncodeSZTPRedirect(uris)
	xid := [3]byte{1, 2, 3}
	valid := dhcp6TestReply(p, 2, xid, uris)
	if _, preferred, seconds, err := dhcp6Lease(valid, 2, xid, p, redirect); err != nil || preferred != 300 || seconds != 600 {
		t.Fatal(preferred, seconds, err)
	}
	for _, raw := range [][]byte{valid[:3], valid[:len(valid)-1], append(append([]byte(nil), valid...), dhcp6Option(2, []byte{0, 1})...), append(append([]byte(nil), valid...), dhcp6Option(14, nil)...), append(append([]byte(nil), valid...), dhcp6Option(13, []byte{0, 2})...)} {
		if _, _, _, err := dhcp6Lease(raw, 2, xid, p, redirect); err == nil {
			t.Fatal("malformed or ambiguous reply accepted")
		}
	}
	for _, change := range []func(*DHCP6ProbeConfig){func(p *DHCP6ProbeConfig) { p.IAID++ }, func(p *DHCP6ProbeConfig) { p.Address = "2001:db8:1::11" }, func(p *DHCP6ProbeConfig) { p.ServerDUID = p.DUID }} {
		other := p
		change(&other)
		if _, _, _, err := dhcp6Lease(valid, 2, xid, other, redirect); err == nil {
			t.Fatal("wrong lease identity accepted")
		}
	}
	if _, _, _, err := dhcp6Lease(valid, 7, xid, p, redirect); err == nil {
		t.Fatal("advertise counted as reply")
	}
	wrong := append([]byte(nil), redirect...)
	wrong[len(wrong)-1] ^= 1
	if _, _, _, err := dhcp6Lease(valid, 2, xid, p, wrong); err == nil {
		t.Fatal("unapproved redirect accepted")
	}
	badLease := dhcp6TestLease(p)
	binary.BigEndian.PutUint32(badLease[12+4+20:], 0)
	client, _ := decodeDUID(p.DUID)
	server, _ := decodeDUID(p.ServerDUID)
	bad := dhcp6Packet(2, xid, client, server, badLease)
	bad = append(bad, dhcp6Option(136, redirect)...)
	if _, _, _, err := dhcp6Lease(bad, 2, xid, p, redirect); err == nil {
		t.Fatal("zero valid lifetime accepted")
	}
	link, _ := netip.ParseAddr(p.LinkAddress)
	peer := netip.MustParseAddr("::1")
	relay := dhcp6Relay(13, link, peer, valid)
	inner, err := dhcp6RelayReply(relay, link, peer)
	if err != nil || !bytes.Equal(inner, valid) {
		t.Fatal("valid relay rejected", err)
	}
	relay[2] ^= 1
	if _, err = dhcp6RelayReply(relay, link, peer); err == nil {
		t.Fatal("wrong relay link accepted")
	}
}
func TestDHCP6ProbeReservedIdentityValidation(t *testing.T) {
	for _, change := range []func(*DHCP6ProbeConfig){func(p *DHCP6ProbeConfig) { p.Server = "[ff02::1:2]:547" }, func(p *DHCP6ProbeConfig) { p.Listen = "[::]:546" }, func(p *DHCP6ProbeConfig) { p.LinkAddress = "192.0.2.1" }, func(p *DHCP6ProbeConfig) { p.Address = "fe80::10" }, func(p *DHCP6ProbeConfig) { p.ServerDUID = p.DUID }, func(p *DHCP6ProbeConfig) { p.DUID = "00:03" }, func(p *DHCP6ProbeConfig) { p.IAID = 0 }} {
		p := dhcp6ProbeFixture()
		change(&p)
		if p.Validate() == nil {
			t.Fatal("unsafe canary accepted", p)
		}
	}
}
func TestDHCP6RelayExchangeOverIsolatedUDP(t *testing.T) {
	// Software fixture only. It must never be recorded as Kea/device evidence.
	server, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.ParseIP("::1"), Port: 0})
	if err != nil {
		t.Skip("isolated IPv6 socket unavailable", err)
	}
	defer server.Close()
	local, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.ParseIP("::1"), Port: 0})
	if err != nil {
		t.Fatal(err)
	}
	listen := local.LocalAddr().String()
	local.Close()
	p := dhcp6ProbeFixture()
	p.Server = server.LocalAddr().String()
	p.Listen = listen
	uris := []string{"https://[2001:db8::2]:8443", "https://backup.example"}
	faults := make(chan string, 1)
	go func() {
		server.SetDeadline(time.Now().Add(3 * time.Second))
		buffer := make([]byte, 4096)
		for step := 0; step < 2; step++ {
			n, from, e := server.ReadFromUDP(buffer)
			if e != nil {
				faults <- e.Error()
				return
			}
			if n < 38 || buffer[0] != 12 {
				faults <- "not relay-forward"
				return
			}
			link := netip.MustParseAddr(p.LinkAddress)
			peer, _ := netip.AddrFromSlice(from.IP)
			peer = peer.Unmap()
			want := dhcp6Relay(12, link, peer, nil)
			if !bytes.Equal(buffer[:34], want[:34]) {
				faults <- "relay binding mismatch"
				return
			}
			opts, e := dhcp6Options(buffer[34:n])
			if e != nil {
				faults <- e.Error()
				return
			}
			inner := opts[9]
			if len(inner) < 4 {
				faults <- "no inner request"
				return
			}
			expected := byte(1)
			reply := byte(2)
			if step == 1 {
				expected = 3
				reply = 7
			}
			if inner[0] != expected {
				faults <- "wrong exchange order"
				return
			}
			request, e := dhcp6Options(inner[4:])
			if e != nil {
				faults <- e.Error()
				return
			}
			if !bytes.Equal(request[6], []byte{0, 136}) {
				faults <- "136 not requested"
				return
			}
			client, _ := decodeDUID(p.DUID)
			if !bytes.Equal(request[1], client) {
				faults <- "wrong client"
				return
			}
			if step == 1 {
				serverID, _ := decodeDUID(p.ServerDUID)
				if !bytes.Equal(request[2], serverID) {
					faults <- "wrong selected server"
					return
				}
			}
			xid := [3]byte{inner[1], inner[2], inner[3]}
			response := dhcp6TestReply(p, reply, xid, uris)
			if _, e = server.WriteToUDP(dhcp6Relay(13, link, peer, response), from); e != nil {
				faults <- e.Error()
				return
			}
		}
		faults <- ""
	}()
	proof, err := p.Verify136(context.Background(), uris)
	if err != nil {
		t.Fatal(err)
	}
	if fault := <-faults; fault != "" {
		t.Fatal(fault)
	}
	if !reflect.DeepEqual(proof.Option136URIs, uris) || proof.Address != p.Address || proof.ServerDUID != p.ServerDUID || proof.LinkAddress != p.LinkAddress || len(proof.TranscriptSHA256) != 64 || strings.Trim(proof.TranscriptSHA256, "0") == "" {
		t.Fatal("incomplete proof", proof)
	}
}
