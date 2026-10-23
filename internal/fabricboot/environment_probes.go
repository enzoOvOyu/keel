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
	"io"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"
)

type ProbeTarget struct {
	URL             string `json:"url"`
	CAFile          string `json:"ca_file"`
	CertificateFile string `json:"certificate_file,omitempty"`
	KeyFile         string `json:"key_file,omitempty"`
	ThroughAgent    bool   `json:"through_agent,omitempty"`
	SHA256          string `json:"sha256,omitempty"`
}
type ProbeConfig struct {
	Version string                 `json:"version"`
	Targets map[string]ProbeTarget `json:"targets"`
}
type environmentProbe struct {
	target ProbeTarget
	client *http.Client
}
type ProbeSuite struct {
	Fingerprint string
	targets     map[string]environmentProbe
}
type ProbeResult struct {
	Service    string `json:"service"`
	State      string `json:"state"`
	Code       string `json:"code"`
	DurationMS int64  `json:"duration_ms"`
}

var probeNames = []string{"kea", "boot", "collector", "repository"}

// Endpoint configuration belongs to the server operator, never request input.
// All probes use explicit TLS trust, bounded reads/timeouts and no redirects.
func LoadProbeSuite(path string) (*ProbeSuite, error) {
	if path == "" {
		return &ProbeSuite{Fingerprint: ManagementPlanDigest("not-configured"), targets: map[string]environmentProbe{}}, nil
	}
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("probe configuration must be a regular private file")
	}
	raw, e := os.ReadFile(path)
	if e != nil || len(raw) > 65536 {
		return nil, errors.New("probe configuration unreadable or too large")
	}
	var cfg ProbeConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cfg) != nil || cfg.Version != "fabric-probes/v1" {
		return nil, errors.New("invalid probe configuration version or fields")
	}
	return NewProbeSuite(cfg)
}
func NewProbeSuite(cfg ProbeConfig) (*ProbeSuite, error) {
	if cfg.Version != "fabric-probes/v1" {
		return nil, errors.New("invalid probe protocol configuration version")
	}
	out := &ProbeSuite{targets: map[string]environmentProbe{}}
	fingerprints := map[string]any{"config": cfg}
	for name, target := range cfg.Targets {
		known := false
		for _, key := range probeNames {
			if key == name {
				known = true
			}
		}
		if !known {
			return nil, errors.New("unknown probe service")
		}
		u, e := url.Parse(target.URL)
		if e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return nil, errors.New("probe targets must be configured HTTPS URLs without credentials or query strings")
		}
		roots := x509.NewCertPool()
		pem, e := os.ReadFile(target.CAFile)
		if e != nil || !roots.AppendCertsFromPEM(pem) {
			return nil, errors.New("probe CA is invalid")
		}
		tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
		fingerprints[name+"_ca"] = ManagementPlanDigest(string(pem))
		if target.CertificateFile != "" || target.KeyFile != "" {
			cert, e := tls.LoadX509KeyPair(target.CertificateFile, target.KeyFile)
			if e != nil {
				return nil, errors.New("probe client certificate/key invalid")
			}
			tlsConfig.Certificates = []tls.Certificate{cert}
			fingerprints[name+"_certificate"] = ManagementPlanDigest(cert.Certificate)
		}
		if name == "kea" && len(tlsConfig.Certificates) == 0 {
			return nil, errors.New("Kea probe requires a client certificate")
		}
		if name == "repository" && !keaHashPattern.MatchString(target.SHA256) {
			return nil, errors.New("repository probe requires the approved canary artifact SHA256")
		}
		out.targets[name] = environmentProbe{target: target, client: &http.Client{Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{TLSClientConfig: tlsConfig, ResponseHeaderTimeout: 3 * time.Second}}}
	}
	out.Fingerprint = ManagementPlanDigest(fingerprints)
	return out, nil
}
func (s *ProbeSuite) Check(ctx context.Context, channel string) []ProbeResult {
	results := make([]ProbeResult, len(probeNames))
	var wg sync.WaitGroup
	for i, name := range probeNames {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			start := time.Now()
			result := ProbeResult{Service: name, State: "unknown", Code: "not_configured"}
			if s != nil {
				if probe, ok := s.targets[name]; ok {
					result.State, result.Code = probe.check(ctx, name, channel)
				}
			}
			result.DurationMS = time.Since(start).Milliseconds()
			results[i] = result
		}(i, name)
	}
	wg.Wait()
	return results
}
func (p environmentProbe) check(ctx context.Context, name, channel string) (string, string) {
	method := http.MethodGet
	var body io.Reader
	if name == "kea" {
		method = http.MethodPost
		command := map[string]any{"command": "config-hash-get"}
		if p.target.ThroughAgent {
			command["service"] = []string{"dhcp4"}
		}
		data, _ := json.Marshal(command)
		body = bytes.NewReader(data)
	}
	req, e := http.NewRequestWithContext(ctx, method, p.target.URL, body)
	if e != nil {
		return "failed", "request_invalid"
	}
	req.Header.Set("Accept", "application/json")
	if name == "kea" {
		req.Header.Set("Content-Type", "application/json")
	}
	response, e := p.client.Do(req)
	if e != nil {
		return "failed", "connection_failed"
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "failed", "http_not_ready"
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return "failed", "response_unreadable"
	}
	if name == "repository" {
		sum := sha256.Sum256(raw)
		if !bytes.EqualFold([]byte(hex.EncodeToString(sum[:])), []byte(p.target.SHA256)) {
			return "failed", "artifact_digest_mismatch"
		}
		return "ready", "artifact_verified"
	}
	if name == "kea" {
		var reply []keaReply
		if json.Unmarshal(raw, &reply) != nil || len(reply) != 1 || reply[0].Result == nil || *reply[0].Result != 0 {
			return "failed", "kea_protocol_rejected"
		}
		var hash struct {
			Hash string `json:"hash"`
		}
		if json.Unmarshal(reply[0].Arguments, &hash) != nil || !keaHashPattern.MatchString(hash.Hash) {
			return "failed", "kea_hash_missing"
		}
		return "ready", "kea_hash_verified"
	}
	var health struct {
		Service      string   `json:"service"`
		Status       string   `json:"status"`
		Capabilities []string `json:"capabilities"`
	}
	if json.Unmarshal(raw, &health) != nil || health.Status != "ready" {
		return "failed", "protocol_not_ready"
	}
	expected := "keel-fabric-collector"
	required := []string{"lldp", "interfaces", "ddm"}
	if name == "boot" {
		expected = "keel-ztp"
		required = []string{"ztp239"}
		if channel == "143" {
			required = []string{"sztp143"}
		}
	}
	if health.Service != expected {
		return "failed", "service_identity_mismatch"
	}
	for _, need := range required {
		found := false
		for _, got := range health.Capabilities {
			if got == need {
				found = true
			}
		}
		if !found {
			return "unknown", "capability_missing"
		}
	}
	return "ready", "protocol_verified"
}
