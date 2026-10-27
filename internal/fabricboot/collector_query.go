package fabricboot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

// A terminal observation certifies that the original command owns no live
// device session. Rejected is a durable tombstone: a delayed POST must never run.
// An HTTP 404 or an unknown state cannot provide that guarantee.
type CollectorRecollectObservation struct {
	Version        string                      `json:"version"`
	ProjectID      string                      `json:"project_id"`
	ExecutionID    string                      `json:"execution_id"`
	CommandID      string                      `json:"command_id"`
	FencingToken   int64                       `json:"fencing_token"`
	ScopeHash      string                      `json:"scope_hash"`
	RequestHash    string                      `json:"request_hash"`
	EvidenceSource string                      `json:"evidence_source"`
	State          string                      `json:"state"`
	FinishedAt     *time.Time                  `json:"finished_at"`
	Result         *CollectorRecollectResponse `json:"result"`
}

func (o CollectorRecollectObservation) Terminal() bool {
	return o.State == "completed" || o.State == "stopped" || o.State == "rejected"
}
func (o CollectorRecollectObservation) Validate(request CollectorRecollectRequest, now time.Time) error {
	if e := request.Validate(); e != nil {
		return e
	}
	if o.Version != "fabric-recollect-status/v1" || o.ProjectID != request.ProjectID || o.ExecutionID != request.ExecutionID || o.CommandID != request.CommandID || o.FencingToken != request.FencingToken || o.ScopeHash != request.ScopeHash || o.RequestHash != netbaseline.ContentHash(request) || o.EvidenceSource != request.EvidenceSource {
		return errors.New("collector query identity or original request mismatch")
	}
	if !o.Terminal() {
		if o.State != "running" && o.State != "unknown" {
			return errors.New("invalid collector query state")
		}
		if o.FinishedAt != nil || o.Result != nil {
			return errors.New("unfinished command returned terminal proof")
		}
		return nil
	}
	if o.FinishedAt == nil || o.FinishedAt.Before(request.NotBefore) || o.FinishedAt.After(now) {
		return errors.New("collector terminal time is invalid")
	}
	if o.State == "completed" {
		if o.Result == nil {
			return errors.New("completed command has no recollection result")
		}
		// Validate historical completion at its original time, without renewing the
		// five-minute sample lifetime at query time.
		return o.Result.Validate(request, *o.FinishedAt)
	}
	if o.Result != nil {
		return errors.New("stopped or rejected command returned observations")
	}
	return nil
}

// QueryRecollection never POSTs and never follows redirects. Endpoint and TLS
// identity remain server-owned. A missing or malformed proof keeps quarantine.
func (s *ProbeSuite) QueryRecollection(ctx context.Context, request CollectorRecollectRequest) (*CollectorRecollectObservation, error) {
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
	origin.Path = "/v1/projects/" + request.ProjectID + "/recollections/" + request.CommandID
	origin.RawPath = ""
	origin.Fragment = ""
	values := url.Values{"execution_id": {request.ExecutionID}, "fencing_token": {strconv.FormatInt(request.FencingToken, 10)}, "scope_hash": {request.ScopeHash}, "request_hash": {netbaseline.ContentHash(request)}}
	origin.RawQuery = values.Encode()
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, origin.String(), nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Accept", "application/json")
	client := *probe.client
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, e := client.Do(req)
	if e != nil {
		return nil, &RecollectionUnknown{Reason: "collector query failed; original command remains unknown"}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, &RecollectionUnknown{Reason: "collector query returned no authenticated terminal proof"}
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if e != nil || len(raw) > 1<<20 {
		return nil, &RecollectionUnknown{Reason: "collector query evidence unreadable or too large"}
	}
	var out CollectorRecollectObservation
	if json.Unmarshal(raw, &out) != nil || out.Validate(request, time.Now().UTC()) != nil {
		return nil, &RecollectionUnknown{Reason: "collector query proof failed validation"}
	}
	return &out, nil
}
