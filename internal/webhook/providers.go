package webhook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
)

// Provider adapters render only the existing public CloudEvent projection.
// https://resend.com/docs/api-reference/emails/send-email
// https://resend.com/docs/dashboard/emails/idempotency-keys
// https://docs.slack.dev/messaging/sending-messages-using-incoming-webhooks/
type EmailDestination struct {
	From string   `json:"from"`
	To   []string `json:"to"`
}

var ErrProviderRejected = errors.New("notification provider rejected acknowledgement")
var ErrDeliveryWindowExpired = errors.New("notification delivery window expired")
var resendToken = regexp.MustCompile(`^re_[A-Za-z0-9_-]{16,200}$`)
var slackPath = regexp.MustCompile(`^/services/[A-Za-z0-9_-]{1,100}/[A-Za-z0-9_-]{1,100}/[A-Za-z0-9_-]{1,200}$`)
var providerMessageID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}$`)

func (s *Sink) DeliveryFormat() string {
	if s.Format == "" {
		return "cloudevents"
	}
	return s.Format
}

func validMailbox(value string) bool {
	if len(value) > 254 || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	parsed, err := mail.ParseAddress(value)
	return err == nil && parsed.Address == value && parsed.Name == "" && strings.Contains(value, "@")
}

func (s *Sink) loadFormat(development bool) error {
	s.Format = s.DeliveryFormat()
	invalid := errors.New("invalid notification provider configuration")
	if s.Format == "cloudevents" {
		if s.APIKeyFile != "" || s.Email != nil {
			return invalid
		}
		return nil
	}
	u, err := url.Parse(s.URL)
	if err != nil || u.RawPath != "" {
		return invalid
	}
	// Only explicit development loopback fixtures may substitute a provider.
	fixture := development && (u.Hostname() == "127.0.0.1" || u.Hostname() == "::1" || strings.EqualFold(u.Hostname(), "localhost"))
	standardTLS := u.Scheme == "https" && (u.Port() == "" || u.Port() == "443")
	switch s.Format {
	case "slack":
		if s.Email != nil || s.APIKeyFile != "" || !slackPath.MatchString(u.Path) || (!fixture && (!standardTLS || u.Hostname() != "hooks.slack.com")) {
			return invalid
		}
	case "resend":
		if s.Email == nil || !validMailbox(s.Email.From) || len(s.Email.To) < 1 || len(s.Email.To) > 16 || u.Path != "/emails" || (!fixture && (!standardTLS || u.Hostname() != "api.resend.com")) {
			return invalid
		}
		sort.Strings(s.Email.To)
		seen := map[string]bool{}
		for _, recipient := range s.Email.To {
			key := strings.ToLower(recipient)
			if !validMailbox(recipient) || seen[key] {
				return invalid
			}
			seen[key] = true
		}
		s.apiKey, err = agentpki.ReadFile(s.APIKeyFile, 256, true)
		if err != nil || !resendToken.Match(s.apiKey) || bytes.Equal(s.apiKey, s.key) {
			return errors.New("independent protected notification provider credential required")
		}
	default:
		return invalid
	}
	return nil
}

func providerCredentialID(key []byte) string {
	if len(key) == 0 {
		return ""
	}
	material := append([]byte("Keel notification provider credential v1\x00"), key...)
	defer clear(material)
	sum := sha256.Sum256(material)
	return hex.EncodeToString(sum[:])
}

type providerEvent struct {
	SpecVersion string    `json:"specversion"`
	ID          string    `json:"id"`
	Source      string    `json:"source"`
	Type        string    `json:"type"`
	Subject     string    `json:"subject"`
	ContentType string    `json:"datacontenttype"`
	Time        time.Time `json:"time"`
	Data        struct {
		ProjectID           string `json:"project_id"`
		AlertID             string `json:"alert_id,omitempty"`
		Severity            string `json:"severity,omitempty"`
		ObservationSource   string `json:"observation_source,omitempty"`
		ExecutionID         string `json:"execution_id"`
		IssueID             string `json:"issue_id,omitempty"`
		FailedVerifications int    `json:"failed_verifications,omitempty"`
		Version             int64  `json:"aggregate_version"`
		State               string `json:"state,omitempty"`
	} `json:"data"`
}

func providerText(s *Sink, eventID string, body []byte) (string, string, error) {
	var event providerEvent
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if len(body) > 64<<10 || decoder.Decode(&event) != nil || decoder.Decode(new(any)) != io.EOF || event.SpecVersion != "1.0" || event.ID != eventID || event.Data.ProjectID != s.ProjectID || event.Source != "urn:keel:project:"+s.ProjectID || (event.Type != "monitor.alert" && !identity.MatchString(event.Data.ExecutionID)) || event.ContentType != "application/json" || event.Time.IsZero() || event.Data.Version < 1 || !eventTypes[event.Type] {
		return "", "", errors.New("invalid public notification event")
	}
	if event.Type == "monitor.alert" {
		if !identity.MatchString(event.Data.AlertID) || event.Subject != "alert/"+event.Data.AlertID || event.Data.ExecutionID != "" || event.Data.State != "" || event.Data.FailedVerifications != 0 || (event.Data.IssueID != "" && !identity.MatchString(event.Data.IssueID)) || (event.Data.Severity != "info" && event.Data.Severity != "warning" && event.Data.Severity != "critical") || (event.Data.ObservationSource != "host" && event.Data.ObservationSource != "switch" && event.Data.ObservationSource != "bmc") {
			return "", "", errors.New("invalid monitoring event projection")
		}
	} else if event.Data.AlertID != "" || event.Data.Severity != "" || event.Data.ObservationSource != "" {
		return "", "", errors.New("monitor fields on execution event")
	} else if event.Type == "issue.escalated" {
		if !identity.MatchString(event.Data.IssueID) || event.Data.FailedVerifications != 2 || event.Subject != "issue/"+event.Data.IssueID {
			return "", "", errors.New("invalid issue escalation projection")
		}
	} else if event.Subject != "execution/"+event.Data.ExecutionID || event.Data.IssueID != "" || event.Data.FailedVerifications != 0 {
		return "", "", errors.New("invalid execution event subject")
	}
	allowed := false
	for _, kind := range s.EventTypes {
		if kind == event.Type {
			allowed = true
		}
	}
	if !allowed {
		return "", "", errors.New("notification event type outside sink policy")
	}
	switch event.Data.State {
	case "", "created", "scheduled", "dispatching", "running", "pausing", "paused", "cancelling", "reconciling", "success", "failed", "killed", "warning":
	default:
		return "", "", errors.New("invalid public notification state")
	}
	if event.Time.After(time.Now().Add(5 * time.Minute)) {
		return "", "", ErrProviderRejected
	}
	if s.Format == "resend" && time.Since(event.Time) >= 23*time.Hour {
		// Every retry stays within the provider's 24h idempotency retention.
		// Old/uncertain mail is stopped rather than resent after that window.
		return "", "", ErrDeliveryWindowExpired
	}
	subject := "[Keel] " + event.Type
	text := fmt.Sprintf("%s\nProject: %s\nExecution: %s\nEvent: %s\nVersion: %d\nTime: %s", subject, s.ProjectID, event.Data.ExecutionID, event.ID, event.Data.Version, event.Time.UTC().Format(time.RFC3339Nano))
	if event.Data.State != "" {
		text += "\nState: " + event.Data.State
	}
	if event.Type == "issue.escalated" {
		text += "\nIssue: " + event.Data.IssueID + "\nTwo scoped verifications failed. DL review and evidence-based replacement assessment required."
	}
	if event.Type == "monitor.alert" {
		text += "\nAlert: " + event.Data.AlertID + "\nSeverity: " + event.Data.Severity + "\nSource: " + event.Data.ObservationSource
		if event.Data.IssueID != "" {
			text += "\nIssue: " + event.Data.IssueID
		}
	}
	return subject, text, nil
}

func (c *Config) deliverProvider(ctx context.Context, s *Sink, eventID string, body []byte) (int, bool, error) {
	subject, text, err := providerText(s, eventID, body)
	if err != nil {
		if errors.Is(err, ErrDeliveryWindowExpired) {
			return 0, false, ErrDeliveryWindowExpired
		}
		return 0, false, ErrProviderRejected
	}
	var wire []byte
	headers := http.Header{"Content-Type": {"application/json"}}
	switch s.Format {
	case "slack":
		// Public fields contain no user-controlled mentions, URLs or markup.
		wire, err = json.Marshal(map[string]any{"text": text, "unfurl_links": false, "unfurl_media": false})
	case "resend":
		if len(s.apiKey) == 0 || s.Email == nil {
			return 0, false, ErrProviderRejected
		}
		wire, err = json.Marshal(map[string]any{"from": s.Email.From, "to": s.Email.To, "subject": subject, "text": text})
		headers.Set("Authorization", "Bearer "+string(s.apiKey))
		// Stable across retries and process restarts; provider retention is 24h.
		headers.Set("Idempotency-Key", "keel/"+s.ID+"/"+eventID)
	default:
		return 0, false, ErrProviderRejected
	}
	if err != nil {
		return 0, false, ErrProviderRejected
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	headers.Set("X-Keel-Event-ID", eventID)
	headers.Set("X-Keel-Timestamp", timestamp)
	headers.Set("X-Keel-Key-ID", s.keyID)
	headers.Set("X-Keel-Signature", signature(s.key, timestamp, eventID, wire))
	status, acknowledgement, err := c.fetch.PostJSONAcknowledgement(ctx, s.URL, headers, wire)
	defer clear(acknowledgement)
	if err != nil {
		return status, true, errors.New("notification provider transport unavailable")
	}
	if status < 200 || status >= 300 {
		return status, status == 408 || status == 429 || status >= 500, nil
	}
	if s.Format == "slack" {
		if status != 200 || string(bytes.TrimSpace(acknowledgement)) != "ok" {
			return status, false, ErrProviderRejected
		}
	} else {
		var ack struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(acknowledgement, &ack) != nil || !providerMessageID.MatchString(ack.ID) {
			return status, true, errors.New("notification provider acknowledgement unavailable")
		}
	}
	return status, false, nil
}
