package execution

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func TestAgentArtifactApprovalFrozenDispatchAndUnknown(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.store.MediaObjects = &agentScriptObjects{}
	f.store.AgentTasksEnabled = true
	f.service.MediaStore = f.store
	project := &platform.ProjectSummary{ID: f.project}
	media, err := f.store.IngestMedia(ctx, f.actor, project, platform.RegisterMediaInput{Type: "tool", Name: "approved-artifact", Version: "v1", Arch: "noarch", OsScope: "linux"}, bytes.NewBufferString("non-executable distribution fixture"), "artifact-test")
	mustAgent(t, err)
	in := ArtifactCreateInput{MachineIDs: []string{f.machine}, MediaID: media.ID, Transport: "agent", TimeoutMS: 1000}
	if _, err = f.service.CreateArtifactDistribution(ctx, f.actor, f.project, in, platform.RandomID(), "test"); err == nil {
		t.Fatal("default-off distribution admitted")
	}
	f.store.ArtifactTransfersEnabled = true
	if _, err = f.service.CreateArtifactDistribution(ctx, f.actor, f.project, in, platform.RandomID(), "test"); err == nil {
		t.Fatal("unapproved media admitted")
	}
	username, password := "artifact-reviewer-"+platform.RandomID()[:10], "isolated-artifact-reviewer-only-password"
	_, err = f.store.CreateUser(ctx, username, username, password, true, false)
	mustAgent(t, err)
	pre, _, err := f.store.NewPreSession(ctx)
	mustAgent(t, err)
	_, approver, err := f.store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: username, Password: password, Method: "local"}, "198.51.100.184", "artifact-test")
	mustAgent(t, err)
	for _, state := range []string{"verified", "approved"} {
		media, err = f.store.TransitionMedia(ctx, approver, project, media.ID, state, "artifact-test")
		mustAgent(t, err)
	}
	key := platform.RandomID()
	x, err := f.service.CreateArtifactDistribution(ctx, f.actor, f.project, in, key, "test")
	mustAgent(t, err)
	replay, err := f.service.CreateArtifactDistribution(ctx, f.actor, f.project, in, key, "test")
	mustAgent(t, err)
	task := x.Snapshot.Flow.Definition.Tasks[0]
	if replay.ID != x.ID || task.Artifact == nil || task.Artifact.SHA256 != media.Sha256 || task.Type != "media.stage" || task.Idempotent || task.Retry != 0 || task.Script != nil {
		t.Fatal("distribution freeze/idempotency lost")
	}
	if _, err = f.service.CreateAgent(ctx, f.actor, f.project, SSHCreateInput{MachineIDs: in.MachineIDs, Operation: "media.stage", ArtifactMediaID: media.ID, TimeoutMS: 1000}, platform.RandomID(), "test"); err == nil {
		t.Fatal("read-only endpoint accepted distribution")
	}
	claim := f.claim(t, x.ID)
	if claim == nil {
		t.Fatal("distribution not claimed")
	}
	var exclusive int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND mode='exclusive' AND state='held' AND resource_id IN($2,$3)`, claim.Attempt.ID, "device:machine:"+f.machine, "agent:machine:"+f.machine).Scan(&exclusive))
	if exclusive != 2 {
		t.Fatal("distribution lacks exclusive device/channel claims")
	}
	runner := &agentScriptLostRunner{base: f.runner, store: f.store, credential: f.credential}
	f.service.AgentRunner = runner
	_ = f.service.RunClaim(ctx, claim)
	if runner.runs != 1 || runner.admission != nil || runner.request.Command.Operation != "media.stage" || len(runner.request.Script) != 0 || len(runner.request.Chunk) != 0 || runner.request.Validate() != nil {
		t.Fatal("frozen dispatch rejected", runner.admission)
	}
	var state string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&state))
	if state != "unknown" {
		t.Fatal("lost proof released distribution", state)
	}
	_ = f.service.RunClaim(ctx, claim)
	if runner.runs != 1 {
		t.Fatal("consumed artifact run replayed")
	}
	f.store.ArtifactTransfersEnabled = false
	query := taskrunner.Request{Action: "query", Command: runner.request.Command}
	mustAgent(t, f.store.AgentRequestAllowed(ctx, query, f.credential))
	query.Command.ArtifactSHA256 = media.Sha256[:63] + "x"
	if err = f.store.AgentRequestAllowed(ctx, query, f.credential); err == nil {
		t.Fatal("changed artifact accepted")
	}
	query = taskrunner.Request{Action: "query", Command: runner.request.Command}
	query.Command.AuthorizationExpiresAt++
	if err = f.store.AgentRequestAllowed(ctx, query, f.credential); err == nil {
		t.Fatal("renewed original authority accepted")
	}
	settle := taskrunner.Request{Action: "settle", Command: runner.request.Command}
	if remaining := time.Until(time.UnixMicro(settle.Command.AuthorizationExpiresAt)); remaining > 0 {
		time.Sleep(remaining + 10*time.Millisecond)
	}
	if err = f.store.AgentRequestAllowed(ctx, settle, f.credential); err == nil {
		t.Fatal("artifact non-start proof lacked human request")
	}
	current, err := scanAttempt(f.store.DB.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1", claim.Attempt.ID))
	mustAgent(t, err)
	input := ScriptNonstartProofInput{ExpectedVersion: current.StateVersion, Confirmed: true}
	proofKey := platform.RandomID()
	if _, err = f.service.RequestScriptNonstartProof(ctx, f.actor, f.project, x.ID, current.ID, input, platform.RandomID(), "test"); err == nil {
		t.Fatal("script-only proof endpoint accepted artifact")
	}
	requested, err := f.service.RequestArtifactNonstartProof(ctx, f.actor, f.project, x.ID, current.ID, input, proofKey, "test")
	mustAgent(t, err)
	replayed, err := f.service.RequestArtifactNonstartProof(ctx, f.actor, f.project, x.ID, current.ID, input, proofKey, "test")
	mustAgent(t, err)
	if !requested.ExpiresAt.Equal(replayed.ExpiresAt) {
		t.Fatal("artifact proof idempotency lost")
	}
	mustAgent(t, f.store.AgentRequestAllowed(ctx, settle, f.credential))
	refreshed, err := f.service.RefreshScriptNonstartProofs(ctx, 4)
	mustAgent(t, err)
	if refreshed != 0 || runner.runs != 1 {
		t.Fatal("request fabricated proof or restarted transfer")
	}
	var retained int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'`, current.ID).Scan(&retained))
	if retained != 2 {
		t.Fatal("request without proof released device")
	}
	_, err = f.store.DB.ExecContext(ctx, `UPDATE users SET enabled=false WHERE id=$1`, f.actor.User.ID)
	mustAgent(t, err)
	if err = f.store.AgentRequestAllowed(ctx, settle, f.credential); err == nil {
		t.Fatal("revoked requester retained artifact recovery authority")
	}

}
