package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testConfig(t *testing.T, target string) (*Config, []byte) {
	t.Helper()
	dir := t.TempDir()
	key := bytes.Repeat([]byte{0x53}, 32)
	keyPath := filepath.Join(dir, "key")
	if err := os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	config := Config{Schema: Schema, AllowedOrigins: []string{target}, Sinks: []Sink{{ID: "fixture", ProjectID: strings.Repeat("a", 32), URL: target + "/events", SigningKeyFile: keyPath, NotBefore: time.Now().UTC().Add(-time.Hour), EventTypes: []string{"execution.state_changed"}}}}
	raw, _ := json.Marshal(config)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(loaded.Close)
	return loaded, key
}
func TestWebhookSignedDeliveryAndReceiverBoundaries(t *testing.T) {
	id := strings.Repeat("a", 32)
	body := []byte(`{"specversion":"1.0","id":"` + id + `"}`)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/cloudevents+json" || !bytes.Equal(raw, body) {
			t.Error("unexpected raw event")
		}
		if err = Verify(bytes.Repeat([]byte{0x53}, 32), r.Header.Get("X-Keel-Timestamp"), r.Header.Get("X-Keel-Event-ID"), r.Header.Get("X-Keel-Signature"), raw, time.Now()); err != nil {
			t.Error(err)
		}
		w.WriteHeader(204)
	}))
	defer server.Close()
	config, key := testConfig(t, server.URL)
	status, retry, err := config.Deliver(context.Background(), &config.Sinks[0], id, body)
	if err != nil || status != 204 || retry || calls.Load() != 1 {
		t.Fatalf("delivery: %d %t %v calls=%d", status, retry, err, calls.Load())
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signed := signature(key, timestamp, id, body)
	if Verify(key, timestamp, id, signed, body, time.Now()) != nil {
		t.Fatal("valid signature rejected")
	}
	for name, check := range map[string]func() error{
		"wrong-key": func() error { return Verify(bytes.Repeat([]byte{1}, 32), timestamp, id, signed, body, time.Now()) },
		"raw-body":  func() error { return Verify(key, timestamp, id, signed, append(body, ' '), time.Now()) },
		"event-id":  func() error { return Verify(key, timestamp, strings.Repeat("b", 32), signed, body, time.Now()) },
		"old":       func() error { return Verify(key, timestamp, id, signed, body, time.Now().Add(301*time.Second)) },
		"future":    func() error { return Verify(key, timestamp, id, signed, body, time.Now().Add(-301*time.Second)) },
		"alias":     func() error { return Verify(key, "0"+timestamp, id, signed, body, time.Now()) },
		"oversize":  func() error { return Verify(key, timestamp, id, signed, bytes.Repeat([]byte{'x'}, 65<<10), time.Now()) },
	} {
		if check() == nil {
			t.Fatal("authentication bypass:", name)
		}
	}
	config.Close()
	if _, _, err = config.Deliver(context.Background(), &config.Sinks[0], id, body); err == nil {
		t.Fatal("closed key still signs")
	}
}
func TestWebhookRetryClassificationAndRedirectIsolation(t *testing.T) {
	var redirected atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Add(1); w.WriteHeader(204) }))
	defer destination.Close()
	var responseCode atomic.Int64
	responseCode.Store(302)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", destination.URL+"/capture")
		w.WriteHeader(int(responseCode.Load()))
	}))
	defer source.Close()
	config, _ := testConfig(t, source.URL)
	for _, test := range []struct {
		status int
		retry  bool
	}{{302, false}, {400, false}, {403, false}, {408, true}, {429, true}, {500, true}, {503, true}, {204, false}} {
		responseCode.Store(int64(test.status))
		status, retry, err := config.Deliver(context.Background(), &config.Sinks[0], strings.Repeat("a", 32), []byte(`{}`))
		if err != nil || status != test.status || retry != test.retry {
			t.Fatalf("status classification: %d %t %v", status, retry, err)
		}
	}
	if redirected.Load() != 0 {
		t.Fatal("notification followed a redirect")
	}
}
func TestWebhookConfigurationDisabledAndProtected(t *testing.T) {
	if config, err := Load("", false); err != nil || config != nil {
		t.Fatal("unset config must disable network")
	}
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key")
	os.WriteFile(keyPath, bytes.Repeat([]byte{1}, 32), 0600)
	valid := Config{Schema: Schema, AllowedOrigins: []string{"https://example.test"}, Sinks: []Sink{{ID: "fixture", ProjectID: strings.Repeat("a", 32), NotBefore: time.Now().UTC(), EventTypes: []string{"execution.state_changed"}, URL: "https://example.test/events", SigningKeyFile: keyPath}}}
	for name, mutate := range map[string]func(*Config){
		"http": func(c *Config) {
			c.AllowedOrigins = []string{"http://example.test"}
			c.Sinks[0].URL = "http://example.test/events"
		},
		"origin":        func(c *Config) { c.Sinks[0].URL = "https://other.test/events" },
		"query-secret":  func(c *Config) { c.Sinks[0].URL += "?token=secret" },
		"fragment":      func(c *Config) { c.Sinks[0].URL += "#secret" },
		"user-secret":   func(c *Config) { c.Sinks[0].URL = "https://user:secret@example.test/events" },
		"project":       func(c *Config) { c.Sinks[0].ProjectID = "any" },
		"history-bound": func(c *Config) { c.Sinks[0].NotBefore = time.Time{} },
		"event":         func(c *Config) { c.Sinks[0].EventTypes = []string{"attempt.receipt_applied"} },
		"duplicate":     func(c *Config) { c.Sinks = append(c.Sinks, c.Sinks[0]) },
		"key":           func(c *Config) { c.Sinks[0].SigningKeyFile = filepath.Join(dir, "missing") },
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(valid)
			var copy Config
			json.Unmarshal(raw, &copy)
			mutate(&copy)
			raw, _ = json.Marshal(copy)
			path := filepath.Join(dir, "config-"+name)
			os.WriteFile(path, raw, 0600)
			if c, err := Load(path, false); err == nil {
				c.Close()
				t.Fatal("invalid policy accepted")
			}
		})
	}
	raw, _ := json.Marshal(valid)
	path := filepath.Join(dir, "public-config")
	os.WriteFile(path, raw, 0644)
	if c, err := Load(path, false); err == nil {
		c.Close()
		t.Fatal("unprotected policy accepted")
	}
}
