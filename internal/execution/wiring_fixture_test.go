package execution

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/wiring"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWiringFixtureFactoryRetainsOriginalSessionAndNeverRestartsQuery(t *testing.T) {
	raw, e := os.ReadFile("../../sim/fixtures/lldp/dual-ended.json")
	if e != nil {
		t.Fatal(e)
	}
	expectedRaw, e := os.ReadFile("../../sim/fixtures/lldp/expectation.json")
	if e != nil {
		t.Fatal(e)
	}
	var expected wiring.Expectation
	if e = json.Unmarshal(expectedRaw, &expected); e != nil {
		t.Fatal(e)
	}
	commandID := strings.Repeat("b", 32)
	plan, e := wiring.BuildPlan(expected, commandID)
	if e != nil {
		t.Fatal(e)
	}
	c := &platform.WiringCommand{HostOrigin: "sim", Expectation: expected, Plan: *plan, Binding: wiring.ReadbackBinding{ProjectID: expected.ProjectID, ExecutionID: strings.Repeat("c", 32), AttemptID: strings.Repeat("d", 32), CommandID: commandID, FencingToken: 1, Origin: "protocol_fixture"}}
	factory, e := NewFixtureWiringAdapters(raw)
	if e != nil {
		t.Fatal(e)
	}
	d, client, e := factory.Open(c)
	if e != nil {
		t.Fatal(e)
	}
	again, againClient, e := factory.Open(c)
	if e != nil || d != again || client != againClient || d.Origin() != "sim" {
		t.Fatal("original synthetic command session was replaced")
	}
	changed := *c
	changed.Binding.FencingToken++
	if _, _, e = factory.Open(&changed); e == nil {
		t.Fatal("different fence reused an original session")
	}
	requests, e := wiring.BuildReadbackRequests(c.Binding, wiring.ReadbackScope{PlanDigest: plan.Digest, CommandID: commandID, ScopeHash: expected.ScopeHash, AdvertisingAt: time.Now().UTC(), Ports: expected.Ports})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = client.QueryRecollection(context.Background(), requests[0]); !errors.Is(e, wiring.ErrOutcomeUnknown) {
		t.Fatal("GET manufactured an undispatched receipt")
	}
	response, e := client.RecollectPorts(context.Background(), requests[0])
	if e != nil || response.EvidenceSource != "protocol_fixture" {
		t.Fatalf("synthetic response: %v", e)
	}
	proof, e := client.QueryRecollection(context.Background(), requests[0])
	if e != nil || proof.State != "completed" {
		t.Fatalf("original completion unavailable: %v", e)
	}
	restarted, e := NewFixtureWiringAdapters(raw)
	if e != nil {
		t.Fatal(e)
	}
	_, newClient, e := restarted.Open(c)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = newClient.QueryRecollection(context.Background(), requests[0]); !errors.Is(e, wiring.ErrOutcomeUnknown) {
		t.Fatal("restart regenerated a volatile completion")
	}
}
