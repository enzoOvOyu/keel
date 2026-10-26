//go:build linux

package provisioninitrd

import (
	"context"
	"crypto/x509"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func tlsFiles(t *testing.T, handler http.Handler) (*HTTPFiles, *httptest.Server) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	parsed, _ := url.Parse(server.URL)
	ip := netip.MustParseAddr(parsed.Hostname())
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	input := Input{ManifestURL: server.URL + "/boot/" + strings.Repeat("3", 32) + "/manifest.json", ManifestSHA256: strings.Repeat("a", 64), GrantID: strings.Repeat("3", 32), BootMAC: "02:00:00:00:00:01", Server: ip, Clients: netip.MustParsePrefix("127.0.0.0/8")}
	files, err := NewHTTPFiles(HTTPConfig{Input: input, RootCAs: roots, Interface: "lo", LocalAddress: netip.MustParseAddr("127.0.0.2"), Development: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(files.Close)
	return files, server
}
func TestInitrdHTTPSFilesUsePinnedTLSAndFixedResources(t *testing.T) {
	hits := 0
	files, _ := tlsFiles(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.HasSuffix(r.URL.Path, "/preflight.json") || r.Header.Get("Accept-Encoding") != "identity" || r.Header.Get("Cookie") != "" {
			t.Error("unexpected HTTP request")
		}
		io.WriteString(w, `{"fixture":true}`)
	}))
	raw, err := files.Get(context.Background(), "preflight.json")
	if err != nil || string(raw) != `{"fixture":true}` {
		t.Fatal("trusted TLS fetch failed", err)
	}
	for _, path := range []string{"../private", "rootfs", "manifest.json?extra=yes", "https://other.example/test"} {
		if _, err := files.Get(context.Background(), path); err == nil {
			t.Fatal("arbitrary resource admitted")
		}
	}
	if hits != 1 {
		t.Fatal("unexpected requests", hits)
	}
}
func TestInitrdHTTPSRejectsRedirectPartialEncodingAndOversize(t *testing.T) {
	for _, mode := range []string{"redirect", "partial", "encoding", "oversize", "empty"} {
		t.Run(mode, func(t *testing.T) {
			hits := 0
			files, _ := tlsFiles(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				switch mode {
				case "redirect":
					http.Redirect(w, r, "https://outside.invalid/secret", http.StatusFound)
				case "partial":
					w.Header().Set("Content-Range", "bytes 0-1/2")
					w.WriteHeader(http.StatusPartialContent)
					io.WriteString(w, "ab")
				case "encoding":
					w.Header().Set("Content-Encoding", "gzip")
					io.WriteString(w, "not-decoded")
				case "oversize":
					io.WriteString(w, strings.Repeat("x", (64<<10)+1))
				case "empty":
					w.WriteHeader(http.StatusOK)
				}
			}))
			if _, err := files.Get(context.Background(), "preflight.json"); err == nil {
				t.Fatal("unsafe response admitted")
			}
			if hits != 1 {
				t.Fatal("unexpected retry or redirect", hits)
			}
		})
	}
}
func TestInitrdHTTPSRequiresIndependentTrustAndExplicitLocalScope(t *testing.T) {
	files, server := tlsFiles(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "public fixture") }))
	transport := files.client.Transport.(*http.Transport)
	transport.TLSClientConfig.RootCAs = x509.NewCertPool()
	if _, err := files.Get(context.Background(), "meta-data"); err == nil {
		t.Fatal("untrusted server certificate accepted")
	}
	parsed, _ := url.Parse(server.URL)
	i := Input{ManifestURL: server.URL + "/boot/" + strings.Repeat("3", 32) + "/manifest.json", ManifestSHA256: strings.Repeat("a", 64), GrantID: strings.Repeat("3", 32), BootMAC: "02:00:00:00:00:01", Server: mustAddr(parsed.Hostname()), Clients: mustPrefix("127.0.0.0/8")}
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	for _, config := range []HTTPConfig{
		{Input: i, Interface: "lo", LocalAddress: mustAddr("127.0.0.2"), Development: true},
		{Input: i, RootCAs: roots, Interface: "lo", LocalAddress: i.Server, Development: true},
		{Input: i, RootCAs: roots, Interface: "lo", LocalAddress: mustAddr("192.0.2.2"), Development: true},
		{Input: i, RootCAs: roots, Interface: "lo", LocalAddress: mustAddr("127.0.0.2")},
	} {
		if _, err := NewHTTPFiles(config); err == nil {
			t.Fatal("missing trust or invalid local scope admitted")
		}
	}
}
