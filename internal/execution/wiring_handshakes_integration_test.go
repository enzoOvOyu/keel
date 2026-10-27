package execution

import (
	"context"
	"github.com/enzoOvOyu/keel/internal/platform"
	"testing"
)

// Authored for the next concentrated P2 run; no live collector or hardware.
func TestWiringHandshakeCannotBootstrapG1OrLeakAcrossProjectsPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	var asset string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT id FROM project_assets WHERE project_id=$1 AND machine_id=$2`, f.project, f.machine).Scan(&asset))
	targets := []platform.WiringTargetInput{{AssetID: asset, PhysicalPortIDs: []string{platform.RandomID()}}}
	facts, e := f.store.PreviewWiring(ctx, f.actor, f.project, targets)
	mustAgent(t, e)
	if facts.Ready || len(facts.Blockers) == 0 {
		t.Fatal("missing real G1/topology acquired readiness")
	}
	if _, e = f.store.CreateWiringHandshake(ctx, f.actor, f.project, platform.WiringHandshakeInput{Targets: targets, Reason: "Attempt without current G1"}, "missing-g1", "test"); e == nil {
		t.Fatal("handshake bypassed prerequisites")
	}
	rows, e := f.store.ListWiringHandshakes(ctx, f.actor, f.project, platform.Pagination{Page: 1, PageSize: 10})
	mustAgent(t, e)
	if rows.Total != 0 {
		t.Fatal("read/rejected handshake minted evidence")
	}
	other, _ := ordinaryTestActor(t, f.store)
	if _, e = f.store.PreviewWiring(ctx, other, f.project, targets); e == nil {
		t.Fatal("target scope leaked to nonmember")
	}
	if _, e = f.store.ListWiringHandshakes(ctx, other, f.project, platform.Pagination{Page: 1, PageSize: 10}); e == nil {
		t.Fatal("history leaked to nonmember")
	}
	if _, e = f.store.GetWiringHandshake(ctx, other, f.project, platform.RandomID()); e == nil {
		t.Fatal("detail leaked to nonmember")
	}
}
