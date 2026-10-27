package execution

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
	"github.com/enzoOvOyu/keel/internal/credentialvault"
	"github.com/enzoOvOyu/keel/internal/platform"
)

type bmcVerificationFixture struct {
	*agentKernelFixture
	asset      string
	identity   credentialvault.SecretIdentity
	vault      *credentialvault.Vault
	hits       atomic.Int32
	config     bmc.SuiteConfig
	configPath string
	master     string
}

// These are HTTPS protocol mocks, even when exercising the real-origin branch.
// They are never hardware PoC or production credential verification evidence.
func credentialVerificationFixture(t *testing.T, origin string) *bmcVerificationFixture {
	t.Helper()
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	v := &bmcVerificationFixture{agentKernelFixture: f, asset: asset}
	var batch, serial string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT credential_batch_ref,serial FROM bom_items WHERE item_uid=$1`, f.machine).Scan(&batch, &serial))
	dir := t.TempDir()
	master := filepath.Join(dir, "master")
	mustAgent(t, credentialvault.GenerateMaster(master))
	vault, e := credentialvault.Load(master)
	mustAgent(t, e)
	v.vault = vault
	v.identity = credentialvault.SecretIdentity{ProjectID: f.project, Ref: batch, Version: "v1", Scope: "bmc", ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
	_, e = f.store.ImportDeviceCredential(ctx, vault, v.identity, platform.DevicePassword{Username: "reader", Password: "fixture-password"}, "", "Verification fixture")
	mustAgent(t, e)
	fixture, e := bmc.NewSimReader(bmc.Target{RequestID: platform.RandomID(), ProjectID: f.project, AssetID: asset, MachineID: f.machine, AssetVersion: 1, Serial: serial, CredentialBatchRef: batch, CredentialVersion: "v1"})
	mustAgent(t, e)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v.hits.Add(1)
		user, password, ok := r.BasicAuth()
		if !ok || user != "reader" || password != "fixture-password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		doc, e := fixture.Read(r.Context(), r.URL.RequestURI())
		if e != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(doc.Body)
	}))
	t.Cleanup(server.Close)
	ca := filepath.Join(dir, "ca.pem")
	mustAgent(t, os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600))
	config := bmc.SuiteConfig{Schema: "keel.bmc-inventory/v1", TimeoutSeconds: 5, Targets: []bmc.TargetConfig{{ProjectID: f.project, MachineID: f.machine, Origin: origin, CredentialBatchRef: batch, Endpoint: server.URL, CAFile: ca}}}
	raw, _ := json.Marshal(config)
	path := filepath.Join(dir, "suite.json")
	mustAgent(t, os.WriteFile(path, raw, 0600))
	mustAgent(t, f.store.ConfigureBMCInventory(path, master))
	v.config = config
	v.configPath = path
	v.master = master
	return v
}
func (v *bmcVerificationFixture) start(t *testing.T) *platform.BMCCredentialVerification {
	t.Helper()
	ctx := context.Background()
	scope, e := v.store.PreviewBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref)
	mustAgent(t, e)
	if !scope.Ready || scope.TargetCount < 1 {
		t.Fatal("credential fixture scope not ready", scope)
	}
	out, e := v.service.CreateBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref, BMCCredentialVerificationInput{ExpectedCredentialVersion: scope.CredentialVersion, ExpectedScopeHash: scope.ScopeHash, Reason: "Full credential verification fixture"}, platform.RandomID(), "fixture")
	mustAgent(t, e)
	return out
}
func (v *bmcVerificationFixture) settle(t *testing.T, out *platform.BMCCredentialVerification) {
	t.Helper()
	for _, target := range out.Targets {
		claim := claimBMC(t, v.agentKernelFixture, target.ExecutionID)
		if claim == nil {
			t.Fatal("verification child was not admitted")
		}
		mustAgent(t, v.service.RunClaim(context.Background(), claim))
	}
	_, e := v.store.ReconcileBMCCredentialVerifications(context.Background(), 10)
	mustAgent(t, e)
}

// Authored for concentrated P2 testing; NOT_RUN during source development.
func TestBMCCredentialFullVerificationSeparatesSimulationPostgres(t *testing.T) {
	for _, origin := range []string{"sim", "real"} {
		t.Run(origin, func(t *testing.T) {
			v := credentialVerificationFixture(t, origin)
			ctx := context.Background()
			out := v.start(t)
			if len(out.Targets) != 1 || v.hits.Load() != 0 {
				t.Fatal("preview/admission dispatched IO or omitted target")
			}
			scope := out.Scope
			input := BMCCredentialVerificationInput{ExpectedCredentialVersion: scope.CredentialVersion, ExpectedScopeHash: scope.ScopeHash, Reason: "Full credential verification fixture"}
			if _, e := v.service.CreateBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref, input, platform.RandomID(), "fixture"); e == nil {
				t.Fatal("duplicate active scope admitted")
			}
			v.settle(t, out)
			detail, e := v.store.GetBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref, out.ID)
			mustAgent(t, e)
			want := "simulated"
			if origin == "real" {
				want = "passed"
			}
			if detail.Outcome != want || v.hits.Load() == 0 {
				t.Fatal("wrong batch outcome", detail.Outcome)
			}
			var verified bool
			mustAgent(t, v.store.DB.QueryRowContext(ctx, `SELECT keel_bmc_credential_verified($1,$2,'v1')`, v.project, v.identity.Ref).Scan(&verified))
			if verified != (origin == "real") {
				t.Fatal("simulation asserted real full-batch credentials")
			}
			if _, e = v.store.GetBMCCredentialVerification(ctx, v.actor, platform.RandomID(), v.identity.Ref, out.ID); e == nil {
				t.Fatal("cross-project verification visible")
			}
			if _, e = v.store.DB.ExecContext(ctx, `UPDATE bmc_credential_verification_results SET code='forged' WHERE verification_id=$1`, out.ID); e == nil {
				t.Fatal("verification result mutable")
			}
			if origin == "real" {
				_, e = v.store.DB.ExecContext(ctx, `UPDATE project_assets SET version=version+1 WHERE id=$1`, v.asset)
				mustAgent(t, e)
				mustAgent(t, v.store.DB.QueryRowContext(ctx, `SELECT keel_bmc_credential_verified($1,$2,'v1')`, v.project, v.identity.Ref).Scan(&verified))
				if verified {
					t.Fatal("historical proof survived current scope change")
				}
			}
		})
	}
}
func TestBMCCredentialVerificationFreezesHeadAndRequiresFullEvidencePostgres(t *testing.T) {
	v := credentialVerificationFixture(t, "sim")
	ctx := context.Background()
	out := v.start(t)
	if _, e := v.store.DB.ExecContext(ctx, `INSERT INTO bmc_credential_verification_results(id,verification_id,outcome,code,targets,finished_at) VALUES($1,$2,'passed','forged','[]',statement_timestamp())`, platform.RandomID(), out.ID); e == nil {
		t.Fatal("unfinished batch accepted forged pass")
	}
	rotated := v.identity
	rotated.Version = "v2"
	_, e := v.store.ImportDeviceCredential(ctx, v.vault, rotated, platform.DevicePassword{Username: "reader", Password: "rotated-fixture"}, "v1", "Rotate while verification in flight")
	mustAgent(t, e)
	for _, target := range out.Targets {
		claim := claimBMC(t, v.agentKernelFixture, target.ExecutionID)
		if claim == nil {
			t.Fatal("old exact credential execution disappeared")
		}
		mustAgent(t, v.service.RunClaim(ctx, claim))
	}
	_, e = v.store.ReconcileBMCCredentialVerifications(ctx, 10)
	mustAgent(t, e)
	detail, e := v.store.GetBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref, out.ID)
	mustAgent(t, e)
	if detail.Outcome != "stale" {
		t.Fatal("old credential batch verified new head", detail.Outcome)
	}
	var state string
	mustAgent(t, v.store.DB.QueryRowContext(ctx, `SELECT verification_state FROM device_credential_heads WHERE project_id=$1 AND credential_batch_ref=$2 AND scope='bmc'`, v.project, v.identity.Ref).Scan(&state))
	if state != "pending" {
		t.Fatal("rotation window ended without full new evidence")
	}
}
func TestBMCIdentityFixtureCannotVerifyCredentialPostgres(t *testing.T) {
	f, _ := bmcExecutionFixture(t)
	ctx := context.Background()
	var batch string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT credential_batch_ref FROM bom_items WHERE item_uid=$1`, f.machine).Scan(&batch))
	scope, e := f.store.PreviewBMCCredentialVerification(ctx, f.actor, f.project, batch)
	mustAgent(t, e)
	if scope.Ready || len(scope.Targets) != 1 || scope.Targets[0].Code != "BMC_CREDENTIAL_NOT_EXERCISED" {
		t.Fatal("identity simulation used as password proof", scope)
	}
}

func (v *bmcVerificationFixture) addMember(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	machine, asset := platform.RandomID(), platform.RandomID()
	serial := "SOFTWARE-BMC-SECOND-" + machine[:10]
	exec := func(q string, args ...any) {
		t.Helper()
		_, e := v.store.DB.ExecContext(ctx, q, args...)
		mustAgent(t, e)
	}
	exec(`INSERT INTO machines(id,serial_number,created_at,updated_at) VALUES($1,$2,now(),now())`, machine, serial)
	exec(`INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at) VALUES($1,$2,$3,$4,'provisioned',now(),now())`, asset, v.project, machine, "second-"+machine[:10])
	exec(`INSERT INTO bom_items(item_uid,category,serial,source_of_record,first_project_id,credential_batch_ref,created_at,updated_at) VALUES($1,'server',$2,'design_bom',$3,$4,now(),now())`, machine, serial, v.project, v.identity.Ref)
	exec(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) SELECT package_id,$1,2,project_id,jsonb_set(row_snapshot,'{values,serial_number}',to_jsonb($2::text)),now() FROM bom_item_revisions WHERE item_uid=$3 AND project_id=$4 ORDER BY recorded_at DESC LIMIT 1`, machine, serial, v.machine, v.project)
	config := v.config.Targets[0]
	config.MachineID = machine
	v.config.Targets = append(v.config.Targets, config)
	raw, _ := json.Marshal(v.config)
	mustAgent(t, os.WriteFile(v.configPath, raw, 0600))
	mustAgent(t, v.store.ConfigureBMCInventory(v.configPath, v.master))
}
func TestBMCCredentialVerificationNeverAcceptsSubsetPostgres(t *testing.T) {
	v := credentialVerificationFixture(t, "real")
	ctx := context.Background()
	before, e := v.store.PreviewBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref)
	mustAgent(t, e)
	v.addMember(t)
	if _, e = v.service.CreateBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref, BMCCredentialVerificationInput{ExpectedCredentialVersion: before.CredentialVersion, ExpectedScopeHash: before.ScopeHash, Reason: "Stale single-member preview"}, platform.RandomID(), "fixture"); e == nil {
		t.Fatal("admitted old subset after BOM addition")
	}
	out := v.start(t)
	if len(out.Targets) != 2 {
		t.Fatal("full scope did not include new BOM member")
	}
	// The shared protocol mock returns the first SN for both endpoints. One valid
	// device must not mask the second device's identity failure.
	v.settle(t, out)
	detail, e := v.store.GetBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref, out.ID)
	mustAgent(t, e)
	if detail.Outcome != "failed" {
		t.Fatal("one valid device verified the full batch", detail.Outcome)
	}
	var verified bool
	mustAgent(t, v.store.DB.QueryRowContext(ctx, `SELECT keel_bmc_credential_verified($1,$2,'v1')`, v.project, v.identity.Ref).Scan(&verified))
	if verified {
		t.Fatal("partial batch marked current credentials verified")
	}
}
func TestBMCCredentialUnknownRemainsPendingWithoutDeviceReplayPostgres(t *testing.T) {
	v := credentialVerificationFixture(t, "sim")
	ctx := context.Background()
	out := v.start(t)
	claim := claimBMC(t, v.agentKernelFixture, out.Targets[0].ExecutionID)
	if claim == nil {
		t.Fatal("claim missing")
	}
	request := out.Targets[0].CollectionID
	report, e := v.store.RunBMCCollection(ctx, request, claim.Attempt.ExecutionID, claim.Attempt.ID, claim.Attempt.CommandID, claim.Attempt.FencingToken)
	mustAgent(t, e)
	mustAgent(t, v.store.RecordBMCCollection(ctx, claim.Attempt.ExecutionID, claim.Attempt.FencingToken, report))
	recovered, e := v.service.recoverBMCReceipt(ctx, claim)
	mustAgent(t, e)
	if !recovered {
		t.Fatal("original receipt missing")
	}
	_, e = v.store.DB.ExecContext(ctx, `UPDATE exec_task_attempts SET state='unknown',lease_until=now()-interval '1 minute' WHERE id=$1`, claim.Attempt.ID)
	mustAgent(t, e)
	_, e = v.store.DB.ExecContext(ctx, `UPDATE exec_executions SET state='reconciling' WHERE id=$1`, claim.Attempt.ExecutionID)
	mustAgent(t, e)
	before := v.hits.Load()
	n, e := v.store.ReconcileBMCCredentialVerifications(ctx, 10)
	mustAgent(t, e)
	if n != 0 || v.hits.Load() != before {
		t.Fatal("unknown verification replayed device IO or finalized")
	}
	detail, e := v.store.GetBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref, out.ID)
	mustAgent(t, e)
	if detail.Outcome != "pending" {
		t.Fatal("unknown receipt counted as completed full-batch proof")
	}
}
func TestBMCCredentialVerificationLimitBlocksEntireScopePostgres(t *testing.T) {
	v := credentialVerificationFixture(t, "sim")
	ctx := context.Background()
	suffix := platform.RandomID()
	// Add 200 current BOM members to the existing one. The declared full count is
	// 201; admission must reject all, even though only one adapter is configured.
	_, e := v.store.DB.ExecContext(ctx, `WITH members AS(SELECT md5($1::text||':machine:'||n) id,'BMC-SCALE-'||$1::text||'-'||n serial FROM generate_series(1,200) n) INSERT INTO machines(id,serial_number,created_at,updated_at) SELECT id,serial,now(),now() FROM members`, suffix)
	mustAgent(t, e)
	_, e = v.store.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at) SELECT md5($1::text||':asset:'||n),$2,md5($1::text||':machine:'||n),'scale-'||n,'provisioned',now(),now() FROM generate_series(1,200) n`, suffix, v.project)
	mustAgent(t, e)
	_, e = v.store.DB.ExecContext(ctx, `INSERT INTO bom_items(item_uid,category,serial,source_of_record,first_project_id,credential_batch_ref,created_at,updated_at) SELECT md5($1::text||':machine:'||n),'server','BMC-SCALE-'||$1::text||'-'||n,'design_bom',$2,$3,now(),now() FROM generate_series(1,200) n`, suffix, v.project, v.identity.Ref)
	mustAgent(t, e)
	_, e = v.store.DB.ExecContext(ctx, `INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) SELECT template.package_id,md5($1::text||':machine:'||n),n+1,$2,jsonb_build_object('values',jsonb_build_object('serial_number','BMC-SCALE-'||$1::text||'-'||n,'credential_batch_ref',$3::text)),now() FROM generate_series(1,200) n CROSS JOIN LATERAL(SELECT package_id FROM bom_item_revisions WHERE item_uid=$4 AND project_id=$2 ORDER BY recorded_at DESC LIMIT 1) template`, suffix, v.project, v.identity.Ref, v.machine)
	mustAgent(t, e)
	scope, e := v.store.PreviewBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref)
	mustAgent(t, e)
	if scope.Ready || scope.TargetCount != 201 || scope.Code != "BMC_VERIFICATION_SCOPE_TOO_LARGE" {
		t.Fatal("oversize scope was truncated or admitted", scope.TargetCount, scope.Code)
	}
	_, e = v.service.CreateBMCCredentialVerification(ctx, v.actor, v.project, v.identity.Ref, BMCCredentialVerificationInput{ExpectedCredentialVersion: scope.CredentialVersion, ExpectedScopeHash: scope.ScopeHash, Reason: "Oversize must reject atomically"}, platform.RandomID(), "fixture")
	if e == nil {
		t.Fatal("oversize batch admitted")
	}
	var count int
	mustAgent(t, v.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM bmc_credential_verifications WHERE project_id=$1`, v.project).Scan(&count))
	if count != 0 || v.hits.Load() != 0 {
		t.Fatal("oversize refusal partially persisted or dispatched")
	}
}
