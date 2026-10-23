package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// RetryDueZTPSimSessions resumes bounded simulated failures after backoff.
// Row locking makes concurrent API replicas and a manual retry mutually exclusive.
// It never advances a device session or projects a switch to managed.
func (s *Store) RetryDueZTPSimSessions(ctx context.Context, limit int) (int, error) {
	if limit < 1 || limit > 100 {
		return 0, apiError(400, "ZTP_RETRY_LIMIT_INVALID", "Retry batch limit must be 1 through 100")
	}
	count := 0
	for count < limit {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return count, err
		}
		var id, projectID string
		var version int64
		var attempt int
		var runInput []byte
		err = tx.QueryRowContext(ctx, `SELECT id,project_id,state_version,attempt,sim_run_input FROM ztp_sessions WHERE mode='sim' AND state='failed' AND next_retry_at<=now() AND attempt<=retry_max ORDER BY next_retry_at,id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&id, &projectID, &version, &attempt, &runInput)
		if errors.Is(err, sql.ErrNoRows) {
			_ = tx.Rollback()
			return count, nil
		}
		if err != nil {
			_ = tx.Rollback()
			return count, err
		}
		now := time.Now().UTC().Truncate(time.Microsecond)
		nextAttempt := attempt + 1
		event := ZTPSessionEvent{Seq: version + 1, FromState: "failed", ToState: "powered", EventCode: "sim.retry", EvidenceRef: "scheduled-backoff", Attempt: nextAttempt, OccurredAt: now}
		if err = insertZTPEvent(ctx, tx, id, event); err != nil {
			_ = tx.Rollback()
			return count, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE ztp_sessions SET state='powered',state_version=$2,attempt=$3,failure_phenomenon=NULL,failure_cause=NULL,failure_next_action=NULL,next_retry_at=NULL,updated_at=$4 WHERE id=$1`, id, version+1, nextAttempt, now)
		if err != nil {
			_ = tx.Rollback()
			return count, err
		}
		if err = appendAudit(ctx, tx, "", projectID, "ztp.sim.auto_retry", id, "", map[string]any{"from": "failed", "to": "powered", "attempt": nextAttempt}); err != nil {
			_ = tx.Rollback()
			return count, err
		}
		if len(runInput) > 0 {
			var input ZTPSimRunInput
			if err = json.Unmarshal(runInput, &input); err != nil {
				_ = tx.Rollback()
				return count, err
			}
			if err = s.resumeZTPSimRunTx(ctx, tx, projectID, id, input); err != nil {
				_ = tx.Rollback()
				return count, err
			}
		}
		if err = tx.Commit(); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
