package fabricboot

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

var keaHashPattern = regexp.MustCompile(`^[0-9A-Fa-f]{64}$`)

// KeaControl talks to a dedicated DHCPv4 management endpoint. It does not
// render DHCP options or authorize a deployment job. No Kea credentials or
// full configuration are logged by this package.
type KeaControl struct {
	endpoint string
	client   *http.Client
	service  []string
}

func NewKeaControl(endpoint string, roots *x509.CertPool, clientCertificate tls.Certificate, throughAgent bool) (*KeaControl, error) {
	if roots == nil || len(roots.Subjects()) == 0 || len(clientCertificate.Certificate) == 0 {
		return nil, errors.New("Kea control requires explicit CA and client certificate")
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{clientCertificate}}
	transport := &http.Transport{TLSClientConfig: cfg, DisableKeepAlives: true, ResponseHeaderTimeout: 5 * time.Second}
	return newKeaControl(endpoint, &http.Client{Transport: transport, Timeout: 15 * time.Second}, throughAgent)
}

func newKeaControl(endpoint string, client *http.Client, throughAgent bool) (*KeaControl, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed == nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.Hostname() == "" || parsed.RawPath != "" || parsed.ForceQuery || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil || parsed.Opaque != "" || client == nil {
		return nil, errors.New("Kea control endpoint must be an HTTPS origin")
	}
	safeClient := *client
	safeClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	out := &KeaControl{endpoint: "https://" + parsed.Host + "/", client: &safeClient}
	if throughAgent {
		out.service = []string{"dhcp4"}
	}
	return out, nil
}

type keaReply struct {
	Result    *int            `json:"result"`
	Arguments json.RawMessage `json:"arguments"`
}

func (k *KeaControl) command(ctx context.Context, name string, arguments json.RawMessage) (*keaReply, error) {
	body := map[string]any{"command": name}
	if len(k.service) > 0 {
		body["service"] = k.service
	}
	if len(arguments) > 0 {
		body["arguments"] = json.RawMessage(arguments)
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, k.endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := k.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Kea %s HTTP status %d", name, response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(raw) > 4<<20 {
		return nil, errors.New("Kea response unreadable or too large")
	}
	var list []keaReply
	if err = json.Unmarshal(raw, &list); err != nil || len(list) != 1 || list[0].Result == nil {
		return nil, errors.New("Kea command response must contain exactly one result")
	}
	if *list[0].Result != 0 {
		return nil, fmt.Errorf("Kea %s rejected with result %d", name, *list[0].Result)
	}
	return &list[0], nil
}

func (k *KeaControl) hash(ctx context.Context) (string, error) {
	reply, err := k.command(ctx, "config-hash-get", nil)
	if err != nil {
		return "", err
	}
	var value struct {
		Hash string `json:"hash"`
	}
	if err = json.Unmarshal(reply.Arguments, &value); err != nil || !keaHashPattern.MatchString(value.Hash) {
		return "", errors.New("Kea config hash missing or invalid")
	}
	return strings.ToUpper(value.Hash), nil
}

type KeaApplyResult struct {
	BeforeHash      string `json:"before_hash"`
	ActiveHash      string `json:"active_hash"`
	CandidateSHA256 string `json:"candidate_sha256"`
}

// ApplyFullDHCP4 performs hash precheck, Kea config-test, a second drift check,
// config-set and live hash readback. Kea config-set replaces the entire Dhcp4
// configuration and is memory-only; callers must use an approved serialized
// job and separately persist the full config and verify actual DORA behavior.
func (k *KeaControl) ApplyFullDHCP4(ctx context.Context, fullConfig json.RawMessage, expectedCurrentHash string) (*KeaApplyResult, error) {
	if !keaHashPattern.MatchString(expectedCurrentHash) {
		return nil, errors.New("expected Kea hash is required")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(fullConfig, &top); err != nil || len(top) < 1 || len(top) > 2 || len(top["Dhcp4"]) == 0 {
		return nil, errors.New("full Dhcp4 configuration is required")
	}
	var dhcp4 map[string]json.RawMessage
	if err := json.Unmarshal(top["Dhcp4"], &dhcp4); err != nil || dhcp4 == nil {
		return nil, errors.New("full Dhcp4 configuration is required")
	}
	for key := range top {
		if key != "Dhcp4" && key != "Logging" {
			return nil, errors.New("unexpected Kea top-level config key")
		}
	}
	canonical, err := json.Marshal(top)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(canonical)
	before, err := k.hash(ctx)
	if err != nil {
		return nil, err
	}
	if before != strings.ToUpper(expectedCurrentHash) {
		return nil, errors.New("Kea active configuration changed before staging")
	}
	if _, err = k.command(ctx, "config-test", canonical); err != nil {
		return nil, err
	}
	unchanged, err := k.hash(ctx)
	if err != nil {
		return nil, err
	}
	if unchanged != before {
		return nil, errors.New("Kea active configuration changed during staging")
	}
	applied, err := k.command(ctx, "config-set", canonical)
	if err != nil {
		return nil, err
	}
	var result struct {
		Hash string `json:"hash"`
	}
	if err = json.Unmarshal(applied.Arguments, &result); err != nil || !keaHashPattern.MatchString(result.Hash) {
		return nil, errors.New("Kea config-set returned no verifiable hash; application status unknown")
	}
	after, err := k.hash(ctx)
	if err != nil {
		return nil, fmt.Errorf("Kea config-set succeeded but readback failed: %w", err)
	}
	if after != strings.ToUpper(result.Hash) {
		return nil, errors.New("Kea config-set hash differs from active readback; application status unknown")
	}
	return &KeaApplyResult{BeforeHash: before, ActiveHash: after, CandidateSHA256: hex.EncodeToString(sum[:])}, nil
}
