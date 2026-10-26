// Package webhook implements protected, project-bound outbound notification sinks.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/mediasource"
)

const Schema = "keel.webhook-sinks/v1"

var identity = regexp.MustCompile(`^[a-f0-9]{32}$`)
var sinkIdentity = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
var eventTypes = map[string]bool{"execution.state_changed": true, "execution.circuit_breaker": true, "execution.authorization_blocked": true}

type Sink struct {
	ID             string            `json:"id"`
	ProjectID      string            `json:"project_id"`
	NotBefore      time.Time         `json:"not_before"`
	EventTypes     []string          `json:"event_types"`
	URL            string            `json:"url"`
	SigningKeyFile string            `json:"signing_key_file"`
	Format         string            `json:"format,omitempty"`
	APIKeyFile     string            `json:"api_key_file,omitempty"`
	Email          *EmailDestination `json:"email,omitempty"`
	apiKey         []byte
	key            []byte
	keyID          string
	policy         string
}

// GenerateSigningKey creates a separate HMAC secret; it never reuses a vault key.
func GenerateSigningKey(path string) (string, error) {
	key := make([]byte, 32)
	defer clear(key)
	if _, err := rand.Read(key); err != nil {
		return "", errors.New("webhook key generation failed")
	}
	if err := agentpki.WriteNew(path, key); err != nil {
		return "", errors.New("new protected webhook key path required")
	}
	return signingKeyID(key), nil
}
func signingKeyID(key []byte) string {
	material := append([]byte("Keel webhook key v1\x00"), key...)
	defer clear(material)
	sum := sha256.Sum256(material)
	return hex.EncodeToString(sum[:])
}
func (s *Sink) PolicyHash() string { return s.policy }
func (s *Sink) KeyID() string      { return s.keyID }

type Config struct {
	Schema         string   `json:"schema"`
	AllowedOrigins []string `json:"allowed_origins"`
	Sinks          []Sink   `json:"sinks"`
	fetch          *mediasource.Fetcher
}

func (c *Config) Close() {
	if c != nil {
		for i := range c.Sinks {
			clear(c.Sinks[i].key)
			c.Sinks[i].key = nil
			clear(c.Sinks[i].apiKey)
			c.Sinks[i].apiKey = nil
		}
	}
}

// Load is disabled with an empty path; no DNS, HTTP or key is touched then.
func Load(path string, development bool) (*Config, error) {
	if path == "" {
		return nil, nil
	}
	raw, err := agentpki.ReadFile(path, 64<<10, true)
	if err != nil {
		return nil, errors.New("protected webhook configuration unavailable")
	}
	defer clear(raw)
	c := &Config{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(c) != nil || decoder.Decode(new(any)) != io.EOF || c.Schema != Schema || len(c.Sinks) < 1 || len(c.Sinks) > 16 || len(c.AllowedOrigins) < 1 || len(c.AllowedOrigins) > 32 {
		return nil, errors.New("invalid webhook configuration")
	}
	c.fetch, err = mediasource.New(strings.Join(c.AllowedOrigins, ","), development)
	if err != nil || c.fetch == nil {
		return nil, errors.New("invalid webhook origin policy")
	}
	seen := map[string]bool{}
	for i := range c.Sinks {
		s := &c.Sinks[i]
		if !sinkIdentity.MatchString(s.ID) || seen[s.ID] || !identity.MatchString(s.ProjectID) || s.NotBefore.IsZero() || len(s.URL) > 2048 || c.fetch.CheckURL(s.URL) != nil || len(s.EventTypes) < 1 || len(s.EventTypes) > len(eventTypes) {
			c.Close()
			return nil, errors.New("invalid project-bound webhook sink")
		}
		seen[s.ID] = true
		sort.Strings(s.EventTypes)
		for j, kind := range s.EventTypes {
			if !eventTypes[kind] || j > 0 && kind == s.EventTypes[j-1] {
				c.Close()
				return nil, errors.New("invalid webhook event types")
			}
		}
		s.key, err = agentpki.ReadFile(s.SigningKeyFile, 32, true)
		if err != nil || len(s.key) != 32 {
			c.Close()
			return nil, errors.New("webhook HMAC key must be a protected 32-byte file")
		}
		s.keyID = signingKeyID(s.key)
		if err = s.loadFormat(development); err != nil {
			c.Close()
			return nil, err
		}
		projection, _ := json.Marshal(struct {
			ID, ProjectID, URL, KeyID string
			NotBefore                 time.Time
			Types                     []string
		}{s.ID, s.ProjectID, s.URL, s.keyID, s.NotBefore.UTC(), s.EventTypes})
		if s.Format != "cloudevents" {
			projection, _ = json.Marshal(struct {
				Base             json.RawMessage
				Format, APIKeyID string
				Email            *EmailDestination
			}{projection, s.Format, providerCredentialID(s.apiKey), s.Email})
		}
		sum := sha256.Sum256(projection)
		s.policy = hex.EncodeToString(sum[:])
	}
	return c, nil
}
func signature(key []byte, timestamp, id string, body []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(timestamp + "\n" + id + "\n"))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
func (c *Config) Deliver(ctx context.Context, sink *Sink, eventID string, body []byte) (status int, retry bool, err error) {
	if c == nil || sink == nil || len(sink.key) != 32 || !identity.MatchString(eventID) {
		return 0, false, errors.New("webhook signing identity unavailable")
	}
	if sink.Format != "" && sink.Format != "cloudevents" {
		return c.deliverProvider(ctx, sink, eventID, body)
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	headers := http.Header{"X-Keel-Event-Id": {eventID}, "X-Keel-Timestamp": {timestamp}, "X-Keel-Key-Id": {sink.keyID}, "X-Keel-Signature": {signature(sink.key, timestamp, eventID, body)}}
	status, err = c.fetch.PostJSON(ctx, sink.URL, headers, body)
	if err != nil {
		return 0, true, errors.New("webhook transport unavailable")
	}
	retry = status == 408 || status == 429 || status >= 500 && status <= 599
	return status, retry, nil
}

// Verify authenticates the raw bounded body and five-minute time window. The
// receiver must additionally persist (project,event ID) to reject replay.
func Verify(key []byte, timestamp, eventID, provided string, body []byte, now time.Time) error {
	at, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || strconv.FormatInt(at, 10) != timestamp || len(key) != 32 || len(body) > 64<<10 || !identity.MatchString(eventID) || at < now.Unix()-300 || at > now.Unix()+300 || !hmac.Equal([]byte(provided), []byte(signature(key, timestamp, eventID, body))) {
		return errors.New("webhook authentication rejected")
	}
	return nil
}
