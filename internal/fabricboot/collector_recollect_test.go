package fabricboot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

func recollectionFixture() (CollectorRecollectRequest, CollectorRecollectResponse, time.Time) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	request := CollectorRecollectRequest{EvidenceSource: "protocol_fixture", Version: "fabric-recollect/v1", ProjectID: strings.Repeat("a", 32), ExecutionID: strings.Repeat("b", 32), CommandID: strings.Repeat("c", 32), FencingToken: 7, ScopeHash: strings.Repeat("d", 64), NotBefore: now.Add(-time.Second), Targets: []CollectorTarget{{SwitchID: strings.Repeat("e", 32), Serial: "SW-A", Ports: []string{"swp1"}}}}
	response := CollectorRecollectResponse{Version: request.Version, ProjectID: request.ProjectID, ExecutionID: request.ExecutionID, CommandID: request.CommandID, FencingToken: request.FencingToken, ScopeHash: request.ScopeHash, EvidenceSource: "protocol_fixture", Devices: []fabricmon.Snapshot{{Version: "fabric-snapshot/v1", Source: "switch", Provider: "fixture", Serial: "SW-A", CollectedAt: now, Capabilities: map[string]bool{}, Ports: []fabricmon.Port{{Name: "swp1"}}}}}
	return request, response, now
}
func TestRecollectionRejectsReplayAndScopeExpansion(t *testing.T) {
	request, valid, now := recollectionFixture()
	if e := valid.Validate(request, now); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name   string
		change func(*CollectorRecollectResponse)
	}{
		{"command", func(v *CollectorRecollectResponse) { v.CommandID = strings.Repeat("f", 32) }},
		{"execution", func(v *CollectorRecollectResponse) { v.ExecutionID = strings.Repeat("f", 32) }},
		{"project", func(v *CollectorRecollectResponse) { v.ProjectID = strings.Repeat("f", 32) }},
		{"fence", func(v *CollectorRecollectResponse) { v.FencingToken++ }},
		{"scope", func(v *CollectorRecollectResponse) { v.ScopeHash = strings.Repeat("f", 64) }},
		{"origin", func(v *CollectorRecollectResponse) { v.EvidenceSource = "device" }},
		{"old cache", func(v *CollectorRecollectResponse) {
			v.Devices[0].CollectedAt = request.NotBefore.Add(-time.Nanosecond)
		}},
		{"future", func(v *CollectorRecollectResponse) { v.Devices[0].CollectedAt = now.Add(time.Nanosecond) }},
		{"missing", func(v *CollectorRecollectResponse) { v.Devices = nil }},
		{"extra device", func(v *CollectorRecollectResponse) { v.Devices = append(v.Devices, v.Devices[0]) }},
		{"wrong switch", func(v *CollectorRecollectResponse) { v.Devices[0].Serial = "SW-B" }},
		{"wrong port", func(v *CollectorRecollectResponse) { v.Devices[0].Ports[0].Name = "swp2" }},
		{"extra port", func(v *CollectorRecollectResponse) {
			v.Devices[0].Ports = append(v.Devices[0].Ports, fabricmon.Port{Name: "swp2"})
		}},
		{"missing port", func(v *CollectorRecollectResponse) { v.Devices[0].Ports = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, e := json.Marshal(valid)
			if e != nil {
				t.Fatal(e)
			}
			var candidate CollectorRecollectResponse
			if e = json.Unmarshal(raw, &candidate); e != nil {
				t.Fatal(e)
			}
			tc.change(&candidate)
			if candidate.Validate(request, now) == nil {
				t.Fatal("accepted unrelated or stale evidence")
			}
		})
	}
}
func TestRecollectionTransportUsesBoundedPOSTAndNeverFollowsRedirect(t *testing.T) {
	request, response, _ := recollectionFixture()
	request.NotBefore = time.Now().UTC().Add(-time.Second)
	response.Devices[0].CollectedAt = time.Now().UTC()
	var redirect bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/projects/"+request.ProjectID+"/recollections" {
			t.Error("collector request escaped fixed route")
			w.WriteHeader(400)
			return
		}
		if r.Method != "POST" || r.Header.Get("Idempotency-Key") != request.CommandID {
			t.Error("recollection did not use frozen command identity")
		}
		var got CollectorRecollectRequest
		if json.NewDecoder(r.Body).Decode(&got) != nil || got.ScopeHash != request.ScopeHash || got.FencingToken != request.FencingToken {
			t.Error("collector lost frozen scope")
		}
		if redirect {
			http.Redirect(w, r, "/unexpected", http.StatusTemporaryRedirect)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.Certificates = server.TLS.Certificates
	client.Transport = transport
	suite := &ProbeSuite{targets: map[string]environmentProbe{"collector": {target: ProbeTarget{URL: server.URL + "/health?ignored=yes"}, client: client}}}
	if _, e := suite.RecollectPorts(context.Background(), request); e != nil {
		t.Fatal(e)
	}
	redirect = true
	if _, e := suite.RecollectPorts(context.Background(), request); e == nil {
		t.Fatal("redirect accepted as fresh evidence")
	}
	var disabled *ProbeSuite
	if _, e := disabled.RecollectPorts(context.Background(), request); e == nil {
		t.Fatal("unconfigured collector admitted")
	}
}
