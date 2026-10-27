package fabricboot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

// Recollection has a separate contract from reading cached project snapshots.
// Only the execution owner may populate the command, fence and frozen scope.
type CollectorTarget struct {
	SwitchID string   `json:"switch_id"`
	Serial   string   `json:"serial"`
	Ports    []string `json:"ports"`
}
type CollectorRecollectRequest struct {
	EvidenceSource string            `json:"evidence_source"`
	Version        string            `json:"version"`
	ProjectID      string            `json:"project_id"`
	ExecutionID    string            `json:"execution_id"`
	CommandID      string            `json:"command_id"`
	FencingToken   int64             `json:"fencing_token"`
	ScopeHash      string            `json:"scope_hash"`
	NotBefore      time.Time         `json:"not_before"`
	Targets        []CollectorTarget `json:"targets"`
}
type CollectorRecollectResponse struct {
	Version        string               `json:"version"`
	ProjectID      string               `json:"project_id"`
	ExecutionID    string               `json:"execution_id"`
	CommandID      string               `json:"command_id"`
	FencingToken   int64                `json:"fencing_token"`
	ScopeHash      string               `json:"scope_hash"`
	EvidenceSource string               `json:"evidence_source"`
	Devices        []fabricmon.Snapshot `json:"devices"`
}

func (r CollectorRecollectRequest) Validate() error {
	if (r.EvidenceSource != "device" && r.EvidenceSource != "protocol_fixture") || r.Version != "fabric-recollect/v1" || !projectPattern.MatchString(r.ProjectID) || !projectPattern.MatchString(r.ExecutionID) || !projectPattern.MatchString(r.CommandID) || r.FencingToken < 1 || len(r.ScopeHash) != 64 || r.NotBefore.IsZero() || len(r.Targets) < 1 || len(r.Targets) > 2 {
		return errors.New("invalid frozen recollection request")
	}
	for _, c := range r.ScopeHash {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return errors.New("invalid recollection scope digest")
		}
	}
	ids, serials := map[string]bool{}, map[string]bool{}
	for _, target := range r.Targets {
		if !projectPattern.MatchString(target.SwitchID) || strings.TrimSpace(target.Serial) == "" || strings.ContainsAny(target.Serial, "\r\n\x00") || len(target.Serial) > 256 || ids[target.SwitchID] || serials[target.Serial] || len(target.Ports) < 1 || len(target.Ports) > 2 {
			return errors.New("invalid or duplicate recollection target")
		}
		ids[target.SwitchID], serials[target.Serial] = true, true
		ports := map[string]bool{}
		for _, port := range target.Ports {
			if strings.TrimSpace(port) == "" || strings.ContainsAny(port, "\r\n\x00") || len(port) > 128 || ports[port] {
				return errors.New("invalid or duplicate recollection port")
			}
			ports[port] = true
		}
	}
	return nil
}

// Validate rejects cached, extra, missing, cross-command and mixed-scope samples.
// Collectors return exactly the requested ports, not an unrestricted switch dump.
func (r CollectorRecollectResponse) Validate(request CollectorRecollectRequest, now time.Time) error {
	if e := request.Validate(); e != nil {
		return e
	}
	if r.Version != request.Version || r.ProjectID != request.ProjectID || r.ExecutionID != request.ExecutionID || r.CommandID != request.CommandID || r.FencingToken != request.FencingToken || r.ScopeHash != request.ScopeHash || r.EvidenceSource != request.EvidenceSource || len(r.Devices) != len(request.Targets) {
		return errors.New("recollection response identity or provenance mismatch")
	}
	allowed := map[string]CollectorTarget{}
	for _, target := range request.Targets {
		allowed[target.Serial] = target
	}
	for _, sample := range r.Devices {
		target, ok := allowed[sample.Serial]
		if !ok {
			return errors.New("unexpected or duplicate recollection device")
		}
		delete(allowed, sample.Serial)
		if sample.CollectedAt.Before(request.NotBefore) || sample.CollectedAt.After(now) || sample.Validate(sample.Serial, now) != nil || len(sample.Ports) != len(target.Ports) {
			return errors.New("recollection sample is stale, invalid or outside the frozen scope")
		}
		if len(sample.FDB) > 0 || len(sample.ARP) > 0 || len(sample.Logs) > 0 || sample.LogWindowStart != nil || len(sample.MetricPoints) > 0 || len(sample.AppliedLossless) > 0 || sample.BGPUp != nil || sample.PTPLocked != nil {
			return errors.New("unrequested switch-wide recollection data")
		}
		ports := map[string]bool{}
		for _, name := range target.Ports {
			ports[name] = true
		}
		for name := range sample.AppliedInterfaces {
			if !ports[name] {
				return errors.New("interface readback outside recollection scope")
			}
		}
		for _, port := range sample.Ports {
			if !ports[port.Name] {
				return errors.New("unexpected or duplicate recollection port")
			}
			delete(ports, port.Name)
		}
	}
	return nil
}
func (s *ProbeSuite) RecollectPorts(ctx context.Context, request CollectorRecollectRequest) (*CollectorRecollectResponse, error) {
	if e := request.Validate(); e != nil {
		return nil, e
	}
	if !s.CollectorConfigured() {
		return nil, errors.New("mTLS collector is not configured")
	}
	probe := s.targets["collector"]
	origin, e := url.Parse(probe.target.URL)
	if e != nil {
		return nil, e
	}
	origin.Path = "/v1/projects/" + request.ProjectID + "/recollections"
	origin.RawPath = ""
	origin.RawQuery = ""
	origin.Fragment = ""
	raw, e := json.Marshal(request)
	if e != nil {
		return nil, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, origin.String(), bytes.NewReader(raw))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Idempotency-Key", request.CommandID)
	client := *probe.client
	client.Timeout = 30 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, e := client.Do(req)
	if e != nil {
		return nil, errors.New("targeted recollection request failed; no proof of fresh observations")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("collector did not return completed recollection evidence")
	}
	raw, e = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return nil, errors.New("recollection evidence is unreadable or too large")
	}
	var out CollectorRecollectResponse
	if e = json.Unmarshal(raw, &out); e != nil {
		return nil, errors.New("invalid recollection evidence")
	}
	if e = out.Validate(request, time.Now().UTC()); e != nil {
		return nil, e
	}
	return &out, nil
}
