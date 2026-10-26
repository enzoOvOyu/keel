package execution

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
)

func hostMediaDefinition(media, operation string) Definition {
	return Definition{APIVersion: "keel.host/v1", Inputs: map[string]Input{}, Tasks: []Task{
		{ID: "identify", Type: "host.identity", Idempotent: true, TimeoutMS: 5000, Resources: []string{"device"}},
		{ID: "media", Type: operation, MediaID: media, DependsOn: []string{"identify"}, TimeoutMS: 30000, Resources: []string{"device"}},
		{ID: "after", Type: "host.identity", DependsOn: []string{"media"}, Idempotent: true, TimeoutMS: 5000, Resources: []string{"device"}},
	}}
}
func TestHostMediaPublicDefinitionRejectsEmbeddedAuthority(t *testing.T) {
	for _, op := range []string{"script.run", "media.stage", "media.fetch"} {
		d := hostMediaDefinition(strings.Repeat("a", 32), op)
		if err := d.Validate(); err != nil {
			t.Fatal(op, err)
		}
		d.Tasks[1].Idempotent = true
		if d.Validate() == nil {
			t.Fatal("media side effects marked idempotent")
		}
		d.Tasks[1].Idempotent = false
		d.Tasks[1].MediaID = "/customer/file"
		if d.Validate() == nil {
			t.Fatal("customer path admitted")
		}
	}
}
func TestHostFlowRetrievalFreezesApprovalAndPublishesOnlyCompleteOutputPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	body := bytes.Repeat([]byte("captured-DAG-content"), 600)
	media, _ := approvedRetrieval(t, f, body)
	definition := hostMediaDefinition(media.ID, "media.fetch")
	flow, err := f.service.Publish(ctx, f.actor, f.project, "", PublishInput{Name: "Retrieve DAG " + platform.RandomID()[:8], Definition: &definition}, platform.RandomID(), "host-media")
	mustAgent(t, err)
	input := HostFlowCreateInput{FlowVersionID: flow.ID, Transport: "agent", MachineIDs: []string{f.machine}}
	f.store.ArtifactTransfersEnabled = false
	if _, err = f.service.CreateHostFlow(ctx, f.actor, f.project, input, platform.RandomID(), "host-media"); err == nil {
		t.Fatal("flow bypassed default-off artifact switch")
	}
	f.store.ArtifactTransfersEnabled = true
	x, err := f.service.CreateHostFlow(ctx, f.actor, f.project, input, platform.RandomID(), "host-media")
	mustAgent(t, err)
	if x.Snapshot.SourceFlowContentHash != flow.ContentHash || x.Snapshot.Flow.ContentHash == flow.ContentHash || x.Snapshot.Flow.Definition.Tasks[1].Artifact == nil {
		t.Fatal("source/frozen hashes or media snapshot lost")
	}
	var raw string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT definition::text FROM exec_flow_versions WHERE id=$1`, flow.ID).Scan(&raw))
	if strings.Contains(raw, `"approved_by"`) {
		t.Fatal("published source was overwritten with approval authority")
	}
	first := f.claim(t, x.ID)
	if first == nil || first.Task.ID != "identify" {
		t.Fatal("initial query absent")
	}
	mustAgent(t, f.service.RunClaim(ctx, first))
	fetch := f.claim(t, x.ID)
	if fetch == nil || fetch.Task.ID != "media" {
		t.Fatal("retrieval dependency not released")
	}
	runner := &artifactCaptureRunner{base: f.runner, store: f.store, credential: f.credential, body: body}
	f.service.AgentRunner = runner
	mustAgent(t, f.service.RunClaim(ctx, fetch))
	var captures int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_artifact_outputs WHERE attempt_id=$1 AND execution_id=$2 AND project_id=$3 AND sha256=$4`, fetch.Attempt.ID, x.ID, f.project, media.Sha256).Scan(&captures))
	if captures != 1 {
		t.Fatal("captured output not tied to DAG attempt")
	}
	f.service.AgentRunner = f.runner
	last := f.claim(t, x.ID)
	if last == nil || last.Task.ID != "after" {
		t.Fatal("final query not released after target commit")
	}
	mustAgent(t, f.service.RunClaim(ctx, last))
}
func TestHostFlowApprovedScriptKeepsOneShotUnknownAndBlocksDependentsPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.store.MediaObjects = &agentScriptObjects{}
	f.store.AgentTasksEnabled = true
	f.service.MediaStore = f.store
	project := &platform.ProjectSummary{ID: f.project}
	media, err := f.store.IngestMedia(ctx, f.actor, project, platform.RegisterMediaInput{Type: "tool", Name: "DAG-script", Version: "v1", Arch: "noarch", OsScope: "linux", Deps: map[string]any{"task_script": platform.MediaTaskScriptProfile{Version: "keel.task-script/v1", Interpreter: "shell"}}}, bytes.NewBufferString("printf software-fixture\n"), "host-media")
	mustAgent(t, err)
	definition := hostMediaDefinition(media.ID, "script.run")
	flow, err := f.service.Publish(ctx, f.actor, f.project, "", PublishInput{Name: "Script DAG " + platform.RandomID()[:8], Definition: &definition}, platform.RandomID(), "host-media")
	mustAgent(t, err)
	in := HostFlowCreateInput{FlowVersionID: flow.ID, Transport: "agent", MachineIDs: []string{f.machine}}
	f.store.AgentScriptsEnabled = true
	if _, err = f.service.CreateHostFlow(ctx, f.actor, f.project, in, platform.RandomID(), "host-media"); err == nil {
		t.Fatal("DAG bypassed independent approval")
	}
	username, password := "DAG-reviewer-"+platform.RandomID()[:8], "isolated-DAG-reviewer-only-password"
	_, err = f.store.CreateUser(ctx, username, username, password, true, false)
	mustAgent(t, err)
	pre, _, err := f.store.NewPreSession(ctx)
	mustAgent(t, err)
	_, reviewer, err := f.store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: username, Password: password, Method: "local"}, "198.51.100.188", "host-media")
	mustAgent(t, err)
	for _, state := range []string{"verified", "approved"} {
		media, err = f.store.TransitionMedia(ctx, reviewer, project, media.ID, state, "host-media")
		mustAgent(t, err)
	}
	x, err := f.service.CreateHostFlow(ctx, f.actor, f.project, in, platform.RandomID(), "host-media")
	mustAgent(t, err)
	first := f.claim(t, x.ID)
	if first == nil {
		t.Fatal("first query absent")
	}
	mustAgent(t, f.service.RunClaim(ctx, first))
	script := f.claim(t, x.ID)
	if script == nil || script.Task.Script == nil {
		t.Fatal("approved script snapshot absent")
	}
	runner := &agentScriptLostRunner{base: f.runner, store: f.store, credential: f.credential}
	f.service.AgentRunner = runner
	if err = f.service.RunClaim(ctx, script); err == nil {
		t.Fatal("lost script result fabricated success")
	}
	if runner.admission != nil || runner.runs != 1 {
		t.Fatal("original script authority rejected or replayed", fmt.Sprint(runner.admission), runner.runs)
	}
	if f.claim(t, x.ID) != nil {
		t.Fatal("dependent released without stopped script proof")
	}
	var locks int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND mode='exclusive' AND state IN('held','reconciling')`, script.Attempt.ID).Scan(&locks))
	if locks != 2 {
		t.Fatal("script DAG lost exclusive device/channel resources")
	}
}
