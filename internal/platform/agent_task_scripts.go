package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// The transport cannot manufacture script authority. It must find the original
// committed dispatch and exactly the frozen, independently approved artifact.
func (s *Store) agentScriptRequestTx(ctx context.Context, tx *sql.Tx, r taskrunner.Request) error {
	c := r.Command
	var executionID string
	var taskRaw []byte
	var now time.Time
	err := tx.QueryRowContext(ctx, `SELECT a.execution_id,task,clock_timestamp() FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id CROSS JOIN LATERAL jsonb_array_elements(x.snapshot->'flow'->'definition'->'tasks') task WHERE a.id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND task->>'id'=a.task_id FOR SHARE OF a,x`, c.AttemptID, c.CommandID, c.FencingToken).Scan(&executionID, &taskRaw, &now)
	if err != nil {
		return taskrunner.ErrRejected
	}
	var task struct {
		ScriptResultFormat string              `json:"script_result_format"`
		Type               string              `json:"type"`
		Idempotent         bool                `json:"idempotent"`
		Retry              int                 `json:"retry"`
		TimeoutMS          int                 `json:"timeout_ms"`
		Script             *TaskScriptArtifact `json:"script"`
	}
	if json.Unmarshal(taskRaw, &task) != nil || task.Script == nil || task.Type != "script.run" || task.Idempotent || task.Retry != 0 || task.TimeoutMS != c.TimeoutMS || task.ScriptResultFormat != c.ScriptResultFormat || task.Script.Command(c) != c {
		return taskrunner.ErrRejected
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM exec_outbox WHERE execution_id=$1 AND event_type='script.dispatch_committed' AND payload->>'attempt_id'=$2 ORDER BY sequence LIMIT 2`, executionID, c.AttemptID)
	if err != nil {
		return err
	}
	count := 0
	var saved struct {
		Command      taskrunner.TaskCommand `json:"command"`
		AuthorizedAt time.Time              `json:"authorized_at"`
		LeaseUntil   time.Time              `json:"lease_until"`
	}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			break
		}
		if err = json.Unmarshal(raw, &saved); err != nil {
			break
		}
		count++
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	expiry := time.UnixMicro(c.AuthorizationExpiresAt)
	if count != 1 || saved.Command != c || !expiry.After(saved.AuthorizedAt) || expiry.After(saved.AuthorizedAt.Add(time.Duration(c.TimeoutMS)*time.Millisecond)) || expiry.After(saved.LeaseUntil.Add(-100*time.Millisecond)) {
		return taskrunner.ErrRejected
	}
	// Historical query/cancel remains available after opt-out or media retirement.
	// It retrieves existing proof and cannot deliver any executable body.
	if r.Action != "run" {
		return nil
	}
	if !s.AgentScriptsEnabled || !expiry.After(now) {
		return taskrunner.ErrRejected
	}
	if _, _, err = s.ResolveTaskScriptTx(ctx, tx, c.ProjectID, task.Script.MediaID, task.Script, false); err != nil {
		return err
	}
	var claims int
	err = tx.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND fencing_token=$2 AND mode='exclusive' AND state='held' AND expires_at>clock_timestamp() AND resource_id IN($3,$4)`, c.AttemptID, c.FencingToken, "device:machine:"+c.MachineID, "agent:machine:"+c.MachineID).Scan(&claims)
	if err != nil {
		return err
	}
	if claims != 2 {
		return taskrunner.ErrRejected
	}
	return nil
}
