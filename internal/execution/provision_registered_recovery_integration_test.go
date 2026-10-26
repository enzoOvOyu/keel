package execution

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// This fixture seeds the already-committed registration/receipt boundary to
// exercise crash settlement only. It does NOT manufacture a transport proof
// for FinalizeProvisionFirstSSH or claim that a real SSH handshake occurred.
func persistedProvisionRegistrationFixture(t *testing.T, badCommitment bool) *liveProvisionBootFixture {
	t.Helper()
	ctx := context.Background()
	f, secret, _ := firstBootReadyFixture(t, true)
	reportFirstBootSSHFixture(t, f, secret)
	c := f.claim
	work, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if work == nil {
		t.Fatal("first SSH work missing")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	observation := taskrunner.Observation{Command: work.Command, State: "success", StartedAt: now.Add(-time.Second), FinishedAt: now, Output: map[string]any{"hostname": work.Plan.Hostname, "os": "linux", "arch": work.Plan.Arch}}
	tx, err := f.store.DB.BeginTx(ctx, nil)
	mustAgent(t, err)
	defer tx.Rollback()
	var version int64
	mustAgent(t, tx.QueryRowContext(ctx, `UPDATE project_assets SET state='provisioned',version=version+1,updated_at=$2 WHERE id=$1 RETURNING version`, f.asset, now).Scan(&version))
	_, err = tx.ExecContext(ctx, `UPDATE machines SET ssh_trust='established',ssh_trust_source='ssh',host_key_fingerprint=$2,ssh_domain_credential=$3,trust_updated_at=$4 WHERE id=$1`, f.machine, work.Plan.HostKey, jsonBytes(work.Plan.Credential), now)
	mustAgent(t, err)
	event := map[string]any{"native_command_id": c.Attempt.CommandID, "verification_command_id": work.Command.CommandID, "verification_id": "", "command": work.Command, "observation": observation, "binding_sha256": work.Plan.BindingSHA256, "report_sha256": work.ReportSHA256, "host_fingerprint": work.Plan.HostKey, "domain_credential": work.Plan.Credential, "asset_version": version, "verified_transport": "ssh", "hardware_evidence": false}
	raw := jsonBytes(event)
	_, err = tx.ExecContext(ctx, `INSERT INTO provision_installation_events(installation_id,event_type,payload,created_at) VALUES($1,'first_boot.ssh_verified',$2,$3)`, f.installation.ID, raw, now)
	mustAgent(t, err)
	_, err = tx.ExecContext(ctx, `UPDATE provision_installations SET state='registered',state_version=state_version+1,updated_at=$2 WHERE id=$1`, f.installation.ID, now)
	mustAgent(t, err)
	commitment := platform.ProvisionFirstSSHTranscriptSHA256(raw)
	if badCommitment {
		commitment = "invalid-fixture-transcript"
	}
	receipt := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: "success", StartedAt: observation.StartedAt, FinishedAt: now, Output: map[string]any{"schema": "keel.provision-installation-proof/v1", "installation_id": f.installation.ID, "request_id": f.installation.RequestID, "snapshot_sha256": f.installation.SnapshotSHA256, "installation_performed": true, "first_ssh_proof_sha256": commitment}}
	raw = jsonBytes(receipt)
	_, err = tx.ExecContext(ctx, `INSERT INTO exec_runner_receipts(command_id,attempt_id,fencing_token,result,payload,digest,observed_at) VALUES($1,$2,$3,'success',$4,$5,$6)`, receipt.CommandID, receipt.AttemptID, receipt.FencingToken, raw, hash(raw), now)
	mustAgent(t, err)
	mustAgent(t, tx.Commit())
	return f
}

func expireRegisteredProvisionFixture(t *testing.T, f *liveProvisionBootFixture) Attempt {
	t.Helper()
	ctx := context.Background()
	_, err := f.store.DB.ExecContext(ctx, `UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1`, f.claim.Attempt.ID)
	mustAgent(t, err)
	_, err = f.store.DB.ExecContext(ctx, `UPDATE exec_resource_leases SET expires_at=now()-interval '1 second' WHERE attempt_id=$1`, f.claim.Attempt.ID)
	mustAgent(t, err)
	n, err := f.service.ReapExpired(ctx)
	mustAgent(t, err)
	if n != 1 {
		t.Fatal("expected single quarantined native attempt", n)
	}
	attempt, err := scanAttempt(f.store.DB.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1", f.claim.Attempt.ID))
	mustAgent(t, err)
	return attempt
}

func TestProvisionInstallationRegisteredRecoveryPostgres(t *testing.T) {
	ctx := context.Background()
	t.Run("durable_success_settles_original_expired_claim_without_network_or_renewal", func(t *testing.T) {
		f := persistedProvisionRegistrationFixture(t, false)
		attempt := expireRegisteredProvisionFixture(t, f)
		progress, err := f.store.ReadProvisionInstallationProgress(ctx, f.actor, f.project, f.installation.ID)
		mustAgent(t, err)
		if progress.State != "registered" || !progress.Resource.MachineReserved || !progress.Resource.ReconciliationRequired || progress.Resource.LeaseState != "reconciling" {
			t.Fatal("registered projection hid crash quarantine")
		}
		before := *attempt.LeaseUntil
		key := platform.RandomID()
		input := ReconcileInput{ExpectedVersion: attempt.StateVersion, Result: "success", Evidence: "Review existing native receipt and exact committed first SSH transcript"}
		out, err := f.service.Reconcile(ctx, f.reviewer, f.project, attempt.ExecutionID, attempt.ID, input, key, "test")
		mustAgent(t, err)
		if out.State != "success" {
			t.Fatal("registered native receipt did not settle", out.State)
		}
		again, err := f.service.Reconcile(ctx, f.reviewer, f.project, attempt.ExecutionID, attempt.ID, input, key, "test")
		mustAgent(t, err)
		if again.ID != out.ID || again.State != out.State {
			t.Fatal("repeated authorized reconciliation diverged")
		}
		var state string
		var until time.Time
		var count int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state,lease_until FROM exec_task_attempts WHERE id=$1`, attempt.ID).Scan(&state, &until))
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_task_attempts WHERE execution_id=$1`, attempt.ExecutionID).Scan(&count))
		if state != "success" || !until.Equal(before) || count != 1 {
			t.Fatal("recovery renewed or replaced the native attempt")
		}
		progress, err = f.store.ReadProvisionInstallationProgress(ctx, f.actor, f.project, f.installation.ID)
		mustAgent(t, err)
		if progress.Resource.MachineReserved || progress.Resource.ReconciliationRequired || progress.Resource.LeaseState != "released" {
			t.Fatal("verified explicit settlement failed to release original claim")
		}
	})
	for _, failure := range []string{"wrong_transcript", "released_claim", "wrong_fence", "changed_asset"} {
		t.Run(failure, func(t *testing.T) {
			f := persistedProvisionRegistrationFixture(t, failure == "wrong_transcript")
			attempt := expireRegisteredProvisionFixture(t, f)
			var err error
			switch failure {
			case "released_claim":
				_, err = f.store.DB.ExecContext(ctx, `UPDATE exec_resource_leases SET state='released' WHERE attempt_id=$1`, attempt.ID)
			case "wrong_fence":
				_, err = f.store.DB.ExecContext(ctx, `UPDATE exec_resource_leases SET fencing_token=fencing_token+1 WHERE attempt_id=$1`, attempt.ID)
			case "changed_asset":
				_, err = f.store.DB.ExecContext(ctx, `UPDATE project_assets SET version=version+1 WHERE id=$1`, f.asset)
			}
			mustAgent(t, err)
			if _, err = f.service.Reconcile(ctx, f.reviewer, f.project, attempt.ExecutionID, attempt.ID, ReconcileInput{ExpectedVersion: attempt.StateVersion, Result: "success", Evidence: "Attempt crash settlement with stale or mismatched durable authority"}, platform.RandomID(), "test"); err == nil {
				t.Fatal("mismatched registered proof released resources", failure)
			}
			var raw []byte
			mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT payload FROM exec_runner_receipts WHERE attempt_id=$1`, attempt.ID).Scan(&raw))
			var r Receipt
			mustAgent(t, json.Unmarshal(raw, &r))
			if r.CommandID != attempt.CommandID {
				t.Fatal("failed reconciliation changed immutable receipt")
			}
		})
	}
}
