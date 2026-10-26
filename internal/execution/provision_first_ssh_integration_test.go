package execution

import (
	"context"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func reportFirstBootSSHFixture(t *testing.T, f *liveProvisionBootFixture, secret *provisionfirstboot.Secret) {
	t.Helper()
	in, err := provisionfirstboot.Sign(secret, firstBootObservationFixture(t, secret), time.Now())
	mustAgent(t, err)
	_, err = f.authority.ReportFirstBoot(context.Background(), f.scope.Scope, f.installation.GrantID, netip.MustParseAddr("192.0.2.2"), *in)
	mustAgent(t, err)
}

// The DB fixtures never perform an SSH handshake. A zero/forged proof must be
// rejected; positive transport coverage is the separate localhost SSH fixture.
func TestProvisionInstallationFirstSSHPostgres(t *testing.T) {
	ctx := context.Background()
	t.Run("dispatch_waits_for_authenticated_first_boot", func(t *testing.T) {
		f, _, _ := firstBootReadyFixture(t, true)
		c := f.claim
		work, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
		mustAgent(t, err)
		if work != nil {
			t.Fatal("SSH dispatched before firstboot identity")
		}
	})
	t.Run("single_native_dispatch_and_unknown_proof_keep_resources", func(t *testing.T) {
		f, secret, _ := firstBootReadyFixture(t, true)
		reportFirstBootSSHFixture(t, f, secret)
		c := f.claim
		if _, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken+1); err == nil {
			t.Fatal("cross-fence dispatch accepted")
		}
		work, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
		mustAgent(t, err)
		if work == nil || work.Plan.HostKey == "" || work.Plan.Peer != netip.MustParseAddr("192.0.2.2") || work.Plan.BindingSHA256 != secret.Binding.SHA256() || work.Command.AttemptID != c.Attempt.ID || work.Command.CommandID != c.Attempt.CommandID || work.Command.Operation != "host.identity" || work.Plan.Credential != f.request.Snapshot.DomainKey {
			t.Fatal("dispatch escaped native/domain/new-pin binding")
		}
		repeated, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
		mustAgent(t, err)
		if repeated != nil {
			t.Fatal("automatic run replay admitted")
		}
		if _, err = f.store.FinalizeProvisionFirstSSH(ctx, f.project, f.installation.ID, taskrunner.ProvisionSSHProof{}); err == nil {
			t.Fatal("self-reported SSH success finalized")
		}
		mustAgent(t, f.store.RecordProvisionFirstSSHRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, "verification_unknown"))
		mustAgent(t, f.store.RecordProvisionFirstSSHRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, "verification_unknown"))
		var trust, state, lease string
		var repairs, receipts int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT m.ssh_trust,a.state FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1`, f.asset).Scan(&trust, &state))
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM exec_resource_leases WHERE attempt_id=$1 AND resource_id=$2`, c.Attempt.ID, "device:machine:"+f.request.MachineID).Scan(&lease))
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_repair_required'`, f.installation.ID).Scan(&repairs))
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_runner_receipts WHERE command_id=$1`, c.Attempt.CommandID).Scan(&receipts))
		if f.read(t).State != "awaiting_first_boot" || trust != "unverified" || state != "provisioning" || lease != "held" || repairs != 1 || receipts != 0 {
			t.Fatal("unknown SSH verification projected success or released resources")
		}
		var repairEventID int64
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT max(id) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_repair_required'`, f.installation.ID).Scan(&repairEventID))
		request := platform.ProvisionSSHRetryInput{ExpectedVersion: f.read(t).StateVersion, RepairEventID: repairEventID, Reason: "query original read-only receipt after missing response"}
		name := "repair-de-" + platform.RandomID()[:8]
		_, err = f.store.CreateUser(ctx, name, name, "repair-role-fixture-long-password", false, false)
		mustAgent(t, err)
		mustAgent(t, f.store.SetMembership(ctx, name, f.project, "DE", false))
		pre, _, err := f.store.NewPreSession(ctx)
		mustAgent(t, err)
		_, engineer, err := f.store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: name, Password: "repair-role-fixture-long-password", Method: "local"}, "198.51.100.32", "repair-test")
		mustAgent(t, err)
		if _, err = f.store.RequestProvisionSSHRetry(ctx, engineer, f.project, f.installation.ID, request, "test"); err == nil {
			t.Fatal("DE admitted SSH repair query")
		}
		_, err = f.store.DB.ExecContext(ctx, `UPDATE users SET enabled=false WHERE id=$1`, engineer.User.ID)
		mustAgent(t, err)
		if _, err = f.store.RequestProvisionSSHRetry(ctx, engineer, f.project, f.installation.ID, request, "test"); err == nil {
			t.Fatal("disabled repair requester admitted")
		}
		queued, err := f.store.RequestProvisionSSHRetry(ctx, f.reviewer, f.project, f.installation.ID, request, "test")
		mustAgent(t, err)
		same, err := f.store.RequestProvisionSSHRetry(ctx, f.reviewer, f.project, f.installation.ID, request, "test")
		mustAgent(t, err)
		if queued.ID != same.ID || queued.Mode != "query" {
			t.Fatal("duplicate pending recovery was not idempotent")
		}
		recovered, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
		mustAgent(t, err)
		if recovered == nil || recovered.Mode != "query" || recovered.RetryID != queued.ID || recovered.Command != work.Command || recovered.Plan.HostKey != work.Plan.HostKey {
			t.Fatal("recovery escaped query-only original-command binding")
		}
		if _, err = f.store.RequestProvisionSSHRetry(ctx, f.reviewer, f.project, f.installation.ID, request, "test"); err == nil {
			t.Fatal("consumed repair enqueued another query before outcome")
		}
		// Missing terminal evidence produces a NEW repair candidate, never run.
		next, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
		mustAgent(t, err)
		if next != nil {
			t.Fatal("consumed query was implicitly replayed")
		}
		if _, err = f.store.RequestProvisionSSHRetry(ctx, f.reviewer, f.project, f.installation.ID, request, "test"); err == nil {
			t.Fatal("stale repair event accepted after a newer outcome")
		}
		forged := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: "success", StartedAt: time.Now(), FinishedAt: time.Now(), Output: map[string]any{"schema": "keel.provision-installation-proof/v1", "installation_id": f.installation.ID, "request_id": f.request.ID, "snapshot_sha256": f.request.SnapshotSHA256, "installation_performed": true, "first_ssh_proof_sha256": strings.Repeat("a", 64)}}
		if err = f.service.RecordReceipt(ctx, forged); err == nil {
			t.Fatal("forged native success receipt admitted")
		}
	})
	t.Run("expired_native_lease_cannot_request_recovery", func(t *testing.T) {
		f, secret, _ := firstBootReadyFixture(t, true)
		reportFirstBootSSHFixture(t, f, secret)
		c := f.claim
		_, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
		mustAgent(t, err)
		mustAgent(t, f.store.RecordProvisionFirstSSHRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, "verification_unknown"))
		var repairID int64
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT max(id) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_repair_required'`, f.installation.ID).Scan(&repairID))
		_, err = f.store.DB.ExecContext(ctx, `UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1`, c.Attempt.ID)
		mustAgent(t, err)
		if _, err = f.store.RequestProvisionSSHRetry(ctx, f.reviewer, f.project, f.installation.ID, platform.ProvisionSSHRetryInput{ExpectedVersion: f.read(t).StateVersion, RepairEventID: repairID, Reason: "attempt to query after lease expired"}, "test"); err == nil {
			t.Fatal("expired native lease recovered implicitly")
		}
	})
	t.Run("revoked_domain_identity_blocks_first_ssh", func(t *testing.T) {
		f, secret, _ := firstBootReadyFixture(t, true)
		reportFirstBootSSHFixture(t, f, secret)
		_, err := f.store.RevokeSSHDomainKey(ctx, f.project, f.request.Snapshot.DomainKey.ID, 1, "withdraw first SSH credential")
		mustAgent(t, err)
		c := f.claim
		if _, err = f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken); err == nil {
			t.Fatal("revoked domain credential authorized connection")
		}
		if f.read(t).State != "awaiting_first_boot" {
			t.Fatal("revocation fabricated installation terminal success")
		}
	})
}

func TestProvisionFirstSSHTranscriptCanonicalNumbers(t *testing.T) {
	a := []byte(`{"command":{"fencing_token":9007199254740993,"attempt_id":"x"},"asset_version":9007199254740995}`)
	b := []byte(`{ "asset_version":9007199254740995,"command":{"attempt_id":"x","fencing_token":9007199254740993}}`)
	if platform.ProvisionFirstSSHTranscriptSHA256(a) != platform.ProvisionFirstSSHTranscriptSHA256(b) {
		t.Fatal("JSONB ordering changed immutable transcript digest")
	}
	c := []byte(`{"command":{"fencing_token":9007199254740992,"attempt_id":"x"},"asset_version":9007199254740995}`)
	if platform.ProvisionFirstSSHTranscriptSHA256(a) == platform.ProvisionFirstSSHTranscriptSHA256(c) {
		t.Fatal("large fence precision was lost")
	}
}

func TestProvisionInstallationFirstSSHOrphanDispatchPostgres(t *testing.T) {
	ctx := context.Background()
	f, secret, _ := firstBootReadyFixture(t, true)
	reportFirstBootSSHFixture(t, f, secret)
	c := f.claim
	work, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if work == nil {
		t.Fatal("dispatch missing")
	}
	// Emulate a committed dispatch whose transport/repair write never returned.
	// The next tick can expose query-only recovery, never infer a safe new run.
	for range 2 {
		finished, err := f.service.runProvisionFirstSSH(ctx, c)
		mustAgent(t, err)
		if finished {
			t.Fatal("orphan dispatch became successful")
		}
	}
	var repairs, dispatches, receipts int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FILTER(WHERE event_type='first_boot.ssh_repair_required'),count(*) FILTER(WHERE event_type='first_boot.ssh_started') FROM provision_installation_events WHERE installation_id=$1`, f.installation.ID).Scan(&repairs, &dispatches))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_runner_receipts WHERE attempt_id=$1`, c.Attempt.ID).Scan(&receipts))
	progress, err := f.store.ReadProvisionInstallationProgress(ctx, f.actor, f.project, f.installation.ID)
	mustAgent(t, err)
	if repairs != 1 || dispatches != 1 || receipts != 0 || progress.SSHRepair == nil || progress.SSHRepair.Status != "available" || progress.SSHRepair.FailedEventID != nil {
		t.Fatal("orphan dispatch did not expose exactly one query-only repair")
	}
	if f.read(t).State != "awaiting_first_boot" {
		t.Fatal("missing outcome changed installation state")
	}
}
