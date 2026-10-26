package execution

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type agentScriptObjects struct {
	key, version string
	body         []byte
}

func (o *agentScriptObjects) Put(_ context.Context, key string, r io.Reader, _ int64) (string, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	o.key, o.version, o.body = key, platform.RandomID(), body
	return o.version, nil
}
func (o *agentScriptObjects) Open(_ context.Context, key, version string) (io.ReadCloser, error) {
	if key != o.key || version != o.version {
		return nil, taskrunner.ErrRejected
	}
	return io.NopCloser(bytes.NewReader(o.body)), nil
}

// Software-only runner: deliberately loses all receipts. It exercises actual
// PostgreSQL authority/leases/one-shot dispatch, never fabricates script success.
type agentScriptLostRunner struct {
	base           *agentKernelRunner
	store          *platform.Store
	credential     *platform.AgentCredential
	request        taskrunner.Request
	runs           int
	probes         int
	probeAdmission error
	admission      error
}

func (r *agentScriptLostRunner) Binding(project, machine string) (taskrunner.Binding, error) {
	return r.base.Binding(project, machine)
}
func (r *agentScriptLostRunner) Do(ctx context.Context, request taskrunner.Request) (taskrunner.Observation, error) {
	if request.Action == "cache" {
		r.probes++
		r.probeAdmission = r.store.AgentRequestAllowed(ctx, request, r.credential)
		// Miss-only fixture: it never fabricates verified cache content or success.
		return taskrunner.Observation{}, taskrunner.ErrUnknown
	}
	if request.Action == "run" {
		r.runs++
		r.request = request
		r.admission = r.store.AgentRequestAllowed(ctx, request, r.credential)
	}
	return taskrunner.Observation{}, taskrunner.ErrUnknown
}

func TestAgentApprovedScriptPostgresAdmissionAndUnknown(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.store.MediaObjects = &agentScriptObjects{}
	f.store.AgentTasksEnabled = true
	f.service.MediaStore = f.store
	project := &platform.ProjectSummary{ID: f.project}
	media, err := f.store.IngestMedia(ctx, f.actor, project, platform.RegisterMediaInput{Type: "tool", Name: "agent-bounded-script", Version: "v1", Arch: "noarch", OsScope: "linux", Deps: map[string]any{"task_script": platform.MediaTaskScriptProfile{Version: "keel.task-script/v1", Interpreter: "shell"}}}, bytes.NewBufferString("printf software-only-fixture\n"), "agent-script-test")
	mustAgent(t, err)
	username, password := "script-reviewer-"+platform.RandomID()[:10], "isolated-script-reviewer-only-password"
	_, err = f.store.CreateUser(ctx, username, username, password, true, false)
	mustAgent(t, err)
	pre, _, err := f.store.NewPreSession(ctx)
	mustAgent(t, err)
	_, approver, err := f.store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: username, Password: password, Method: "local"}, "198.51.100.184", "agent-script-test")
	mustAgent(t, err)
	for _, state := range []string{"verified", "approved"} {
		media, err = f.store.TransitionMedia(ctx, approver, project, media.ID, state, "agent-script-test")
		mustAgent(t, err)
	}
	in := SSHScriptCreateInput{PreferCache: true, ResultFormat: "json", MachineIDs: []string{f.machine}, MediaID: media.ID, TimeoutMS: 5000}
	if _, err = f.service.CreateAgentScript(ctx, f.actor, f.project, in, platform.RandomID(), "test"); err == nil {
		t.Fatal("default-off Agent script admitted")
	}
	f.store.AgentScriptsEnabled = true
	key := platform.RandomID()
	x, err := f.service.CreateAgentScript(ctx, f.actor, f.project, in, key, "test")
	mustAgent(t, err)
	replay, err := f.service.CreateAgentScript(ctx, f.actor, f.project, in, key, "test")
	mustAgent(t, err)
	if replay.ID != x.ID || x.Snapshot.Flow.Definition.Tasks[0].Script == nil || x.Snapshot.Flow.Definition.Tasks[0].Idempotent || !x.Snapshot.Flow.Definition.Tasks[0].ScriptPreferCache {
		t.Fatal("script snapshot/idempotency lost")
	}
	if _, err = f.service.CreateAgent(ctx, f.actor, f.project, SSHCreateInput{MachineIDs: in.MachineIDs, Operation: "script.run", ScriptMediaID: media.ID}, platform.RandomID(), "test"); err == nil {
		t.Fatal("read-only endpoint accepted script")
	}
	claim := f.claim(t, x.ID)
	if claim == nil {
		t.Fatal("script was not claimed")
	}
	var exclusive int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND mode='exclusive' AND state='held' AND resource_id IN($2,$3)`, claim.Attempt.ID, "device:machine:"+f.machine, "agent:machine:"+f.machine).Scan(&exclusive))
	if exclusive != 2 {
		t.Fatal("Agent script lacks exclusive shared-device/channel claims")
	}
	runner := &agentScriptLostRunner{base: f.runner, store: f.store, credential: f.credential}
	f.service.AgentRunner = runner
	_ = f.service.RunClaim(ctx, claim)
	if runner.probes != 1 || runner.probeAdmission != nil || len(runner.request.Script) == 0 || runner.request.UseCachedScript || !runner.request.Command.ScriptCacheAllowed || runner.runs != 1 || runner.admission != nil || runner.request.Validate() != nil || runner.request.Command.ScriptResultFormat != "json" {
		t.Fatal("committed Agent dispatch rejected", runner.runs, runner.admission)
	}
	var state string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&state))
	if state != "unknown" {
		t.Fatal("lost script receipt did not retain unknown")
	}
	_ = f.service.RunClaim(ctx, claim)
	if runner.runs != 1 {
		t.Fatal("script run replayed after consumption")
	}
	f.store.AgentScriptsEnabled = false
	probe := taskrunner.Request{Action: "cache", Command: runner.request.Command}
	if err = f.store.AgentRequestAllowed(ctx, probe, f.credential); err == nil {
		t.Fatal("cache probe admitted after opt-out/unknown")
	}
	query := taskrunner.Request{Action: "query", Command: runner.request.Command}
	mustAgent(t, f.store.AgentRequestAllowed(ctx, query, f.credential))
	if err = f.store.AgentRequestAllowed(ctx, runner.request, f.credential); err == nil {
		t.Fatal("script run admitted after opt-out/unknown")
	}
	query.Command.AuthorizationExpiresAt++
	if err = f.store.AgentRequestAllowed(ctx, query, f.credential); err == nil {
		t.Fatal("query renewed original script authorization")
	}
	if competing := f.claim(t, f.create(t).ID); competing != nil {
		t.Fatal("unknown script released shared device isolation")
	}
}
