package fabricboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

func TestCollectorQueryPreservesOriginalCompletionAndRejectsForeignProof(t *testing.T) {
	request, result, now := recollectionFixture()
	observation := CollectorRecollectObservation{Version: "fabric-recollect-status/v1", ProjectID: request.ProjectID, ExecutionID: request.ExecutionID, CommandID: request.CommandID, FencingToken: request.FencingToken, ScopeHash: request.ScopeHash, RequestHash: netbaseline.ContentHash(request), EvidenceSource: request.EvidenceSource, State: "completed", FinishedAt: &now, Result: &result}
	// Recovery after ten minutes proves termination, not fresh evidence. The
	// original timestamp is preserved for the domain's expiry assessment.
	if e := observation.Validate(request, now.Add(10*time.Minute)); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		change func(*CollectorRecollectObservation)
	}{
		{"request hash", func(o *CollectorRecollectObservation) { o.RequestHash = "foreign" }},
		{"fence", func(o *CollectorRecollectObservation) { o.FencingToken++ }},
		{"missing result", func(o *CollectorRecollectObservation) { o.Result = nil }},
		{"missing terminal time", func(o *CollectorRecollectObservation) { o.FinishedAt = nil }},
		{"running with result", func(o *CollectorRecollectObservation) { o.State = "running" }},
		{"rejected with samples", func(o *CollectorRecollectObservation) { o.State = "rejected" }},
		{"invented terminal state", func(o *CollectorRecollectObservation) { o.State = "not_found" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := observation
			tc.change(&candidate)
			if candidate.Validate(request, now) == nil {
				t.Fatal("accepted invalid termination proof")
			}
		})
	}
	for _, state := range []string{"running", "unknown"} {
		candidate := observation
		candidate.State = state
		candidate.FinishedAt = nil
		candidate.Result = nil
		if candidate.Validate(request, now) != nil || candidate.Terminal() {
			t.Fatal("unresolved command treated as terminal")
		}
	}
}
func TestCollectorQueryUsesOnlyGETAndKeepsMissingProofUnknown(t *testing.T) {
	request, result, _ := recollectionFixture()
	request.NotBefore = time.Now().UTC().Add(-time.Second)
	result.Devices[0].CollectedAt = time.Now().UTC()
	finished := time.Now().UTC()
	observation := CollectorRecollectObservation{Version: "fabric-recollect-status/v1", ProjectID: request.ProjectID, ExecutionID: request.ExecutionID, CommandID: request.CommandID, FencingToken: request.FencingToken, ScopeHash: request.ScopeHash, RequestHash: netbaseline.ContentHash(request), EvidenceSource: request.EvidenceSource, State: "completed", FinishedAt: &finished, Result: &result}
	status := 200
	calls := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.URL.Path != "/v1/projects/"+request.ProjectID+"/recollections/"+request.CommandID || r.URL.Query().Get("request_hash") != observation.RequestHash {
			t.Error("query changed the frozen command or dispatched work")
		}
		w.WriteHeader(status)
		if status == 200 {
			_ = json.NewEncoder(w).Encode(observation)
		}
	}))
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.Certificates = server.TLS.Certificates
	client.Transport = transport
	suite := &ProbeSuite{targets: map[string]environmentProbe{"collector": {target: ProbeTarget{URL: server.URL + "/ignored"}, client: client}}}
	got, e := suite.QueryRecollection(context.Background(), request)
	if e != nil || !got.Terminal() {
		t.Fatal("missing bounded query result", e)
	}
	status = 404
	if _, e = suite.QueryRecollection(context.Background(), request); e == nil {
		t.Fatal("HTTP 404 falsely proved the original task never ran")
	}
	if calls != 2 {
		t.Fatalf("query retried unexpectedly: %d calls", calls)
	}
}
