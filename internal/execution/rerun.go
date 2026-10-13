package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// FR-1.4 rerun semantics. A rerun re-opens a finished execution for the
// selected targets only: each selected target gets a fresh attempt chain
// (attempt numbers restart at 1 for the new chain generation), the
// execution flips back to dispatching until the new attempts settle, and
// the circuit breaker keeps guarding the rerun exactly like the original
// run. Only terminal executions can rerun; targets must belong to the
// execution's original set; failed_only selects exactly the targets whose
// newest chain failed. The rerun lineage is recorded (rerun_of/rerun_scope)
// so audits can trace which pass produced which attempts.

type RerunInput struct {
	TargetIDs  []string `json:"target_ids"`
	FailedOnly bool     `json:"failed_only"`
}

// Rerun re-opens a terminal execution for the selected targets.
func (s *Service) Rerun(ctx context.Context, a *Actor, projectID, executionID string, in RerunInput, trace string) (*Execution, error) {
	if len(in.TargetIDs) > 2000 {
		return nil, invalid("at most 2000 targets per rerun")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, projectID, "Admin", "DE", "DL", "SRE"); e != nil {
		return nil, e
	}
	x, e := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 AND project_id=$2", executionID, projectID))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, conflict("execution not found")
	}
	if e != nil {
		return nil, e
	}
	// Only terminal executions rerun; an active execution answers 409.
	switch x.State {
	case "success", "failed", "killed", "partial":
	default:
		return nil, conflict(fmt.Sprintf("execution is %s; only finished executions rerun", x.State))
	}
	// Resolve the selected target set.
	selected := map[string]bool{}
	if in.FailedOnly {
		rows, e := tx.QueryContext(ctx, `SELECT DISTINCT ON (target_id) target_id FROM exec_task_attempts
			WHERE execution_id=$1 AND state='failed' ORDER BY target_id, attempt_no DESC, created_at DESC`, executionID)
		if e != nil {
			return nil, e
		}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return nil, e
			}
			selected[id] = true
		}
		if e = rows.Err(); e != nil {
			return nil, e
		}
		if len(selected) == 0 {
			return nil, conflict("no failed targets to rerun")
		}
	} else {
		for _, id := range in.TargetIDs {
			selected[id] = true
		}
	}
	if len(selected) == 0 {
		return nil, invalid("rerun needs targets or failed_only")
	}
	// Every selected target must belong to the execution's original set.
	original := map[string]bool{}
	for _, target := range x.Snapshot.Targets {
		original[target.ID] = true
	}
	scope := make([]string, 0, len(selected))
	for id := range selected {
		if !original[id] {
			return nil, conflict("target does not belong to this execution")
		}
		scope = append(scope, id)
	}
	sortStrings(scope)
	// Fresh attempt chains for the selected targets: one row per task per
	// target, mirroring the original dispatch shape.
	now := time.Now().UTC()
	for i, task := range x.Snapshot.Flow.Definition.Tasks {
		for _, targetID := range scope {
			// Attempt numbers continue the existing chain for this target and
			// task (the unique key spans execution+target+task+attempt), so a
			// rerun is a new generation of the same chain rather than a clash.
			var next int
			if e = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(attempt_no),0)+1 FROM exec_task_attempts WHERE execution_id=$1 AND target_id=$2 AND task_id=$3",
				executionID, targetID, task.ID).Scan(&next); e != nil {
				return nil, e
			}
			if _, e = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,created_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
				platform.RandomID(), executionID, targetID, task.ID, i, next, platform.RandomID(), now); e != nil {
				return nil, e
			}
		}
	}
	scopeJSON, _ := json.Marshal(scope)
	if _, e = tx.ExecContext(ctx, `UPDATE exec_executions SET state='dispatching',desired_state='running',state_version=state_version+1,
		finished_at=NULL,rerun_of=$2,rerun_scope=$3 WHERE id=$1`, executionID, x.ID, string(scopeJSON)); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, projectID, "execution.rerun", executionID, trace, map[string]any{
		"targets": len(scope), "failed_only": in.FailedOnly,
	}); e != nil {
		return nil, e
	}
	if e = event(ctx, tx, executionID, "execution.rerun", map[string]any{
		"targets": len(scope), "failed_only": in.FailedOnly, "trace_id": trace,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	row := s.DB.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1", executionID)
	updated, e := scanExecution(row)
	if e != nil {
		return nil, e
	}
	return &updated, nil
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
