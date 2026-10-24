package httpapi

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestHTMLStyleNonceMatchesCSPWithoutAllowingArbitraryInlineStyles(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "index.html"), []byte("<!doctype html><html><head><title>Keel</title></head><body></body></html>"), 0600); e != nil {
		t.Fatal(e)
	}
	handler := New("test", dir)
	previous := ""
	for _, path := range []string{"/", "/index.html", "/p/example/media"} {
		request := httptest.NewRequest("GET", path, nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != 200 {
			t.Fatalf("%s: %d", path, response.Code)
		}
		match := regexp.MustCompile(`name="keel-style-nonce" content="([a-f0-9]{32})"`).FindStringSubmatch(response.Body.String())
		if len(match) != 2 {
			t.Fatal("HTML nonce absent")
		}
		csp := response.Header().Get("Content-Security-Policy")
		if !strings.Contains(csp, "style-src 'self' 'nonce-"+match[1]+"'") || strings.Contains(csp, "unsafe-inline") || match[1] == previous {
			t.Fatal("CSP nonce mismatch, reuse or overly broad style policy")
		}
		if response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("nonce-bearing HTML must not use cached bytes")
		}
		previous = match[1]
	}
}
