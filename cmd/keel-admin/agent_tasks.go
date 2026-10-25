package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentrunner"
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
)

// These owner-side operations still use a real local login and the kernel's
// current-session authorization. Database ownership is not an actor identity.
func runAgentTasks(command string, args []string) error {
	f := flag.NewFlagSet(command, flag.ContinueOnError)
	f.SetOutput(os.Stderr)
	username := f.String("username", "", "enabled local platform administrator")
	passwordFile := f.String("password-file", "", "owner-only local-login password file; otherwise terminal prompt")
	project := f.String("project", "", "project ID")
	machine := f.String("machine", "", "comma-separated existing machine IDs, maximum32")
	operation := f.String("operation", "host.identity", "host.identity or host.wait")
	timeout := f.Int("timeout-ms", 5000, "bounded task timeout, 100..30000ms")
	wait := f.Int("wait-ms", 0, "host.wait duration, 0..30000ms")
	id := f.String("execution", "", "existing Agent execution ID")
	action := f.String("action", "", "pause, resume or cancel")
	expected := f.Int64("expected-version", 0, "current execution version; attempt version for reconcile")
	attempt := f.String("attempt", "", "unknown Agent attempt ID")
	key := f.String("idempotency-key", "", "stable explicit key reused only for the same operation")
	if e := f.Parse(args); e != nil {
		return e
	}
	if f.NArg() != 0 {
		return errors.New("unexpected Agent task positional arguments")
	}
	common := map[string]bool{"username": true, "password-file": true, "project": true}
	allowed := map[string]map[string]bool{
		"agent-task-create":    {"machine": true, "operation": true, "timeout-ms": true, "wait-ms": true, "idempotency-key": true},
		"agent-task-status":    {"execution": true},
		"agent-task-action":    {"execution": true, "action": true, "expected-version": true, "idempotency-key": true},
		"agent-task-reconcile": {"execution": true, "attempt": true, "expected-version": true, "idempotency-key": true},
	}
	fields, known := allowed[command]
	if !known {
		return errors.New("unsupported Agent task command")
	}
	invalid := false
	f.Visit(func(x *flag.Flag) {
		if !common[x.Name] && !fields[x.Name] {
			invalid = true
		}
	})
	if invalid {
		return errors.New("unsupported Agent task flag")
	}
	if strings.TrimSpace(*username) == "" || *project == "" {
		return errors.New("--username and --project are required")
	}
	if command != "agent-task-status" && strings.TrimSpace(*key) == "" {
		return errors.New("--idempotency-key is required; reuse it only for the same operation")
	}
	if command != "agent-task-create" && *id == "" {
		return errors.New("--execution is required")
	}
	if command == "agent-task-create" && *machine == "" {
		return errors.New("--machine is required")
	}
	if command == "agent-task-action" && (*expected < 1 || (*action != "pause" && *action != "resume" && *action != "cancel")) {
		return errors.New("--action pause|resume|cancel and positive --expected-version are required")
	}
	if command == "agent-task-reconcile" && (*expected < 1 || *attempt == "") {
		return errors.New("--attempt and positive --expected-version are required")
	}
	dsn := os.Getenv("KEEL_DATABASE_URL")
	if dsn == "" {
		return errors.New("KEEL_DATABASE_URL is required")
	}
	password, e := readPassword(*passwordFile)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, e := platform.Open(ctx, dsn)
	if e != nil {
		return e
	}
	defer store.Close()
	if e = store.CheckMigrations(ctx); e != nil {
		return e
	}
	pre, _, e := store.NewPreSession(ctx)
	if e != nil {
		return e
	}
	trace := fmt.Sprintf("agent-cli:uid%d", os.Geteuid())
	_, actor, e := store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: *username, Password: password, Method: "local"}, "127.0.0.1", trace)
	password = ""
	if e != nil {
		return e
	}
	defer func() {
		logout, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		_ = store.Logout(logout, actor, trace)
	}()
	if !actor.User.IsAdmin {
		return platform.ErrForbidden
	}
	service := execution.New(store.DB)
	// Binding reads registered task-session metadata; this CLI never owns an
	// Agent transport, sends a remote command, or acts as another dispatcher.
	service.AgentRunner = agentrunner.New(store)
	var result any
	switch command {
	case "agent-task-create":
		result, e = service.CreateAgent(ctx, actor, *project, execution.SSHCreateInput{MachineIDs: strings.Split(*machine, ","), Operation: *operation, TimeoutMS: *timeout, WaitMS: *wait}, *key, trace)
	case "agent-task-status":
		// Hold the current-account/session lock until the scoped read completes.
		tx, err := store.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if e = platform.RecheckSessionTx(ctx, tx, actor); e != nil {
			return e
		}
		var detail *execution.Detail
		detail, e = service.Get(ctx, *project, *id)
		if e == nil && detail.Execution.Mode != "agent" {
			return errors.New("Agent execution required")
		}
		if e == nil {
			e = tx.Commit()
		}
		result = detail
	case "agent-task-action":
		detail, err := service.Get(ctx, *project, *id)
		if err != nil {
			return err
		}
		if detail.Execution.Mode != "agent" {
			return errors.New("Agent execution required")
		}
		result, e = service.Control(ctx, actor, *project, *id, execution.ControlInput{Action: *action, ExpectedVersion: *expected}, *key, trace)
	case "agent-task-reconcile":
		// Only the worker may query the connected Agent. The operator consumes an
		// already persisted terminal receipt; missing proof never releases leases.
		var raw []byte
		e = store.DB.QueryRowContext(ctx, `SELECT r.payload FROM exec_runner_receipts r JOIN exec_task_attempts a ON a.command_id=r.command_id AND a.id=r.attempt_id AND a.fencing_token=r.fencing_token JOIN exec_executions x ON x.id=a.execution_id WHERE x.project_id=$1 AND x.id=$2 AND x.mode='agent' AND a.id=$3`, *project, *id, *attempt).Scan(&raw)
		if e != nil {
			return errors.New("persisted Agent terminal receipt unavailable; resources remain quarantined")
		}
		var receipt execution.Receipt
		if json.Unmarshal(raw, &receipt) != nil {
			return errors.New("persisted Agent receipt is invalid")
		}
		result, e = service.Reconcile(ctx, actor, *project, *id, *attempt, execution.ReconcileInput{ExpectedVersion: *expected, Result: receipt.Result, Evidence: "Persisted mTLS Agent terminal receipt for command " + receipt.CommandID}, *key, trace)
	}
	if e != nil {
		return e
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}
