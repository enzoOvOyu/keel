package netbaseline

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var ref = regexp.MustCompile(`^[a-f0-9]{32}$`)
var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Gate struct {
	ID             string `json:"id"`
	State          string `json:"state"`
	Location       string `json:"location"`
	Code           string `json:"code"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}
type Operation struct {
	ApprovedVariables   map[string]any  `json:"approved_variables,omitempty"`
	ID                  string          `json:"operation_id"`
	Phase               string          `json:"phase"`
	ProjectID           string          `json:"project_id"`
	DeviceID            string          `json:"device_id"`
	Serial              string          `json:"serial"`
	FencingToken        int64           `json:"fencing_token"`
	CredentialBatchRef  string          `json:"credential_batch_ref"`
	BaselineID          string          `json:"baseline_id"`
	TemplateHash        string          `json:"template_hash"`
	Intended            json.RawMessage `json:"intended"`
	ExpectedRunningHash string          `json:"expected_running_hash,omitempty"`
	RollbackRef         string          `json:"rollback_ref,omitempty"`
}
type OperationResult struct {
	Version        string          `json:"version"`
	OperationID    string          `json:"operation_id"`
	ProjectID      string          `json:"project_id"`
	DeviceID       string          `json:"device_id"`
	Serial         string          `json:"serial"`
	InputSHA256    string          `json:"input_sha256"`
	FencingToken   int64           `json:"fencing_token"`
	State          string          `json:"state"`
	Code           string          `json:"code"`
	EvidenceSource string          `json:"evidence_source"`
	Provider       string          `json:"provider"`
	ObservedAt     time.Time       `json:"observed_at"`
	Gates          []Gate          `json:"gates"`
	Running        json.RawMessage `json:"running,omitempty"`
	RunningHash    string          `json:"running_hash,omitempty"`
	SnapshotRef    string          `json:"snapshot_ref,omitempty"`
	ExitCode       int             `json:"exit_code"`
	DurationMS     int64           `json:"duration_ms"`
}
type MutationUnknown struct{ Phase string }

func (e *MutationUnknown) Error() string {
	return "network mutation result is unknown during " + e.Phase
}

type Client struct {
	endpoint string
	http     *http.Client
	writes   bool
}

func NewClient(endpoint string, client *http.Client, writes bool) (*Client, error) {
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || client == nil {
		return nil, errors.New("network adapter needs a configured HTTPS endpoint")
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{endpoint: u.Scheme + "://" + u.Host, http: &copy, writes: writes}, nil
}
func (c *Client) WriteEnabled() bool { return c != nil && c.writes }
func ContentHash(value any) string {
	if input, ok := value.(json.RawMessage); ok {
		d := json.NewDecoder(bytes.NewReader(input))
		d.UseNumber()
		var parsed any
		if d.Decode(&parsed) != nil {
			return ""
		}
		value = parsed
	}
	raw, e := json.Marshal(value)
	if e != nil {
		return ""
	}
	s := sha256.Sum256(raw)
	return hex.EncodeToString(s[:])
}

// Secrets stay at the credential adapter. Normalized intended/running data may
// use references but never embed passwords, private keys or access tokens.
func ValidateConfiguration(raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > MaxRenderedBytes {
		return errors.New("invalid network configuration size")
	}
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil || value == nil {
		return errors.New("network configuration must be an object")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return errors.New("configuration contains trailing data")
	}
	var walk func(any) error
	walk = func(v any) error {
		switch x := v.(type) {
		case string:
			if bytes.Contains([]byte(x), []byte("PRIVATE KEY-----")) {
				return errors.New("private key material is not allowed in network configuration")
			}
		case map[string]any:
			for key, item := range x {
				for _, sensitive := range []string{"password", "secret", "token", "private_key", "private-key", "community"} {
					if strings.ToLower(key) == sensitive {
						object, ok := item.(map[string]any)
						reference, valid := object["credential_ref"].(string)
						if !ok || !valid || len(object) != 1 || reference == "" {
							return errors.New("secret settings must use credential_ref")
						}
					}
				}
				if e := walk(item); e != nil {
					return e
				}
			}
		case []any:
			for _, item := range x {
				if e := walk(item); e != nil {
					return e
				}
			}
		}
		return nil
	}
	return walk(value)
}
func (c *Client) Do(ctx context.Context, input Operation) (*OperationResult, error) {
	if c == nil || !ref.MatchString(input.ProjectID) || !ref.MatchString(input.DeviceID) || !ref.MatchString(input.ID) {
		return nil, errors.New("invalid network adapter operation scope")
	}
	mutating := false
	switch input.Phase {
	case "template-test", "prepare", "verify":
	case "apply", "rollback":
		mutating = true
	default:
		return nil, errors.New("unsupported network operation")
	}
	if mutating && (!c.writes || input.FencingToken < 1) {
		return nil, errors.New("network writes require operator opt-in and a fenced claim")
	}
	if e := ValidateConfiguration(input.Intended); e != nil {
		return nil, e
	}
	body, e := json.Marshal(input)
	if e != nil {
		return nil, e
	}
	expected := ContentHash(input)
	request, e := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/projects/"+input.ProjectID+"/network-operations", bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", input.ID)
	failed := func() (*OperationResult, error) {
		if mutating {
			return nil, &MutationUnknown{Phase: input.Phase}
		}
		return nil, errors.New("network adapter evidence unavailable or invalid")
	}
	response, e := c.http.Do(request)
	if e != nil {
		return failed()
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return failed()
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if e != nil || len(raw) > 4<<20 {
		return failed()
	}
	var out OperationResult
	if json.Unmarshal(raw, &out) != nil || out.Version != "network-operation/v1" || out.OperationID != input.ID || out.ProjectID != input.ProjectID || out.DeviceID != input.DeviceID || out.Serial != input.Serial || out.InputSHA256 != expected || out.FencingToken != input.FencingToken || out.Provider == "" || (out.EvidenceSource != "device" && out.EvidenceSource != "protocol_fixture") || out.ObservedAt.IsZero() || time.Since(out.ObservedAt) > time.Minute || out.ObservedAt.After(time.Now().Add(5*time.Second)) {
		return failed()
	}
	if out.State != "completed" && out.State != "rejected" {
		return failed()
	}
	if mutating && out.State == "rejected" && out.ExitCode == 0 {
		return failed()
	}
	if len(out.Gates) > 64 {
		return failed()
	}
	seen := map[string]bool{}
	for _, gate := range out.Gates {
		if gate.ID == "" || seen[gate.ID] || !digest.MatchString(gate.EvidenceSHA256) {
			return failed()
		}
		seen[gate.ID] = true
		switch gate.State {
		case "PASS", "WARN", "FAIL", "UNKNOWN":
		default:
			return failed()
		}
	}
	if len(out.Running) > 0 {
		if ValidateConfiguration(out.Running) != nil || ContentHash(json.RawMessage(out.Running)) != out.RunningHash {
			return failed()
		}
	}
	return &out, nil
}
func RequireGates(result *OperationResult, ids ...string) error {
	if result == nil || result.State != "completed" || result.ExitCode != 0 {
		return errors.New("network operation did not complete successfully")
	}
	values := map[string]string{}
	for _, g := range result.Gates {
		values[g.ID] = g.State
	}
	for _, id := range ids {
		if values[id] != "PASS" {
			return errors.New("required network check is not PASS: " + id)
		}
	}
	return nil
}
