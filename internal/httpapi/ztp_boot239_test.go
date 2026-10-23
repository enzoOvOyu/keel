package httpapi

import (
	"context"
	"crypto/tls"
	"github.com/enzoOvOyu/keel/internal/platform"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDirectBootPeerIgnoresForwardedHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "http://boot.example.invalid/api/v1/boot/ztp", nil)
	req.RemoteAddr = "192.0.2.8:4567"
	req.Header.Set("X-Forwarded-For", "198.51.100.9")
	got, ok := directBootPeer(req.RemoteAddr)
	if !ok || got != "192.0.2.8" {
		t.Fatalf("TCP peer changed by forwarded header: %q", got)
	}
	for _, bad := range []string{"198.51.100.9", "[2001:db8::1]:1234", "not-an-ip:1234"} {
		if _, ok := directBootPeer(bad); ok {
			t.Fatalf("bad TCP peer accepted: %q", bad)
		}
	}
	req.Header.Add("CUMULUS-SERIAL", "SW-1")
	req.Header.Add("CUMULUS-SERIAL", "SW-2")
	if _, ok := singleBootHeader(req, "CUMULUS-SERIAL"); ok {
		t.Fatal("ambiguous identity header accepted")
	}
}

func TestZTP239DedicatedHandlerAndPublicAPIBoundary(t *testing.T) {
	store := &platform.Store{}
	dedicated := NewZTP239Handler(store)
	base := httptest.NewRequest(http.MethodGet, "https://boot.example.invalid/api/v1/boot/ztp?project=x", nil)
	base.RemoteAddr = "192.0.2.8:4000"
	base.TLS = &tls.ConnectionState{}
	check := func(req *http.Request, want int) {
		t.Helper()
		rec := httptest.NewRecorder()
		dedicated.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("status %d, want %d", rec.Code, want)
		}
	}
	plain := base.Clone(context.Background())
	plain.TLS = nil
	check(plain, http.StatusForbidden)
	forwarded := base.Clone(context.Background())
	forwarded.Header.Set("X-Forwarded-For", "192.0.2.8")
	check(forwarded, http.StatusForbidden)
	missingHeaders := base.Clone(context.Background())
	check(missingHeaders, http.StatusBadRequest)
	wrongPath := base.Clone(context.Background())
	wrongPath.URL.Path = "/api/v1"
	check(wrongPath, http.StatusNotFound)
	post := base.Clone(context.Background())
	post.Method = http.MethodPost
	check(post, http.StatusMethodNotAllowed)
	public := newRouter("test", t.TempDir(), store, platform.Config{})
	rec := httptest.NewRecorder()
	public.ServeHTTP(rec, base)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("public API still serves device path: %d", rec.Code)
	}
}
