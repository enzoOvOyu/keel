package execution

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// This DB-only fixture seeds a terminal-failure event to exercise downstream
// authorization and immutable probe transitions. It does not claim that an SSH
// failure was measured; genuine sealed failure proofs have localhost coverage
// in taskrunner, and a zero/self-reported proof is explicitly rejected here.
func TestProvisionInstallationKnownFailedSSHPostgres(t *testing.T) {
	ctx := context.Background()
	f, secret, _ := firstBootReadyFixture(t, true)
	reportFirstBootSSHFixture(t, f, secret)
	c := f.claim
	original, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if err = f.store.RecordProvisionFirstSSHFailure(ctx, f.project, f.installation.ID, taskrunner.ProvisionSSHProof{}); err == nil {
		t.Fatal("fabricated failed receipt became new-run permission")
	}
	mustAgent(t, f.store.RecordProvisionFirstSSHRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, "verification_unknown"))
	var repairID int64
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT max(id) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_repair_required'`, f.installation.ID).Scan(&repairID))
	input := platform.ProvisionSSHVerificationInput{ExpectedVersion: f.read(t).StateVersion, RepairEventID: repairID, FailedEventID: repairID, Reason: "explicit new read-only identity verification"}
	if _, err = f.store.RequestProvisionSSHVerification(ctx, f.reviewer, f.project, f.installation.ID, input, "test"); err == nil {
		t.Fatal("unknown receipt admitted replacement run")
	}
	queued, err := f.store.RequestProvisionSSHRetry(ctx, f.reviewer, f.project, f.installation.ID, platform.ProvisionSSHRetryInput{ExpectedVersion: input.ExpectedVersion, RepairEventID: repairID, Reason: "query original immutable failed command"}, "test")
	mustAgent(t, err)
	query, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if query.Mode != "query" || query.RetryID != queued.ID {
		t.Fatal("failed receipt was not queried first")
	}
	now := time.Now().UTC()
	failure := taskrunner.Observation{Command: original.Command, State: "failed", StartedAt: now, FinishedAt: now, Output: map[string]any{"reason": "software database fixture; no actual SSH performed"}}
	raw, _ := json.Marshal(map[string]any{"command": original.Command, "observation": failure, "native_command_id": c.Attempt.CommandID, "binding_sha256": secret.Binding.SHA256(), "queried": true, "retry_id": queued.ID})
	var failedID int64
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `INSERT INTO provision_installation_events(installation_id,event_type,payload,created_at) VALUES($1,'first_boot.ssh_terminal_failed',$2,now()) RETURNING id`, f.installation.ID, raw).Scan(&failedID))
	mustAgent(t, f.store.RecordProvisionFirstSSHRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, "verification_unknown"))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT max(id) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_repair_required'`, f.installation.ID).Scan(&repairID))
	input.RepairEventID = repairID
	input.FailedEventID = failedID
	verification, err := f.store.RequestProvisionSSHVerification(ctx, f.reviewer, f.project, f.installation.ID, input, "test")
	mustAgent(t, err)
	duplicate, err := f.store.RequestProvisionSSHVerification(ctx, f.reviewer, f.project, f.installation.ID, input, "test")
	mustAgent(t, err)
	if verification.ID != duplicate.ID {
		t.Fatal("same failure created duplicate replacement authorization")
	}
	if _, err = f.store.RequestProvisionSSHRetry(ctx, f.reviewer, f.project, f.installation.ID, platform.ProvisionSSHRetryInput{ExpectedVersion: input.ExpectedVersion, RepairEventID: repairID, Reason: "cannot enqueue concurrent query and replacement probe"}, "test"); err == nil {
		t.Fatal("concurrent recovery types admitted")
	}
	probe, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if probe == nil || probe.Mode != "run" || probe.NativeCommandID != c.Attempt.CommandID || probe.Command.CommandID == c.Attempt.CommandID || probe.Command.AttemptID != c.Attempt.ID || probe.Command.FencingToken != c.Attempt.FencingToken || probe.VerificationID != verification.ID {
		t.Fatal("new read-only probe escaped original native lease binding")
	}
	var native string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT command_id FROM provision_installations WHERE id=$1`, f.installation.ID).Scan(&native))
	if native != c.Attempt.CommandID {
		t.Fatal("immutable installation native command was replaced")
	}
	var binding bool
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_verification_consumed' AND payload->>'verification_command_id'=$2 AND payload->>'native_command_id'=$3 AND payload->>'fencing_token'=$4)`, f.installation.ID, probe.Command.CommandID, c.Attempt.CommandID, strconv.FormatInt(c.Attempt.FencingToken, 10)).Scan(&binding))
	if !binding {
		t.Fatal("replacement probe lacks append-only authorization binding")
	}
	mustAgent(t, f.store.RecordProvisionFirstSSHRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, "verification_unknown"))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT max(id) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_repair_required'`, f.installation.ID).Scan(&repairID))
	input.RepairEventID = repairID
	if _, err = f.store.RequestProvisionSSHVerification(ctx, f.reviewer, f.project, f.installation.ID, input, "test"); err == nil {
		t.Fatal("old failed probe authorized replay after new probe became unknown")
	}
	recovery, err := f.store.RequestProvisionSSHRetry(ctx, f.reviewer, f.project, f.installation.ID, platform.ProvisionSSHRetryInput{ExpectedVersion: input.ExpectedVersion, RepairEventID: repairID, Reason: "unknown replacement probe only queries its own receipt"}, "test")
	mustAgent(t, err)
	next, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if next == nil || next.Mode != "query" || next.Command != probe.Command || next.RetryID != recovery.ID || next.NativeCommandID != native {
		t.Fatal("unknown new probe queried original or reran work")
	}
	if f.read(t).State != "awaiting_first_boot" {
		t.Fatal("read-only recovery marked installation successful")
	}
}
