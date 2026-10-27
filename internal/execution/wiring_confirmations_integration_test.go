package execution

import (
	"context"
	"github.com/enzoOvOyu/keel/internal/platform"
	"testing"
)

// Concentrated P2 verification source; no physical helper or device connection.
func TestWiringDLConfirmationCannotManufactureMissingStepThreePostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.store.WiringEnabled = true
	in := platform.WiringConfirmationInput{AssetID: platform.RandomID(), Step3ID: platform.RandomID(), Reason: "Attempt to confirm without original stopped evidence"}
	if _, e := f.store.ConfirmWiring(ctx, f.actor, f.project, platform.RandomID(), in, "missing-step-three", "test"); e == nil {
		t.Fatal("DL confirmation accepted a missing or unowned report")
	}
	var n int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM validation_wiring_confirmations WHERE project_id=$1`, f.project).Scan(&n))
	if n != 0 {
		t.Fatal("rejected confirmation left G2 evidence")
	}
	other, _ := ordinaryTestActor(t, f.store)
	if _, e := f.store.GetWiringConfirmation(ctx, other, f.project, platform.RandomID()); e == nil {
		t.Fatal("unrelated actor could read scoped G2")
	}
}
func TestWiringRetestRequiresOriginalDiscoveryAndRepairPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.store.WiringEnabled = true
	in := platform.WiringRetestInput{Version: 1, ReportID: platform.RandomID(), Reason: "Attempt verification without a trusted wiring repair"}
	if _, e := f.store.CreateWiringRetest(ctx, f.actor, platform.RandomID(), in, "missing-wiring-repair", "test"); e == nil {
		t.Fatal("unbound repair manufactured a successful retest")
	}
	var n int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM validation_wiring_issue_retests WHERE project_id=$1`, f.project).Scan(&n))
	if n != 0 {
		t.Fatal("rejected retest wrote verification evidence")
	}
}
