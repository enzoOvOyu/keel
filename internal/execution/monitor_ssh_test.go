package execution

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/precollect"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func TestMonitorSSHCannotUseGenericHostAdmission(t *testing.T) {
	service := New(nil)
	if _, err := service.CreateSSH(context.Background(), nil, strings.Repeat("1", 32), SSHCreateInput{Operation: "host.monitor"}, "key", "trace"); err == nil {
		t.Fatal("generic SSH admitted monitor domain operation")
	}
	if _, err := ValidateFlow(FlowValidationInput{YAMLSource: strings.Replace(hostFlowSample, "host.identity", "host.monitor", 1)}); err == nil {
		t.Fatal("generic Flow admitted monitor snapshots")
	}
}

// A software transport fixture, never a remote connection or hardware proof.
type monitorSSHFixtureRunner struct {
	binding     taskrunner.Binding
	observation taskrunner.Observation
	raw         []byte
	runs        int
}

func (runner *monitorSSHFixtureRunner) Binding(string, string) (taskrunner.Binding, error) {
	return runner.binding, nil
}
func (runner *monitorSSHFixtureRunner) Do(_ context.Context, request taskrunner.Request) (taskrunner.Observation, error) {
	if request.Validate() != nil {
		return taskrunner.Observation{}, taskrunner.ErrRejected
	}
	if request.Action == "run" {
		runner.runs++
		started := time.Now().UTC().Truncate(time.Microsecond)
		observed := time.Now().UTC()
		snapshot := monitor.HostSnapshot{Version: monitor.HostSnapshotVersion, NetworkOnly: request.Command.MonitorNetworkOnly, StartedAt: started, CollectedAt: observed, Samples: []monitor.Sample{{Key: "link.state", Entity: "nic0", Quality: "unknown", ObservedAt: observed}, {Key: "dom.rx_power", Entity: "nic0", Quality: "unknown", ObservedAt: observed}}}
		runner.raw, _ = json.Marshal(snapshot)
		runner.observation = taskrunner.Observation{Command: request.Command, State: "success", StartedAt: started, FinishedAt: time.Now().UTC().Add(time.Microsecond), Output: map[string]any{"stopped": true, "reason": "monitor_observed", "monitor_version": monitor.HostSnapshotVersion, "monitor_sha256": precollect.Hash(runner.raw), "monitor_size": len(runner.raw)}}
	}
	if request.Command != runner.observation.Command {
		return taskrunner.Observation{}, taskrunner.ErrRejected
	}
	if request.Action == "read" {
		if request.ChunkSHA256 != precollect.Hash(runner.raw) || request.ChunkOffset >= int64(len(runner.raw)) {
			return taskrunner.Observation{}, taskrunner.ErrRejected
		}
		chunk := runner.raw[request.ChunkOffset:min(int64(len(runner.raw)), request.ChunkOffset+precollect.ChunkBytes)]
		return taskrunner.Observation{Command: request.Command, State: "accepted", Output: map[string]any{"monitor_chunk_offset": request.ChunkOffset, "monitor_size": len(runner.raw), "monitor_sha256": precollect.Hash(runner.raw), "chunk_sha256": precollect.Hash(chunk), "chunk": base64.StdEncoding.EncodeToString(chunk)}}, nil
	}
	return runner.observation, nil
}

func TestMonitorSSHOriginalSnapshotProjectionPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.store.MonitorEnabled = true
	f.store.MediaObjects = &bmcTestObjects{}
	f.service.MediaStore = f.store
	runner := &monitorSSHFixtureRunner{binding: taskrunner.Binding{HostKey: "SHA256:software-monitor-fixture", ConfigHash: strings.Repeat("a", 64)}}
	f.service.SSHRunner = runner
	_, err := f.store.DB.ExecContext(ctx, `UPDATE machines SET ssh_trust='established',ssh_trust_source='ssh',host_key_fingerprint=$2 WHERE id=$1`, f.machine, runner.binding.HostKey)
	mustAgent(t, err)
	var asset string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT id FROM project_assets WHERE project_id=$1 AND machine_id=$2`, f.project, f.machine).Scan(&asset))
	input := MonitorSSHInput{AssetID: asset, NetworkOnly: true, Reason: "Software-only original snapshot projection"}
	key := platform.RandomID()
	execution, err := f.service.CreateMonitorSSHSnapshot(ctx, f.actor, f.project, input, key, "test")
	mustAgent(t, err)
	replay, err := f.service.CreateMonitorSSHSnapshot(ctx, f.actor, f.project, input, key, "test")
	mustAgent(t, err)
	if replay.ID != execution.ID {
		t.Fatal("snapshot idempotency lost")
	}
	if _, err = f.service.CreateMonitorSSHSnapshot(ctx, f.actor, f.project, input, platform.RandomID(), "test"); err == nil {
		t.Fatal("in-flight snapshot was recollected")
	}
	claim := claimBMC(t, f, execution.ID)
	if claim == nil {
		t.Fatal("monitor snapshot not claimable")
	}
	mustAgent(t, f.service.RunClaim(ctx, claim))
	var state string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&state))
	if state != "success" {
		t.Fatal("original stopped capture not settled", state)
	}
	processed, err := f.store.TickMonitorSSHSources(ctx, 200)
	mustAgent(t, err)
	if processed != 2 {
		t.Fatal("original sample denominator lost", processed)
	}
	processed, err = f.store.TickMonitorSSHSources(ctx, 200)
	mustAgent(t, err)
	if processed != 0 || runner.runs != 1 {
		t.Fatal("projection replay recollected or duplicated")
	}
	snapshot, err := f.store.MonitorSnapshot(ctx, f.actor, f.project, asset, 0)
	mustAgent(t, err)
	if len(snapshot.Items) != 2 {
		t.Fatal("missing projected snapshot")
	}
	for _, item := range snapshot.Items {
		if item.Source != "host" || item.Method != "ssh" || !item.SnapshotOnly || item.SourceHash != precollect.Hash(runner.raw) || item.Sample.Quality != "unknown" || item.PhysicalPortID != "" {
			t.Fatal("source acquired healthy/mapped identity", item)
		}
	}
	trend, err := f.store.MonitorTrend(ctx, f.actor, f.project, asset, "link.state", "nic0", "host", "ssh", "real", "1h")
	mustAgent(t, err)
	found := false
	for _, bucket := range trend.Items {
		if bucket.Samples > 0 {
			found = true
			if !bucket.SnapshotOnly || bucket.ExpectedSamples != 0 {
				t.Fatal("snapshot fabricated continuous coverage")
			}
		}
	}
	if !found {
		t.Fatal("snapshot trend missing")
	}
	_, err = f.store.DB.ExecContext(ctx, `UPDATE monitor_ssh_captures SET raw_sha256=repeat('b',64) WHERE command_id=$1`, claim.Attempt.CommandID)
	if err == nil {
		t.Fatal("original capture mutable")
	}
}

func TestMonitorSSHSchedulerRequiresPersistentPolicyAndCurrentOwnerPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.store.MonitorEnabled = true
	f.store.MediaObjects = &bmcTestObjects{}
	f.service.MediaStore = f.store
	runner := &monitorSSHFixtureRunner{binding: taskrunner.Binding{HostKey: "SHA256:software-scheduler-fixture", ConfigHash: strings.Repeat("b", 64)}}
	f.service.SSHRunner = runner
	_, err := f.store.DB.ExecContext(ctx, `UPDATE machines SET ssh_trust='established',ssh_trust_source='ssh',host_key_fingerprint=$2 WHERE id=$1`, f.machine, runner.binding.HostKey)
	mustAgent(t, err)
	scheduled, err := f.service.TickMonitorAgentless(ctx, 8)
	mustAgent(t, err)
	if scheduled != 0 {
		t.Fatal("implicit default policy delegated SSH polling")
	}
	policy, err := f.store.SaveMonitorPolicy(ctx, f.actor, f.project, platform.MonitorPolicyInput{Policy: monitor.DefaultPolicy("M1"), Reason: "Explicit software scheduler delegation"}, platform.RandomID(), "test")
	mustAgent(t, err)
	// Background authorization comes from the persistent policy author, not an
	// invented browser session. Logging out does not erase the saved delegation.
	_, err = f.store.DB.ExecContext(ctx, `UPDATE sessions SET revoked_at=now() WHERE id=$1`, f.actor.ID)
	mustAgent(t, err)
	scheduled, err = f.service.TickMonitorAgentless(ctx, 8)
	mustAgent(t, err)
	if scheduled != 1 {
		t.Fatal("saved current policy did not schedule", scheduled)
	}
	scheduled, err = f.service.TickMonitorAgentless(ctx, 8)
	mustAgent(t, err)
	if scheduled != 0 {
		t.Fatal("pending periodic snapshot was recreated")
	}
	var networkOnly bool
	var interval int
	var policyID string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT network_only,interval_seconds,policy_id FROM monitor_ssh_requests WHERE project_id=$1 AND machine_id=$2`, f.project, f.machine).Scan(&networkOnly, &interval, &policyID))
	if !networkOnly || interval != 600 || policyID != policy.ID || runner.runs != 0 {
		t.Fatal("polling scope/policy changed or scheduler directly contacted a device")
	}
	tx, err := f.store.DB.BeginTx(ctx, nil)
	mustAgent(t, err)
	defer tx.Rollback()
	invalidActor := &Actor{User: platform.UserSummary{ID: platform.RandomID()}}
	input := SSHCreateInput{Monitor: &MonitorSSHInput{AssetID: platform.RandomID(), NetworkOnly: true}, monitorDelegation: &monitorSSHDelegation{PolicyID: policy.ID}, MachineIDs: []string{f.machine}, Operation: "host.monitor"}
	if err = monitorSSHDelegateTx(ctx, tx, invalidActor, f.project, input); err == nil {
		t.Fatal("invented owner inherited persistent policy authority")
	}
}
