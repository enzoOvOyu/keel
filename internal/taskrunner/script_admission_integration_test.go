package taskrunner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type scriptAdmissionObjects struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (o *scriptAdmissionObjects) Put(_ context.Context, key string, body io.Reader, _ int64) (string, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.values == nil {
		o.values = map[string][]byte{}
	}
	version := platform.RandomID()
	o.values[key+"/"+version] = data
	return version, nil
}
func (o *scriptAdmissionObjects) Open(_ context.Context, key, version string) (io.ReadCloser, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	data, ok := o.values[key+"/"+version]
	if !ok {
		return nil, errors.New("fixture object absent")
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func TestApprovedSSHScriptAdmissionAndQuarantine(t *testing.T) {
	f, key, _, master := keyInstallAdmission(t)
	approval := approvedInstall(t, f, key)
	installed := f.execute(approval, f.creator)
	if err := f.service.RunClaim(f.ctx, f.claim(installed.ID)); err != nil {
		t.Fatal(err)
	}
	var config map[string][]taskrunner.TargetConfig
	raw, err := os.ReadFile(f.ssh.configFile)
	if err != nil || json.Unmarshal(raw, &config) != nil {
		t.Fatal("fixture binding", err)
	}
	target := &config["targets"][0]
	target.CredentialProvider = "vault"
	target.PrivateKeyFile = ""
	target.CredentialRef = key.Ref
	target.CredentialVersion = key.Version
	target.CredentialExpiresAt = &key.ExpiresAt
	raw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f.ssh.configFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = f.store.ConfigureSSHRunner(f.ssh.configFile, master); err != nil {
		t.Fatal(err)
	}
	f.ssh.runner = f.store.SSHRunner
	f.service.SSHRunner = f.ssh.runner
	f.store.MediaObjects = &scriptAdmissionObjects{}
	f.service.MediaStore = f.store
	media, err := f.store.IngestMedia(f.ctx, f.creator, f.project, platform.RegisterMediaInput{Type: "tool", Name: "fixture-approved-script", Version: "v1", Arch: "noarch", OsScope: "linux", ManifestRef: "keel.task-script/shell/v1", Deps: map[string]any{}}, bytes.NewBufferString("printf protocol-fixture-only\n"), "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"verified", "approved"} {
		media, err = f.store.TransitionMedia(f.ctx, f.approver, f.project, media.ID, state, "test")
		if err != nil {
			t.Fatal(err)
		}
	}
	in := execution.SSHScriptCreateInput{MachineIDs: []string{f.machine}, MediaID: media.ID, TimeoutMS: 1000}
	if _, err = f.service.CreateSSHScript(f.ctx, f.creator, f.project.ID, in, platform.RandomID(), "test"); err == nil {
		t.Fatal("default-off script admitted")
	}
	f.store.SSHScriptsEnabled = true
	idem := platform.RandomID()
	x, err := f.service.CreateSSHScript(f.ctx, f.creator, f.project.ID, in, idem, "test")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.service.CreateSSHScript(f.ctx, f.creator, f.project.ID, in, idem, "test")
	if err != nil || replay.ID != x.ID {
		t.Fatal("script creation not idempotent", err)
	}
	task := x.Snapshot.Flow.Definition.Tasks[0]
	if task.Script == nil || task.Script.MediaID != media.ID || task.Script.SHA256 != media.Sha256 || task.Retry != 0 || task.Idempotent {
		t.Fatal("script snapshot/replay policy", task)
	}
	claim := f.claim(x.ID)
	// This test stops at real control-plane claiming; it never runs a script or
	// manufactures a stopped receipt. Unknown remains quarantined below.
	var mode string
	if err = f.store.DB.QueryRowContext(f.ctx, `SELECT mode FROM exec_resource_leases WHERE attempt_id=$1 AND resource_id=$2 AND state='held'`, claim.Attempt.ID, "device:machine:"+f.machine).Scan(&mode); err != nil || mode != "exclusive" {
		t.Fatal("script did not claim exclusive shared device resource", mode, err)
	}
	// The fixture helper deliberately has scripts disabled. Dispatch must still
	// commit once before IO, then remain query-only with no target execution.
	if err = f.service.RunClaim(f.ctx, claim); err == nil {
		t.Fatal("disabled target helper executed a script")
	}
	if err = f.service.RunClaim(f.ctx, claim); err == nil {
		t.Fatal("orphan script dispatch replayed")
	}
	var dispatches int
	if err = f.store.DB.QueryRowContext(f.ctx, `SELECT count(*) FROM exec_outbox WHERE execution_id=$1 AND event_type='script.dispatch_committed'`, x.ID).Scan(&dispatches); err != nil || dispatches != 1 {
		t.Fatal("dispatch identity was not consumed exactly once", dispatches, err)
	}
	readOnly, err := f.service.CreateSSH(f.ctx, f.creator, f.project.ID, execution.SSHCreateInput{MachineIDs: []string{f.machine}, Operation: "host.identity"}, platform.RandomID(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.PumpOutbox(f.ctx, "script-conflict", 100); err != nil {
		t.Fatal(err)
	}
	other, err := f.service.Claim(f.ctx, "script-conflict", 2*time.Second)
	if err != nil || other != nil {
		t.Fatal("read-only work overlapped exclusive script", readOnly.ID, other, err)
	}
	f.forceUnknown(claim)
	other, err = f.service.Claim(f.ctx, "script-unknown", 2*time.Second)
	if err != nil || other != nil {
		t.Fatal("unknown script released device quarantine", other, err)
	}
	if _, err = f.service.CreateSSH(f.ctx, f.creator, f.project.ID, execution.SSHCreateInput{MachineIDs: []string{f.machine}, Operation: "script.run", ScriptMediaID: media.ID}, platform.RandomID(), "test"); err == nil {
		t.Fatal("read-only endpoint admitted side-effecting script")
	}
}
