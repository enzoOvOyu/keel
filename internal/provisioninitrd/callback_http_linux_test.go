//go:build linux

package provisioninitrd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
)

func TestInitrdCallbackHTTPSUsesFixedDestinationAndNoAmbientCredential(t *testing.T) {
	hits := 0
	token := strings.Repeat("a", 64)
	raw := []byte(`{"private_protocol_fixture":true}`)
	files, _ := tlsFiles(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != "POST" || r.URL.Path != "/boot/"+strings.Repeat("3", 32)+"/enroll" || r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("Cookie") != "" || r.Header.Get("Origin") != "" {
			t.Error("callback request escaped fixed boundary")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(body, raw) {
			t.Error("callback payload changed", err)
		}
		_ = json.NewEncoder(w).Encode(provisionboot.BootAcknowledgment{InstallationID: strings.Repeat("4", 32), BootID: strings.Repeat("5", 32), Sequence: 1, State: "booting", ExpiresAt: time.Now().Add(time.Minute)})
	}))
	if _, err := files.PostCallback(context.Background(), "enroll", token, raw); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatal("unexpected repeated POST", hits)
	}
	for _, operation := range []string{"../enroll", "write-permit", "first-boot"} {
		if _, err := files.PostCallback(context.Background(), operation, token, raw); err == nil {
			t.Fatal("unsupported operation sent")
		}
	}
	if _, err := files.PostCallback(context.Background(), "preflight", token, raw); err == nil || hits != 1 {
		t.Fatal("bootstrap secret reused on report")
	}
}
func TestInitrdCallbackHTTPSRedirectAndEncodingFailOnce(t *testing.T) {
	for _, code := range []int{302, 307, 308, 200} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			hits := 0
			files, _ := tlsFiles(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits++
				if code == 200 {
					w.Header().Set("Content-Encoding", "gzip")
				} else {
					w.Header().Set("Location", "https://other.example.test/leak")
				}
				w.WriteHeader(code)
				io.WriteString(w, `{"untrusted":true}`)
			}))
			if _, err := files.PostCallback(context.Background(), "enroll", strings.Repeat("a", 64), []byte(`{}`)); err == nil || hits != 1 {
				t.Fatal("redirect/encoding accepted or retried", hits, err)
			}
		})
	}
}
