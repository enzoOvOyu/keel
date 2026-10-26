package execution

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type ScriptNonstartProofInput struct {
	ExpectedVersion int64 `json:"expected_version"`
	Confirmed       bool  `json:"confirmed"`
}

type ScriptNonstartProofRequest struct {
	ExecutionID string    `json:"execution_id"`
	AttemptID   string    `json:"attempt_id"`
	State       string    `json:"state"`
	RequestedAt time.Time `json:"requested_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// Request only journals explicit human authority. The owning worker obtains
// target proof; a separate existing Reconcile still owns resource release.
func (s *Service) RequestScriptNonstartProof(ctx context.Context, a *Actor, project, id, attemptID string, in ScriptNonstartProofInput, key, trace string) (*ScriptNonstartProofRequest, error) {
	if !in.Confirmed || in.ExpectedVersion < 1 {
		return nil, invalid("Explicit confirmation and positive expected_version are required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = authorize(ctx, tx, a, project, "Admin", "DL", "SRE"); err != nil {
		return nil, err
	}
	idem, prior, err := idempotency(ctx, tx, a, "script.nonstart:"+attemptID, key, in)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		out, err := decode[ScriptNonstartProofRequest](prior)
		return &out, err
	}
	x, err := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 AND project_id=$2 FOR UPDATE", id, project))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if x.Mode != "ssh" && x.Mode != "agent" {
		return nil, conflict("Only native script executions support non-start proof")
	}
	attempt, err := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE execution_id=$1 AND id=$2 FOR UPDATE", id, attemptID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if attempt.State != "unknown" || attempt.StateVersion != in.ExpectedVersion {
		return nil, conflict("Attempt is not unknown or its version changed")
	}
	task, err := taskByID(x.Snapshot.Flow.Definition, attempt.TaskID)
	if err != nil {
		return nil, err
	}
	if task.Script == nil || task.Type != "script.run" {
		return nil, conflict("Original approved script dispatch is required")
	}
	var target Target
	for _, candidate := range x.Snapshot.Targets {
		if candidate.ID == attempt.TargetID {
			target = candidate
		}
	}
	command, err := originalScriptCommandTx(ctx, tx, &Claim{Attempt: attempt, Target: target, Task: task, Parameters: x.Parameters})
	if err != nil {
		return nil, failure(409, "SCRIPT_DISPATCH_EVIDENCE_REQUIRED", "Original script dispatch evidence is required; resources remain quarantined")
	}
	var now time.Time
	if err = tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return nil, err
	}
	if time.UnixMicro(command.AuthorizationExpiresAt).After(now) {
		return nil, conflict("Original script authorization has not expired")
	}
	out := &ScriptNonstartProofRequest{ExecutionID: id, AttemptID: attemptID, State: "requested", RequestedAt: now, ExpiresAt: now.Add(5 * time.Minute)}
	payload := map[string]any{"attempt_id": attemptID, "attempt_version": attempt.StateVersion, "command": command, "requested_by": a.User.ID, "expires_at": out.ExpiresAt}
	if err = event(ctx, tx, id, "script.nonstart_requested", payload); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, a.User.ID, project, "script.nonstart_requested", attemptID, trace, payload); err != nil {
		return nil, err
	}
	if err = remember(ctx, tx, idem, in, out); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

// Refresh uses the existing outbox as the request journal, never another queue.
// A bounded rotating scan prevents a missing target monopolizing recovery.
func (s *Service) RefreshScriptNonstartProofs(ctx context.Context, limit int) (int, error) {
	if s.AgentRunner == nil && s.SSHRunner == nil {
		return 0, nil
	}
	if limit < 1 || limit > 16 {
		limit = 4
	}
	s.scriptProofMu.Lock()
	defer s.scriptProofMu.Unlock()
	type candidate struct {
		sequence           int64
		execution, attempt string
	}
	var candidates []candidate
	for scan := 0; scan < 2; scan++ {
		rows, err := s.DB.QueryContext(ctx, `SELECT o.sequence,o.execution_id,a.id FROM exec_outbox o JOIN exec_task_attempts a ON a.execution_id=o.execution_id AND a.id=o.payload->>'attempt_id' JOIN exec_executions x ON x.id=a.execution_id WHERE o.event_type='script.nonstart_requested' AND o.created_at>clock_timestamp()-interval '5 minutes' AND o.sequence>$1 AND a.state='unknown' AND x.mode IN('ssh','agent') AND NOT EXISTS(SELECT 1 FROM exec_runner_receipts r WHERE r.command_id=a.command_id) ORDER BY o.sequence LIMIT $2`, s.scriptProofCursor, limit)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var c candidate
			if err = rows.Scan(&c.sequence, &c.execution, &c.attempt); err != nil {
				rows.Close()
				return 0, err
			}
			candidates = append(candidates, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return 0, err
		}
		if len(candidates) > 0 {
			break
		}
		s.scriptProofCursor = 0
	}
	refreshed := 0
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return refreshed, ctx.Err()
		}
		s.scriptProofCursor = candidate.sequence
		if s.refreshScriptNonstartProof(ctx, candidate.execution, candidate.attempt) == nil {
			refreshed++
		}
	}
	return refreshed, nil
}

func (s *Service) refreshScriptNonstartProof(ctx context.Context, id, attemptID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	x, err := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1", id))
	if err != nil {
		return err
	}
	attempt, err := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE execution_id=$1 AND id=$2 AND state='unknown'", id, attemptID))
	if err != nil {
		return err
	}
	task, err := taskByID(x.Snapshot.Flow.Definition, attempt.TaskID)
	if err != nil {
		return err
	}
	if task.Script == nil {
		return taskrunner.ErrRejected
	}
	var target Target
	for _, candidate := range x.Snapshot.Targets {
		if candidate.ID == attempt.TargetID {
			target = candidate
		}
	}
	runner := s.SSHRunner
	switch x.Mode {
	case "ssh":
		err = s.sshFrozenTx(ctx, tx, target)
	case "agent":
		runner = s.AgentRunner
		err = s.agentFrozenTx(ctx, tx, target)
	default:
		return taskrunner.ErrRejected
	}
	if err != nil {
		return err
	}
	if runner == nil {
		return taskrunner.ErrUnknown
	}
	command, err := originalScriptCommandTx(ctx, tx, &Claim{Attempt: attempt, Target: target, Task: task, Parameters: x.Parameters})
	if err != nil {
		return err
	}
	allowed, err := platform.ScriptNonstartProofAllowedTx(ctx, tx, command)
	if err != nil {
		return err
	}
	if !allowed {
		return taskrunner.ErrRejected
	}
	tx.Rollback()
	work, done := context.WithTimeout(ctx, 3*time.Second)
	defer done()
	o, err := runner.Do(work, taskrunner.Request{Action: "settle", Command: command})
	if err != nil || !o.Terminal() {
		return taskrunner.ErrUnknown
	}
	if o.Validate(command) != nil {
		return taskrunner.ErrRejected
	}
	return s.recordHost(ctx, x.Mode, o)
}
