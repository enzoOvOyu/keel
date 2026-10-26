package execution

import (
	"context"
	"sync"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// Exercises current database authority and native settlement only. No installer,
// block device, listener or SSH transport is executed by these fixtures.
func TestProvisionInstallationPrewriteRecoveryPostgres(t *testing.T) {
	ctx := context.Background()
	t.Run("atomic_explicit_stop_preserves_asset_and_never_retries", func(t *testing.T) {
		f := liveProvisionBoot(t)
		var beforeState, beforeTrust string
		var beforeVersion int64
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT a.state,a.version,m.ssh_trust FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1`, f.asset).Scan(&beforeState, &beforeVersion, &beforeTrust))
		attempt := expireRegisteredProvisionFixture(t, f)
		current := f.read(t)
		in := platform.ProvisionRequestActionInput{ExpectedVersion: current.StateVersion, Reason: "Close the quarantined attempt after server proves no write permit exists"}
		key := platform.RandomID()
		// Concurrent identical requests must share one stop/receipt/audit result.
		var wg sync.WaitGroup
		results := make(chan *Execution, 2)
		failures := make(chan error, 2)
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				out, err := f.service.ReconcileProvisionBeforeWrite(ctx, f.reviewer, f.project, current.ID, in, key, "test")
				results <- out
				failures <- err
			}()
		}
		wg.Wait()
		close(results)
		close(failures)
		for err := range failures {
			mustAgent(t, err)
		}
		for out := range results {
			if out == nil || out.State != "failed" || out.ID != attempt.ExecutionID {
				t.Fatal("original native task not settled")
			}
		}
		after := f.read(t)
		if after.State != "failed" || after.PermitIssuedAt != nil || after.StateVersion != current.StateVersion+1 {
			t.Fatal("stop did not record exact pre-write failure")
		}
		var afterState, afterTrust, lease string
		var afterVersion int64
		var attempts, receipts, stops int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT a.state,a.version,m.ssh_trust FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1`, f.asset).Scan(&afterState, &afterVersion, &afterTrust))
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM exec_resource_leases WHERE resource_id=$1 AND attempt_id=$2`, "device:machine:"+f.machine, attempt.ID).Scan(&lease))
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_task_attempts WHERE execution_id=$1`, attempt.ExecutionID).Scan(&attempts))
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_runner_receipts WHERE attempt_id=$1`, attempt.ID).Scan(&receipts))
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM provision_installation_events WHERE installation_id=$1 AND event_type='installation.stopped_before_write'`, current.ID).Scan(&stops))
		if beforeState != afterState || beforeTrust != afterTrust || beforeVersion != afterVersion || lease != "released" || attempts != 1 || receipts != 1 || stops != 1 {
			t.Fatal("prewrite recovery changed assets, retried or duplicated evidence")
		}
		changed := in
		changed.Reason = "Different recovery intent"
		if _, err := f.service.ReconcileProvisionBeforeWrite(ctx, f.reviewer, f.project, current.ID, changed, key, "test"); err == nil {
			t.Fatal("idempotency key changed payload")
		}
	})
	for _, failure := range []string{"live_attempt", "stale_version", "issued_permit", "released_claim", "unprivileged_actor", "disabled"} {
		t.Run(failure, func(t *testing.T) {
			var f *liveProvisionBootFixture
			if failure == "issued_permit" {
				f, _, _ = firstBootReadyFixture(t, true)
			} else {
				f = liveProvisionBoot(t)
			}
			if failure != "live_attempt" {
				expireRegisteredProvisionFixture(t, f)
			}
			current := f.read(t)
			in := platform.ProvisionRequestActionInput{ExpectedVersion: current.StateVersion, Reason: "Attempt explicit pre-write recovery"}
			actor := f.reviewer
			if failure == "stale_version" {
				in.ExpectedVersion--
			}
			if failure == "released_claim" {
				_, err := f.store.DB.ExecContext(ctx, `UPDATE exec_resource_leases SET state='released' WHERE attempt_id=$1`, f.claim.Attempt.ID)
				mustAgent(t, err)
			}
			if failure == "unprivileged_actor" {
				actor = &Actor{User: platform.UserSummary{ID: platform.RandomID()}}
			}
			if failure == "disabled" {
				f.store.ProvisionInstallEnabled = false
			}
			if _, err := f.service.ReconcileProvisionBeforeWrite(ctx, actor, f.project, current.ID, in, platform.RandomID(), "test"); err == nil {
				t.Fatal("unsafe prewrite recovery admitted", failure)
			}
			if f.read(t).State != current.State {
				t.Fatal("rejected stop changed domain state")
			}
			var count int
			mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_runner_receipts WHERE attempt_id=$1`, f.claim.Attempt.ID).Scan(&count))
			if count != 0 {
				t.Fatal("rejected stop invented native receipt")
			}
		})
	}
}
