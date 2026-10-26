package execution

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func failHostFlowRoot(t *testing.T, f *agentKernelFixture, x *Execution) *Detail {
	t.Helper()
	ctx := context.Background()
	claim := f.claim(t, x.ID)
	if claim == nil {
		t.Fatal("source root absent")
	}
	now := time.Now().UTC()
	proof := taskrunner.Observation{Command: sshCommand(claim), State: "failed", StartedAt: now, FinishedAt: now, Output: map[string]any{"reason": "explicit-software-fixture-failure"}}
	mustAgent(t, f.service.recordHost(ctx, x.Mode, proof))
	mustAgent(t, f.service.Complete(ctx, claim))
	if f.claim(t, x.ID) != nil {
		t.Fatal("failed source dispatched a dependent")
	}
	parent, err := f.service.Get(ctx, f.project, x.ID)
	mustAgent(t, err)
	if parent.Execution.State != "failed" {
		t.Fatal("source did not settle", parent.Execution.State)
	}
	return parent
}

func nativeRerunRequest(preview *RerunCandidates) RerunInput {
	return RerunInput{Mode: preview.Mode, ExpectedVersion: preview.SourceVersion, SelectionHash: preview.SelectionHash, ChangeReason: "reviewed explicit new native workflow execution", ConfirmFullWorkflow: true}
}

func TestHostFlowRerunCreatesIndependentAgentDAGPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	flow := publishHostFixture(t, f)
	x, err := f.service.CreateHostFlow(ctx, f.actor, f.project, HostFlowCreateInput{FlowVersionID: flow.ID, Transport: "agent", MachineIDs: []string{f.machine}}, platform.RandomID(), "host-rerun")
	mustAgent(t, err)
	parent := failHostFlowRoot(t, f, x)
	preview, err := f.service.RerunCandidates(ctx, f.actor, f.project, x.ID, "failed", nil)
	mustAgent(t, err)
	if len(preview.Targets) != 1 || !preview.RequiresFullWorkflowConfirmation || preview.ExecutionMode != "agent" || preview.Targets[0].MachineID != f.machine || preview.Targets[0].SerialNumber == "" {
		t.Fatal("native confirmation identity missing", preview)
	}
	in := nativeRerunRequest(preview)
	in.ConfirmFullWorkflow = false
	if _, err = f.service.Rerun(ctx, f.actor, f.project, x.ID, in, platform.RandomID(), "host-rerun"); err == nil {
		t.Fatal("native rerun accepted without full DAG confirmation")
	}
	in.ConfirmFullWorkflow = true
	// Publishing a newer revision must not substitute the original source.
	_, err = f.service.Publish(ctx, f.actor, f.project, flow.FlowID, PublishInput{Name: flow.Name, YAMLSource: strings.Replace(hostFlowSample, "wait_ms: 321", "wait_ms: 654", 1), ExpectedVersion: 1}, platform.RandomID(), "host-rerun")
	mustAgent(t, err)
	key := platform.RandomID()
	child, err := f.service.Rerun(ctx, f.actor, f.project, x.ID, in, key, "host-rerun")
	mustAgent(t, err)
	again, err := f.service.Rerun(ctx, f.actor, f.project, x.ID, in, key, "host-rerun")
	mustAgent(t, err)
	if child.ID == x.ID || child.ID != again.ID || child.Mode != "agent" || child.FlowVersionID != flow.ID || child.Snapshot.RerunSource == nil || child.Snapshot.RerunSource.ExecutionID != x.ID || child.Snapshot.Flow.ContentHash != x.Snapshot.Flow.ContentHash || child.Snapshot.Targets[0].ID != x.Snapshot.Targets[0].ID {
		t.Fatal("native rerun lost immutable source lineage", child)
	}
	var seen []string
	for _, expected := range []string{"identify", "wait"} {
		claim := f.claim(t, child.ID)
		if claim == nil || claim.Task.ID != expected {
			t.Fatal("rerun did not repeat original complete DAG", expected, claim)
		}
		for _, original := range parent.Attempts {
			if original.CommandID == claim.Attempt.CommandID {
				t.Fatal("rerun reused original command authority")
			}
		}
		seen = append(seen, claim.Attempt.CommandID)
		mustAgent(t, f.service.RunClaim(ctx, claim))
	}
	if len(seen) != 2 || seen[0] == seen[1] {
		t.Fatal("new commands not independently bound")
	}
	finished, err := f.service.Get(ctx, f.project, child.ID)
	mustAgent(t, err)
	if finished.Execution.State != "success" {
		t.Fatal("rerun did not settle", finished.Execution.State)
	}
	original, err := f.service.Get(ctx, f.project, x.ID)
	mustAgent(t, err)
	if original.Execution.StateVersion != parent.Execution.StateVersion || hash(jsonBytes(original.Execution.Snapshot)) != hash(jsonBytes(parent.Execution.Snapshot)) {
		t.Fatal("rerun changed source execution")
	}
	one, err := f.service.RerunCandidates(ctx, f.actor, f.project, child.ID, "targets", []string{child.Snapshot.Targets[0].ID})
	mustAgent(t, err)
	single := nativeRerunRequest(one)
	single.TargetIDs = []string{child.Snapshot.Targets[0].ID}
	singleChild, err := f.service.Rerun(ctx, f.actor, f.project, child.ID, single, platform.RandomID(), "host-rerun")
	mustAgent(t, err)
	if len(singleChild.Snapshot.Targets) != 1 || singleChild.Snapshot.RerunSource.ExecutionID != child.ID {
		t.Fatal("single-target native lineage missing")
	}
	// Idempotent retries return the committed result even after native transport
	// is disabled. A new request must re-run current transport admission.
	f.service.AgentRunner = nil
	again, err = f.service.Rerun(ctx, f.actor, f.project, x.ID, in, key, "host-rerun")
	mustAgent(t, err)
	if again.ID != child.ID {
		t.Fatal("idempotent native retry changed execution")
	}
	if _, err = f.service.Rerun(ctx, f.actor, f.project, x.ID, in, platform.RandomID(), "host-rerun"); err == nil {
		t.Fatal("new native rerun bypassed disabled transport")
	}
	f.service.AgentRunner = f.runner
	_, err = f.store.DB.ExecContext(ctx, `UPDATE project_assets SET version=version+1 WHERE project_id=$1 AND machine_id=$2`, f.project, f.machine)
	mustAgent(t, err)
	if _, err = f.service.Rerun(ctx, f.actor, f.project, x.ID, in, platform.RandomID(), "host-rerun"); err == nil {
		t.Fatal("native rerun silently adopted a changed asset revision")
	}
	mustAgent(t, f.store.Logout(ctx, f.actor, "host-rerun-logout"))
	if _, err = f.service.Rerun(ctx, f.actor, f.project, x.ID, in, key, "host-rerun"); err == nil {
		t.Fatal("revoked actor retrieved cached native rerun")
	}
}

func TestHostFlowRerunRefusesRetiredFrozenArtifactPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	media, _ := approvedRetrieval(t, f, bytes.Repeat([]byte("approved-rerun-artifact"), 8))
	definition := hostMediaDefinition(media.ID, "media.fetch")
	flow, err := f.service.Publish(ctx, f.actor, f.project, "", PublishInput{Name: "Frozen artifact rerun " + platform.RandomID()[:8], Definition: &definition}, platform.RandomID(), "host-rerun")
	mustAgent(t, err)
	x, err := f.service.CreateHostFlow(ctx, f.actor, f.project, HostFlowCreateInput{FlowVersionID: flow.ID, Transport: "agent", MachineIDs: []string{f.machine}}, platform.RandomID(), "host-rerun")
	mustAgent(t, err)
	failHostFlowRoot(t, f, x)
	if !validNativeHostRerun(x) {
		t.Fatal("frozen internal artifact was mistaken for a public definition")
	}
	preview, err := f.service.RerunCandidates(ctx, f.actor, f.project, x.ID, "failed", nil)
	mustAgent(t, err)
	in := nativeRerunRequest(preview)
	child, err := f.service.Rerun(ctx, f.actor, f.project, x.ID, in, platform.RandomID(), "host-rerun")
	mustAgent(t, err)
	if child.Snapshot.Flow.ContentHash != x.Snapshot.Flow.ContentHash || child.Snapshot.Flow.Definition.Tasks[1].Artifact == nil {
		t.Fatal("approved artifact authority not re-frozen")
	}
	_, err = f.store.TransitionMedia(ctx, f.actor, &platform.ProjectSummary{ID: f.project}, media.ID, "retired", "host-rerun")
	mustAgent(t, err)
	if _, err = f.service.Rerun(ctx, f.actor, f.project, x.ID, in, platform.RandomID(), "host-rerun"); err == nil {
		t.Fatal("rerun admitted retired artifact")
	}
}

func TestHostFlowRerunSnapshotRejectsOwnedWorkflows(t *testing.T) {
	definition, err := (PublishInput{YAMLSource: hostFlowSample}).Parse()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"agent", "ssh"} {
		parent := &Execution{Mode: mode, Snapshot: Snapshot{Mode: mode, Flow: FlowVersion{Definition: definition, ContentHash: hash(jsonBytes(definition))}, SourceFlowContentHash: hash(jsonBytes(definition))}}
		if !validNativeHostRerun(parent) {
			t.Fatal("valid published native workflow rejected", mode)
		}
		parent.Snapshot.SourceFlowContentHash = strings.Repeat("f", 64)
		if validNativeHostRerun(parent) {
			t.Fatal("unbound native source hash accepted")
		}
	}
	for _, api := range []string{"keel.provision/v1", "keel.ssh/v1", "keel.control/v1", "keel.fabric/v1"} {
		owned := definition
		owned.APIVersion = api
		parent := &Execution{Mode: "ssh", Snapshot: Snapshot{Mode: "ssh", Flow: FlowVersion{Definition: owned, ContentHash: hash(jsonBytes(owned))}, SourceFlowContentHash: hash(jsonBytes(owned))}}
		if validNativeHostRerun(parent) {
			t.Fatal("domain-owned workflow entered generic rerun", api)
		}
	}
}
