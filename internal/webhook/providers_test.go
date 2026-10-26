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
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func providerConfig(t *testing.T, origin, format string, change func(*Sink)) *Config {
	t.Helper()
	dir := t.TempDir()
	keyPath, tokenPath := filepath.Join(dir, "hmac"), filepath.Join(dir, "api")
	if err := os.WriteFile(keyPath, bytes.Repeat([]byte{0x61}, 32), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tokenPath, []byte("re_fixture_provider_token_123456"), 0600); err != nil {
		t.Fatal(err)
	}
	sink := Sink{ID: "provider-fixture", ProjectID: strings.Repeat("a", 32), NotBefore: time.Now().UTC().Add(-time.Hour), EventTypes: []string{"execution.state_changed"}, Format: format, SigningKeyFile: keyPath}
	if format == "resend" {
		sink.URL, sink.APIKeyFile, sink.Email = origin+"/emails", tokenPath, &EmailDestination{From: "keel@example.com", To: []string{"ops@example.com"}}
	} else {
		sink.URL = origin + "/services/T1/B1/fixture"
	}
	if change != nil {
		change(&sink)
	}
	raw, err := json.Marshal(Config{Schema: Schema, AllowedOrigins: []string{origin}, Sinks: []Sink{sink}})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, "sinks.json")
	if err = os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(configPath, true)
	if change != nil { // Negative fixtures explicitly request a load failure.
		if err == nil {
			c.Close()
			t.Fatal("invalid provider config accepted")
		}
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func publicProviderEvent(t *testing.T, project, id string, at time.Time) []byte {
	t.Helper()
	value := map[string]any{"specversion": "1.0", "id": id, "source": "urn:keel:project:" + project, "type": "execution.state_changed", "subject": "execution/" + strings.Repeat("b", 32), "time": at.UTC(), "datacontenttype": "application/json", "data": map[string]any{"project_id": project, "execution_id": strings.Repeat("b", 32), "aggregate_version": 2, "state": "failed"}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestWebhookProviderPayloadAcknowledgementAndIdempotency(t *testing.T) {
	for _, format := range []string{"slack", "resend"} {
		t.Run(format, func(t *testing.T) {
			var calls atomic.Int64
			var reply atomic.Value
			if format == "slack" {
				reply.Store("ok")
			} else {
				reply.Store(`{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`)
			}
			var firstBody []byte
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
				if err != nil {
					t.Error(err)
				}
				if err = Verify(bytes.Repeat([]byte{0x61}, 32), r.Header.Get("X-Keel-Timestamp"), r.Header.Get("X-Keel-Event-ID"), r.Header.Get("X-Keel-Signature"), raw, time.Now()); err != nil {
					t.Error("signature must cover actual provider JSON", err)
				}
				if r.Header.Get("Content-Type") != "application/json" {
					t.Error("incorrect provider content type")
				}
				if firstBody == nil {
					firstBody = append([]byte(nil), raw...)
				} else if !bytes.Equal(firstBody, raw) {
					t.Error("retry changed provider message")
				}
				if format == "resend" {
					if r.Header.Get("Authorization") != "Bearer re_fixture_provider_token_123456" || r.Header.Get("Idempotency-Key") != "keel/provider-fixture/"+strings.Repeat("c", 32) {
						t.Error("missing bounded provider authority or stable idempotency key")
					}
				} else if r.Header.Get("Authorization") != "" {
					t.Error("mail credential leaked into IM")
				}
				var message map[string]json.RawMessage
				if json.Unmarshal(raw, &message) != nil || !bytes.Contains(message["text"], []byte("State: failed")) || !bytes.Contains(message["text"], []byte(strings.Repeat("a", 32))) || bytes.Contains(raw, []byte("provider_token")) {
					t.Error("incorrect public message projection")
				}
				_, _ = io.WriteString(w, reply.Load().(string))
			}))
			defer server.Close()
			config := providerConfig(t, server.URL, format, nil)
			id := strings.Repeat("c", 32)
			body := publicProviderEvent(t, config.Sinks[0].ProjectID, id, time.Now())
			for i := 0; i < 2; i++ {
				status, retry, err := config.Deliver(context.Background(), &config.Sinks[0], id, body)
				if status != 200 || retry || err != nil {
					t.Fatal("provider acknowledgement", status, retry, err)
				}
			}
			reply.Store("invalid_payload")
			status, _, err := config.Deliver(context.Background(), &config.Sinks[0], id, body)
			if status != 200 || err == nil {
				t.Fatal("HTTP 200 without protocol acknowledgement counted as delivered")
			}
			before := calls.Load()
			foreign := publicProviderEvent(t, strings.Repeat("d", 32), id, time.Now())
			if _, _, err = config.Deliver(context.Background(), &config.Sinks[0], id, foreign); err == nil || calls.Load() != before {
				t.Fatal("cross-project provider dispatch")
			}
			if format == "resend" {
				old := publicProviderEvent(t, config.Sinks[0].ProjectID, id, time.Now().Add(-24*time.Hour))
				if _, retry, err := config.Deliver(context.Background(), &config.Sinks[0], id, old); err == nil || retry || calls.Load() != before {
					t.Fatal("mail replayed outside idempotency retention")
				}
			}
		})
	}
}

func TestWebhookProviderConfigurationAndPolicyBoundaries(t *testing.T) {
	for _, change := range []func(*Sink){
		func(s *Sink) { s.Format = "unknown" },
		func(s *Sink) { s.Email.To = []string{"ops@example.com\r\nBcc: other@example.com"} },
		func(s *Sink) { s.Email.To = []string{"ops@example.com", "ops@example.com"} },
		func(s *Sink) { s.APIKeyFile = s.SigningKeyFile },
		func(s *Sink) { s.URL = "http://127.0.0.1/other" },
	} {
		providerConfig(t, "http://127.0.0.1", "resend", change)
	}
	providerConfig(t, "http://127.0.0.1", "slack", func(s *Sink) { s.APIKeyFile = s.SigningKeyFile })
	providerConfig(t, "https://unapproved-provider.example", "slack", func(s *Sink) {})
	one := providerConfig(t, "http://127.0.0.1", "resend", nil)
	raw, err := json.Marshal(one)
	if err != nil {
		t.Fatal(err)
	}
	reload := func(change func(*Sink)) *Config {
		t.Helper()
		var copy Config
		if err := json.Unmarshal(raw, &copy); err != nil {
			t.Fatal(err)
		}
		if change != nil {
			change(&copy.Sinks[0])
		}
		encoded, err := json.Marshal(copy)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "copy.json")
		if err = os.WriteFile(path, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		loaded, err := Load(path, true)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(loaded.Close)
		return loaded
	}
	if reload(nil).Sinks[0].PolicyHash() != one.Sinks[0].PolicyHash() {
		t.Fatal("configuration file path changed policy identity")
	}
	changed := reload(func(s *Sink) { s.Email.To = []string{"different@example.com"} })
	if changed.Sinks[0].PolicyHash() == one.Sinks[0].PolicyHash() {
		t.Fatal("recipient change retained old immutable policy identity")
	}
	token := filepath.Join(t.TempDir(), "new-api-key")
	if err = os.WriteFile(token, []byte("re_independent_rotated_token_123456"), 0600); err != nil {
		t.Fatal(err)
	}
	changed = reload(func(s *Sink) { s.APIKeyFile = token })
	if changed.Sinks[0].PolicyHash() == one.Sinks[0].PolicyHash() {
		t.Fatal("provider key rotation retained old immutable policy identity")
	}
	s := &one.Sinks[0]
	one.Close()
	if len(s.apiKey) != 0 || len(s.key) != 0 {
		t.Fatal("closed notification credentials retained")
	}
}
