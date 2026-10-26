package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func (s *Store) agentArtifactRequestTx(ctx context.Context, tx *sql.Tx, r taskrunner.Request) error {
	c := r.Command
	var execution string
	var raw []byte
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT a.execution_id,task,clock_timestamp() FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id CROSS JOIN LATERAL jsonb_array_elements(x.snapshot->'flow'->'definition'->'tasks') task WHERE a.id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND task->>'id'=a.task_id FOR SHARE OF a,x`, c.AttemptID, c.CommandID, c.FencingToken).Scan(&execution, &raw, &now); err != nil {
		return taskrunner.ErrRejected
	}
	var t struct {
		Artifact   *TaskArtifact `json:"artifact"`
		Type       string        `json:"type"`
		TimeoutMS  int           `json:"timeout_ms"`
		Retry      int           `json:"retry"`
		Idempotent bool          `json:"idempotent"`
	}
	if json.Unmarshal(raw, &t) != nil || t.Artifact == nil || t.Type != c.Operation || c.Operation != "media.stage" && c.Operation != "media.fetch" || t.TimeoutMS != c.TimeoutMS || t.Retry != 0 || t.Idempotent || t.Artifact.Command(c) != c {
		return taskrunner.ErrRejected
	}
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM exec_outbox WHERE execution_id=$1 AND event_type='artifact.dispatch_committed' AND payload->>'attempt_id'=$2 ORDER BY sequence LIMIT 2`, execution, c.AttemptID)
	if err != nil {
		return err
	}
	count := 0
	var original struct {
		Command      taskrunner.TaskCommand `json:"command"`
		AuthorizedAt time.Time              `json:"authorized_at"`
		LeaseUntil   time.Time              `json:"lease_until"`
	}
	for rows.Next() {
		var payload []byte
		if err = rows.Scan(&payload); err != nil {
			break
		}
		if err = json.Unmarshal(payload, &original); err != nil {
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
	if count != 1 || original.Command != c || !expiry.After(original.AuthorizedAt) || expiry.After(original.AuthorizedAt.Add(time.Duration(c.TimeoutMS)*time.Millisecond)) || expiry.After(original.LeaseUntil.Add(-100*time.Millisecond)) {
		return taskrunner.ErrRejected
	}
	if r.Action == "settle" {
		if expiry.After(now) {
			return taskrunner.ErrRejected
		}
		allowed, err := BoundedNonstartProofAllowedTx(ctx, tx, c)
		if err != nil {
			return err
		}
		if !allowed {
			return taskrunner.ErrRejected
		}
		return nil
	}
	if r.Action == "query" || r.Action == "cancel" {
		return nil
	}
	if !expiry.After(now) || !s.ArtifactTransfersEnabled {
		return taskrunner.ErrRejected
	}
	if _, err = s.ResolveTaskArtifactTx(ctx, tx, c.ProjectID, c.ArtifactMediaID, t.Artifact); err != nil {
		return err
	}
	var claims int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND fencing_token=$2 AND mode='exclusive' AND state='held' AND expires_at>clock_timestamp() AND resource_id IN($3,$4)`, c.AttemptID, c.FencingToken, "device:machine:"+c.MachineID, "agent:machine:"+c.MachineID).Scan(&claims); err != nil {
		return err
	}
	if claims != 2 {
		return taskrunner.ErrRejected
	}
	return nil
}
