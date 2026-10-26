package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

const controlKVSample = `api_version: keel.control/v1
inputs: {}
tasks:
  - id: put
    type: kv.put
    retry: 0
    idempotent: false
    timeout_ms: 2000
    resources: [project]
    kv: {key: delivery.note, value: "Ready", expected_version: 0}
  - id: get
    type: kv.get
    depends_on: [put]
    retry: 0
    idempotent: true
    timeout_ms: 2000
    resources: [project]
    kv: {key: delivery.note}
`

func TestControlKVMediaFreeDefinition(t *testing.T) {
	v, err := ValidateFlow(FlowValidationInput{YAMLSource: controlKVSample})
	if err != nil {
		t.Fatal(err)
	}
	if len(v.TaskOrder) != 2 || v.TaskOrder[0] != "put" || v.Definition.Tasks[0].MediaID != "" {
		t.Fatal("invalid media-free plan", v)
	}
	for _, source := range []string{
		strings.Replace(controlKVSample, "keel.control/v1", "keel.sim/v1", 1),
		strings.Replace(controlKVSample, "keel.control/v1", "keel.host/v1", 1),
		strings.Replace(controlKVSample, "expected_version: 0", "expected_version: -1", 1),
		strings.Replace(controlKVSample, ", expected_version: 0", "", 1),
		strings.Replace(controlKVSample, "retry: 0", "retry: 1", 1),
		strings.Replace(controlKVSample, "idempotent: false", "idempotent: true", 1),
		strings.Replace(controlKVSample, "delivery.note", "credentials.password", 1),
		strings.Replace(controlKVSample, "resources: [project]", "resources: [device]", 1),
		strings.Replace(controlKVSample, "type: kv.get", "type: host.identity", 1),
		strings.Replace(controlKVSample, "kv: {key: delivery.note}", "kv: {key: delivery.note, value: unexpected}", 1),
	} {
		if _, err := ValidateFlow(FlowValidationInput{YAMLSource: source}); err == nil {
			t.Fatal("unsafe control flow accepted", source)
		}
	}
}

func publishControlKV(t *testing.T, f *agentKernelFixture, source string) *FlowVersion {
	t.Helper()
	v, err := f.service.Publish(context.Background(), f.actor, f.project, "", PublishInput{Name: "KV " + platform.RandomID()[:8], YAMLSource: source}, platform.RandomID(), "kv")
	mustAgent(t, err)
	return v
}
func claimControlKV(t *testing.T, f *agentKernelFixture, id string) *Claim {
	t.Helper()
	ctx := context.Background()
	_, err := f.service.PumpOutbox(ctx, "kv-source-fixture", 1000)
	mustAgent(t, err)
	c, err := f.service.claimExecution(ctx, id, "kv-source-fixture", 10*time.Second)
	mustAgent(t, err)
	return c
}
func TestControlKVAtomicReceiptCASAndProjectIsolationPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	// The fixture supplies the existing isolated DB/account; no target runner is
	// available or used for control workflows.
	f.service.AgentRunner = nil
	f.service.SSHRunner = nil
	flow := publishControlKV(t, f, controlKVSample)
	in := ControlFlowCreateInput{FlowVersionID: flow.ID}
	key := platform.RandomID()
	x, err := f.service.CreateControlFlow(ctx, f.actor, f.project, in, key, "kv")
	mustAgent(t, err)
	again, err := f.service.CreateControlFlow(ctx, f.actor, f.project, in, key, "kv")
	mustAgent(t, err)
	if again.ID != x.ID || x.Mode != "control" || len(x.Snapshot.Catalog) != 0 {
		t.Fatal("control admission/idempotency corrupted")
	}
	if _, err = f.service.Create(ctx, f.actor, f.project, CreateInput{Mode: "sim", FlowVersionID: flow.ID, TargetIDs: []string{f.machine}}, platform.RandomID(), "kv"); err == nil {
		t.Fatal("control admitted as simulation")
	}
	put := claimControlKV(t, f, x.ID)
	if put == nil || put.Task.Type != "kv.put" {
		t.Fatal("put not dispatched")
	}
	if claimControlKV(t, f, x.ID) != nil {
		t.Fatal("get ran before put receipt")
	}
	mustAgent(t, f.service.RunClaim(ctx, put))
	// Lost caller response may repeat the original claim, never the mutation.
	mustAgent(t, f.service.RunClaim(ctx, put))
	get := claimControlKV(t, f, x.ID)
	if get == nil || get.Task.Type != "kv.get" {
		t.Fatal("get dependency blocked")
	}
	mustAgent(t, f.service.RunClaim(ctx, get))
	var version int64
	var value string
	var receipts int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT value,version FROM exec_project_kv WHERE project_id=$1 AND key='delivery.note'`, f.project).Scan(&value, &version))
	if version != 1 || value != "Ready" {
		t.Fatal("duplicate put changed version", version, value)
	}
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_runner_receipts WHERE command_id IN($1,$2)`, put.Attempt.CommandID, get.Attempt.CommandID).Scan(&receipts))
	if receipts != 2 {
		t.Fatal("KV changes escaped matching receipts")
	}
	detail, err := f.service.Get(ctx, f.project, x.ID)
	mustAgent(t, err)
	if detail.Execution.State != "success" {
		t.Fatal("control DAG not complete")
	}
	// Same frozen create-only put in a NEW execution must fail, without changing
	// the existing value/version or running a dependent read.
	clash, err := f.service.CreateControlFlow(ctx, f.actor, f.project, in, platform.RandomID(), "kv")
	mustAgent(t, err)
	mustAgent(t, f.service.RunClaim(ctx, claimControlKV(t, f, clash.ID)))
	detail, err = f.service.Get(ctx, f.project, clash.ID)
	mustAgent(t, err)
	if detail.Execution.State != "failed" {
		t.Fatal("CAS conflict did not fail", detail)
	}
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT version FROM exec_project_kv WHERE project_id=$1 AND key='delivery.note'`, f.project).Scan(&version))
	if version != 1 {
		t.Fatal("CAS failure overwrote original value")
	}
	other, err := f.store.CreateProject(ctx, f.actor, platform.CreateProjectInput{Name: "Other KV", Slug: "other-kv-" + platform.RandomID()[:8]}, platform.RandomID(), "kv")
	mustAgent(t, err)
	if _, err = f.service.CreateControlFlow(ctx, f.actor, other.ID, in, platform.RandomID(), "kv"); err == nil {
		t.Fatal("foreign revision admitted")
	}
	var foreign int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_project_kv WHERE project_id=$1`, other.ID).Scan(&foreign))
	if foreign != 0 {
		t.Fatal("KV leaked to another project")
	}
	readFlow, err := f.service.Publish(ctx, f.actor, other.ID, "", PublishInput{Name: "Foreign KV read", YAMLSource: `api_version: keel.control/v1
inputs: {}
tasks:
  - id: inspect
    type: kv.get
    retry: 0
    idempotent: true
    timeout_ms: 2000
    resources: [project]
    kv: {key: delivery.note}
`}, platform.RandomID(), "kv")
	mustAgent(t, err)
	read, err := f.service.CreateControlFlow(ctx, f.actor, other.ID, ControlFlowCreateInput{FlowVersionID: readFlow.ID}, platform.RandomID(), "kv")
	mustAgent(t, err)
	mustAgent(t, f.service.RunClaim(ctx, claimControlKV(t, f, read.ID)))
	readDetail, err := f.service.Get(ctx, other.ID, read.ID)
	mustAgent(t, err)
	if readDetail.Execution.State != "failed" || readDetail.Attempts[0].Receipt.Output["reason"] != "kv_key_not_found" {
		t.Fatal("read leaked another project's key")
	}
}

func TestControlKVExpiredClaimHasNonCommitProofPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	flow := publishControlKV(t, f, controlKVSample)
	x, err := f.service.CreateControlFlow(ctx, f.actor, f.project, ControlFlowCreateInput{FlowVersionID: flow.ID}, platform.RandomID(), "kv")
	mustAgent(t, err)
	c := claimControlKV(t, f, x.ID)
	_, err = f.store.DB.ExecContext(ctx, `UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1`, c.Attempt.ID)
	mustAgent(t, err)
	if f.service.RunClaim(ctx, c) == nil {
		t.Fatal("expired command changed KV")
	}
	_, err = f.service.ReapExpired(ctx)
	mustAgent(t, err)
	detail, err := f.service.Get(ctx, f.project, x.ID)
	mustAgent(t, err)
	var unknown Attempt
	for _, a := range detail.Attempts {
		if a.ID == c.Attempt.ID {
			unknown = a
		}
	}
	if unknown.State != "unknown" || unknown.Receipt == nil || unknown.Receipt.Output["reason"] != "kv_transaction_not_committed" {
		t.Fatal("missing transactional non-commit proof", unknown)
	}
	var keys int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_project_kv WHERE project_id=$1`, f.project).Scan(&keys))
	if keys != 0 {
		t.Fatal("expired uncommitted command left KV effects")
	}
	_, err = f.service.Reconcile(ctx, f.actor, f.project, x.ID, c.Attempt.ID, ReconcileInput{Result: "failed", ExpectedVersion: unknown.StateVersion, Evidence: "reviewed the original atomic non-commit receipt"}, platform.RandomID(), "kv")
	mustAgent(t, err)
	if f.service.RunClaim(ctx, c) != nil { // A terminal original claim is query-only.
		t.Fatal("original terminal control command cannot be safely observed")
	}
}

func TestControlKVPauseKillAndCurrentAuthorityPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	flow := publishControlKV(t, f, controlKVSample)
	x, err := f.service.CreateControlFlow(ctx, f.actor, f.project, ControlFlowCreateInput{FlowVersionID: flow.ID}, platform.RandomID(), "kv")
	mustAgent(t, err)
	x, err = f.service.Control(ctx, f.actor, f.project, x.ID, ControlInput{Action: "pause", ExpectedVersion: x.StateVersion}, platform.RandomID(), "kv")
	mustAgent(t, err)
	if claimControlKV(t, f, x.ID) != nil {
		t.Fatal("paused metadata write dispatched")
	}
	x, err = f.service.Control(ctx, f.actor, f.project, x.ID, ControlInput{Action: "resume", ExpectedVersion: x.StateVersion}, platform.RandomID(), "kv")
	mustAgent(t, err)
	c := claimControlKV(t, f, x.ID)
	current, err := f.service.Get(ctx, f.project, x.ID)
	mustAgent(t, err)
	_, err = f.service.Control(ctx, f.actor, f.project, x.ID, ControlInput{Action: "cancel", ExpectedVersion: current.Execution.StateVersion}, platform.RandomID(), "kv")
	mustAgent(t, err)
	mustAgent(t, f.service.RunClaim(ctx, c))
	var keys int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_project_kv WHERE project_id=$1`, f.project).Scan(&keys))
	if keys != 0 {
		t.Fatal("kill-before-transaction wrote metadata")
	}
	x, err = f.service.CreateControlFlow(ctx, f.actor, f.project, ControlFlowCreateInput{FlowVersionID: flow.ID}, platform.RandomID(), "kv")
	mustAgent(t, err)
	c = claimControlKV(t, f, x.ID)
	_, err = f.store.DB.ExecContext(ctx, `UPDATE users SET enabled=false WHERE id=$1`, f.actor.User.ID)
	mustAgent(t, err)
	mustAgent(t, f.service.RunClaim(ctx, c))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_project_kv WHERE project_id=$1`, f.project).Scan(&keys))
	if keys != 0 {
		t.Fatal("revoked creator wrote metadata after claim")
	}
}
