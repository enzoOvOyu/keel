package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/wiring"
)

// Authored for concentrated P2 verification. No device connection or helper.
func TestWiringLedgerCannotCreateACommandFromMissingPrerequisitesPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.store.WiringEnabled = true
	binding := wiring.ReadbackBinding{ProjectID: f.project, ExecutionID: platform.RandomID(), AttemptID: platform.RandomID(), CommandID: platform.RandomID(), FencingToken: 1, Origin: "protocol_fixture"}
	tx, e := f.store.DB.BeginTx(ctx, nil)
	mustAgent(t, e)
	if _, e = f.store.RegisterWiringCommandTx(ctx, tx, platform.RandomID(), platform.RandomID(), platform.RandomID(), "sim", binding); e == nil {
		t.Fatal("missing Step 1/lease/execution admitted LLDP")
	}
	tx.Rollback()
	ledger := &platform.WiringReadbackLedger{Store: f.store}
	request := fabricboot.CollectorRecollectRequest{Version: "fabric-recollect/v1", ProjectID: f.project, ExecutionID: binding.ExecutionID, CommandID: platform.RandomID(), FencingToken: 1, ScopeHash: strings.Repeat("a", 64), NotBefore: time.Now().UTC(), EvidenceSource: "protocol_fixture", Targets: []fabricboot.CollectorTarget{{SwitchID: platform.RandomID(), Serial: "unbound-switch", Ports: []string{"swp1"}}}}
	if _, e = ledger.Begin(ctx, binding, request); e == nil {
		t.Fatal("unregistered command produced permission to send")
	}
	if _, e = f.store.RecordWiringCapture(ctx, binding, []byte(`{}`)); e == nil {
		t.Fatal("unbound raw upload became a capture")
	}
	var commands, dispatches, captures int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM validation_wiring_commands WHERE project_id=$1`, f.project).Scan(&commands))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM validation_wiring_readback_dispatches`).Scan(&dispatches))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM validation_wiring_capture_records`).Scan(&captures))
	if commands != 0 || dispatches != 0 || captures != 0 {
		t.Fatal("rejected operations retained fabricated evidence")
	}
}
