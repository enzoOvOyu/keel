package execution

import (
	"context"
	"github.com/enzoOvOyu/keel/internal/platform"
	"strings"
	"testing"
)

func TestWiringExecutionRejectsMissingPrerequisitesAtomicallyPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.store.WiringEnabled = true
	service := New(f.store.DB)
	service.WiringStore = f.store
	input := platform.WiringRunInput{BatchID: platform.RandomID(), HandshakeID: platform.RandomID(), FactsHash: strings.Repeat("a", 64), Reason: "Attempt LLDP without an independent Step 1"}
	var before, after int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_executions WHERE project_id=$1`, f.project).Scan(&before))
	if _, e := service.CreateWiringExecution(ctx, f.actor, f.project, input, "missing-wiring-scope", "test"); e == nil {
		t.Fatal("missing batch/handshake admitted wiring")
	}
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_executions WHERE project_id=$1`, f.project).Scan(&after))
	if before != after {
		t.Fatal("rejected wiring admission left a partial execution")
	}
	var runs int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM validation_wiring_runs WHERE project_id=$1`, f.project).Scan(&runs))
	if runs != 0 {
		t.Fatal("rejected admission wrote a domain request")
	}
}
