package execution

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// Agent admission pins the registered public identity and asset in the same
// transaction as execution creation. A transport reconnect is never a new task.
func (s *Service) agentTargetTx(ctx context.Context, tx *sql.Tx, project, machine string) (Target, error) {
	if s.AgentRunner == nil {
		return Target{}, failure(503, "AGENT_DISABLED", "Agent task transport is not configured")
	}
	t := Target{ProjectID: project, ResourceKey: machine, Mode: "agent"}
	identity, e := (&platform.Store{DB: s.DB}).ActiveAgentIdentityTx(ctx, tx, project, machine)
	if errors.Is(e, platform.ErrAgentDenied) {
		return t, failure(409, "AGENT_IDENTITY_REQUIRED", "A current registered Agent identity for this provisioned project machine is required")
	}
	if e != nil {
		return t, e
	}
	e = tx.QueryRowContext(ctx, `SELECT hostname,version,COALESCE(model_version_id,''),created_at FROM project_assets WHERE project_id=$1 AND machine_id=$2 FOR SHARE`, project, machine).Scan(&t.Name, &t.Revision, &t.ModelVersion, &t.CreatedAt)
	if e != nil {
		return t, e
	}
	expected := platform.AgentCertificateBinding(identity)
	b, e := s.AgentRunner.Binding(project, machine)
	if e != nil {
		return t, failure(503, "AGENT_OFFLINE", "No authenticated Agent task stream is currently available")
	}
	if b != expected {
		return t, conflict("Agent transport identity does not match the registered project machine")
	}
	t.Agent = &b
	return t, nil
}
func (s *Service) agentFrozenTx(ctx context.Context, tx *sql.Tx, t Target) error {
	current, e := s.agentTargetTx(ctx, tx, t.ProjectID, t.ResourceKey)
	if e != nil {
		return e
	}
	if t.Mode != "agent" || t.Agent == nil || current.Agent == nil || *current.Agent != *t.Agent || current.Revision != t.Revision || current.ModelVersion != t.ModelVersion {
		return conflict("Agent identity or asset revision changed")
	}
	return nil
}

// A new Agent receipt and credential revocation share a database locking
// boundary. No live transport is required, so reconnect recovery still works.
func (s *Service) agentReceiptAllowedTx(ctx context.Context, tx *sql.Tx, r Receipt) error {
	var runner, mode, project, targetID string
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT a.runner,x.mode,x.project_id,a.target_id,x.snapshot FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id WHERE a.id=$1`, r.AttemptID).Scan(&runner, &mode, &project, &targetID, &raw)
	if e != nil {
		return e
	}
	if runner != "agent" && mode != "agent" {
		return nil
	}
	if runner != "agent" || mode != "agent" {
		return taskrunner.ErrRejected
	}
	snapshot, e := decode[Snapshot](raw)
	if e != nil {
		return e
	}
	for _, target := range snapshot.Targets {
		if target.ID != targetID {
			continue
		}
		identity, e := (&platform.Store{DB: s.DB}).ActiveAgentIdentityTx(ctx, tx, project, target.ResourceKey)
		if e != nil {
			return e
		}
		if target.Mode != "agent" || target.ProjectID != project || target.Agent == nil || *target.Agent != platform.AgentCertificateBinding(identity) || target.Revision != identity.AssetVersion {
			return taskrunner.ErrRejected
		}
		return nil
	}
	return taskrunner.ErrRejected
}

// CreateAgent admits only bounded host queries, then creates an ordinary
// execution consumed by the existing scheduler/outbox/lease machinery.
func (s *Service) CreateAgent(ctx context.Context, a *Actor, project string, in SSHCreateInput, key, trace string) (*Execution, error) {
	if in.Operation != "host.identity" && in.Operation != "host.wait" {
		return nil, invalid("Only host.identity and host.wait are admitted by the Agent channel")
	}
	if in.TimeoutMS == 0 {
		in.TimeoutMS = 5000
	}
	check := taskrunner.TaskCommand{APIVersion: taskrunner.APIVersion, ProjectID: project, MachineID: "00000000000000000000000000000000", CommandID: "00000000000000000000000000000000", AttemptID: "00000000000000000000000000000000", FencingToken: 1, Operation: in.Operation, TimeoutMS: in.TimeoutMS, WaitMS: in.WaitMS}
	if check.Validate() != nil || len(in.MachineIDs) < 1 || len(in.MachineIDs) > 32 {
		return nil, invalid("1..32 machine IDs and bounded Agent host queries are required")
	}
	ids := append([]string{}, in.MachineIDs...)
	sort.Strings(ids)
	in.MachineIDs = ids
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, project, "Admin", "DE", "DL", "SRE"); e != nil {
		return nil, e
	}
	idem, prior, e := idempotency(ctx, tx, a, "agent.execute:"+project, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		x, e := decode[Execution](prior)
		return &x, e
	}
	targets := []Target{}
	targetIDs := []string{}
	for i, machine := range ids {
		if !opaqueID.MatchString(machine) || (i > 0 && machine == ids[i-1]) {
			return nil, invalid("Unique valid machine IDs are required")
		}
		target, e := s.agentTargetTx(ctx, tx, project, machine)
		if e != nil {
			return nil, e
		}
		e = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key)VALUES($1,$2,'machine',$3) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), project, machine).Scan(&target.ID)
		if e != nil {
			return nil, e
		}
		targets = append(targets, target)
		targetIDs = append(targetIDs, target.ID)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	task := Task{ID: "agent-query", Type: in.Operation, Idempotent: true, Retry: 0, TimeoutMS: in.TimeoutMS, Resources: []string{"device", "agent"}}
	definition := Definition{APIVersion: "keel.agent/v1", Inputs: map[string]Input{}, Tasks: []Task{task}}
	flow := FlowVersion{ID: platform.RandomID(), FlowID: platform.RandomID(), ProjectID: project, Name: "Agent " + in.Operation, Version: 1, Definition: definition, ContentHash: hash(jsonBytes(definition)), CreatedAt: now}
	flow.Name += " " + flow.FlowID[:8]
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at)VALUES($1,$2,$3,$4,$5,$5)`, flow.FlowID, project, flow.Name, a.User.ID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at)VALUES($1,$2,$3,1,$4,$5,$6,$7)`, flow.ID, flow.FlowID, project, jsonBytes(definition), flow.ContentHash, a.User.ID, now); e != nil {
		return nil, e
	}
	params := map[string]any{"agent_wait_ms": in.WaitMS}
	x := &Execution{ID: platform.RandomID(), ProjectID: project, FlowVersionID: flow.ID, Mode: "agent", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, Concurrency: 1, CircuitBreakerPercent: 100, CreatedBy: a.User.ID, CreatedAt: now, TargetSetHash: hash(jsonBytes(targetIDs))}
	x.Snapshot = Snapshot{Mode: "agent", Flow: flow, Targets: targets, Parameters: params, TargetSetHash: x.TargetSetHash, Admission: "Bounded read-only host query through a current registered mTLS Agent and exact provisioned project asset; no hardware/G3/installation acceptance", FrozenAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at)VALUES($1,$2,$3,'agent',$4,$5,$6,1,100,$7,$8)`, x.ID, project, flow.ID, jsonBytes(params), jsonBytes(x.Snapshot), x.TargetSetHash, a.User.ID, now); e != nil {
		return nil, e
	}
	for _, target := range targets {
		if _, e = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot)VALUES($1,$2,$3)`, x.ID, target.ID, jsonBytes(target)); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at)VALUES($1,$2,$3,$4,0,1,$5,'agent',$6)`, platform.RandomID(), x.ID, target.ID, task.ID, platform.RandomID(), now); e != nil {
			return nil, e
		}
	}
	if e = event(ctx, tx, x.ID, "execution.created", map[string]any{"mode": "agent", "target_count": len(targets), "operation": in.Operation}); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, project, "execution.agent.create", x.ID, trace, map[string]any{"operation": in.Operation, "target_count": len(targets), "target_hash": x.TargetSetHash}); e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, x); e != nil {
		return nil, e
	}
	return x, tx.Commit()
}

// FetchAgentReceipt is query-only. Loss or absence of proof never releases a
// quarantined resource and never submits a second run of the old command.
func (s *Service) FetchAgentReceipt(ctx context.Context, a *Actor, project, id, attemptID string, version int64, key, trace string) (*Execution, error) {
	if version < 1 {
		return nil, invalid("expected_version must be positive")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, project, "Admin", "DL", "SRE"); e != nil {
		return nil, e
	}
	x, e := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE project_id=$1 AND id=$2", project, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if x.Mode != "agent" {
		return nil, conflict("Agent receipt recovery requires an Agent execution")
	}
	attempt, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE execution_id=$1 AND id=$2", id, attemptID))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	var recorded []byte
	e = tx.QueryRowContext(ctx, `SELECT payload FROM exec_runner_receipts WHERE command_id=$1 AND attempt_id=$2 AND fencing_token=$3`, attempt.CommandID, attempt.ID, attempt.FencingToken).Scan(&recorded)
	if e == nil {
		r, e := decode[Receipt](recorded)
		if e != nil {
			return nil, e
		}
		tx.Rollback()
		return s.Reconcile(ctx, a, project, id, attemptID, ReconcileInput{ExpectedVersion: version, Result: r.Result, Evidence: "Verified mTLS Agent terminal receipt for command " + attempt.CommandID}, key, trace)
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if attempt.State != "unknown" || attempt.StateVersion != version {
		return nil, conflict("Attempt is not unknown or its version changed")
	}
	var target Target
	for _, candidate := range x.Snapshot.Targets {
		if candidate.ID == attempt.TargetID {
			target = candidate
		}
	}
	if e = s.agentFrozenTx(ctx, tx, target); e != nil {
		return nil, e
	}
	tx.Rollback()
	task, e := taskByID(x.Snapshot.Flow.Definition, attempt.TaskID)
	if e != nil {
		return nil, e
	}
	claim := &Claim{Attempt: attempt, Target: target, Task: task, Parameters: x.Parameters}
	query, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	command := sshCommand(claim)
	observation, e := s.AgentRunner.Do(query, taskrunner.Request{Action: "query", Command: command})
	if e != nil || !observation.Terminal() {
		return nil, failure(409, "AGENT_RECEIPT_PENDING", "No terminal Agent receipt; resources remain quarantined")
	}
	if observation.Validate(command) != nil {
		return nil, taskrunner.ErrRejected
	}
	if e = s.recordHost(ctx, "agent", observation); e != nil {
		return nil, e
	}
	return s.Reconcile(ctx, a, project, id, attemptID, ReconcileInput{ExpectedVersion: version, Result: observation.State, Evidence: "Verified mTLS Agent terminal receipt for command " + attempt.CommandID}, key, trace)
}

// RefreshAgentReceipts asks only for proof of already-unknown attempts. It does
// not settle or unlock them: a currently authorized operator must reconcile the
// immutable receipt through the existing versioned control-plane transaction.
func (s *Service) RefreshAgentReceipts(ctx context.Context, limit int) (int, error) {
	if s.AgentRunner == nil {
		return 0, nil
	}
	if limit < 1 || limit > 32 {
		limit = 4
	}
	s.agentReceiptMu.Lock()
	defer s.agentReceiptMu.Unlock()
	if s.agentReceiptCursorAt.IsZero() {
		s.agentReceiptCursorAt = time.Unix(0, 0)
	}
	type candidate struct {
		attempt, execution string
		createdAt          time.Time
	}
	candidates := []candidate{}
	// An in-memory scan cursor is only fairness state, never a delivery queue.
	// It advances before each bounded query so unavailable nodes cannot starve
	// later unknown attempts, including when the caller's total budget expires.
	for scan := 0; scan < 2; scan++ {
		rows, e := s.DB.QueryContext(ctx, `SELECT a.id,a.execution_id,a.created_at FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id WHERE a.runner='agent' AND a.state='unknown' AND x.mode='agent' AND NOT EXISTS(SELECT 1 FROM exec_runner_receipts r WHERE r.command_id=a.command_id) AND (a.created_at,a.id)>($2,$3) ORDER BY a.created_at,a.id LIMIT $1`, limit, s.agentReceiptCursorAt, s.agentReceiptCursorID)
		if e != nil {
			return 0, e
		}
		for rows.Next() {
			var c candidate
			if e = rows.Scan(&c.attempt, &c.execution, &c.createdAt); e != nil {
				rows.Close()
				return 0, e
			}
			candidates = append(candidates, c)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return 0, e
		}
		if len(candidates) > 0 {
			break
		}
		s.agentReceiptCursorAt = time.Unix(0, 0)
		s.agentReceiptCursorID = ""
	}
	refreshed := 0
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return refreshed, ctx.Err()
		}
		s.agentReceiptCursorAt = candidate.createdAt
		s.agentReceiptCursorID = candidate.attempt
		if e := s.refreshAgentReceipt(ctx, candidate.execution, candidate.attempt); e != nil {
			if ctx.Err() != nil {
				return refreshed, ctx.Err()
			}
			continue
		}
		refreshed++
	}
	return refreshed, nil
}
func (s *Service) refreshAgentReceipt(ctx context.Context, executionID, attemptID string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	x, e := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 AND mode='agent'", executionID))
	if e != nil {
		return e
	}
	attempt, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE execution_id=$1 AND id=$2 AND runner='agent' AND state='unknown'", executionID, attemptID))
	if e != nil {
		return e
	}
	var target Target
	for _, candidate := range x.Snapshot.Targets {
		if candidate.ID == attempt.TargetID {
			target = candidate
		}
	}
	if e = s.agentFrozenTx(ctx, tx, target); e != nil {
		return e
	}
	tx.Rollback()
	task, e := taskByID(x.Snapshot.Flow.Definition, attempt.TaskID)
	if e != nil {
		return e
	}
	claim := &Claim{Attempt: attempt, Target: target, Task: task, Parameters: x.Parameters}
	command := sshCommand(claim)
	query, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	observation, e := s.AgentRunner.Do(query, taskrunner.Request{Action: "query", Command: command})
	if e != nil || !observation.Terminal() {
		return taskrunner.ErrUnknown
	}
	if observation.Validate(command) != nil {
		return taskrunner.ErrRejected
	}
	return s.recordHost(ctx, "agent", observation)
}
