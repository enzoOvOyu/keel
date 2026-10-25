package mediasource

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestSourceOriginsAndURLBoundaries(t *testing.T) {
	for _, config := range []string{"http://mirror.example", "https://user:secret@mirror.example", "https://mirror.example/path", "https://mirror.example?token=secret", "https://mirror.example#fragment", "file:///etc/passwd", "https://mirror.example:0", "https://mirror.example:65536"} {
		if _, err := New(config, false); !errors.Is(err, ErrNotAllowed) {
			t.Fatalf("unsafe configuration accepted: %s", config)
		}
	}
	if f, err := New("", false); err != nil || f != nil {
		t.Fatal("empty policy did not disable fetching")
	}
	f, err := New("https://mirror.example", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"http://mirror.example/a", "https://other.example/a", "https://mirror.example.evil/a", "https://user:secret@mirror.example/a", "https://mirror.example/a?token=secret", "https://mirror.example/a?", "https://mirror.example/a#part", "file:///etc/passwd", "https://mirror.example:444/a"} {
		if _, err = f.Open(context.Background(), source); !errors.Is(err, ErrNotAllowed) {
			t.Fatalf("unsafe source accepted: %s", source)
		}
	}
}
func TestSourcePinnedDNSAndNetworkBoundaries(t *testing.T) {
	f, err := New("https://mirror.example", false)
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	f.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		called++
		if address != "192.0.2.8:443" {
			t.Fatalf("DNS answer not pinned: %s", address)
		}
		return nil, errors.New("test-only no connection")
	}
	for _, address := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "169.254.169.254", "100.100.100.200", "fd00:ec2::254", "fe80::1", "0.0.0.0", "::", "224.0.0.1", "ff02::1"} {
		f.resolve = func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr(address)}, nil
		}
		if _, err = f.dialSource(context.Background(), "tcp", "mirror.example:443"); !errors.Is(err, ErrNotAllowed) {
			t.Fatalf("unsafe resolved address accepted: %s", address)
		}
	}
	if called != 0 {
		t.Fatal("blocked DNS address reached a socket")
	}
	f.resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("192.0.2.8")}, nil
	}
	_, err = f.dialSource(context.Background(), "tcp", "mirror.example:443")
	if err == nil || called != 1 {
		t.Fatal("validated address not dialed")
	}
	// Development does not allow an arbitrary DNS name to rebind to loopback.
	f, err = New("http://mirror.example", true)
	if err != nil {
		t.Fatal(err)
	}
	f.resolve = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	f.dial = func(context.Context, string, string) (net.Conn, error) {
		t.Fatal("rebound source reached loopback")
		return nil, nil
	}
	if _, err = f.dialSource(context.Background(), "tcp", "mirror.example:80"); !errors.Is(err, ErrNotAllowed) {
		t.Fatal("DNS rebinding accepted")
	}
}
func TestSourceActualHTTPAndRedirectPolicy(t *testing.T) {
	hits, unexpected := 0, 0
	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { unexpected++; w.Write([]byte("forbidden")) }))
	defer forbidden.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("X-CSRF-Token") != "" {
			t.Error("caller credentials forwarded")
		}
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/file", http.StatusFound)
		case "/escape":
			http.Redirect(w, r, forbidden.URL+"/private", http.StatusFound)
		case "/loop":
			http.Redirect(w, r, "/loop", http.StatusFound)
		case "/partial":
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte("partial"))
		case "/compressed":
			w.Header().Set("Content-Encoding", "gzip")
			w.Write([]byte("encoded"))
		default:
			w.Write([]byte("source bytes"))
		}
	}))
	defer server.Close()
	// The client must not inherit environment proxies, including for allowed origins.
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	f, err := New(server.URL, true)
	if err != nil {
		t.Fatal(err)
	}
	body, err := f.Open(context.Background(), server.URL+"/redirect")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	body.Close()
	if err != nil || string(data) != "source bytes" {
		t.Fatal("actual source read failed")
	}
	if _, err = f.Open(context.Background(), server.URL+"/escape"); !errors.Is(err, ErrNotAllowed) || unexpected != 0 {
		t.Fatal("redirect escaped origin policy")
	}
	before := hits
	if _, err = f.Open(context.Background(), server.URL+"/loop"); !errors.Is(err, ErrNotAllowed) || hits-before != 4 {
		t.Fatal("redirect chain was not bounded")
	}
	for _, path := range []string{"/partial", "/compressed"} {
		if _, err = f.Open(context.Background(), server.URL+path); !errors.Is(err, ErrUnavailable) {
			t.Fatal("unsafe response accepted")
		}
	}
	// Default ports canonicalize without broadening the origin or credentials policy.
	u, _ := url.Parse("https://MIRROR.EXAMPLE:443/file")
	origin, _, err := sourceIdentity(u, false)
	if err != nil || !strings.EqualFold(origin, "https://mirror.example:443") {
		t.Fatal("origin normalization failed")
	}
}
