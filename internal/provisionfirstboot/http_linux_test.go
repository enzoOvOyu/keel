//go:build linux

package provisionfirstboot

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestFirstBootHTTPSPinsPeerAndNeverRetriesOrRedirects(t *testing.T) {
	for _, mode := range []string{"acknowledged", "redirect", "partial", "encoding", "wrong_nonce", "alias", "lost_ack"} {
		t.Run(mode, func(t *testing.T) {
			now := time.Now().UTC().Truncate(time.Microsecond)
			secret, report := firstBootFixture(t, now)
			hits := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if r.Method != http.MethodPost || r.URL.Path != "/boot/"+secret.Binding.GrantID+"/first-boot" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" || r.Header.Get("Accept-Encoding") != "identity" {
					t.Error("unexpected first boot request")
				}
				raw, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
					return
				}
				in, err := DecodeSubmission(raw)
				if err != nil || VerifySubmission(secret.Binding, *in, time.Now()) != nil {
					t.Error("unbound first boot report", err)
					return
				}
				switch mode {
				case "redirect":
					http.Redirect(w, r, "https://outside.invalid/first-boot", http.StatusFound)
					return
				case "partial":
					w.Header().Set("Content-Range", "bytes 0-1/2")
					w.WriteHeader(http.StatusPartialContent)
					return
				case "encoding":
					w.Header().Set("Content-Encoding", "gzip")
				case "lost_ack":
					conn, _, err := w.(http.Hijacker).Hijack()
					if err == nil {
						conn.Close()
					}
					return
				}
				ack := Acknowledgment{InstallationID: secret.Binding.InstallationID, BootID: secret.Binding.BootID, BindingSHA256: secret.Binding.SHA256(), Nonce: in.Report.Nonce, State: "awaiting_first_boot", HostFingerprint: in.Report.HostFingerprint, ReceivedAt: time.Now().UTC().Truncate(time.Microsecond)}
				if mode == "wrong_nonce" {
					ack.Nonce = strings.Repeat("a", 32)
				}
				encoded, _ := json.Marshal(ack)
				if mode == "alias" {
					encoded = []byte(strings.Replace(string(encoded), `"installation_id":`, `"Installation_id":`, 1))
				}
				w.Write(encoded)
			}))
			defer server.Close()
			u, _ := url.Parse(server.URL)
			secret.Binding.Origin = server.URL
			secret.Binding.Server = netip.MustParseAddr(u.Hostname())
			secret.Binding.Clients = netip.MustParsePrefix("127.0.0.0/8")
			report.BindingSHA256 = secret.Binding.SHA256()
			roots := x509.NewCertPool()
			roots.AddCert(server.Certificate())
			transport, err := NewHTTPTransport(HTTPConfig{Binding: secret.Binding, RootCAs: roots, Interface: "lo", LocalAddress: netip.MustParseAddr("127.0.0.2"), Development: true})
			if err != nil {
				t.Fatal(err)
			}
			defer transport.Close()
			in, err := Sign(secret, report, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.Submit(context.Background(), *in)
			if (err == nil) != (mode == "acknowledged") || hits != 1 {
				t.Fatalf("callback status %v, HTTP attempts %d", err, hits)
			}
		})
	}
}

func TestFirstBootTransportRequiresExplicitIndependentTrust(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	secret, _ := firstBootFixture(t, now)
	if _, err := NewHTTPTransport(HTTPConfig{Binding: secret.Binding, Interface: "lo", LocalAddress: netip.MustParseAddr("127.0.0.2"), Development: true}); err == nil {
		t.Fatal("missing CA admitted")
	}
	if _, err := NewHTTPTransport(HTTPConfig{Binding: secret.Binding, RootCAs: x509.NewCertPool(), Interface: "lo", LocalAddress: secret.Binding.Server}); err == nil {
		t.Fatal("non-BOM/default network admitted")
	}
}
