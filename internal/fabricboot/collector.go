package fabricboot

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ReadCollectorBatch pulls the current normalized observations from the fixed
// operator endpoint. It does not accept browser telemetry or run a second queue.
// Raw bodies may contain unexpected fields; callers persist only typed data.
func (s *ProbeSuite) ReadCollectorBatch(ctx context.Context, projectID string) ([]byte, error) {
	if s == nil || !projectPattern.MatchString(projectID) {
		return nil, errors.New("collector is not configured")
	}
	probe, ok := s.targets["collector"]
	if !ok {
		return nil, errors.New("collector is not configured")
	}
	transport, ok := probe.client.Transport.(*http.Transport)
	if !ok || transport.TLSClientConfig == nil || len(transport.TLSClientConfig.Certificates) == 0 {
		return nil, errors.New("collector data access requires a client certificate")
	}
	origin, e := url.Parse(probe.target.URL)
	if e != nil {
		return nil, e
	}
	origin.Path = "/v1/projects/" + projectID + "/snapshots"
	origin.RawPath = ""
	origin.RawQuery = ""
	client := *probe.client
	client.Timeout = 10 * time.Second
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, origin.String(), nil)
	if e != nil {
		return nil, e
	}
	request.Header.Set("Accept", "application/json")
	response, e := client.Do(request)
	if e != nil {
		return nil, errors.New("collector data request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, errors.New("collector data service is not ready")
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	if e != nil || len(raw) > 8<<20 {
		return nil, errors.New("collector payload unreadable or too large")
	}
	return raw, nil
}
func (s *ProbeSuite) CollectorConfigured() bool {
	if s == nil {
		return false
	}
	p, ok := s.targets["collector"]
	if !ok {
		return false
	}
	t, ok := p.client.Transport.(*http.Transport)
	return ok && t.TLSClientConfig != nil && len(t.TLSClientConfig.Certificates) > 0
}

// CheckCollector reuses the fixed authenticated health protocol. G2 must not
// depend on boot/Kea/repository probes from the earlier N-S3 onboarding phase.
func (s *ProbeSuite) CheckCollector(ctx context.Context) ProbeResult {
	out := ProbeResult{Service: "collector", State: "unknown", Code: "not_configured"}
	start := time.Now()
	if s.CollectorConfigured() {
		out.State, out.Code = s.targets["collector"].check(ctx, "collector", "")
	}
	out.DurationMS = time.Since(start).Milliseconds()
	return out
}
