package agentidentity_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentclient"
	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestAgentWorkerAndCLIExecutionLifecycle(t *testing.T) {
	workerBin, adminBin, agentBin := os.Getenv("KEEL_TEST_AGENT_WORKER_BIN"), os.Getenv("KEEL_TEST_AGENT_ADMIN_BIN"), os.Getenv("KEEL_TEST_AGENT_AGENT_BIN")
	if workerBin == "" || adminBin == "" || agentBin == "" {
		t.Skip("isolated worker/admin/Agent binaries absent; use check-agent-integration.py")
	}
	f := newIdentityFixture(t)
	ctx := context.Background()
	dir := privateDirectory(t)
	env := make([]string, 0, len(os.Environ()))
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "KEEL_") {
			env = append(env, entry)
		}
	}
	env = append(env, "KEEL_DATABASE_URL="+os.Getenv("KEEL_TEST_AGENT_DATABASE_URL"))
	run := func(binary string, args ...string) ([]byte, error) {
		work, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		command := exec.CommandContext(work, binary, args...)
		command.Env = env
		return command.CombinedOutput()
	}
	start := func(binary, name string, extra []string, args ...string) func() error {
		t.Helper()
		log, err := os.OpenFile(filepath.Join(dir, name+".log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		requireNoError(t, err)
		command := exec.Command(binary, args...)
		command.Env = append(append([]string{}, env...), extra...)
		command.Stdout, command.Stderr = log, log
		requireNoError(t, command.Start())
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		var once sync.Once
		var result error
		stop := func() error {
			once.Do(func() {
				_ = command.Process.Signal(syscall.SIGTERM)
				select {
				case result = <-done:
				case <-time.After(5 * time.Second):
					_ = command.Process.Kill()
					result = <-done
				}
				_ = log.Close()
			})
			return result
		}
		t.Cleanup(func() { _ = stop() })
		return stop
	}
	await := func(label string, duration time.Duration, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(duration)
		for !condition() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out: %s; own process logs: %s", label, dir)
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	requireNoError(t, err)
	endpoint := listener.Addr().String()
	requireNoError(t, listener.Close())
	config := f.config
	config.Listen = endpoint
	config.TasksEnabled = true
	raw, err := json.Marshal(config)
	requireNoError(t, err)
	workerConfig := filepath.Join(dir, "worker.json")
	requireNoError(t, agentpki.WriteNew(workerConfig, raw))
	workerID := "agent-cli-worker-" + platform.RandomID()[:12]
	workerEnv := []string{"KEEL_AGENT_WORKER_FILE=" + workerConfig, "KEEL_WORKER_ID=" + workerID, "KEEL_WORKER_CONCURRENCY=2", "KEEL_WORKER_LEASE=5s"}
	stopWorker := start(workerBin, "worker", workerEnv)
	await("real worker SQL heartbeat", 8*time.Second, func() bool {
		var ready bool
		requireNoError(t, f.store.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM exec_worker_heartbeats WHERE id=$1 AND mode='mixed' AND observed_at>now()-interval '5 seconds')`, workerID).Scan(&ready))
		return ready
	})
	// With the same occupied listen port, a healthcheck must read persistent
	// health only. An accidental second listener would fail this actual process.
	healthContext, stopHealth := context.WithTimeout(ctx, 8*time.Second)
	defer stopHealth()
	health := exec.CommandContext(healthContext, workerBin, "--healthcheck")
	health.Env = append(append([]string{}, env...), workerEnv...)
	if output, e := health.CombinedOutput(); e != nil {
		t.Fatalf("worker healthcheck tried to bind, or stale heartbeat: %s", output)
	}
	machine, serial := f.target(t)
	_, token := f.grant(t, machine)
	tokenFile := filepath.Join(dir, "enrollment.token")
	requireNoError(t, agentpki.WriteNew(tokenFile, []byte(token)))
	state := filepath.Join(dir, "agent-state")
	requireNoError(t, os.Mkdir(state, 0700))
	client := agentclient.Config{TasksEnabled: true, Endpoint: endpoint, ServerName: "127.0.0.1", ServerCA: f.caPath, AgentCA: f.caPath, ProjectID: f.project, MachineID: machine, Serial: serial, StateDirectory: state, TokenFile: tokenFile}
	raw, err = json.Marshal(client)
	requireNoError(t, err)
	clientConfig := filepath.Join(dir, "agent.json")
	requireNoError(t, agentpki.WriteNew(clientConfig, raw))
	output, err := run(agentBin, "enroll", "--config", clientConfig)
	requireNoError(t, err)
	if strings.Contains(string(output), token) || strings.Contains(string(output), "PRIVATE KEY") {
		t.Fatal("Agent enrollment leaked material")
	}
	stopAgent := start(agentBin, "agent", nil, "run", "--config", clientConfig)
	await("Agent task session on actual worker", 8*time.Second, func() bool {
		var ready bool
		requireNoError(t, f.store.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM agent_connections c JOIN agent_credentials a ON a.id=c.credential_id WHERE a.project_id=$1 AND a.machine_id=$2 AND c.disconnected_at IS NULL AND c.task_session_id IS NOT NULL AND c.task_disconnected_at IS NULL)`, f.project, machine).Scan(&ready))
		return ready
	})
	passwordFile := filepath.Join(dir, "operator.password")
	requireNoError(t, agentpki.WriteNew(passwordFile, []byte("agent-integration-only-long-password")))
	common := []string{"--username", f.actor.User.Username, "--password-file", passwordFile, "--project", f.project}
	admin := func(command string, args ...string) []byte {
		t.Helper()
		parameters := append(append([]string{command}, common...), args...)
		out, e := run(adminBin, parameters...)
		if e != nil {
			t.Fatalf("%s failed: %s", command, out)
		}
		if strings.Contains(string(out), token) || strings.Contains(string(out), "PRIVATE KEY") || strings.Contains(string(out), "agent-integration-only-long-password") {
			t.Fatal("CLI exposed secret")
		}
		return out
	}
	create := func(operation string, wait int) execution.Execution {
		t.Helper()
		args := []string{"--machine", machine, "--operation", operation, "--timeout-ms", "15000", "--idempotency-key", platform.RandomID()}
		if wait != 0 {
			args = append(args, "--wait-ms", "12000")
		}
		out := admin("agent-task-create", args...)
		var execution execution.Execution
		requireNoError(t, json.Unmarshal(out, &execution))
		if execution.Mode != "agent" || len(execution.ID) != 32 {
			t.Fatal("CLI did not create real Agent execution")
		}
		// A repeated create must return the exact original execution, not another run.
		if second := admin("agent-task-create", args...); string(second) != string(out) {
			t.Fatal("stable create retry changed execution")
		}
		return execution
	}
	identity := create("host.identity", 0)
	await("real Agent identity terminal receipt", 8*time.Second, func() bool {
		var state string
		requireNoError(t, f.store.DB.QueryRow(`SELECT state FROM exec_executions WHERE id=$1`, identity.ID).Scan(&state))
		return state == "success"
	})
	var detail execution.Detail
	requireNoError(t, json.Unmarshal(admin("agent-task-status", "--execution", identity.ID), &detail))
	if detail.Execution.State != "success" || len(detail.Attempts) != 1 || detail.Attempts[0].Receipt == nil {
		t.Fatal("CLI status has no persisted Agent terminal proof")
	}
	if detail.Attempts[0].Receipt.Output["transport"] != "agent" || detail.Attempts[0].Receipt.Output["hardware_evidence"] != false {
		t.Fatal("Agent proof provenance invalid")
	}
	waiting := create("host.wait", 12000)
	await("real Agent wait running", 8*time.Second, func() bool {
		var running bool
		requireNoError(t, f.store.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM exec_task_attempts WHERE execution_id=$1 AND state='running')`, waiting.ID).Scan(&running))
		return running
	})
	requireNoError(t, json.Unmarshal(admin("agent-task-status", "--execution", waiting.ID), &detail))
	cancelOutput := admin("agent-task-action", "--execution", waiting.ID, "--action", "cancel", "--expected-version", strconv.FormatInt(detail.Execution.StateVersion, 10), "--idempotency-key", platform.RandomID())
	if !json.Valid(cancelOutput) {
		t.Fatal("invalid public cancellation output")
	}
	await("cancelled Agent wait has terminal stopped proof", 8*time.Second, func() bool {
		var done bool
		requireNoError(t, f.store.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_runner_receipts r ON r.command_id=a.command_id AND r.attempt_id=a.id AND r.fencing_token=a.fencing_token WHERE a.execution_id=$1 AND a.state='cancelled' AND r.result='cancelled')`, waiting.ID).Scan(&done))
		return done
	})
	// Completed CLI actions must not leave live local-login sessions behind.
	var live int
	requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM sessions WHERE user_id=$1 AND revoked_at IS NULL AND id<>$2`, f.actor.User.ID, f.actor.ID).Scan(&live))
	if live != 0 {
		t.Fatal("owner CLI left authenticated sessions active")
	}
	requireNoError(t, stopAgent())
	requireNoError(t, stopWorker())
}
