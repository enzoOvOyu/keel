package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/platform"
	"sort"
	"strings"
	"time"
)

// PumpOutbox treats notifications as scheduling hints. The database remains the
// queue of record: consuming a duplicate event cannot create another attempt.
// Advancing created->scheduled and acknowledging the event share one transaction.
func (s *Service) PumpOutbox(ctx context.Context, consumer string, limit int) (int, error) {
	if len(consumer) < 1 || len(consumer) > 120 {
		return 0, invalid("invalid consumer")
	}
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, "SELECT id,execution_id,event_type FROM exec_outbox WHERE published_at IS NULL ORDER BY sequence LIMIT $1 FOR UPDATE SKIP LOCKED", limit)
	if e != nil {
		return 0, e
	}
	type entry struct{ id, eid, kind string }
	entries := []entry{}
	for rows.Next() {
		var v entry
		if e = rows.Scan(&v.id, &v.eid, &v.kind); e != nil {
			rows.Close()
			return 0, e
		}
		entries = append(entries, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	// Never hold outbox locks while waiting for an execution lock. A completion
	// holds execution first and inserts an outbox FK referencing that same row.
	for _, v := range entries {
		if v.kind == "execution.created" {
			var id string
			e = tx.QueryRowContext(ctx, "SELECT id FROM exec_executions WHERE id=$1 FOR UPDATE SKIP LOCKED", v.eid).Scan(&id)
			if errors.Is(e, sql.ErrNoRows) {
				continue
			}
			if e != nil {
				return 0, e
			}
			if _, e = tx.ExecContext(ctx, "UPDATE exec_executions SET state='scheduled',state_version=state_version+1 WHERE id=$1 AND state='created' AND desired_state='running'", id); e != nil {
				return 0, e
			}
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO exec_consumed_events VALUES($1,$2,now()) ON CONFLICT DO NOTHING", consumer, v.id); e != nil {
			return 0, e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE exec_outbox SET published_at=now() WHERE id=$1", v.id); e != nil {
			return 0, e
		}
	}
	return len(entries), tx.Commit()
}

// Claim serializes each execution briefly, then locks each resource in sorted
// order. Savepoints release resource-row locks for blocked candidates; expired
// leases remain conflicting until reconciliation, never a license to overlap.
func (s *Service) Claim(ctx context.Context, worker string, lease time.Duration) (*Claim, error) {
	if len(worker) < 1 || len(worker) > 120 || lease < 200*time.Millisecond || lease > time.Minute {
		return nil, invalid("invalid worker or lease duration")
	}
	cursorTime := time.Unix(0, 0)
	cursorID := ""
	for {
		rows, e := s.DB.QueryContext(ctx, "SELECT id,created_at FROM exec_executions WHERE desired_state='running' AND state IN('scheduled','dispatching','running') AND (created_at,id)>($1,$2) AND (mode='sim' OR ($3 AND mode='kea') OR ($4 AND mode='fabric') OR ($5 AND mode='ssh') OR ($6 AND mode='agent') OR ($7 AND mode='provision')) ORDER BY created_at,id LIMIT 64", cursorTime, cursorID, s.KeaStore != nil, s.NetworkStore != nil, s.SSHRunner != nil, s.AgentRunner != nil, s.ProvisionStore != nil && (s.ProvisionStore.ProvisionPrepareEnabled || s.ProvisionStore.ProvisionInstallEnabled))
		if e != nil {
			return nil, e
		}
		type candidate struct {
			id string
			at time.Time
		}
		candidates := []candidate{}
		for rows.Next() {
			var c candidate
			if e = rows.Scan(&c.id, &c.at); e != nil {
				rows.Close()
				return nil, e
			}
			candidates = append(candidates, c)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		if len(candidates) == 0 {
			return nil, nil
		}
		for _, c := range candidates {
			claim, e := s.claimExecution(ctx, c.id, worker, lease)
			if e != nil {
				return nil, e
			}
			if claim != nil {
				return claim, nil
			}
			cursorTime = c.at
			cursorID = c.id
		}
		if len(candidates) < 64 {
			return nil, nil
		}
	}
}

func (s *Service) claimExecution(ctx context.Context, id, worker string, lease time.Duration) (*Claim, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	// Lock the original submitter before any domain row, consistent with account
	// revocation. UI logout does not cancel an accepted job; removed execution
	// permission or account disable prevents subsequent task dispatch.
	var creator, projectID, executionMode string
	e = tx.QueryRowContext(ctx, "SELECT created_by,project_id,mode FROM exec_executions WHERE id=$1", id).Scan(&creator, &projectID, &executionMode)
	if e != nil {
		return nil, e
	}
	if executionMode == "provision" && (s.ProvisionStore == nil || (!s.ProvisionStore.ProvisionPrepareEnabled && !s.ProvisionStore.ProvisionInstallEnabled)) {
		return nil, nil
	}
	if executionMode == "agent" && s.AgentRunner == nil {
		return nil, nil
	}
	if executionMode == "ssh" && s.SSHRunner == nil {
		return nil, nil
	}
	if executionMode == "fabric" && s.NetworkStore == nil {
		return nil, nil
	}
	if executionMode == "kea" && s.KeaStore == nil {
		return nil, nil
	}
	var enabled, admin bool
	e = tx.QueryRowContext(ctx, "SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE", creator).Scan(&enabled, &admin)
	if e != nil {
		return nil, e
	}
	var projectState string
	e = tx.QueryRowContext(ctx, "SELECT state FROM projects WHERE id=$1 FOR SHARE", projectID).Scan(&projectState)
	if e != nil {
		return nil, e
	}
	allowed := enabled && projectState != "archived" && projectState != "delivered"
	if !admin {
		var role string
		e = tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE user_id=$1 AND project_id=$2 FOR SHARE", creator, projectID).Scan(&role)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		allowed = allowed && (role == "Admin" || ((executionMode == "kea" || executionMode == "fabric") && role == "NDE") || ((executionMode == "sim" || executionMode == "ssh" || executionMode == "agent" || executionMode == "provision") && (role == "DE" || role == "DL" || role == "SRE")))
	}
	x, e := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 AND desired_state='running' AND state IN('scheduled','dispatching','running') FOR UPDATE SKIP LOCKED", id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if !allowed {
		if _, e = tx.ExecContext(ctx, "UPDATE exec_executions SET desired_state='paused',state_version=state_version+1 WHERE id=$1", id); e != nil {
			return nil, e
		}
		if e = settle(ctx, tx, id); e != nil {
			return nil, e
		}
		if e = event(ctx, tx, id, "execution.authorization_blocked", map[string]string{"reason": "submitter_or_project_authority_changed"}); e != nil {
			return nil, e
		}
		return nil, tx.Commit()
	}
	var running int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM exec_task_attempts WHERE execution_id=$1 AND state='running'", id).Scan(&running); e != nil {
		return nil, e
	}
	if running >= x.Concurrency {
		return nil, nil
	}
	for offset := 0; ; offset += 256 {
		rows, e := tx.QueryContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE execution_id=$1 AND state='pending' ORDER BY task_index,target_id,attempt_no LIMIT 256 OFFSET $2", id, offset)
		if e != nil {
			return nil, e
		}
		pending := []Attempt{}
		for rows.Next() {
			a, e := scanAttempt(rows)
			if e != nil {
				rows.Close()
				return nil, e
			}
			pending = append(pending, a)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		for _, attempt := range pending {
			task, e := taskByID(x.Snapshot.Flow.Definition, attempt.TaskID)
			if e != nil {
				return nil, e
			}
			ready := true
			for _, dep := range task.DependsOn {
				var state string
				e = tx.QueryRowContext(ctx, "SELECT state FROM exec_task_attempts WHERE execution_id=$1 AND target_id=$2 AND task_id=$3 ORDER BY attempt_no DESC LIMIT 1", id, attempt.TargetID, dep).Scan(&state)
				if e != nil {
					return nil, e
				}
				if state != "success" {
					ready = false
				}
			}
			if !ready {
				continue
			}
			var target SimTarget
			for _, t := range x.Snapshot.Targets {
				if t.ID == attempt.TargetID {
					target = t
					break
				}
			}
			if target.ID == "" {
				return nil, fmt.Errorf("frozen target missing")
			}
			if x.Mode == "provision" {
				if e = s.provisionFrozenTx(ctx, tx, target); e != nil {
					var blocked *platform.Error
					if !errors.As(e, &blocked) {
						return nil, e
					}
					if _, e = tx.ExecContext(ctx, "UPDATE exec_executions SET desired_state='paused',state_version=state_version+1 WHERE id=$1", id); e != nil {
						return nil, e
					}
					if e = settle(ctx, tx, id); e != nil {
						return nil, e
					}
					if e = event(ctx, tx, id, "execution.authorization_blocked", map[string]string{"reason": "provision_preparation_binding_changed"}); e != nil {
						return nil, e
					}
					return nil, tx.Commit()
				}
			}
			if err := s.hostTaskFrozenTx(ctx, tx, x.ProjectID, x.Mode, task); err != nil {
				var blocked *platform.Error
				if !errors.As(err, &blocked) {
					return nil, err
				}
				if _, e = tx.ExecContext(ctx, "UPDATE exec_executions SET desired_state='paused',state_version=state_version+1 WHERE id=$1", id); e != nil {
					return nil, e
				}
				if e = settle(ctx, tx, id); e != nil {
					return nil, e
				}
				if e = event(ctx, tx, id, "execution.authorization_blocked", map[string]string{"reason": "host_task_admission_changed_or_disabled"}); e != nil {
					return nil, e
				}
				return nil, tx.Commit()
			}
			if x.Mode == "ssh" {
				if e = s.sshFrozenTx(ctx, tx, target); e != nil {
					var blocked *platform.Error
					if !errors.As(e, &blocked) {
						return nil, e
					}
					if _, e = tx.ExecContext(ctx, "UPDATE exec_executions SET desired_state='paused',state_version=state_version+1 WHERE id=$1", id); e != nil {
						return nil, e
					}
					if e = settle(ctx, tx, id); e != nil {
						return nil, e
					}
					if e = event(ctx, tx, id, "execution.authorization_blocked", map[string]string{"reason": "ssh_binding_or_trust_changed"}); e != nil {
						return nil, e
					}
					return nil, tx.Commit()
				}
			}
			if x.Mode == "agent" {
				if e = s.agentFrozenTx(ctx, tx, target); e != nil {
					var blocked *platform.Error
					if !errors.As(e, &blocked) {
						return nil, e
					}
					// A transport gap leaves the same pending command untouched.
					// Actual binding/asset revocation requires an explicit resume.
					if blocked.Code == "AGENT_OFFLINE" {
						continue
					}
					if _, e = tx.ExecContext(ctx, "UPDATE exec_executions SET desired_state='paused',state_version=state_version+1 WHERE id=$1", id); e != nil {
						return nil, e
					}
					if e = settle(ctx, tx, id); e != nil {
						return nil, e
					}
					if e = event(ctx, tx, id, "execution.authorization_blocked", map[string]string{"reason": "agent_identity_or_asset_changed"}); e != nil {
						return nil, e
					}
					return nil, tx.Commit()
				}
			}
			if x.Mode == "fabric" {
				job, _ := x.Parameters["network_push_id"].(string)
				phase, _ := x.Parameters["network_phase"].(string)
				admitted, err := s.NetworkStore.NetworkClaimAllowedTx(ctx, tx, x.ProjectID, job, target.ResourceKey, phase)
				if err != nil {
					return nil, err
				}
				if !admitted {
					continue
				}
			}
			resources := []string{}
			for _, kind := range task.Resources {
				if x.Mode == "ssh" || x.Mode == "agent" || x.Mode == "provision" {
					resources = append(resources, kind+":machine:"+target.ResourceKey)
				} else if x.Mode == "fabric" {
					resources = append(resources, kind+":switch:"+target.ResourceKey)
				} else if x.Mode == "kea" {
					resources = append(resources, "kea:"+target.ResourceKey)
				} else {
					resources = append(resources, kind+":sim:"+target.ID)
				}
			}
			sort.Strings(resources)
			mode := "exclusive"
			if task.Type == "sim.read" || (x.Mode == "ssh" || x.Mode == "agent") && task.Script == nil && task.Artifact == nil {
				mode = "shared"
			}
			if _, e = tx.ExecContext(ctx, "SAVEPOINT resource_candidate"); e != nil {
				return nil, e
			}
			available := true
			for _, resource := range resources {
				if _, e = tx.ExecContext(ctx, "INSERT INTO exec_resources VALUES($1) ON CONFLICT DO NOTHING", resource); e != nil {
					return nil, e
				}
				var locked string
				if e = tx.QueryRowContext(ctx, "SELECT id FROM exec_resources WHERE id=$1 FOR UPDATE", resource).Scan(&locked); e != nil {
					return nil, e
				}
				var conflict bool
				e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM exec_resource_leases WHERE resource_id=$1 AND state IN('held','reconciling') AND ($2='exclusive' OR mode='exclusive' OR state='reconciling'))", resource, hostResourceLeaseMode(x.Mode, resource, mode)).Scan(&conflict)
				if e != nil {
					return nil, e
				}
				if conflict {
					available = false
					break
				}
			}
			if !available {
				if _, e = tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT resource_candidate"); e != nil {
					return nil, e
				}
				if _, e = tx.ExecContext(ctx, "RELEASE SAVEPOINT resource_candidate"); e != nil {
					return nil, e
				}
				continue
			}
			var fence int64
			if e = tx.QueryRowContext(ctx, "SELECT nextval('exec_fencing_tokens')").Scan(&fence); e != nil {
				return nil, e
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			until := now.Add(lease)
			for _, resource := range resources {
				if _, e = tx.ExecContext(ctx, "INSERT INTO exec_resource_leases VALUES($1,$2,$3,$4,'held',$5)", resource, attempt.ID, hostResourceLeaseMode(x.Mode, resource, mode), fence, until); e != nil {
					return nil, e
				}
			}
			if _, e = tx.ExecContext(ctx, "UPDATE exec_task_attempts SET state='running',state_version=state_version+1,lease_owner=$2,lease_until=$3,fencing_token=$4,started_at=$5 WHERE id=$1 AND state='pending'", attempt.ID, worker, until, fence, now); e != nil {
				return nil, e
			}
			if _, e = tx.ExecContext(ctx, "UPDATE exec_executions SET state='running',state_version=state_version+1,started_at=COALESCE(started_at,$2) WHERE id=$1", id, now); e != nil {
				return nil, e
			}
			if e = event(ctx, tx, id, "attempt.started", map[string]any{"attempt_id": attempt.ID, "command_id": attempt.CommandID, "target_id": target.ID, "fencing_token": fence, "mode": x.Mode}); e != nil {
				return nil, e
			}
			attempt.State = "running"
			attempt.StateVersion++
			attempt.LeaseOwner = worker
			attempt.LeaseUntil = &until
			attempt.StartedAt = &now
			attempt.FencingToken = fence
			if e = tx.Commit(); e != nil {
				return nil, e
			}
			return &Claim{Attempt: attempt, Task: task, Target: target, Parameters: x.Parameters, LeaseDuration: lease}, nil
		}
		if len(pending) < 256 {
			return nil, nil
		}
	}
}

// Heartbeat refuses a lease that has already expired; a late process cannot
// renew itself back into authority. The bool requests cooperative cancellation.
func (s *Service) Heartbeat(ctx context.Context, c *Claim) (bool, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	until := time.Now().UTC().Add(c.LeaseDuration)
	result, e := tx.ExecContext(ctx, "UPDATE exec_task_attempts SET lease_until=$4 WHERE id=$1 AND lease_owner=$2 AND fencing_token=$3 AND state='running' AND lease_until>now()", c.Attempt.ID, c.Attempt.LeaseOwner, c.Attempt.FencingToken, until)
	if e != nil {
		return false, e
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return false, failure(409, "STALE_LEASE", "task lease is expired or no longer owned")
	}
	if _, e = tx.ExecContext(ctx, "UPDATE exec_resource_leases SET expires_at=$3 WHERE attempt_id=$1 AND fencing_token=$2 AND state='held'", c.Attempt.ID, c.Attempt.FencingToken, until); e != nil {
		return false, e
	}
	var desired string
	if e = tx.QueryRowContext(ctx, "SELECT desired_state FROM exec_executions WHERE id=$1", c.Attempt.ExecutionID).Scan(&desired); e != nil {
		return false, e
	}
	return desired == "killed" || s.scriptHeartbeatTx(ctx, tx, c), tx.Commit()
}
func (s *Service) RecordReceipt(ctx context.Context, r Receipt) error {
	if r.Result != "success" && r.Result != "failed" && r.Result != "cancelled" {
		return invalid("invalid receipt result")
	}
	if r.FinishedAt.Before(r.StartedAt) {
		return invalid("receipt finish precedes start")
	}
	data := jsonBytes(r)
	if len(data) > 16384 {
		return invalid("receipt exceeds 16 KiB")
	}
	digest := hash(data)
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var runner string
	e = tx.QueryRowContext(ctx, "SELECT runner FROM exec_task_attempts WHERE id=$1 AND command_id=$2 AND fencing_token=$3 AND state IN('running','unknown','success','failed','cancelled')", r.AttemptID, r.CommandID, r.FencingToken).Scan(&runner)
	if errors.Is(e, sql.ErrNoRows) {
		return failure(409, "STALE_FENCE", "receipt does not match claimed attempt")
	}
	if e != nil {
		return e
	}
	// A durable proof remains historical fact after credential revocation. Only
	// new Agent proofs must serialize against revocation in this transaction.
	var existing string
	e = tx.QueryRowContext(ctx, "SELECT digest FROM exec_runner_receipts WHERE command_id=$1", r.CommandID).Scan(&existing)
	if e == nil {
		if existing != digest {
			return failure(409, "RECEIPT_CONFLICT", "command receipt is immutable")
		}
		return tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if runner == "provision" {
		attempt, err := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1", r.AttemptID))
		if err != nil {
			return err
		}
		if _, err = provisionReceiptTarget(ctx, tx, attempt, r); err != nil {
			return err
		}
	}
	if e = s.agentReceiptAllowedTx(ctx, tx, r); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO exec_runner_receipts VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(command_id) DO NOTHING", r.CommandID, r.AttemptID, r.FencingToken, r.Result, data, digest)
	if e != nil {
		return e
	}
	var prior string
	if e = tx.QueryRowContext(ctx, "SELECT digest FROM exec_runner_receipts WHERE command_id=$1", r.CommandID).Scan(&prior); e != nil {
		return e
	}
	if prior != digest {
		return failure(409, "RECEIPT_CONFLICT", "command receipt is immutable")
	}
	return tx.Commit()
}
func (s *Service) Complete(ctx context.Context, c *Claim) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id string
	if e = tx.QueryRowContext(ctx, "SELECT id FROM exec_executions WHERE id=$1 FOR UPDATE", c.Attempt.ExecutionID).Scan(&id); e != nil {
		return e
	}
	a, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1 FOR UPDATE", c.Attempt.ID))
	if e != nil {
		return e
	}
	if a.FencingToken != c.Attempt.FencingToken || a.LeaseOwner != c.Attempt.LeaseOwner {
		return failure(409, "STALE_FENCE", "attempt ownership changed")
	}
	var data []byte
	if e = tx.QueryRowContext(ctx, "SELECT payload FROM exec_runner_receipts WHERE command_id=$1 AND attempt_id=$2 AND fencing_token=$3", a.CommandID, a.ID, a.FencingToken).Scan(&data); errors.Is(e, sql.ErrNoRows) {
		return failure(409, "RECEIPT_REQUIRED", "durable runner receipt is required")
	}
	if e != nil {
		return e
	}
	r, e := decode[Receipt](data)
	if e != nil {
		return e
	}
	if a.State == r.Result {
		return nil
	}
	if a.State != "running" || a.LeaseUntil == nil || !a.LeaseUntil.After(time.Now()) {
		return failure(409, "STALE_LEASE", "late completion requires explicit reconciliation")
	}
	if e = s.applyProvisionReceipt(ctx, tx, a, r); e != nil {
		return e
	}
	if e = s.applySSHTrustReceipt(ctx, tx, a, r); e != nil {
		return e
	}
	if e = applyReceipt(ctx, tx, a, r, true); e != nil {
		return e
	}
	if e = settle(ctx, tx, id); e != nil {
		return e
	}
	return tx.Commit()
}
func applyReceipt(ctx context.Context, tx *sql.Tx, a Attempt, r Receipt, allowRetry bool) error {
	if _, e := tx.ExecContext(ctx, "UPDATE exec_task_attempts SET state=$2,state_version=state_version+1,result=$3,finished_at=$4 WHERE id=$1", a.ID, r.Result, jsonBytes(r), r.FinishedAt); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, "UPDATE exec_resource_leases SET state='released' WHERE attempt_id=$1 AND fencing_token=$2", a.ID, a.FencingToken); e != nil {
		return e
	}
	x, e := loadExecutionControl(ctx, tx, a.ExecutionID)
	if e != nil {
		return e
	}
	task, e := taskByID(x.Definition, a.TaskID)
	if e != nil {
		return e
	}
	if allowRetry && r.Result == "failed" && task.Idempotent && a.AttemptNo <= task.Retry && x.DesiredState != "killed" {
		_, e = tx.ExecContext(ctx, "INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,now())", platform.RandomID(), a.ExecutionID, a.TargetID, a.TaskID, a.TaskIndex, a.AttemptNo+1, platform.RandomID())
		if e != nil {
			return e
		}
	}
	return event(ctx, tx, a.ExecutionID, "attempt.receipt_applied", map[string]any{"attempt_id": a.ID, "result": r.Result, "fencing_token": a.FencingToken, "mode": x.Mode})
}
func (s *Service) ReapExpired(ctx context.Context) (int, error) {
	rows, e := s.DB.QueryContext(ctx, "SELECT DISTINCT execution_id FROM exec_task_attempts WHERE state='running' AND lease_until<=now() LIMIT 100")
	if e != nil {
		return 0, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return 0, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	total := 0
	for _, id := range ids {
		n, e := s.reapExecution(ctx, id)
		if e != nil {
			return total, e
		}
		total += n
	}
	return total, nil
}
func (s *Service) reapExecution(ctx context.Context, id string) (int, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var projectID string
	e = tx.QueryRowContext(ctx, "SELECT project_id FROM exec_executions WHERE id=$1 FOR UPDATE SKIP LOCKED", id).Scan(&projectID)
	if errors.Is(e, sql.ErrNoRows) {
		return 0, nil
	}
	if e != nil {
		return 0, e
	}
	rows, e := tx.QueryContext(ctx, "UPDATE exec_task_attempts SET state='unknown',state_version=state_version+1 WHERE execution_id=$1 AND state='running' AND lease_until<=now() RETURNING id", id)
	if e != nil {
		return 0, e
	}
	attempts := []string{}
	for rows.Next() {
		var a string
		if e = rows.Scan(&a); e != nil {
			rows.Close()
			return 0, e
		}
		attempts = append(attempts, a)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	if len(attempts) == 0 {
		return 0, nil
	}
	if _, e = tx.ExecContext(ctx, "UPDATE ssh_trust_requests SET state='unknown',state_version=state_version+1 WHERE execution_id=$1 AND state='consumed'", id); e != nil {
		return 0, e
	}
	if _, e = tx.ExecContext(ctx, `WITH changed AS (UPDATE provision_installations SET state=CASE WHEN state='queued' THEN 'failed' ELSE 'unknown' END,state_version=state_version+1,failure_reason='execution_lease_expired',updated_at=now() WHERE execution_id=$1 AND state IN('queued','booting','preflight_ready','awaiting_storage_review','storage_approved','installing','awaiting_first_boot') RETURNING id,state) INSERT INTO provision_installation_events(installation_id,event_type,payload,created_at) SELECT id,'execution.lease_expired',jsonb_build_object('state',state),now() FROM changed`, id); e != nil {
		return 0, e
	}
	for _, a := range attempts {
		if _, e = tx.ExecContext(ctx, "UPDATE exec_resource_leases SET state='reconciling' WHERE attempt_id=$1 AND state='held'", a); e != nil {
			return 0, e
		}
	}
	if e = settle(ctx, tx, id); e != nil {
		return 0, e
	}
	if e = event(ctx, tx, id, "attempt.result_unknown", map[string]any{"attempt_ids": attempts, "reason": "lease_expired"}); e != nil {
		return 0, e
	}
	if e = audit(ctx, tx, "", projectID, "execution.result_unknown", id, "worker", map[string]any{"attempt_ids": attempts}); e != nil {
		return 0, e
	}
	return len(attempts), tx.Commit()
}
func (s *Service) Reconcile(ctx context.Context, a *Actor, projectID, id, attemptID string, in ReconcileInput, key, trace string) (*Execution, error) {
	if len(strings.TrimSpace(in.Evidence)) < 10 || len(in.Evidence) > 2000 {
		return nil, invalid("reconciliation requires 10..2000 characters of evidence")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = authorize(ctx, tx, a, projectID, "Admin", "DL", "SRE"); e != nil {
		return nil, e
	}
	idem, prior, e := idempotency(ctx, tx, a, "execution.reconcile:"+attemptID, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		x, e := decode[Execution](prior)
		return &x, e
	}
	var exists string
	e = tx.QueryRowContext(ctx, "SELECT id FROM exec_executions WHERE id=$1 AND project_id=$2 FOR UPDATE", id, projectID).Scan(&exists)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, failure(404, "NOT_FOUND", "Execution not found")
	}
	if e != nil {
		return nil, e
	}
	attempt, e := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1 AND execution_id=$2 FOR UPDATE", attemptID, id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, failure(404, "NOT_FOUND", "Attempt not found")
	}
	if e != nil {
		return nil, e
	}
	if attempt.State != "unknown" || attempt.StateVersion != in.ExpectedVersion {
		return nil, conflict("attempt is not unknown or its version changed")
	}
	var data []byte
	e = tx.QueryRowContext(ctx, "SELECT payload FROM exec_runner_receipts WHERE command_id=$1 AND fencing_token=$2", attempt.CommandID, attempt.FencingToken).Scan(&data)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, failure(409, "RECEIPT_REQUIRED", "no stopped/finished runner receipt exists; resource remains quarantined")
	}
	if e != nil {
		return nil, e
	}
	r, e := decode[Receipt](data)
	if e != nil {
		return nil, e
	}
	if in.Result != r.Result {
		return nil, failure(409, "EVIDENCE_CONFLICT", "claimed result does not match the durable runner receipt")
	}
	// Reconciliation never silently creates a retry, even for an idempotent task.
	if e = s.applyProvisionReceipt(ctx, tx, attempt, r); e != nil {
		return nil, e
	}
	if e = s.applySSHTrustReceipt(ctx, tx, attempt, r); e != nil {
		return nil, e
	}
	if e = applyReceipt(ctx, tx, attempt, r, false); e != nil {
		return nil, e
	}
	if e = settle(ctx, tx, id); e != nil {
		return nil, e
	}
	if e = audit(ctx, tx, a.User.ID, projectID, "execution.reconcile", attemptID, trace, map[string]any{"evidence": in.Evidence, "result": r.Result, "command_id": r.CommandID}); e != nil {
		return nil, e
	}
	x, e := scanExecution(tx.QueryRowContext(ctx, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1", id))
	if e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, x); e != nil {
		return nil, e
	}
	return &x, tx.Commit()
}
func (s *Service) WorkerHeartbeat(ctx context.Context, id string, active int) error {
	mode := "sim"
	if s.KeaStore != nil || s.NetworkStore != nil || s.SSHRunner != nil || s.AgentRunner != nil || s.ProvisionStore != nil {
		mode = "mixed"
	}
	_, e := s.DB.ExecContext(ctx, "INSERT INTO exec_worker_heartbeats(id,mode,active_count,observed_at) VALUES($1,$2,$3,now()) ON CONFLICT(id) DO UPDATE SET mode=EXCLUDED.mode,active_count=EXCLUDED.active_count,observed_at=EXCLUDED.observed_at", id, mode, active)
	return e
}
func (s *Service) WorkerHealthy(ctx context.Context, id string) error {
	var yes bool
	mode := "sim"
	if s.KeaStore != nil || s.NetworkStore != nil || s.SSHRunner != nil || s.AgentRunner != nil || s.ProvisionStore != nil {
		mode = "mixed"
	}
	e := s.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM exec_worker_heartbeats WHERE id=$1 AND mode=$2 AND observed_at>now()-interval '10 seconds')", id, mode).Scan(&yes)
	if e != nil {
		return e
	}
	if !yes {
		return errors.New("execution worker heartbeat stale")
	}
	return nil
}

var _ = json.Valid

// Agent currently accepts one local run at a time. Serialize only that channel;
// the common read-only device lease remains shared with SSH/BMC readers.
func hostResourceLeaseMode(executionMode, resource, defaultMode string) string {
	if executionMode == "agent" && strings.HasPrefix(resource, "agent:machine:") {
		return "exclusive"
	}
	return defaultMode
}
