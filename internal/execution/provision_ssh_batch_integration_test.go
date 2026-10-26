package execution

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// Synthetic queried-failure boundary only; no transport measurement is claimed.
func TestProvisionInstallationSSHBatchPostgres(t *testing.T) {
	ctx := context.Background()
	f, secret, _ := firstBootReadyFixture(t, true)
	reportFirstBootSSHFixture(t, f, secret)
	c := f.claim
	work, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	now := time.Now().UTC()
	failed := taskrunner.Observation{Command: work.Command, State: "failed", StartedAt: now, FinishedAt: now, Output: map[string]any{"reason": "synthetic committed queried-failure boundary; no SSH"}}
	raw, _ := json.Marshal(map[string]any{"command": work.Command, "observation": failed, "native_command_id": c.Attempt.CommandID, "binding_sha256": secret.Binding.SHA256(), "queried": true})
	var failedID, repairID int64
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `INSERT INTO provision_installation_events(installation_id,event_type,payload,created_at) VALUES($1,'first_boot.ssh_terminal_failed',$2,now()) RETURNING id`, f.installation.ID, raw).Scan(&failedID))
	mustAgent(t, f.store.RecordProvisionFirstSSHRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, "verification_unknown"))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT max(id) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_repair_required'`, f.installation.ID).Scan(&repairID))
	good := platform.ProvisionSSHBatchItem{InstallationID: f.installation.ID, ExpectedVersion: f.read(t).StateVersion, RepairEventID: repairID, FailedEventID: failedID}
	in := platform.ProvisionSSHBatchInput{Items: []platform.ProvisionSSHBatchItem{good}, Reason: "explicit read-only batch after known failure"}
	bad := good
	bad.InstallationID = strings.Repeat("f", 32)
	invalid := in
	invalid.Items = []platform.ProvisionSSHBatchItem{good, bad}
	if _, err = f.store.RequestProvisionSSHBatch(ctx, f.reviewer, f.project, invalid, platform.RandomID(), "test"); err == nil {
		t.Fatal("missing member accepted")
	}
	var count int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_verification_requested'`, f.installation.ID).Scan(&count))
	if count != 0 {
		t.Fatal("failed batch left partial authorization")
	}
	invalid.Items = []platform.ProvisionSSHBatchItem{good, good}
	if _, err = f.store.RequestProvisionSSHBatch(ctx, f.reviewer, f.project, invalid, platform.RandomID(), "test"); err == nil {
		t.Fatal("duplicate member accepted")
	}
	key := platform.RandomID()
	var wg sync.WaitGroup
	ids := make(chan string, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, e := f.store.RequestProvisionSSHBatch(ctx, f.reviewer, f.project, in, key, "test")
			failures <- e
			if e == nil {
				ids <- out.Verifications[0].ID
			}
		}()
	}
	wg.Wait()
	close(failures)
	close(ids)
	for e := range failures {
		mustAgent(t, e)
	}
	var first string
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatal("concurrent batch created different verification")
		}
	}
	in.Reason = "changed reason"
	if _, err = f.store.RequestProvisionSSHBatch(ctx, f.reviewer, f.project, in, key, "test"); err == nil {
		t.Fatal("idempotency input changed")
	}
	in.Reason = "explicit read-only batch after known failure"
	probe, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if probe == nil || probe.VerificationID != first || probe.Mode != "run" || probe.Command.CommandID == c.Attempt.CommandID || probe.NativeCommandID != c.Attempt.CommandID {
		t.Fatal("batch changed immutable native binding")
	}
	out, err := f.store.RequestProvisionSSHBatch(ctx, f.reviewer, f.project, in, key, "test")
	mustAgent(t, err)
	if out.Verifications[0].ID != first {
		t.Fatal("repeated accepted batch replayed consumed authorization")
	}
	if _, err = f.store.RequestProvisionSSHBatch(ctx, f.reviewer, f.project, in, platform.RandomID(), "test"); err == nil {
		t.Fatal("consumed failure authorized new work")
	}
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_verification_requested'`, f.installation.ID).Scan(&count))
	if count != 1 || f.read(t).State != "awaiting_first_boot" {
		t.Fatal("batch duplicated authorization or projected success")
	}
}
