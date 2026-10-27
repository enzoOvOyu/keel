package execution

import (
	"context"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// Source coverage for the next centralized milestone. It uses the existing
// disposable PostgreSQL fixture, never device tools or a running service.
func TestTopologyReadsAndValidationCannotBootstrapG1Postgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	preview, e := f.store.PreviewTopology(ctx, f.actor, f.project, asset)
	mustAgent(t, e)
	if preview.Ready || preview.Code != "TOPOLOGY_G1_REQUIRED" || preview.G1ID != "" {
		t.Fatalf("missing G1 became ready: %+v", preview)
	}
	history, e := f.store.ListTopology(ctx, f.actor, f.project, asset, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	if history.Total != 0 || len(history.Items) != 0 {
		t.Fatal("GET generated a snapshot", history)
	}
	if _, e = f.store.ValidateTopology(ctx, f.actor, f.project, asset, platform.TopologyValidateInput{ScopeHash: preview.ScopeHash, Reason: "Verify missing prerequisite"}, "topology-missing", "test"); e == nil {
		t.Fatal("comparison bootstrapped G1")
	}
	if _, e = f.store.GetTopology(ctx, f.actor, f.project, asset, platform.RandomID()); e == nil {
		t.Fatal("unknown snapshot exposed evidence")
	}
	if _, e = f.store.PreviewTopology(ctx, f.actor, platform.RandomID(), asset); e == nil {
		t.Fatal("cross-project asset preview exposed scope")
	}
	if _, e = f.store.GetTopology(ctx, f.actor, f.project, platform.RandomID(), platform.RandomID()); e == nil {
		t.Fatal("foreign asset exposed snapshot")
	}
	var count int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM pci_topology_snapshots WHERE project_id=$1`, f.project).Scan(&count))
	if count != 0 {
		t.Fatal("blocked admission persisted topology")
	}
	gate, e := f.store.CurrentG1(ctx, f.actor, f.project, asset)
	mustAgent(t, e)
	if gate.EffectiveVerdict != "BLOCKED" || gate.Evidence != nil {
		t.Fatal("topology fabricated G1", gate)
	}
}
func TestTopologyInputRejectsUnscopedOrUnexplainedMutation(t *testing.T) {
	store := &platform.Store{}
	for _, in := range []platform.TopologyValidateInput{{}, {ScopeHash: strings.Repeat("a", 64), Reason: "x"}, {ScopeHash: "bad", Reason: "A real reason"}} {
		if _, e := store.ValidateTopology(context.Background(), nil, strings.Repeat("1", 32), strings.Repeat("2", 32), in, "key", "test"); e == nil {
			t.Fatal("invalid input accepted")
		}
	}
}

func TestTopologyFleetFiltersCurrentVerdictBeforePaginationPostgres(t *testing.T) {
	f, _ := bmcExecutionFixture(t)
	ctx := context.Background()
	page := platform.Pagination{Page: 1, PageSize: 1}
	rows, e := f.store.TopologyFleet(ctx, f.actor, f.project, "UNAVAILABLE", page)
	mustAgent(t, e)
	if rows.Total != 1 || len(rows.Items) != 1 || rows.Items[0].MachineID != f.machine || rows.Items[0].EffectiveVerdict != "UNAVAILABLE" {
		t.Fatalf("%+v", rows)
	}
	pass, e := f.store.TopologyFleet(ctx, f.actor, f.project, "PASS", page)
	mustAgent(t, e)
	if pass.Total != 0 || len(pass.Items) != 0 {
		t.Fatal("uncollected device passed filter")
	}
	page.Page = 2
	empty, e := f.store.TopologyFleet(ctx, f.actor, f.project, "UNAVAILABLE", page)
	mustAgent(t, e)
	if empty.Total != 1 || len(empty.Items) != 0 {
		t.Fatal("filter pagination changed total")
	}
	if _, e = f.store.TopologyFleet(ctx, f.actor, f.project, "forged", page); e == nil {
		t.Fatal("invalid verdict filter admitted")
	}
	other, _ := ordinaryTestActor(t, f.store)
	if _, e = f.store.TopologyFleet(ctx, other, f.project, "", page); e == nil {
		t.Fatal("project topology inventory leaked")
	}
}
