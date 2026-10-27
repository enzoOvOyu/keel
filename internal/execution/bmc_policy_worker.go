package execution

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/schedule"
)

const bmcRetryEligible = `r.state='admitted' AND r.policy_version=p.state_version AND r.attempt_no<p.max_retries AND x.state='failed' AND x.desired_state='running' AND NOT EXISTS(SELECT 1 FROM bmc_policy_runs child WHERE child.parent_run_id=r.id) AND EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_runner_receipts receipt ON receipt.attempt_id=a.id AND receipt.command_id=a.command_id AND receipt.fencing_token=a.fencing_token JOIN bmc_collection_records evidence ON evidence.execution_id=a.execution_id AND evidence.fencing_token=a.fencing_token WHERE a.execution_id=x.id AND a.state='failed' AND receipt.result='failed' AND receipt.payload#>>'{output,stopped}'='true' AND EXISTS(SELECT 1 FROM jsonb_array_elements(evidence.report->'failures') f WHERE f->>'retryable'='true'))`

// TickBMCInventoryPolicies admits bounded ordinary Executions. It never touches a
// device or secret. Missed cron ticks coalesce; original unknown attempts remain
// under the kernel's quarantine and explicit reconciliation rules.
func (s *Service) TickBMCInventoryPolicies(ctx context.Context, limit int) (int, error) {
	if s.BMCStore == nil || !s.BMCStore.BMCInventoryEnabled {
		return 0, nil
	}
	if limit < 1 || limit > 100 {
		return 0, invalid("Inventory admission batch must be 1..100")
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT p.id FROM bmc_collection_policies p WHERE p.enabled AND ((p.trigger_kind='cron' AND p.next_due<=statement_timestamp()) OR (p.trigger_kind='event' AND EXISTS(SELECT 1 FROM bmc_inventory_events e WHERE e.project_id=p.project_id AND e.event_type=p.event_type AND e.sequence>p.last_event_sequence)) OR EXISTS(SELECT 1 FROM bmc_policy_runs r JOIN exec_executions x ON x.id=r.execution_id WHERE r.policy_id=p.id AND `+bmcRetryEligible+` AND x.finished_at+make_interval(secs=>p.retry_delay_seconds)<=statement_timestamp())) ORDER BY coalesce(p.next_due,p.updated_at),p.id LIMIT $1`, limit)
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
	count := 0
	for _, id := range ids {
		admitted, e := s.tickBMCPolicy(ctx, id)
		if e != nil {
			return count, e
		}
		if admitted {
			count++
		}
	}
	return count, nil
}

// A policy is a persistent delegation by its owner. This path checks the current
// user and membership, not a manufactured or expired browser session.
func bmcPolicyOwnerTx(ctx context.Context, tx *sql.Tx, p *BMCPolicy) (*Actor, error) {
	actor := &Actor{User: platform.UserSummary{ID: p.CreatedBy}}
	var enabled bool
	e := tx.QueryRowContext(ctx, `SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE`, p.CreatedBy).Scan(&enabled, &actor.User.IsAdmin)
	if errors.Is(e, sql.ErrNoRows) || e == nil && !enabled {
		return nil, platform.ErrForbidden
	}
	if e != nil {
		return nil, e
	}
	var state string
	e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, p.ProjectID).Scan(&state)
	if e != nil {
		return nil, e
	}
	if state == "archived" || state == "delivered" {
		return nil, conflict("Project lifecycle prevents scheduled inventory")
	}
	if !actor.User.IsAdmin {
		var role string
		e = tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE`, p.ProjectID, p.CreatedBy).Scan(&role)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, platform.ErrForbidden
		}
		if e != nil {
			return nil, e
		}
		if role != "Admin" && role != "DE" && role != "DL" && role != "SRE" {
			return nil, platform.ErrForbidden
		}
	}
	return actor, nil
}
func (s *Service) tickBMCPolicy(ctx context.Context, id string) (bool, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	if e = advisory(ctx, tx, "keel-admin-initialization"); e != nil {
		return false, e
	}
	p, e := scanBMCPolicy(tx.QueryRowContext(ctx, `SELECT `+bmcPolicyFields+` FROM bmc_collection_policies WHERE id=$1 AND enabled FOR UPDATE SKIP LOCKED`, id))
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	run := BMCPolicyRun{ID: platform.RandomID(), PolicyID: p.ID, ProjectID: p.ProjectID, PolicyVersion: p.StateVersion, PolicySnapshot: *p, State: "blocked", Code: "BMC_ADMISSION_BLOCKED", ScheduledAt: now, CreatedAt: now}
	// Retry only a confirmed stopped failure with an explicitly retryable domain
	// failure. Unknown, cancelled, unsupported and evidence-free crashes cannot
	// qualify. Parent uniqueness prevents two workers from creating two children.
	var parent string
	var attempt int
	e = tx.QueryRowContext(ctx, `SELECT r.id,r.attempt_no+1 FROM bmc_policy_runs r JOIN bmc_collection_policies p ON p.id=r.policy_id JOIN exec_executions x ON x.id=r.execution_id WHERE p.id=$1 AND `+bmcRetryEligible+` AND x.finished_at+make_interval(secs=>p.retry_delay_seconds)<=statement_timestamp() ORDER BY x.finished_at,r.id LIMIT 1`, p.ID).Scan(&parent, &attempt)
	if e == nil {
		run.TriggerKind = "retry"
		run.TriggerKey = "retry:" + parent
		run.SourceRef = parent
		run.ParentRunID = &parent
		run.AttemptNo = attempt
	} else if !errors.Is(e, sql.ErrNoRows) {
		return false, e
	} else if p.TriggerKind == "cron" {
		if p.NextDue == nil || p.NextDue.After(now) {
			return false, nil
		}
		run.TriggerKind = "cron"
		run.TriggerKey = fmt.Sprintf("v%d:%s", p.StateVersion, p.NextDue.UTC().Format(time.RFC3339))
		run.SourceRef = p.NextDue.UTC().Format(time.RFC3339)
		run.ScheduledAt = *p.NextDue
		cron, e := schedule.Parse(p.CronExpression)
		if e != nil {
			return false, e
		}
		next, e := cron.Next(now)
		if e != nil {
			return false, e
		}
		p.NextDue = &next
	} else {
		var seq int64
		var machine, batch string
		e = tx.QueryRowContext(ctx, `SELECT sequence,source_ref,coalesce(machine_id,''),coalesce(credential_batch_ref,''),occurred_at FROM bmc_inventory_events WHERE project_id=$1 AND event_type=$2 AND sequence>$3 ORDER BY sequence LIMIT 1`, p.ProjectID, p.EventType, p.LastEventSequence).Scan(&seq, &run.SourceRef, &machine, &batch, &run.ScheduledAt)
		if errors.Is(e, sql.ErrNoRows) {
			return false, nil
		}
		if e != nil {
			return false, e
		}
		p.LastEventSequence = seq
		run.TriggerKind = "event"
		run.TriggerKey = fmt.Sprintf("v%d:event:%d", p.StateVersion, seq)
		var applies bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_assets a WHERE a.id=$1 AND a.project_id=$2 AND ($3='' OR a.machine_id=$3) AND ($4='' OR $4=(SELECT v.row_snapshot#>>'{values,credential_batch_ref}' FROM bom_item_revisions v JOIN bom_import_packages b ON b.id=v.package_id AND b.project_id=v.project_id AND b.state='committed' WHERE v.project_id=a.project_id AND v.item_uid=a.machine_id ORDER BY b.committed_at DESC,b.id DESC LIMIT 1)))`, p.AssetID, p.ProjectID, machine, batch).Scan(&applies)
		if e != nil {
			return false, e
		}
		if !applies {
			if e = advanceBMCPolicyTx(ctx, tx, p, now); e != nil {
				return false, e
			}
			return false, tx.Commit()
		}
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bmc_policy_runs WHERE policy_id=$1 AND trigger_key=$2)`, p.ID, run.TriggerKey).Scan(&exists); e != nil {
		return false, e
	}
	if exists {
		if e = advanceBMCPolicyTx(ctx, tx, p, now); e != nil {
			return false, e
		}
		return false, tx.Commit()
	}
	actor, authorityErr := bmcPolicyOwnerTx(ctx, tx, p)
	var business *platform.Error
	if authorityErr != nil {
		if !errors.As(authorityErr, &business) {
			return false, authorityErr
		}
		run.Code = "BMC_POLICY_AUTHORITY_REVOKED"
		p.Enabled = false
		p.StateVersion++
	} else {
		var busy bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bmc_collection_requests q JOIN exec_executions x ON x.id=q.execution_id WHERE q.project_id=$1 AND q.asset_id=$2 AND x.parameters#>>'{bmc_trigger,policy_id}'=$3 AND x.state NOT IN('success','failed','killed','warning'))`, p.ProjectID, p.AssetID, p.ID).Scan(&busy)
		if e != nil {
			return false, e
		}
		if busy {
			run.Code = "BMC_COLLECTION_BUSY"
		} else {
			var version int64
			e = tx.QueryRowContext(ctx, `SELECT version FROM project_assets WHERE id=$1 AND project_id=$2 FOR SHARE`, p.AssetID, p.ProjectID).Scan(&version)
			if errors.Is(e, sql.ErrNoRows) {
				run.Code = "BMC_ASSET_REMOVED"
			} else if e != nil {
				return false, e
			} else {
				if _, e = tx.ExecContext(ctx, `SAVEPOINT bmc_policy_admission`); e != nil {
					return false, e
				}
				trigger := &BMCExecutionTrigger{PolicyID: p.ID, PolicyVersion: run.PolicyVersion, RunID: run.ID, TriggerKey: run.TriggerKey, SourceRef: run.SourceRef, AttemptNo: run.AttemptNo}
				if run.ParentRunID != nil {
					trigger.ParentRunID = *run.ParentRunID
				}
				x, admissionErr := s.createBMCCollectionTx(ctx, tx, actor, p.ProjectID, p.AssetID, platform.BMCCollectionInput{Level: p.Level, ExpectedAssetVersion: version, Reason: "Inventory policy " + p.ID + " " + run.TriggerKey}, "bmc-policy:"+run.ID, trigger, "")
				if admissionErr != nil {
					if !errors.As(admissionErr, &business) {
						return false, admissionErr
					}
					if _, e = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT bmc_policy_admission`); e != nil {
						return false, e
					}
					run.Code = business.Code
				} else {
					run.ExecutionID = &x.ID
					run.State = "admitted"
					run.Code = "BMC_COLLECTION_ADMITTED"
				}
				if _, e = tx.ExecContext(ctx, `RELEASE SAVEPOINT bmc_policy_admission`); e != nil {
					return false, e
				}
			}
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO bmc_policy_runs(id,policy_id,project_id,policy_version,policy_snapshot,trigger_key,trigger_kind,source_ref,parent_run_id,attempt_no,execution_id,state,code,scheduled_at,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, run.ID, run.PolicyID, run.ProjectID, run.PolicyVersion, jsonBytes(run.PolicySnapshot), run.TriggerKey, run.TriggerKind, run.SourceRef, run.ParentRunID, run.AttemptNo, run.ExecutionID, run.State, run.Code, run.ScheduledAt, run.CreatedAt)
	if e != nil {
		return false, e
	}
	if e = advanceBMCPolicyTx(ctx, tx, p, now); e != nil {
		return false, e
	}
	if e = audit(ctx, tx, p.CreatedBy, p.ProjectID, "bmc.policy.trigger", p.ID, "worker", map[string]any{"run_id": run.ID, "trigger_kind": run.TriggerKind, "state": run.State, "code": run.Code, "execution_id": run.ExecutionID, "policy_version": run.PolicyVersion}); e != nil {
		return false, e
	}
	return run.State == "admitted", tx.Commit()
}
func advanceBMCPolicyTx(ctx context.Context, tx *sql.Tx, p *BMCPolicy, now time.Time) error {
	_, e := tx.ExecContext(ctx, `UPDATE bmc_collection_policies SET next_due=$2,last_event_sequence=$3,enabled=$4,state_version=$5,updated_at=$6 WHERE id=$1`, p.ID, p.NextDue, p.LastEventSequence, p.Enabled, p.StateVersion, now)
	return e
}
