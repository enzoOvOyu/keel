package execution

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// Isolated control-plane proof: no hardware capture or gate success is invented.
func TestGateDashboardAndWaiverCannotBootstrapEvidencePostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	dashboard, e := f.store.GateDashboard(ctx, f.actor, f.project)
	mustAgent(t, e)
	if dashboard.KPI.Assets != 1 || dashboard.KPI.NetworkNormal != 0 || dashboard.KPI.NetworkUnknown != 1 || len(dashboard.Items) != 1 || len(dashboard.Items[0].Cards) != 6 {
		t.Fatalf("missing evidence became healthy: %+v", dashboard)
	}
	for _, card := range dashboard.Items[0].Cards {
		if card.EffectiveVerdict != "BLOCKED" || card.EvidenceID != "" || card.WaiverID != "" {
			t.Fatalf("fabricated gate: %+v", card)
		}
	}
	for _, card := range dashboard.Items[0].Cards[3:] {
		if card.Reason != "P3_GATE_DOMAIN_NOT_IMPLEMENTED" {
			t.Fatalf("future domain hidden: %+v", card)
		}
	}
	scope, e := f.store.GateWaiverScope(ctx, f.actor, f.project, asset)
	mustAgent(t, e)
	if scope.CanApprove || scope.EvidenceID != "" {
		t.Fatal("no-evidence scope eligible", scope)
	}
	if _, e = f.store.ApproveGateWaiver(ctx, f.actor, f.project, asset, platform.GateWaiverInput{SourceHash: strings.Repeat("a", 64), Reason: "approve only observed fields", RemediationPlan: "recover original collection", ValidUntil: time.Now().Add(time.Minute)}, "gate-no-evidence", "test"); e == nil {
		t.Fatal("waiver created evidence")
	}
	history, e := f.store.ListGateWaivers(ctx, f.actor, f.project, asset, platform.Pagination{Page: 1, PageSize: 10})
	mustAgent(t, e)
	if history.Total != 0 || len(history.Items) != 0 {
		t.Fatal("rejected approval wrote history")
	}
	if _, e = f.store.GateWaiverScope(ctx, f.actor, platform.RandomID(), asset); e == nil {
		t.Fatal("foreign project exposed scope")
	}
	if e = f.store.RevokeGateWaiver(ctx, f.actor, f.project, asset, platform.RandomID(), "revocation has no original waiver", "test"); e == nil {
		t.Fatal("revoked nonexistent proof")
	}
	var waiver sql.NullString
	var accepted bool
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT keel_current_g1_waiver($1),keel_g1_evidence_accepted($1,$2,now()+interval '1 minute')`, platform.RandomID(), platform.RandomID()).Scan(&waiver, &accepted))
	if waiver.Valid || accepted {
		t.Fatal("SQL accepted fabricated evidence")
	}
	// An explicit project tombstone must suppress a global allowlist. Updating
	// any policy changes its fingerprint; no old waiver may reuse it.
	global, override := platform.RandomID(), platform.RandomID()
	_, e = f.store.DB.ExecContext(ctx, `INSERT INTO param_keys(id,scope_key,key,value_type,default_value,created_by,created_at,updated_at)VALUES($1,'global','gate.g1.waiver_fields','string','["nic.dom"]',$2,now(),now()) ON CONFLICT(scope_key,key)DO UPDATE SET default_value=EXCLUDED.default_value,state='active',updated_at=clock_timestamp()`, global, f.actor.User.ID)
	mustAgent(t, e)
	var id, hash string
	var fields []byte
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT policy_id,policy_hash,fields FROM keel_g1_waiver_policy($1)`, f.project).Scan(&id, &hash, &fields))
	if id == "" || hash == "" || string(fields) != `["nic.dom"]` {
		t.Fatal("global policy unavailable", id, string(fields))
	}
	_, e = f.store.DB.ExecContext(ctx, `INSERT INTO param_keys(id,scope_key,key,value_type,default_value,state,created_by,created_at,updated_at)VALUES($1,'project:'||$2,'gate.g1.waiver_fields','string','[]','retired',$3,now(),now())`, override, f.project, f.actor.User.ID)
	mustAgent(t, e)
	var count int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM keel_g1_waiver_policy($1)`, f.project).Scan(&count))
	if count != 0 {
		t.Fatal("retired project policy fell back to global")
	}
	_, e = f.store.DB.ExecContext(ctx, `UPDATE param_keys SET state='active',default_value='["nic.driver_version"]',updated_at=clock_timestamp() WHERE id=$1`, override)
	mustAgent(t, e)
	var overrideHash string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT policy_id,policy_hash,fields FROM keel_g1_waiver_policy($1)`, f.project).Scan(&id, &overrideHash, &fields))
	if id != override || hash == overrideHash || string(fields) != `["nic.driver_version"]` {
		t.Fatal("project policy not bound to version")
	}
	// The safe configured policy still cannot replace an absent original proof.
	scope, e = f.store.GateWaiverScope(ctx, f.actor, f.project, asset)
	mustAgent(t, e)
	if scope.CanApprove {
		t.Fatal("policy bypassed original proof")
	}
}
