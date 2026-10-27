package execution

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
	"github.com/enzoOvOyu/keel/internal/platform"
)

func testBMCPolicy(t *testing.T, f *agentKernelFixture, asset, kind string, retries int) *BMCPolicy {
	t.Helper()
	in := BMCPolicyInput{Level: "L1", TriggerKind: kind, Enabled: true, MaxRetries: retries, RetryDelaySeconds: 30, Reason: "Inventory schedule fixture"}
	if kind == "cron" {
		in.CronExpression = "*/5 * * * *"
	} else {
		in.EventType = "asset.provisioned"
	}
	p, e := f.service.SaveBMCPolicy(context.Background(), f.actor, f.project, asset, "", in, platform.RandomID(), "fixture")
	mustAgent(t, e)
	return p
}
func dueBMCPolicy(t *testing.T, f *agentKernelFixture, id string) {
	t.Helper()
	_, e := f.store.DB.ExecContext(context.Background(), `UPDATE bmc_collection_policies SET next_due=now()-interval '2 hours' WHERE id=$1`, id)
	mustAgent(t, e)
}
func appendTestBMCEvent(t *testing.T, db *sql.DB, project, kind, source, machine, batch string) {
	t.Helper()
	tx, e := db.BeginTx(context.Background(), nil)
	mustAgent(t, e)
	defer tx.Rollback()
	mustAgent(t, platform.AppendBMCInventoryEventTx(context.Background(), tx, project, kind, source, machine, batch))
	mustAgent(t, tx.Commit())
}

// Cases are authored for milestone validation, not executed by source increments.
func TestBMCPolicyConcurrentCronCoalescesAndPreservesRevisionPostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	p := testBMCPolicy(t, f, asset, "cron", 0)
	dueBMCPolicy(t, f, p.ID)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := f.service.tickBMCPolicy(ctx, p.ID); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		mustAgent(t, e)
	}
	runs, e := f.service.ListBMCPolicyRuns(ctx, f.actor, f.project, asset, p.ID, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	if runs.Total != 1 || runs.Items[0].State != "admitted" || runs.Items[0].ExecutionID == nil || runs.Items[0].PolicyVersion != 1 {
		t.Fatalf("duplicate or missing cron admission: %+v", runs)
	}
	policies, e := f.service.ListBMCPolicies(ctx, f.actor, f.project, asset)
	mustAgent(t, e)
	if len(policies) != 1 || policies[0].NextDue == nil || !policies[0].NextDue.After(time.Now()) {
		t.Fatal("missed cron intervals were not coalesced")
	}
	var frozen string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT parameters#>>'{bmc_trigger,run_id}' FROM exec_executions WHERE id=$1`, *runs.Items[0].ExecutionID).Scan(&frozen))
	if frozen != runs.Items[0].ID {
		t.Fatal("execution did not freeze its exact trigger")
	}
	in := BMCPolicyInput{Level: "L3", TriggerKind: "cron", CronExpression: "0 0 * * *", Enabled: false, MaxRetries: 0, RetryDelaySeconds: 60, ExpectedVersion: 1, Reason: "Disable schedule fixture"}
	saved, e := f.service.SaveBMCPolicy(ctx, f.actor, f.project, asset, p.ID, in, platform.RandomID(), "fixture")
	mustAgent(t, e)
	if saved.Enabled || saved.StateVersion != 2 {
		t.Fatal("policy CAS or disable lost")
	}
	if _, e = f.service.SaveBMCPolicy(ctx, f.actor, f.project, asset, p.ID, in, platform.RandomID(), "fixture"); e == nil {
		t.Fatal("stale policy overwrite accepted")
	}
	runs, e = f.service.ListBMCPolicyRuns(ctx, f.actor, f.project, asset, p.ID, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	if runs.Items[0].PolicySnapshot.Level != "L1" || runs.Items[0].PolicySnapshot.StateVersion != 1 {
		t.Fatal("policy edit rewrote immutable history")
	}
	if _, e = f.store.DB.ExecContext(ctx, `UPDATE bmc_policy_runs SET code='forged' WHERE id=$1`, runs.Items[0].ID); e == nil {
		t.Fatal("immutable run changed")
	}
}
func TestBMCPolicyTrustedEventScopeAndNoHistoricalReplayPostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	appendTestBMCEvent(t, f.store.DB, f.project, "asset.provisioned", "before-policy", f.machine, "")
	p := testBMCPolicy(t, f, asset, "event", 0)
	if n, e := f.service.TickBMCInventoryPolicies(ctx, 20); e != nil || n != 0 {
		t.Fatalf("historical replay: %d %v", n, e)
	}
	appendTestBMCEvent(t, f.store.DB, f.project, "asset.provisioned", "after-policy", f.machine, "")
	appendTestBMCEvent(t, f.store.DB, f.project, "asset.provisioned", "after-policy", f.machine, "")
	n, e := f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	if n != 1 {
		t.Fatalf("event was not admitted once: %d", n)
	}
	n, e = f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	if n != 0 {
		t.Fatal("event replayed")
	}
	runs, e := f.service.ListBMCPolicyRuns(ctx, f.actor, f.project, asset, p.ID, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	if runs.Total != 1 || runs.Items[0].SourceRef != "after-policy" {
		t.Fatal("event history source or scope lost")
	}
	if _, e = f.service.ListBMCPolicyRuns(ctx, f.actor, f.project, platform.RandomID(), p.ID, platform.Pagination{Page: 1, PageSize: 20}); e == nil {
		t.Fatal("cross-asset history exposed")
	}
	// An unrelated batch event advances only the cursor; it cannot create a job.
	p2 := testBMCPolicy(t, f, asset, "event", 0)
	_, e = f.store.DB.ExecContext(ctx, `UPDATE bmc_collection_policies SET event_type='credential.batch.rotated',state_version=state_version+1 WHERE id=$1`, p2.ID)
	mustAgent(t, e)
	appendTestBMCEvent(t, f.store.DB, f.project, "credential.batch.rotated", "unrelated-batch", "", "other-batch")
	n, e = f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	if n != 0 {
		t.Fatal("unrelated credential batch created work")
	}
}
func TestBMCPolicyRevokedOwnerBlocksAndDisablesPostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	p := testBMCPolicy(t, f, asset, "cron", 0)
	dueBMCPolicy(t, f, p.ID)
	_, e := f.store.DB.ExecContext(ctx, `UPDATE users SET enabled=false WHERE id=$1`, f.actor.User.ID)
	mustAgent(t, e)
	n, e := f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	if n != 0 {
		t.Fatal("revoked owner admitted device work")
	}
	var enabled bool
	var state, code string
	var execution *string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT p.enabled,r.state,r.code,r.execution_id FROM bmc_collection_policies p JOIN bmc_policy_runs r ON r.policy_id=p.id WHERE p.id=$1`, p.ID).Scan(&enabled, &state, &code, &execution))
	if enabled || state != "blocked" || code != "BMC_POLICY_AUTHORITY_REVOKED" || execution != nil {
		t.Fatal("authority revocation was not retained safely")
	}
}
func TestBMCPolicyRetryRequiresStoppedEvidencePostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	p := testBMCPolicy(t, f, asset, "cron", 1)
	dueBMCPolicy(t, f, p.ID)
	n, e := f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	if n != 1 {
		t.Fatal("initial admission missing")
	}
	runs, e := f.service.ListBMCPolicyRuns(ctx, f.actor, f.project, asset, p.ID, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	original := runs.Items[0]
	c := claimBMC(t, f, *original.ExecutionID)
	if c == nil {
		t.Fatal("inventory claim missing")
	}
	request, _ := c.Parameters["bmc_collection_id"].(string)
	report, e := f.store.RunBMCCollection(ctx, request, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, e)
	// Inject a declared sim timeout after retaining the fixture's exact raw data.
	report.State = "partial"
	report.Failures = append(report.Failures, bmc.Failure{Path: "/redfish/v1/Systems", Class: "timeout", Code: "request_timeout", Retryable: true})
	report.Degraded = true
	mustAgent(t, f.store.RecordBMCCollection(ctx, c.Attempt.ExecutionID, c.Attempt.FencingToken, report))
	// Report persistence alone cannot retry an active/unknown attempt.
	n, e = f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	if n != 0 {
		t.Fatal("uncompleted original was retried")
	}
	recovered, e := f.service.recoverBMCReceipt(ctx, c)
	mustAgent(t, e)
	if !recovered {
		t.Fatal("original report recovery missing")
	}
	mustAgent(t, f.service.Complete(ctx, c))
	_, e = f.store.DB.ExecContext(ctx, `UPDATE exec_executions SET finished_at=now()-interval '1 minute' WHERE id=$1`, *original.ExecutionID)
	mustAgent(t, e)
	n, e = f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	if n != 1 {
		t.Fatalf("stopped retryable failure not admitted: %d", n)
	}
	n, e = f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	if n != 0 {
		t.Fatal("retry parent admitted a second child")
	}
	runs, e = f.service.ListBMCPolicyRuns(ctx, f.actor, f.project, asset, p.ID, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	if runs.Total != 2 {
		t.Fatal("retry lineage missing")
	}
	var retry *BMCPolicyRun
	for i := range runs.Items {
		if runs.Items[i].TriggerKind == "retry" {
			retry = &runs.Items[i]
		}
	}
	if retry == nil || retry.ParentRunID == nil || *retry.ParentRunID != original.ID || retry.AttemptNo != 1 || retry.ExecutionID == nil || *retry.ExecutionID == *original.ExecutionID {
		t.Fatal("retry reused command or lost immutable parent")
	}
}
func TestBMCPolicyValidationRejectsAmbiguousOrUnboundedSettings(t *testing.T) {
	base := BMCPolicyInput{Level: "L1", TriggerKind: "cron", CronExpression: "0 0 * * *", Enabled: true, RetryDelaySeconds: 30, Reason: "Schedule fixture"}
	for _, change := range []func(*BMCPolicyInput){func(p *BMCPolicyInput) { p.CronExpression = "0 0 31 2 *" }, func(p *BMCPolicyInput) { p.MaxRetries = 4 }, func(p *BMCPolicyInput) { p.RetryDelaySeconds = 29 }, func(p *BMCPolicyInput) { p.EventType = "asset.provisioned" }, func(p *BMCPolicyInput) { p.TriggerKind = "event"; p.EventType = "external.webhook" }, func(p *BMCPolicyInput) { p.Level = "L4" }} {
		in := base
		change(&in)
		if _, e := validateBMCPolicy(in, time.Now()); e == nil {
			t.Fatalf("accepted invalid settings: %+v", in)
		}
	}
}
func TestBMCPolicyUnknownWithDurableReportNeverRetriesPostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	p := testBMCPolicy(t, f, asset, "cron", 3)
	dueBMCPolicy(t, f, p.ID)
	_, e := f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	runs, e := f.service.ListBMCPolicyRuns(ctx, f.actor, f.project, asset, p.ID, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	c := claimBMC(t, f, *runs.Items[0].ExecutionID)
	if c == nil {
		t.Fatal("claim missing")
	}
	request, _ := c.Parameters["bmc_collection_id"].(string)
	report, e := f.store.RunBMCCollection(ctx, request, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, e)
	report.State = "partial"
	report.Degraded = true
	report.Failures = append(report.Failures, bmc.Failure{Path: "/redfish/v1/Systems", Class: "timeout", Code: "request_timeout", Retryable: true})
	mustAgent(t, f.store.RecordBMCCollection(ctx, c.Attempt.ExecutionID, c.Attempt.FencingToken, report))
	recovered, e := f.service.recoverBMCReceipt(ctx, c)
	mustAgent(t, e)
	if !recovered {
		t.Fatal("durable receipt missing")
	}
	// Inject the state of an expired original command before explicit reconciliation.
	_, e = f.store.DB.ExecContext(ctx, `UPDATE exec_task_attempts SET state='unknown',lease_until=now()-interval '1 minute' WHERE id=$1`, c.Attempt.ID)
	mustAgent(t, e)
	_, e = f.store.DB.ExecContext(ctx, `UPDATE exec_executions SET state='reconciling',finished_at=now()-interval '1 minute' WHERE id=$1`, c.Attempt.ExecutionID)
	mustAgent(t, e)
	n, e := f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	if n != 0 {
		t.Fatal("unknown original retried despite quarantine")
	}
	runs, e = f.service.ListBMCPolicyRuns(ctx, f.actor, f.project, asset, p.ID, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	if runs.Total != 1 {
		t.Fatal("unknown original acquired a retry child")
	}
}
func TestBMCPolicyIndependentLevelsShareKernelQueuePostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	l1 := testBMCPolicy(t, f, asset, "cron", 0)
	l3 := testBMCPolicy(t, f, asset, "cron", 0)
	_, e := f.store.DB.ExecContext(ctx, `UPDATE bmc_collection_policies SET level='L3',state_version=state_version+1 WHERE id=$1`, l3.ID)
	mustAgent(t, e)
	dueBMCPolicy(t, f, l1.ID)
	dueBMCPolicy(t, f, l3.ID)
	n, e := f.service.TickBMCInventoryPolicies(ctx, 20)
	mustAgent(t, e)
	if n != 2 {
		t.Fatalf("independent L1/L3 policies suppressed: %d", n)
	}
	runs1, e := f.service.ListBMCPolicyRuns(ctx, f.actor, f.project, asset, l1.ID, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	runs3, e := f.service.ListBMCPolicyRuns(ctx, f.actor, f.project, asset, l3.ID, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	first := claimBMC(t, f, *runs1.Items[0].ExecutionID)
	if first == nil {
		t.Fatal("first collection did not claim")
	}
	second := claimBMC(t, f, *runs3.Items[0].ExecutionID)
	if second != nil {
		t.Fatal("independent policies bypassed the shared exclusive BMC lease")
	}
	mustAgent(t, f.service.RunClaim(ctx, first))
	second = claimBMC(t, f, *runs3.Items[0].ExecutionID)
	if second == nil {
		t.Fatal("queued hardware-detail collection did not resume after BMC lease release")
	}
	mustAgent(t, f.service.RunClaim(ctx, second))
}
