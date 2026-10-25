package mediasource

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
)

func TestWebhookPostUsesPinnedOriginAndMetadataBoundaries(t *testing.T) {
	f, err := New("https://receiver.example", false)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.dial = func(context.Context, string, string) (net.Conn, error) {
		calls++
		return nil, errors.New("fixture denies actual dial")
	}
	for _, address := range []string{"127.0.0.1", "169.254.169.254", "100.100.100.200", "fd00:ec2::254", "::ffff:127.0.0.1", "0.0.0.0", "ff02::1"} {
		f.resolve = func(context.Context, string) ([]netip.Addr, error) {
			return []netip.Addr{netip.MustParseAddr(address)}, nil
		}
		if _, err = f.PostJSON(context.Background(), "https://receiver.example/events", http.Header{}, []byte(`{}`)); err == nil {
			t.Fatal("metadata destination allowed")
		}
	}
	if calls != 0 {
		t.Fatal("untrusted address was dialed")
	}
	if _, err = f.PostJSON(context.Background(), "https://other.example/events", http.Header{}, []byte(`{}`)); !errors.Is(err, ErrNotAllowed) {
		t.Fatal("origin bypass")
	}
	if _, err = f.PostJSON(context.Background(), "https://receiver.example/events", http.Header{}, bytes.Repeat([]byte{'x'}, 65<<10)); !errors.Is(err, ErrNotAllowed) {
		t.Fatal("body limit bypass")
	}
}
