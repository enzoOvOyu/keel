package agentidentity_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentclient"
	"github.com/enzoOvOyu/keel/internal/agentpki"
	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"github.com/enzoOvOyu/keel/internal/agentrunner"
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func waitTaskBinding(t *testing.T, f *identityFixture, machine string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, e := f.broker.Binding(f.project, machine); e == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("authenticated task stream did not register")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func claimAgent(t *testing.T, s *execution.Service, id string) *execution.Claim {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, e := s.PumpOutbox(ctx, "agent-test", 100)
		requireNoError(t, e)
		claim, e := s.Claim(ctx, "agent-test", 2*time.Second)
		requireNoError(t, e)
		if claim != nil {
			if claim.Attempt.ExecutionID != id {
				t.Fatal("claim escaped owned test execution")
			}
			return claim
		}
		if time.Now().After(deadline) {
			t.Fatal("existing dispatcher did not claim Agent execution")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestAgentTasksPostgresExecutionAndRecovery(t *testing.T) {
	f := newIdentityFixture(t, true)
	ctx := context.Background()
	machine, serial := f.target(t)
	_, token := f.grant(t, machine)
	dir := privateDirectory(t)
	tokenFile := filepath.Join(dir, "token")
	requireNoError(t, agentpki.WriteNew(tokenFile, []byte(token)))
	cfg := agentclient.Config{Endpoint: f.endpoint, ServerName: "127.0.0.1", ServerCA: f.caPath, AgentCA: f.caPath, ProjectID: f.project, MachineID: machine, Serial: serial, StateDirectory: dir, TokenFile: tokenFile, TasksEnabled: true}
	identity, e := agentclient.Enroll(ctx, cfg, "integration-v1")
	requireNoError(t, e)
	s := execution.New(f.store.DB)
	s.AgentRunner = f.broker
	t.Run("anonymous_task_stream_denied", func(t *testing.T) {
		work, stop := context.WithTimeout(ctx, 3*time.Second)
		defer stop()
		stream, e := f.client(t, nil).Tasks(work)
		requireNoError(t, e)
		_ = stream.Send(&pb.TaskReport{ConnectionId: platform.RandomID()})
		_, e = stream.Recv()
		requireDenied(t, e)
	})
	if _, e = s.CreateAgent(ctx, f.actor, f.project, execution.SSHCreateInput{MachineIDs: []string{machine}, Operation: "host.identity"}, platform.RandomID(), "agent-test"); e == nil {
		t.Fatal("offline Agent admitted before task handshake")
	}
	start := func() (context.CancelFunc, <-chan error) {
		work, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- agentclient.Stream(work, cfg, "integration-v1", nil) }()
		waitTaskBinding(t, f, machine)
		return stop, done
	}
	stop, done := start()
	defer func() {
		stop()
		if done == nil {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("Agent task stream did not stop")
		}
	}()
	create := func(operation string, wait int) *execution.Execution {
		t.Helper()
		x, e := s.CreateAgent(ctx, f.actor, f.project, execution.SSHCreateInput{MachineIDs: []string{machine}, Operation: operation, TimeoutMS: 10000, WaitMS: wait}, platform.RandomID(), "agent-test")
		requireNoError(t, e)
		return x
	}
	t.Run("real_readonly_terminal_receipt", func(t *testing.T) {
		x := create("host.identity", 0)
		other := agentrunner.New(f.store)
		requireNoError(t, other.ConfigureAuthority(f.authority.ID))
		if _, err := other.Binding(f.project, machine); err == nil {
			t.Fatal("another worker borrowed the live connection metadata")
		}
		otherService := execution.New(f.store.DB)
		otherService.AgentRunner = other
		_, e = otherService.PumpOutbox(ctx, "other-agent-worker", 100)
		requireNoError(t, e)
		wrongClaim, err := otherService.Claim(ctx, "other-agent-worker", 2*time.Second)
		requireNoError(t, err)
		if wrongClaim != nil {
			t.Fatal("worker without this Agent transport stole its command")
		}
		claim := claimAgent(t, s, x.ID)
		requireNoError(t, s.RunClaim(ctx, claim))
		detail, e := s.Get(ctx, f.project, x.ID)
		requireNoError(t, e)
		if detail.Execution.State != "success" {
			t.Fatalf("no successful Agent execution: %s", detail.Execution.State)
		}
		var raw []byte
		requireNoError(t, f.store.DB.QueryRow(`SELECT payload FROM exec_runner_receipts WHERE command_id=$1`, claim.Attempt.CommandID).Scan(&raw))
		var receipt execution.Receipt
		requireNoError(t, json.Unmarshal(raw, &receipt))
		observed := receipt.Output["observation"].(map[string]any)
		hostname, _ := os.Hostname()
		if receipt.Output["transport"] != "agent" || receipt.Output["hardware_evidence"] != false || observed["hostname"] != hostname {
			t.Fatal("receipt is not real bounded host evidence")
		}
		if _, e = f.broker.Do(ctx, taskrunner.Request{Action: "run", Command: taskrunner.TaskCommand{APIVersion: taskrunner.APIVersion, ProjectID: f.project, MachineID: machine, CommandID: platform.RandomID(), AttemptID: platform.RandomID(), FencingToken: 999, Operation: "host.identity", TimeoutMS: 1000}}); !errors.Is(e, taskrunner.ErrRejected) {
			t.Fatal("transport bypassed existing execution admission", e)
		}
	})
	t.Run("disconnect_quarantine_query_only_recovery", func(t *testing.T) {
		x := create("host.wait", 8000)
		claim := claimAgent(t, s, x.ID)
		running := make(chan error, 1)
		go func() { running <- s.RunClaim(ctx, claim) }()
		path := filepath.Join(dir, "tasks", claim.Attempt.CommandID, "receipt.json")
		deadline := time.Now().Add(5 * time.Second)
		for {
			raw, err := os.ReadFile(path)
			var observation taskrunner.Observation
			if err == nil && json.Unmarshal(raw, &observation) == nil && observation.State == "running" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("Agent did not persist running before disconnect")
			}
			time.Sleep(10 * time.Millisecond)
		}
		stop()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("Agent failed to persist stop on stream loss")
		}
		select {
		case e = <-running:
			if e == nil {
				t.Fatal("missing transport proof was falsely completed")
			}
		case <-time.After(7 * time.Second):
			t.Fatal("worker did not bound lost transport")
		}
		raw, e := os.ReadFile(path)
		requireNoError(t, e)
		var local taskrunner.Observation
		requireNoError(t, json.Unmarshal(raw, &local))
		if !local.Terminal() {
			t.Fatal("Agent lost local terminal proof")
		}
		// Inject only this owned attempt's lease expiry to exercise actual reaper.
		_, e = f.store.DB.Exec(`UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1`, claim.Attempt.ID)
		requireNoError(t, e)
		_, e = s.ReapExpired(ctx)
		requireNoError(t, e)
		var version int64
		var state string
		requireNoError(t, f.store.DB.QueryRow(`SELECT state,state_version FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&state, &version))
		if state != "unknown" {
			t.Fatal("lost receipt did not quarantine", state)
		}
		var held int
		requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'`, claim.Attempt.ID).Scan(&held))
		if held == 0 {
			t.Fatal("unknown resources were released")
		}
		// Recovery belongs to a currently authorized operator, even after the old
		// submitter is disabled. The worker may only query the already-owned command.
		replacement := "recover-" + platform.RandomID()[:10]
		password := "agent-recovery-fixture-only-password"
		_, e = f.store.CreateUser(ctx, replacement, replacement, password, true, false)
		requireNoError(t, e)
		requireNoError(t, f.store.SetMembership(ctx, replacement, f.project, "Admin", false))
		pre, _, e := f.store.NewPreSession(ctx)
		requireNoError(t, e)
		_, currentAdmin, e := f.store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: replacement, Password: password, Method: "local"}, "198.51.100.155", "agent-recovery")
		requireNoError(t, e)
		requireNoError(t, f.store.DisableUser(ctx, f.actor.User.Username))
		f.actor = currentAdmin
		// Existing heartbeat connection anti-thrash is one second. New stream retains
		// the same enrolled key and local receipts; it must not submit the old run.
		time.Sleep(1100 * time.Millisecond)
		stop, done = start()
		count, e := s.RefreshAgentReceipts(ctx, 4)
		requireNoError(t, e)
		if count != 1 {
			t.Fatal("reconnected Agent did not yield old durable proof", count)
		}
		_, e = s.FetchAgentReceipt(ctx, f.actor, f.project, x.ID, claim.Attempt.ID, version, platform.RandomID(), "agent-test")
		requireNoError(t, e)
		after, e := os.ReadFile(path)
		requireNoError(t, e)
		if string(after) != string(raw) {
			t.Fatal("receipt recovery re-executed the command")
		}
		requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state IN('held','reconciling')`, claim.Attempt.ID).Scan(&held))
		if held != 0 {
			t.Fatal("verified reconciliation retained lease")
		}
	})
	t.Run("credential_revocation_closes_task_authority", func(t *testing.T) {
		requireNoError(t, f.store.RevokeAgentCredential(ctx, f.project, identity.CredentialID, 1, "task fixture retired"))
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, e = f.broker.Binding(f.project, machine); e != nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("revoked Agent task binding remained active")
			}
			time.Sleep(10 * time.Millisecond)
		}
		select {
		case e = <-done:
			if status.Code(e) != codes.PermissionDenied {
				t.Fatalf("revoked stream did not fail identity: %v", e)
			}
			done = nil
		case <-time.After(5 * time.Second):
			t.Fatal("revocation did not close client tasks")
		}
	})
}

func TestAgentTaskRevocationBeforeTerminalReport(t *testing.T) {
	f := newIdentityFixture(t, true)
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	machine, serial := f.target(t)
	credential, pair := f.enroll(t, machine, serial)
	client := f.client(t, &pair)
	heartbeat, e := client.Heartbeats(ctx)
	requireNoError(t, e)
	defer heartbeat.CloseSend()
	requireNoError(t, heartbeat.Send(&pb.Heartbeat{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", BootId: platform.RandomID(), Sequence: 1}))
	ack, e := heartbeat.Recv()
	requireNoError(t, e)
	stream, e := client.Tasks(ctx)
	requireNoError(t, e)
	defer stream.CloseSend()
	requireNoError(t, stream.Send(&pb.TaskReport{ConnectionId: ack.ConnectionId}))
	_, e = stream.Recv()
	requireNoError(t, e)
	service := execution.New(f.store.DB)
	service.AgentRunner = f.broker
	x, e := service.CreateAgent(ctx, f.actor, f.project, execution.SSHCreateInput{MachineIDs: []string{machine}, Operation: "host.identity", TimeoutMS: 5000}, platform.RandomID(), "revocation-test")
	requireNoError(t, e)
	claim := claimAgent(t, service, x.ID)
	done := make(chan error, 1)
	go func() { done <- service.RunClaim(ctx, claim) }()
	frame, e := stream.Recv()
	requireNoError(t, e)
	var request taskrunner.Request
	requireNoError(t, json.Unmarshal(frame.RequestJson, &request))
	requireNoError(t, f.store.RevokeAgentCredential(ctx, f.project, credential.ID, 1, "revoke before delayed terminal report"))
	now := time.Now().UTC()
	raw, e := json.Marshal(taskrunner.Observation{Command: request.Command, State: "success", StartedAt: now, FinishedAt: now, Output: map[string]any{"fixture": "delayed revoked peer report"}})
	requireNoError(t, e)
	_ = stream.Send(&pb.TaskReport{ConnectionId: ack.ConnectionId, RequestId: frame.RequestId, ObservationJson: raw})
	_, e = stream.Recv()
	requireDenied(t, e)
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("revoked peer report completed execution")
		}
	case <-ctx.Done():
		t.Fatal("worker did not stop waiting for revoked peer")
	}
	var count int
	requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM exec_runner_receipts WHERE command_id=$1`, claim.Attempt.CommandID).Scan(&count))
	if count != 0 {
		t.Fatal("new revoked credential proof persisted")
	}
	requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state IN('held','reconciling')`, claim.Attempt.ID).Scan(&count))
	if count == 0 {
		t.Fatal("revoked proof released unknown resources")
	}
}
