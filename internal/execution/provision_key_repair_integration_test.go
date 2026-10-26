package execution

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// Software DB/admission fixture only. No SSH connection or actual key install.
func TestProvisionInstallationKeyRepairPostgres(t *testing.T) {
	ctx := context.Background()
	f, secret, _ := firstBootReadyFixture(t, true)
	reportFirstBootSSHFixture(t, f, secret)
	c := f.claim
	first, err := f.store.PrepareProvisionFirstSSH(ctx, f.project, f.installation.ID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	mustAgent(t, f.store.RecordProvisionFirstSSHRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, "verification_unknown"))
	var eventID int64
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT max(id) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_repair_required'`, f.installation.ID).Scan(&eventID))
	in := platform.ProvisionKeyRepairInput{ExpectedVersion: f.read(t).StateVersion, RepairEventID: eventID, ConfirmSerial: f.request.Snapshot.Target.Serial, Reason: "explicitly restore frozen key with independently approved temporary bootstrap"}
	if _, err = f.store.RequestProvisionKeyRepair(ctx, f.actor, f.project, f.installation.ID, in, platform.RandomID(), "test"); err == nil {
		t.Fatal("invented bootstrap credential")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	mustAgent(t, err)
	raw, err := x509.MarshalPKCS8PrivateKey(private)
	mustAgent(t, err)
	dir := t.TempDir()
	keyfile := filepath.Join(dir, "bootstrap.pem")
	mustAgent(t, os.WriteFile(keyfile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), 0600))
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	config, err := json.Marshal(map[string]any{"targets": []taskrunner.TargetConfig{{ProjectID: f.project, MachineID: f.request.MachineID, Address: "192.0.2.2:22", User: "keel-ctrl", HostKey: first.Plan.HostKey, CredentialRef: "fixture-explicit-bootstrap", CredentialVersion: "v1", CredentialExpiresAt: &expiry, PrivateKeyFile: keyfile}}})
	mustAgent(t, err)
	path := filepath.Join(dir, "targets.json")
	mustAgent(t, os.WriteFile(path, config, 0600))
	runner, err := taskrunner.Load(path)
	mustAgent(t, err)
	defer runner.Close()
	f.store.SSHRunner = runner
	idem := platform.RandomID()
	r, err := f.store.RequestProvisionKeyRepair(ctx, f.actor, f.project, f.installation.ID, in, idem, "test")
	mustAgent(t, err)
	if r.Snapshot.Credential != f.request.Snapshot.DomainKey || r.Snapshot.HostKey != first.Plan.HostKey || r.Snapshot.AttemptID != c.Attempt.ID {
		t.Fatal("repair escaped frozen installation identity")
	}
	prior, err := f.store.RequestProvisionKeyRepair(ctx, f.actor, f.project, f.installation.ID, in, idem, "test")
	mustAgent(t, err)
	if prior.ID != r.ID {
		t.Fatal("idempotency created another repair")
	}
	action := platform.ProvisionKeyRepairActionInput{ExpectedVersion: r.StateVersion, Reason: "independent exact public key and host approval"}
	if _, err = f.store.ActProvisionKeyRepair(ctx, f.actor, f.project, f.installation.ID, r.ID, "approve", action, platform.RandomID(), "test"); err == nil {
		t.Fatal("self approval accepted")
	}
	batch := platform.ProvisionKeyRepairBatchInput{Action: "approve", Items: []platform.ProvisionKeyRepairBatchItem{{InstallationID: f.installation.ID, RepairID: r.ID, ExpectedVersion: r.StateVersion}, {InstallationID: "ffffffffffffffffffffffffffffffff", RepairID: platform.RandomID(), ExpectedVersion: 1}}, Reason: "all-or-none independent repair approval"}
	if _, err = f.store.RequestProvisionKeyRepairBatch(ctx, f.reviewer, f.project, batch, platform.RandomID(), "test"); err == nil {
		t.Fatal("missing batch member accepted")
	}
	afterRollback, err := f.store.ReadProvisionInstallationProgress(ctx, f.actor, f.project, f.installation.ID)
	mustAgent(t, err)
	if afterRollback.KeyRepair == nil || afterRollback.KeyRepair.State != "pending" || afterRollback.KeyRepair.StateVersion != r.StateVersion {
		t.Fatal("partial batch approval leaked")
	}
	batch.Items = batch.Items[:1]
	batchKey := platform.RandomID()
	approved, err := f.store.RequestProvisionKeyRepairBatch(ctx, f.reviewer, f.project, batch, batchKey, "test")
	mustAgent(t, err)
	if len(approved.Requests) != 1 || approved.Requests[0].ID != r.ID || approved.Requests[0].State != "approved" {
		t.Fatal("batch approval identity changed")
	}
	r = &approved.Requests[0]
	replay, err := f.store.RequestProvisionKeyRepairBatch(ctx, f.reviewer, f.project, batch, batchKey, "test")
	mustAgent(t, err)
	if replay.Requests[0].StateVersion != r.StateVersion {
		t.Fatal("idempotent batch advanced state")
	}

	work, err := f.store.PrepareProvisionKeyRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if work == nil || work.Mode != "run" || work.Command.CommandID == c.Attempt.CommandID || work.Command.AttemptID != c.Attempt.ID || work.Command.FencingToken != c.Attempt.FencingToken || work.Command.PublicKey != f.request.Snapshot.DomainKey.PublicKey {
		t.Fatal("repair lost native fencing")
	}
	repeated, err := f.store.PrepareProvisionKeyRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if repeated != nil {
		t.Fatal("mutation automatically replayed")
	}
	forged := taskrunner.ProvisionKeyRepairProof{}
	if err = f.store.FinishProvisionKeyRepair(ctx, f.project, f.installation.ID, *work, &forged); err == nil {
		t.Fatal("forged key proof accepted")
	}
	mustAgent(t, f.store.FinishProvisionKeyRepair(ctx, f.project, f.installation.ID, *work, nil))
	progress, err := f.store.ReadProvisionInstallationProgress(ctx, f.actor, f.project, f.installation.ID)
	mustAgent(t, err)
	if progress.KeyRepair == nil || progress.KeyRepair.State != "unknown" || progress.State != "awaiting_first_boot" || !progress.Resource.MachineReserved {
		t.Fatal("uncertain mutation released reservation")
	}
	action.ExpectedVersion = progress.KeyRepair.StateVersion
	if _, err = f.store.ActProvisionKeyRepair(ctx, f.reviewer, f.project, f.installation.ID, r.ID, "revoke", action, platform.RandomID(), "test"); err == nil {
		t.Fatal("unknown mutation revoked into another run")
	}
	_, err = f.store.ActProvisionKeyRepair(ctx, f.reviewer, f.project, f.installation.ID, r.ID, "query", action, platform.RandomID(), "test")
	mustAgent(t, err)
	query, err := f.store.PrepareProvisionKeyRepair(ctx, f.project, f.installation.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, err)
	if query == nil || query.Mode != "query" || query.Command != work.Command {
		t.Fatal("recovery replaced original mutation command")
	}
}
