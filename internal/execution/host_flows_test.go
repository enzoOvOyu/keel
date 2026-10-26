package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

const hostFlowSample = `api_version: keel.host/v1
inputs: {}
tasks:
  - id: wait
    type: host.wait
    depends_on: [identify]
    retry: 0
    idempotent: true
    timeout_ms: 5000
    wait_ms: 321
    resources: [device]
  - id: identify
    type: host.identity
    retry: 0
    idempotent: true
    timeout_ms: 5000
    resources: [device]
`

func TestHostFlowPublicBoundary(t *testing.T) {
	validation, err := ValidateFlow(FlowValidationInput{YAMLSource: hostFlowSample})
	if err != nil {
		t.Fatal(err)
	}
	if len(validation.TaskOrder) != 2 || validation.TaskOrder[0] != "identify" || validation.TaskOrder[1] != "wait" || validation.Warnings[0] != "bounded_readonly_host_flow_requires_explicit_native_admission" {
		t.Fatal("wrong plan or evidence boundary", validation)
	}
	for _, source := range []string{strings.Replace(hostFlowSample, "type: host.identity", "type: script.run", 1), strings.Replace(hostFlowSample, "retry: 0", "retry: 1", 1), strings.Replace(hostFlowSample, "idempotent: true", "idempotent: false", 1), strings.Replace(hostFlowSample, "resources: [device]", "resources: [machine]", 1), strings.Replace(hostFlowSample, "wait_ms: 321", "wait_ms: 31000", 1), strings.Replace(hostFlowSample, "inputs: {}", "inputs: {token: {type: string}}", 1), strings.Replace(hostFlowSample, "id: identify", "id: identify\n    depends_on: [wait]", 1), strings.Replace(hostFlowSample, "type: host.identity", "type: host.identity\n    sim: {outcome: success}", 1)} {
		if _, err := ValidateFlow(FlowValidationInput{YAMLSource: source}); err == nil {
			t.Fatal("unsupported host capability admitted", source)
		}
	}
	d := validation.Definition
	d.APIVersion = "keel.sim/v1"
	if d.Validate() == nil {
		t.Fatal("host workflow admitted as simulation")
	}
}
func publishHostFixture(t *testing.T, f *agentKernelFixture) *FlowVersion {
	t.Helper()
	flow, err := f.service.Publish(context.Background(), f.actor, f.project, "", PublishInput{Name: "Host DAG " + platform.RandomID()[:8], YAMLSource: hostFlowSample}, platform.RandomID(), "host-flow")
	mustAgent(t, err)
	return flow
}
func TestHostFlowDAGAgentAdmissionAndReceiptsPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	flow := publishHostFixture(t, f)
	in := HostFlowCreateInput{FlowVersionID: flow.ID, Transport: "agent", MachineIDs: []string{f.machine}, Concurrency: 4}
	key := platform.RandomID()
	x, err := f.service.CreateHostFlow(ctx, f.actor, f.project, in, key, "host-flow")
	mustAgent(t, err)
	again, err := f.service.CreateHostFlow(ctx, f.actor, f.project, in, key, "host-flow")
	mustAgent(t, err)
	if x.ID != again.ID || x.FlowVersionID != flow.ID || x.Snapshot.Flow.ContentHash != flow.ContentHash {
		t.Fatal("original revision/idempotency lost")
	}
	if _, err = f.service.Create(ctx, f.actor, f.project, CreateInput{Mode: "sim", FlowVersionID: flow.ID, TargetIDs: []string{f.machine}}, platform.RandomID(), "host-flow"); err == nil {
		t.Fatal("host workflow admitted through simulation")
	}
	claim := f.claim(t, x.ID)
	if claim == nil || claim.Task.ID != "identify" {
		t.Fatal("DAG ignored dependency", claim)
	}
	var locks int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND ((mode='shared' AND resource_id=$2) OR (mode='exclusive' AND resource_id=$3))`, claim.Attempt.ID, "device:machine:"+f.machine, "agent:machine:"+f.machine).Scan(&locks))
	if locks != 2 {
		t.Fatal("host DAG bypassed transport/device resources")
	}
	if f.claim(t, x.ID) != nil {
		t.Fatal("dependent task ran before original success")
	}
	mustAgent(t, f.service.RunClaim(ctx, claim))
	next := f.claim(t, x.ID)
	if next == nil || next.Task.ID != "wait" {
		t.Fatal("dependent task not scheduled")
	}
	command := sshCommand(next)
	if command.WaitMS != 321 {
		t.Fatal("per-task wait lost", command)
	}
	mustAgent(t, f.store.AgentRequestAllowed(ctx, taskrunner.Request{Action: "run", Command: command}, f.credential))
	changed := command
	changed.WaitMS++
	if f.store.AgentRequestAllowed(ctx, taskrunner.Request{Action: "run", Command: changed}, f.credential) == nil {
		t.Fatal("broker allowed altered frozen wait")
	}
	mustAgent(t, f.service.RunClaim(ctx, next))
	var state string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM exec_executions WHERE id=$1`, x.ID).Scan(&state))
	if state != "success" {
		t.Fatal("DAG not settled", state)
	}
	// New publication does not mutate the old execution snapshot or wait binding.
	_, err = f.service.Publish(ctx, f.actor, f.project, flow.FlowID, PublishInput{Name: flow.Name, YAMLSource: strings.Replace(hostFlowSample, "wait_ms: 321", "wait_ms: 654", 1), ExpectedVersion: 1}, platform.RandomID(), "host-flow")
	mustAgent(t, err)
	detail, err := f.service.Get(ctx, f.project, x.ID)
	mustAgent(t, err)
	if detail.Execution.Snapshot.Flow.ID != flow.ID {
		t.Fatal("execution revision changed")
	}
}
func TestHostFlowUnknownBlocksDependencyUntilExplicitReconcilePostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	flow := publishHostFixture(t, f)
	x, err := f.service.CreateHostFlow(ctx, f.actor, f.project, HostFlowCreateInput{FlowVersionID: flow.ID, Transport: "agent", MachineIDs: []string{f.machine}}, platform.RandomID(), "host-flow")
	mustAgent(t, err)
	claim := f.claim(t, x.ID)
	if claim == nil {
		t.Fatal("claim absent")
	}
	f.runner.mu.Lock()
	f.runner.loseRunAndQuery = true
	f.runner.mu.Unlock()
	if f.service.RunClaim(ctx, claim) == nil {
		t.Fatal("lost result reported success")
	}
	_, err = f.store.DB.ExecContext(ctx, `UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1`, claim.Attempt.ID)
	mustAgent(t, err)
	_, err = f.service.ReapExpired(ctx)
	mustAgent(t, err)
	if f.claim(t, x.ID) != nil {
		t.Fatal("unknown dependency dispatched next task")
	}
	f.runner.mu.Lock()
	f.runner.loseRunAndQuery = false
	f.runner.mu.Unlock()
	_, err = f.service.RefreshAgentReceipts(ctx, 4)
	mustAgent(t, err)
	if f.claim(t, x.ID) != nil {
		t.Fatal("query receipt implicitly released unknown")
	}
	var version int64
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state_version FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&version))
	_, err = f.service.FetchAgentReceipt(ctx, f.actor, f.project, x.ID, claim.Attempt.ID, version, platform.RandomID(), "host-flow")
	mustAgent(t, err)
	next := f.claim(t, x.ID)
	if next == nil || next.Task.ID != "wait" {
		t.Fatal("explicit reconciliation did not release dependency")
	}
	f.runner.mu.Lock()
	runs := f.runner.runCount
	f.runner.mu.Unlock()
	if runs != 1 {
		t.Fatal("original command replayed", runs)
	}
}

func TestHostFlowFailureSkipsDependentAndPauseStopsDispatchPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	flow := publishHostFixture(t, f)
	input := HostFlowCreateInput{FlowVersionID: flow.ID, Transport: "agent", MachineIDs: []string{f.machine}}
	x, err := f.service.CreateHostFlow(ctx, f.actor, f.project, input, platform.RandomID(), "host-flow")
	mustAgent(t, err)
	paused, err := f.service.Control(ctx, f.actor, f.project, x.ID, ControlInput{Action: "pause", ExpectedVersion: x.StateVersion}, platform.RandomID(), "host-flow")
	mustAgent(t, err)
	if f.claim(t, x.ID) != nil {
		t.Fatal("paused flow dispatched")
	}
	_, err = f.service.Control(ctx, f.actor, f.project, x.ID, ControlInput{Action: "resume", ExpectedVersion: paused.StateVersion}, platform.RandomID(), "host-flow")
	mustAgent(t, err)
	claim := f.claim(t, x.ID)
	if claim == nil {
		t.Fatal("resumed root missing")
	}
	now := time.Now().UTC()
	proof := taskrunner.Observation{Command: sshCommand(claim), State: "failed", StartedAt: now, FinishedAt: now, Output: map[string]any{"reason": "software-fixture-failed"}}
	mustAgent(t, f.service.recordHost(ctx, "agent", proof))
	mustAgent(t, f.service.Complete(ctx, claim))
	if f.claim(t, x.ID) != nil {
		t.Fatal("failed root dispatched dependent")
	}
	var state string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM exec_task_attempts WHERE execution_id=$1 AND task_id='wait'`, x.ID).Scan(&state))
	if state != "skipped" {
		t.Fatal("dependent did not settle", state)
	}
	input.FlowVersionID = platform.RandomID()
	if _, err = f.service.CreateHostFlow(ctx, f.actor, f.project, input, platform.RandomID(), "host-flow"); err == nil {
		t.Fatal("unknown revision accepted")
	}
}
