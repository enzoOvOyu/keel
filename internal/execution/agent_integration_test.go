package execution

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// This external Runner fixture isolates the durable execution kernel. It does
// not pretend to prove gRPC, physical-device execution or Agent installation.
// Actual mTLS/client proofs are owned by agentidentity's integration suite.
type agentKernelRunner struct {
	mu                   sync.Mutex
	binding              taskrunner.Binding
	observations         map[string]taskrunner.Observation
	runCount, queryCount int
	loseRunAndQuery      bool
	offline              bool
}

func (r *agentKernelRunner) Binding(project, machine string) (taskrunner.Binding, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.offline {
		return taskrunner.Binding{}, taskrunner.ErrUnknown
	}
	return r.binding, nil
}
func (r *agentKernelRunner) Do(ctx context.Context, request taskrunner.Request) (taskrunner.Observation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() != nil {
		return taskrunner.Observation{}, ctx.Err()
	}
	if request.Action == "run" {
		r.runCount++
		now := time.Now().UTC().Truncate(time.Microsecond)
		r.observations[request.Command.CommandID] = taskrunner.Observation{Command: request.Command, State: "success", StartedAt: now, FinishedAt: now, Output: map[string]any{"hostname": "software-agent-kernel-fixture", "reason": "fixture-completed"}}
	} else {
		r.queryCount++
	}
	if r.loseRunAndQuery {
		return taskrunner.Observation{}, taskrunner.ErrUnknown
	}
	observation, ok := r.observations[request.Command.CommandID]
	if !ok {
		return observation, taskrunner.ErrUnknown
	}
	return observation, nil
}

type agentKernelFixture struct {
	store                      *platform.Store
	service                    *Service
	actor                      *Actor
	project, machine, username string
	credential                 *platform.AgentCredential
	runner                     *agentKernelRunner
}

func mustAgent(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func agentKernel(t *testing.T) *agentKernelFixture {
	t.Helper()
	dsn := os.Getenv("KEEL_TEST_AGENT_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated Agent PostgreSQL database absent")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Port() != "15433" || !strings.HasPrefix(u.Path, "/keel_test_agent_") {
		t.Fatal("refusing non-disposable Agent database")
	}
	ctx := context.Background()
	store, e := platform.Open(ctx, dsn)
	mustAgent(t, e)
	t.Cleanup(func() { store.Close() })
	mustAgent(t, store.Migrate(ctx))
	suffix := platform.RandomID()[:10]
	username := "agent-kernel-" + suffix
	password := "agent-kernel-test-only-long-password"
	var initialized bool
	mustAgent(t, store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE is_admin)`).Scan(&initialized))
	_, e = store.CreateUser(ctx, username, username, password, true, !initialized)
	mustAgent(t, e)
	pre, _, e := store.NewPreSession(ctx)
	mustAgent(t, e)
	_, actor, e := store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: username, Password: password, Method: "local"}, "198.51.100.183", "agent-kernel")
	mustAgent(t, e)
	project, e := store.CreateProject(ctx, actor, platform.CreateProjectInput{Name: "Agent kernel " + suffix, Slug: "agent-kernel-" + suffix}, platform.RandomID(), "agent-kernel")
	mustAgent(t, e)
	machine := platform.RandomID()
	serial := "SOFTWARE-AGENT-KERNEL-" + suffix
	_, e = store.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,created_at,updated_at)VALUES($1,$2,now(),now())`, machine, serial)
	mustAgent(t, e)
	_, e = store.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at)VALUES($1,$2,$3,$4,'provisioned',now(),now())`, platform.RandomID(), project.ID, machine, "agent-kernel-"+suffix)
	mustAgent(t, e)
	f := &agentKernelFixture{store: store, service: New(store.DB), actor: actor, project: project.ID, machine: machine, username: username}
	f.enroll(t)
	return f
}

func (f *agentKernelFixture) enroll(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	var serial string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT serial_number FROM machines WHERE id=$1", f.machine).Scan(&serial))
	directory := t.TempDir()
	mustAgent(t, os.Chmod(directory, 0700))
	ca, key, e := agentpki.NewAuthority(time.Now())
	mustAgent(t, e)
	caFile, keyFile := filepath.Join(directory, "ca.pem"), filepath.Join(directory, "ca-key.pem")
	mustAgent(t, agentpki.WriteNew(caFile, ca))
	mustAgent(t, agentpki.WriteNew(keyFile, key))
	authority, e := agentpki.Load(caFile, keyFile)
	mustAgent(t, e)
	_, token, e := f.store.IssueAgentEnrollment(ctx, f.project, f.machine, authority.ID, "isolated software execution kernel fixture", time.Minute)
	mustAgent(t, e)
	_, clientKey, e := ed25519.GenerateKey(rand.Reader)
	mustAgent(t, e)
	der, e := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, clientKey)
	mustAgent(t, e)
	credential, e := f.store.EnrollAgent(ctx, authority, f.project, f.machine, serial, "kernel-fixture-v1", token, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	mustAgent(t, e)
	f.credential = credential
	f.runner = &agentKernelRunner{binding: platform.AgentCertificateBinding(credential), observations: map[string]taskrunner.Observation{}}
	f.service.AgentRunner = f.runner
}
func (f *agentKernelFixture) create(t *testing.T) *Execution {
	t.Helper()
	out, e := f.service.CreateAgent(context.Background(), f.actor, f.project, SSHCreateInput{MachineIDs: []string{f.machine}, Operation: "host.identity"}, platform.RandomID(), "agent-kernel")
	mustAgent(t, e)
	return out
}
func (f *agentKernelFixture) claim(t *testing.T, executionID string) *Claim {
	t.Helper()
	ctx := context.Background()
	_, e := f.service.PumpOutbox(ctx, "agent-kernel", 1000)
	mustAgent(t, e)
	// Claim the fixture's own execution while retaining all normal claim locks.
	claim, e := f.service.claimExecution(ctx, executionID, "agent-kernel", time.Second)
	mustAgent(t, e)
	return claim
}
func TestAgentExecutionKernelPostgres(t *testing.T) {
	ctx := context.Background()
	t.Run("admission_idempotency_receipt_and_shared_device_key", func(t *testing.T) {
		f := agentKernel(t)
		in := SSHCreateInput{MachineIDs: []string{f.machine}, Operation: "host.identity"}
		key := platform.RandomID()
		first, e := f.service.CreateAgent(ctx, f.actor, f.project, in, key, "agent-kernel")
		mustAgent(t, e)
		second, e := f.service.CreateAgent(ctx, f.actor, f.project, in, key, "agent-kernel")
		mustAgent(t, e)
		if first.ID != second.ID || first.Mode != "agent" || first.Snapshot.Targets[0].Agent == nil {
			t.Fatal("Agent admission lost immutable identity or idempotency")
		}
		for _, bad := range []SSHCreateInput{{MachineIDs: []string{f.machine}, Operation: "trust.verify"}, {MachineIDs: []string{f.machine}, Operation: "trust.install"}, {MachineIDs: []string{f.machine, f.machine}, Operation: "host.identity"}, {MachineIDs: []string{f.machine}, Operation: "host.wait", WaitMS: 30001}, {MachineIDs: []string{f.machine}, Operation: "host.identity", TimeoutMS: 30001}} {
			if _, e = f.service.CreateAgent(ctx, f.actor, f.project, bad, platform.RandomID(), "agent-kernel"); e == nil {
				t.Fatal("unsafe Agent task admitted")
			}
		}
		if _, e = f.service.CreateAgent(ctx, nil, f.project, in, platform.RandomID(), "agent-kernel"); e == nil {
			t.Fatal("unauthenticated Agent task admitted")
		}
		other, e := f.store.CreateProject(ctx, f.actor, platform.CreateProjectInput{Name: "Other Agent project", Slug: "agent-other-" + platform.RandomID()[:8]}, platform.RandomID(), "agent-kernel")
		mustAgent(t, e)
		if _, e = f.service.CreateAgent(ctx, f.actor, other.ID, in, platform.RandomID(), "agent-kernel"); e == nil {
			t.Fatal("cross-project machine admitted")
		}
		claim := f.claim(t, first.ID)
		if claim == nil || claim.Target.Mode != "agent" {
			t.Fatal("Agent attempt not claimed")
		}
		var count int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND ((mode='shared' AND resource_id=$2) OR (mode='exclusive' AND resource_id=$3))`, claim.Attempt.ID, "device:machine:"+f.machine, "agent:machine:"+f.machine).Scan(&count))
		if count != 2 {
			t.Fatal("Agent did not share the SSH machine resource namespace")
		}
		blocked := f.create(t)
		if competing := f.claim(t, blocked.ID); competing != nil {
			t.Fatal("same-machine Agent runs overlapped despite exclusive Agent channel lease")
		}
		mustAgent(t, f.service.RunClaim(ctx, claim))
		var state string
		var raw string
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&state))
		if state != "success" {
			t.Fatal("terminal Agent receipt not applied")
		}
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT payload::text FROM exec_runner_receipts WHERE command_id=$1`, claim.Attempt.CommandID).Scan(&raw))
		if !strings.Contains(raw, `"transport": "agent"`) || !strings.Contains(raw, `"hardware_evidence": false`) {
			t.Fatal("Agent receipt lost source/evidence boundaries")
		}
		f.runner.mu.Lock()
		runs := f.runner.runCount
		f.runner.mu.Unlock()
		if runs != 1 {
			t.Fatal("Agent run replayed")
		}
	})
	t.Run("revocation_pauses_before_dispatch", func(t *testing.T) {
		f := agentKernel(t)
		execution := f.create(t)
		mustAgent(t, f.store.RevokeAgentCredential(ctx, f.project, f.credential.ID, 1, "fixture retirement"))
		if claim := f.claim(t, execution.ID); claim != nil {
			t.Fatal("revoked Agent was dispatched")
		}
		var desired string
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT desired_state FROM exec_executions WHERE id=$1`, execution.ID).Scan(&desired))
		if desired != "paused" {
			t.Fatal("revoked admission did not pause")
		}
	})
	t.Run("revocation_before_new_receipt_preserves_lease", func(t *testing.T) {
		f := agentKernel(t)
		claim := f.claim(t, f.create(t).ID)
		if claim == nil {
			t.Fatal("claim missing")
		}
		proof, e := f.runner.Do(ctx, taskrunner.Request{Action: "run", Command: sshCommand(claim)})
		mustAgent(t, e)
		mustAgent(t, f.store.RevokeAgentCredential(ctx, f.project, f.credential.ID, 1, "revoked before terminal proof persistence"))
		if e = f.service.recordHost(ctx, "agent", proof); e == nil {
			t.Fatal("new proof accepted after credential revocation")
		}
		if e = f.service.Complete(ctx, claim); e == nil {
			t.Fatal("missing proof released revoked Agent task")
		}
		var receipts, leases int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_runner_receipts WHERE command_id=$1`, claim.Attempt.CommandID).Scan(&receipts))
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='held'`, claim.Attempt.ID).Scan(&leases))
		if receipts != 0 || leases != 2 {
			t.Fatal("revoked new proof was stored or resource isolation was lost")
		}
	})
	t.Run("durable_receipt_survives_later_revocation", func(t *testing.T) {
		f := agentKernel(t)
		claim := f.claim(t, f.create(t).ID)
		if claim == nil {
			t.Fatal("claim missing")
		}
		proof, e := f.runner.Do(ctx, taskrunner.Request{Action: "run", Command: sshCommand(claim)})
		mustAgent(t, e)
		mustAgent(t, f.service.recordHost(ctx, "agent", proof))
		mustAgent(t, f.store.RevokeAgentCredential(ctx, f.project, f.credential.ID, 1, "revoked after durable proof"))
		mustAgent(t, f.service.recordHost(ctx, "agent", proof))
		mustAgent(t, f.service.Complete(ctx, claim))
		var state string
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&state))
		if state != "success" {
			t.Fatal("later revocation invalidated durable historical proof")
		}
	})
	t.Run("transport_gap_keeps_same_pending_command", func(t *testing.T) {
		f := agentKernel(t)
		execution := f.create(t)
		f.runner.mu.Lock()
		f.runner.offline = true
		f.runner.mu.Unlock()
		if claim := f.claim(t, execution.ID); claim != nil {
			t.Fatal("offline Agent claimed")
		}
		var state, desired string
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT a.state,x.desired_state FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id WHERE x.id=$1`, execution.ID).Scan(&state, &desired))
		if state != "pending" || desired != "running" {
			t.Fatal("transport gap changed pending attempt")
		}
		f.runner.mu.Lock()
		f.runner.offline = false
		f.runner.mu.Unlock()
		claim := f.claim(t, execution.ID)
		if claim == nil {
			t.Fatal("transport recovery failed to claim original attempt")
		}
		mustAgent(t, f.service.RunClaim(ctx, claim))
	})
	t.Run("current_submitter_required_at_last_dispatch_boundary", func(t *testing.T) {
		f := agentKernel(t)
		execution := f.create(t)
		claim := f.claim(t, execution.ID)
		if claim == nil {
			t.Fatal("claim missing")
		}
		replacement := "agent-replacement-" + platform.RandomID()[:8]
		_, e := f.store.CreateUser(ctx, replacement, "Replacement admin", "agent-kernel-replacement-password", true, false)
		mustAgent(t, e)
		mustAgent(t, f.store.SetMembership(ctx, replacement, f.project, "Admin", false))
		mustAgent(t, f.store.DisableUser(ctx, f.username))
		if e = f.service.RunClaim(ctx, claim); e == nil {
			t.Fatal("disabled creator dispatched Agent work")
		}
		f.runner.mu.Lock()
		runs := f.runner.runCount
		f.runner.mu.Unlock()
		if runs != 0 {
			t.Fatal("remote run after authority revocation")
		}
	})
	t.Run("unknown_quarantine_query_only_refresh_and_operator_reconcile", func(t *testing.T) {
		f := agentKernel(t)
		execution := f.create(t)
		claim := f.claim(t, execution.ID)
		if claim == nil {
			t.Fatal("claim missing")
		}
		f.runner.mu.Lock()
		f.runner.loseRunAndQuery = true
		f.runner.mu.Unlock()
		if e := f.service.RunClaim(ctx, claim); !errors.Is(e, taskrunner.ErrUnknown) {
			t.Fatal("response loss fabricated terminal proof", e)
		}
		_, e := f.store.DB.ExecContext(ctx, `UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1`, claim.Attempt.ID)
		mustAgent(t, e)
		_, e = f.service.ReapExpired(ctx)
		mustAgent(t, e)
		blocked := f.create(t)
		if next := f.claim(t, blocked.ID); next != nil {
			t.Fatal("unknown Agent attempt did not quarantine common machine resource")
		}
		f.runner.mu.Lock()
		f.runner.loseRunAndQuery = false
		f.runner.mu.Unlock()
		refreshed, e := f.service.RefreshAgentReceipts(ctx, 4)
		mustAgent(t, e)
		if refreshed != 1 {
			t.Fatal("missing query-only receipt refresh", refreshed)
		}
		var state string
		var version int64
		var held int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state,state_version FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&state, &version))
		if state != "unknown" {
			t.Fatal("automatic refresh resolved unknown without operator")
		}
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'`, claim.Attempt.ID).Scan(&held))
		if held != 2 {
			t.Fatal("automatic receipt refresh released quarantine")
		}
		if _, e = f.service.FetchAgentReceipt(ctx, f.actor, f.project, execution.ID, claim.Attempt.ID, version-1, platform.RandomID(), "agent-kernel"); e == nil {
			t.Fatal("stale reconciliation accepted")
		}
		_, e = f.service.FetchAgentReceipt(ctx, f.actor, f.project, execution.ID, claim.Attempt.ID, version, platform.RandomID(), "agent-kernel")
		mustAgent(t, e)
		mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&state))
		if state != "success" {
			t.Fatal("verified receipt did not settle unknown")
		}
		f.runner.mu.Lock()
		runs, queries := f.runner.runCount, f.runner.queryCount
		f.runner.mu.Unlock()
		if runs != 1 || queries < 2 {
			t.Fatal("recovery resubmitted run or omitted query")
		}
	})
}
