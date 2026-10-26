package provisionboot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

// Software transport fixture only: it asserts authority calls, not an actual
// PostgreSQL lease, bootable OS, DHCP server, physical inventory or SSH trust.
type authorityFixture struct {
	mu      sync.Mutex
	grant   Grant
	data    []byte
	denied  bool
	corrupt bool
	checks  int
	opens   int
}

func (f *authorityFixture) LookupBoot(context.Context, Scope, Client) (Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.denied {
		return Grant{}, ErrDenied
	}
	return f.grant, nil
}
func (f *authorityFixture) CheckBoot(context.Context, Scope, Grant, Client) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	if f.denied {
		return ErrDenied
	}
	return nil
}
func (f *authorityFixture) AuthorizeFile(ctx context.Context, s Scope, id string, peer netip.Addr, resource string) (Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.denied || id != f.grant.ID {
		return Grant{}, ErrDenied
	}
	return f.grant, nil
}
func (f *authorityFixture) CheckFile(context.Context, Scope, Grant, netip.Addr, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.checks++
	if f.denied {
		return ErrDenied
	}
	return nil
}
func (f *authorityFixture) OpenFile(context.Context, Scope, Grant, string) (Artifact, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens++
	raw := append([]byte{}, f.data...)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	if f.corrupt {
		hash = strings.Repeat("f", 64)
	}
	return Artifact{File: memoryFile{bytes.NewReader(raw)}, SHA256: hash, Size: int64(len(raw)), ObjectVersion: "immutable-fixture-v1"}, nil
}

type memoryFile struct{ *bytes.Reader }

func (memoryFile) Close() error { return nil }
func fixture() (Scope, *authorityFixture) {
	s := Scope{ID: strings.Repeat("1", 32), ProjectID: strings.Repeat("2", 32), Interface: "fixture0", Server: netip.MustParseAddr("192.0.2.1"), Clients: netip.MustParsePrefix("192.0.2.0/24"), Origin: "https://boot.example.test", KeaNetworks: []netip.Prefix{netip.MustParsePrefix("198.51.100.0/24")}}
	g := Grant{ScopeID: s.ID, ID: strings.Repeat("3", 32), ProjectID: s.ProjectID, RequestID: strings.Repeat("4", 32), MachineID: strings.Repeat("5", 32), AttemptID: strings.Repeat("6", 32), CommandID: strings.Repeat("7", 32), SnapshotSHA256: strings.Repeat("8", 64), FencingToken: 42, MAC: "02:aa:bb:cc:dd:ee", Arch: "amd64", ExpiresAt: time.Now().Add(time.Minute)}
	return s, &authorityFixture{grant: g, data: []byte("approved fixture bytes")}
}
func discover(arch uint16, vendor, user string) []byte {
	b := make([]byte, 240)
	b[0] = 1
	b[1] = 1
	b[2] = 6
	copy(b[4:8], []byte{1, 2, 3, 4})
	b[10] = 128
	copy(b[28:34], []byte{2, 170, 187, 204, 221, 238})
	copy(b[236:240], []byte{99, 130, 83, 99})
	b = append(b, 53, 1, 1, 93, 2, byte(arch>>8), byte(arch), 60, byte(len(vendor)))
	b = append(b, vendor...)
	if user != "" {
		b = append(b, 77, byte(len(user)))
		b = append(b, user...)
	}
	return append(b, 255)
}
func options(packet []byte) map[byte][]byte {
	out := map[byte][]byte{}
	for i := 240; i < len(packet); {
		code := packet[i]
		i++
		if code == 255 {
			break
		}
		if code == 0 {
			continue
		}
		n := int(packet[i])
		i++
		out[code] = packet[i : i+n]
		i += n
	}
	return out
}
func TestProxyBootHTTPFirstAndNoAddressLease(t *testing.T) {
	for _, test := range []struct {
		name                            string
		arch                            uint16
		vendor, user, transport, suffix string
	}{{"uefi-http", 16, "HTTPClient", "", "http", "ipxe.efi"}, {"ipxe-script", 7, "PXEClient", "iPXE", "ipxe", "boot.ipxe"}, {"ipxe-rfc3004", 7, "PXEClient", "\x04iPXE", "ipxe", "boot.ipxe"}, {"legacy", 0, "PXEClient", "", "tftp", "undionly.kpxe"}, {"uefi-pxe", 7, "PXEClient", "", "tftp", "ipxe.efi"}} {
		t.Run(test.name, func(t *testing.T) {
			s, a := fixture()
			p := Proxy{Scope: s, Authority: a}
			reply, err := p.Respond(context.Background(), discover(test.arch, test.vendor, test.user), netip.MustParseAddrPort("0.0.0.0:68"), 67)
			if err != nil {
				t.Fatal(err)
			}
			if reply.Transport != test.transport || reply.Destination != netip.MustParseAddrPort("192.0.2.255:68") {
				t.Fatal("wrong scoped boot response", reply)
			}
			opts := options(reply.Packet)
			if !strings.HasSuffix(string(opts[67]), test.suffix) || a.checks != 1 || !bytes.Equal(reply.Packet[16:20], make([]byte, 4)) {
				t.Fatal("incorrect boot or unauthorized address allocation")
			}
			for _, code := range []byte{1, 3, 6, 50, 51, 58, 59} {
				if opts[code] != nil {
					t.Fatal("proxy supplied address lease configuration", code)
				}
			}
			if test.transport == "http" && string(opts[60]) != "HTTPClient" {
				t.Fatal("HTTP vendor class missing")
			}
			if test.transport == "ipxe" && strings.HasSuffix(string(opts[67]), "ipxe.efi") {
				t.Fatal("infinite iPXE chainloading loop")
			}
		})
	}
}
func TestProxyBootRejectsUnsafeScopeAndClient(t *testing.T) {
	s, a := fixture()
	good := discover(7, "PXEClient", "iPXE")
	cases := map[string]func([]byte) []byte{
		"relay": func(b []byte) []byte { b[24] = 192; return b }, "response": func(b []byte) []byte { b[0] = 2; return b }, "multicast-mac": func(b []byte) []byte { b[28] = 3; return b }, "overload": func(b []byte) []byte { return append(b[:len(b)-1], 52, 1, 1, 255) }, "relay-option": func(b []byte) []byte { return append(b[:len(b)-1], 82, 1, 1, 255) }, "duplicate-type": func(b []byte) []byte { return append(b[:len(b)-1], 53, 1, 1, 255) }, "truncated": func(b []byte) []byte { return b[:len(b)-2] }, "unsupported-ebc": func(b []byte) []byte { return discover(9, "PXEClient", "") }, "normal-client": func(b []byte) []byte { return discover(7, "desktop", "") }, "address-request": func(b []byte) []byte { b[242] = 3; return b },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := Proxy{Scope: s, Authority: a}
			if _, err := p.Respond(context.Background(), mutate(append([]byte{}, good...)), netip.MustParseAddrPort("0.0.0.0:68"), 67); err == nil {
				t.Fatal("unsafe request answered")
			}
		})
	}
	overlap := s
	overlap.KeaNetworks = []netip.Prefix{netip.MustParsePrefix("192.0.0.0/16")}
	if overlap.Validate() == nil {
		t.Fatal("overlapping network-team Kea scope accepted")
	}
	p := Proxy{Scope: s, Authority: a}
	if _, err := p.Respond(context.Background(), good, netip.MustParseAddrPort("203.0.113.22:68"), 67); err == nil {
		t.Fatal("foreign subnet accepted")
	}
	a.denied = true
	if _, err := p.Respond(context.Background(), good, netip.MustParseAddrPort("0.0.0.0:68"), 67); err == nil {
		t.Fatal("revoked lease accepted")
	}
}
func TestBootHTTPVerifiesBytesBeforeRangeAndConditional(t *testing.T) {
	s, a := fixture()
	g := Gateway{Scope: s, Authority: a}
	path := s.Origin + "/boot/" + a.grant.ID + "/rootfs"
	makeRequest := func(method string) *http.Request {
		r := httptest.NewRequest(method, path, nil)
		r.RemoteAddr = "192.0.2.22:40000"
		return r
	}
	r := makeRequest("GET")
	r.Header.Set("Range", "bytes=1-4")
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != string(a.data[1:5]) {
		t.Fatal("verified range failed", w.Code, w.Body.String())
	}
	etag := w.Header().Get("ETag")
	if etag == "" || a.checks == 0 {
		t.Fatal("no immutable identity or lease check")
	}
	a.corrupt = true
	r = makeRequest("GET")
	r.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("cached conditional bypassed actual-byte validation", w.Code)
	}
	a.corrupt = false
	a.denied = true
	r = makeRequest("HEAD")
	w = httptest.NewRecorder()
	g.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal("HEAD bypassed lease revocation")
	}
}
func TestBootHTTPDoesNotTrustForwardingOrPaths(t *testing.T) {
	s, a := fixture()
	g := Gateway{Scope: s, Authority: a}
	for _, test := range []struct{ path, peer, host, method string }{{"/boot/" + a.grant.ID + "/rootfs", "203.0.113.1:40000", "boot.example.test", "GET"}, {"/boot/" + a.grant.ID + "/../rootfs", "192.0.2.22:40000", "boot.example.test", "GET"}, {"/boot/" + a.grant.ID + "/rootfs?uri=http://other", "192.0.2.22:40000", "boot.example.test", "GET"}, {"/boot/" + a.grant.ID + "/rootfs", "192.0.2.22:40000", "wrong.example.test", "GET"}, {"/boot/" + a.grant.ID + "/rootfs", "192.0.2.22:40000", "boot.example.test", "POST"}} {
		r := httptest.NewRequest(test.method, s.Origin+test.path, nil)
		r.RemoteAddr = test.peer
		r.Host = test.host
		r.Header.Set("X-Forwarded-For", "192.0.2.22")
		w := httptest.NewRecorder()
		g.ServeHTTP(w, r)
		if w.Code == 200 || w.Code == 206 {
			t.Fatal("unauthorized boot access", test)
		}
	}
	if a.opens != 0 {
		t.Fatal("rejected access opened media")
	}
}
func TestTFTPOnlyApprovedBootstrapAndExactBlocks(t *testing.T) {
	s, a := fixture()
	a.data = bytes.Repeat([]byte{42}, 1024)
	request := append([]byte{0, 1}, []byte("boot/"+a.grant.ID+"/undionly.kpxe\x00octet\x00")...)
	transfer, err := OpenTFTP(context.Background(), s, a, netip.MustParseAddr("192.0.2.22"), request)
	if err != nil {
		t.Fatal(err)
	}
	defer transfer.Close()
	packet, err := transfer.Next(nil)
	if err != nil || len(packet) != 516 || binary.BigEndian.Uint16(packet[2:4]) != 1 {
		t.Fatal("wrong initial block", err)
	}
	ack := []byte{0, 4, 0, 0}
	duplicate, err := transfer.Next(ack)
	if err != nil || duplicate != nil || transfer.Done() {
		t.Fatal("duplicate ACK retransmitted DATA or ended transfer")
	}
	retry, err := transfer.Retry()
	if err != nil || !bytes.Equal(retry, packet) {
		t.Fatal("bounded timeout retry lost current DATA")
	}
	ack[3] = 1
	packet, err = transfer.Next(ack)
	if err != nil || len(packet) != 516 || binary.BigEndian.Uint16(packet[2:4]) != 2 {
		t.Fatal("wrong second block")
	}
	ack[3] = 2
	packet, err = transfer.Next(ack)
	if err != nil || len(packet) != 4 {
		t.Fatal("missing terminating empty block")
	}
	ack[3] = 3
	packet, err = transfer.Next(ack)
	if err != nil || packet != nil {
		t.Fatal("final ACK did not close transfer")
	}
	bad := append([]byte{0, 1}, []byte("boot/"+a.grant.ID+"/rootfs\x00octet\x00")...)
	if _, err = OpenTFTP(context.Background(), s, a, netip.MustParseAddr("192.0.2.22"), bad); err == nil {
		t.Fatal("OS media exposed by TFTP")
	}
}
func TestTFTPStopsAfterLeaseLoss(t *testing.T) {
	s, a := fixture()
	request := append([]byte{0, 1}, []byte("boot/"+a.grant.ID+"/ipxe.efi\x00octet\x00")...)
	transfer, err := OpenTFTP(context.Background(), s, a, netip.MustParseAddr("192.0.2.22"), request)
	if err != nil {
		t.Fatal(err)
	}
	defer transfer.Close()
	a.denied = true
	if _, err = transfer.Next(nil); err == nil {
		t.Fatal("revoked transfer still sent bytes")
	}
}

var _ io.ReadSeekCloser = memoryFile{}

func FuzzParseBootRequests(f *testing.F) {
	f.Add(discover(16, "HTTPClient", ""))
	f.Add([]byte{0, 1, 2, 3})
	f.Add([]byte("boot/../ipxe.efi"))
	f.Fuzz(func(t *testing.T, raw []byte) { ParseClient(raw); ParseTFTPRequest(raw) })
}
func TestTFTPOptionFallbackUsesFixedBlockSize(t *testing.T) {
	s, a := fixture()
	a.data = bytes.Repeat([]byte{7}, 600)
	request := append([]byte{0, 1}, []byte("boot/"+a.grant.ID+"/ipxe.efi\x00OCTET\x00blksize\x001468\x00tsize\x000\x00")...)
	transfer, err := OpenTFTP(context.Background(), s, a, netip.MustParseAddr("192.0.2.22"), request)
	if err != nil {
		t.Fatal(err)
	}
	defer transfer.Close()
	packet, err := transfer.Next(nil)
	if err != nil || len(packet) != 516 || binary.BigEndian.Uint16(packet[:2]) != 3 {
		t.Fatal("option fallback did not use base DATA/512", err)
	}
}

func TestBootArtifactPinnedVersionBoundaries(t *testing.T) {
	raw := []byte("approved immutable boot bytes")
	sum := sha256.Sum256(raw)
	for _, tc := range []struct {
		version string
		valid   bool
	}{{"real-version", true}, {strings.Repeat("v", 4096), true}, {"", false}, {"null", false}, {strings.Repeat("v", 4097), false}} {
		artifact := Artifact{File: memoryFile{bytes.NewReader(raw)}, Size: int64(len(raw)), SHA256: hex.EncodeToString(sum[:]), ObjectVersion: tc.version}
		err := verifyArtifact(context.Background(), artifact, "kernel")
		if (err == nil) != tc.valid {
			t.Fatalf("version length %d valid=%v error=%v", len(tc.version), tc.valid, err)
		}
	}
}
