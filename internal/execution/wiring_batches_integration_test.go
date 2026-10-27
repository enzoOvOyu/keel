package execution

import (
	"context"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestWiringStep1CannotManufactureAHandshakeOrLeakBatchPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	if _, e := f.store.CreateWiringBatch(ctx, f.actor, f.project, platform.WiringBatchInput{HandshakeID: platform.RandomID(), FactsHash: strings.Repeat("a", 64), Reason: "Try missing readiness evidence"}, "missing-handshake", "test"); e == nil {
		t.Fatal("batch admitted without real current handshake")
	}
	var count int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM validation_wiring_batches WHERE project_id=$1`, f.project).Scan(&count))
	if count != 0 {
		t.Fatal("rejected batch wrote evidence")
	}
	other, _ := ordinaryTestActor(t, f.store)
	if _, e := f.store.GetWiringBatch(ctx, other, f.project, platform.RandomID()); e == nil {
		t.Fatal("batch details leaked to nonmember")
	}
	if _, e := f.store.CreateWiringBatch(ctx, other, f.project, platform.WiringBatchInput{HandshakeID: platform.RandomID(), FactsHash: strings.Repeat("a", 64), Reason: "Unrelated actor request"}, "nonmember", "test"); e == nil {
		t.Fatal("nonmember could consume a project handshake")
	}
}
