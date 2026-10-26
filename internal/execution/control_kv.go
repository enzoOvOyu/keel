package execution

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// KV is public project metadata, explicitly separate from secrets and device
// state. Put always carries a CAS version: zero means create, never overwrite.
type KVSpec struct {
	Key             string  `json:"key" yaml:"key"`
	Value           *string `json:"value,omitempty" yaml:"value,omitempty"`
	ExpectedVersion *int64  `json:"expected_version,omitempty" yaml:"expected_version,omitempty"`
}

var kvKey = regexp.MustCompile(`^[a-z][a-z0-9_.-]{0,63}$`)

func validateKVTask(t Task) error {
	if t.KV == nil || !kvKey.MatchString(t.KV.Key) || t.Retry != 0 || t.TimeoutMS < 100 || t.TimeoutMS > 5000 || len(t.Resources) != 1 || t.Resources[0] != "project" || t.Sim != (SimSpec{}) || t.WaitMS != 0 || t.MediaID != "" || t.Script != nil || t.Artifact != nil || t.ScriptResultFormat != "" || t.ScriptPreferCache {
		return invalid("control tasks require bounded media-free KV settings and exactly the project resource")
	}
	for _, word := range []string{"password", "secret", "token", "credential", "private_key"} {
		if strings.Contains(t.KV.Key, word) {
			return invalid("KV is public metadata; secret-bearing keys are not supported")
		}
	}
	switch t.Type {
	case "kv.get":
		if !t.Idempotent || t.KV.Value != nil || t.KV.ExpectedVersion != nil {
			return invalid("kv.get is read-only and takes only a key")
		}
	case "kv.put":
		if t.Idempotent || t.KV.Value == nil || t.KV.ExpectedVersion == nil || *t.KV.ExpectedVersion < 0 || *t.KV.ExpectedVersion >= 9223372036854775807 || !utf8.ValidString(*t.KV.Value) || utf8.RuneCountInString(*t.KV.Value) > 4096 || len(jsonBytes(*t.KV.Value)) > 8192 || strings.ContainsRune(*t.KV.Value, 0) {
			return invalid("kv.put requires public text up to4096 characters, an exact nonnegative expected_version and no automatic retry")
		}
	default:
		return invalid("control workflows support only kv.get and kv.put")
	}
	return nil
}

// Caller holds the execution row after expiring this attempt. Original KV
// transactions need that same row, so absence of a receipt proves that no KV
// transaction committed. Preserve unknown/quarantine until explicit reconcile.
func recordControlUncommittedTx(ctx context.Context, tx *sql.Tx, attemptID string) error {
	a, err := scanAttempt(tx.QueryRowContext(ctx, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1 AND runner='control' AND state='unknown' FOR UPDATE", attemptID))
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	started := now
	if a.StartedAt != nil {
		started = *a.StartedAt
	}
	r := Receipt{CommandID: a.CommandID, AttemptID: a.ID, FencingToken: a.FencingToken, Result: "failed", StartedAt: started, FinishedAt: now, Output: map[string]any{"source": "control_plane", "reason": "kv_transaction_not_committed", "mutation_committed": false, "hardware_evidence": false}}
	data := jsonBytes(r)
	_, err = tx.ExecContext(ctx, `INSERT INTO exec_runner_receipts VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(command_id) DO NOTHING`, r.CommandID, r.AttemptID, r.FencingToken, r.Result, data, hash(data))
	return err
}

type ControlFlowCreateInput struct {
	FlowVersionID string `json:"flow_version_id"`
}

func (s *Service) CreateControlFlow(ctx context.Context, a *Actor, project string, in ControlFlowCreateInput, key, trace string) (*Execution, error) {
	if !opaqueID.MatchString(in.FlowVersionID) {
		return nil, invalid("a published control flow version is required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = authorize(ctx, tx, a, project, "Admin", "DE", "DL", "SRE"); err != nil {
		return nil, err
	}
	idem, prior, err := idempotency(ctx, tx, a, "control.flow.create:"+project, key, in)
	if err != nil {
		return nil, err
	}
	if prior != nil {
		x, e := decode[Execution](prior)
		return &x, e
	}
	flow, err := scanFlow(tx.QueryRowContext(ctx, "SELECT "+flowColumns+" FROM exec_flow_versions v JOIN exec_flows f ON f.id=v.flow_id WHERE v.id=$1 AND v.project_id=$2", in.FlowVersionID, project))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if flow.Definition.APIVersion != "keel.control/v1" || flow.ContentHash != hash(jsonBytes(flow.Definition)) {
		return nil, invalid("only a valid published control workflow can use this endpoint")
	}
	if err = flow.Definition.Validate(); err != nil {
		return nil, err
	}
	policy, err := platform.ResolveExecutionCircuitBreakerTx(ctx, tx, project)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	target := Target{ProjectID: project, Name: "Project workflow metadata", ResourceKey: project, Mode: "control", Revision: 1, ModelVersion: "not_applicable", CreatedAt: now}
	err = tx.QueryRowContext(ctx, `INSERT INTO exec_targets(id,project_id,kind,resource_key) VALUES($1,$2,'project',$2) ON CONFLICT(project_id,kind,resource_key) DO UPDATE SET resource_key=EXCLUDED.resource_key RETURNING id`, platform.RandomID(), project).Scan(&target.ID)
	if err != nil {
		return nil, err
	}
	params := map[string]any{}
	x := &Execution{ID: platform.RandomID(), ProjectID: project, FlowVersionID: flow.ID, Mode: "control", State: "created", StateVersion: 1, DesiredState: "running", Parameters: params, TargetSetHash: hash(jsonBytes([]string{target.ID})), Concurrency: 1, CircuitBreakerPercent: policy.DefaultPercent, CreatedBy: a.User.ID, CreatedAt: now}
	x.Snapshot = Snapshot{CircuitBreakerPolicy: policy, Mode: "control", Flow: flow, Targets: []Target{target}, Parameters: params, TargetSetHash: x.TargetSetHash, Admission: "Public project metadata only; no credentials, media, device transport or hardware evidence; CAS writes and immutable receipts commit atomically", FrozenAt: now}
	_, err = tx.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,mode,parameters,snapshot,target_set_hash,concurrency,circuit_breaker_percent,created_by,created_at) VALUES($1,$2,$3,'control',$4,$5,$6,1,$7,$8,$9)`, x.ID, project, flow.ID, jsonBytes(params), jsonBytes(x.Snapshot), x.TargetSetHash, x.CircuitBreakerPercent, a.User.ID, now)
	if err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO exec_execution_targets(execution_id,target_id,snapshot) VALUES($1,$2,$3)`, x.ID, target.ID, jsonBytes(target)); err != nil {
		return nil, err
	}
	for i, task := range flow.Definition.Tasks {
		_, err = tx.ExecContext(ctx, `INSERT INTO exec_task_attempts(id,execution_id,target_id,task_id,task_index,attempt_no,command_id,runner,created_at) VALUES($1,$2,$3,$4,$5,1,$6,'control',$7)`, platform.RandomID(), x.ID, target.ID, task.ID, i, platform.RandomID(), now)
		if err != nil {
			return nil, err
		}
	}
	if err = event(ctx, tx, x.ID, "execution.created", map[string]any{"mode": "control", "target_count": 1}); err != nil {
		return nil, err
	}
	if err = audit(ctx, tx, a.User.ID, project, "execution.control.create", x.ID, trace, map[string]any{"flow_version_id": flow.ID, "flow_content_hash": flow.ContentHash}); err != nil {
		return nil, err
	}
	if err = remember(ctx, tx, idem, in, x); err != nil {
		return nil, err
	}
	return x, tx.Commit()
}

// A single database transaction owns all observable effects. A worker can never
// commit a KV change without the matching durable receipt and attempt outcome.
func (s *Service) runControlClaim(ctx context.Context, c *Claim) error {
	work, cancel := context.WithTimeout(ctx, time.Duration(c.Task.TimeoutMS)*time.Millisecond)
	defer cancel()
	tx, err := s.DB.BeginTx(work, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var creator, project string
	if err = tx.QueryRowContext(work, `SELECT created_by,project_id FROM exec_executions WHERE id=$1 AND mode='control'`, c.Attempt.ExecutionID).Scan(&creator, &project); err != nil {
		return err
	}
	var enabled, admin bool
	if err = tx.QueryRowContext(work, `SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE`, creator).Scan(&enabled, &admin); err != nil {
		return err
	}
	var projectState string
	if err = tx.QueryRowContext(work, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, project).Scan(&projectState); err != nil {
		return err
	}
	allowed := enabled && projectState != "archived" && projectState != "delivered"
	if !admin {
		var role string
		err = tx.QueryRowContext(work, `SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE`, project, creator).Scan(&role)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		allowed = allowed && (role == "Admin" || role == "DE" || role == "DL" || role == "SRE")
	}
	x, err := scanExecution(tx.QueryRowContext(work, "SELECT "+executionColumns+" FROM exec_executions WHERE id=$1 FOR UPDATE", c.Attempt.ExecutionID))
	if err != nil {
		return err
	}
	a, err := scanAttempt(tx.QueryRowContext(work, "SELECT "+attemptColumns+" FROM exec_task_attempts WHERE id=$1 AND execution_id=$2 FOR UPDATE", c.Attempt.ID, x.ID))
	if err != nil {
		return err
	}
	if a.FencingToken != c.Attempt.FencingToken || a.LeaseOwner != c.Attempt.LeaseOwner || a.CommandID != c.Attempt.CommandID {
		return failure(409, "STALE_FENCE", "Control attempt ownership changed")
	}
	if a.State == "success" || a.State == "failed" || a.State == "cancelled" {
		return nil
	}
	if a.State != "running" || a.LeaseUntil == nil {
		return failure(409, "STALE_LEASE", "Control attempt is no longer running")
	}
	var live bool
	if err = tx.QueryRowContext(work, `SELECT $1::timestamptz>clock_timestamp() AND EXISTS(SELECT 1 FROM exec_resource_leases WHERE attempt_id=$2 AND fencing_token=$3 AND resource_id=$4 AND state='held' AND expires_at>clock_timestamp())`, *a.LeaseUntil, a.ID, a.FencingToken, "project-kv:"+project).Scan(&live); err != nil {
		return err
	}
	if !live {
		return failure(409, "STALE_LEASE", "Control authorization window expired")
	}
	task, err := taskByID(x.Snapshot.Flow.Definition, a.TaskID)
	if err != nil {
		return err
	}
	if x.Snapshot.Flow.Definition.APIVersion != "keel.control/v1" || validateKVTask(task) != nil || hash(jsonBytes(task)) != hash(jsonBytes(c.Task)) {
		return invalid("Control task does not match its frozen definition")
	}
	if err = advisory(work, tx, "project-kv:"+project); err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	r := Receipt{CommandID: a.CommandID, AttemptID: a.ID, FencingToken: a.FencingToken, Result: "success", StartedAt: now, Output: map[string]any{"source": "control_plane", "operation": task.Type, "key": task.KV.Key, "hardware_evidence": false}}
	if !allowed || x.DesiredState == "killed" {
		r.Result = "cancelled"
		r.Output["reason"] = "authorization_or_execution_stopped_before_kv"
	} else {
		var value string
		var version int64
		err = tx.QueryRowContext(work, `SELECT value,version FROM exec_project_kv WHERE project_id=$1 AND key=$2`, project, task.KV.Key).Scan(&value, &version)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if task.Type == "kv.get" {
			if errors.Is(err, sql.ErrNoRows) {
				r.Result = "failed"
				r.Output["reason"] = "kv_key_not_found"
			} else {
				r.Output["value"], r.Output["version"] = value, version
			}
		} else if version != *task.KV.ExpectedVersion {
			r.Result = "failed"
			r.Output["reason"] = "kv_version_conflict"
			r.Output["current_version"] = version
		} else {
			var count int
			if version == 0 {
				if err = tx.QueryRowContext(work, `SELECT count(*) FROM exec_project_kv WHERE project_id=$1`, project).Scan(&count); err != nil {
					return err
				}
			}
			if count >= 128 {
				r.Result = "failed"
				r.Output["reason"] = "kv_project_key_limit"
			} else {
				result, err := tx.ExecContext(work, `INSERT INTO exec_project_kv(project_id,key,value,version,updated_by_command,updated_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(project_id,key) DO UPDATE SET value=EXCLUDED.value,version=EXCLUDED.version,updated_by_command=EXCLUDED.updated_by_command,updated_at=EXCLUDED.updated_at WHERE exec_project_kv.version=$7`, project, task.KV.Key, *task.KV.Value, version+1, a.CommandID, now, *task.KV.ExpectedVersion)
				if err != nil {
					return err
				}
				changed, err := result.RowsAffected()
				if err != nil {
					return err
				}
				if changed != 1 {
					r.Result = "failed"
					r.Output["reason"] = "kv_version_conflict"
				} else {
					r.Output["value"], r.Output["version"] = *task.KV.Value, version+1
				}
			}
		}
	}
	// Deadline is checked again after any lock wait and before committing effects.
	if err = tx.QueryRowContext(work, `SELECT $1::timestamptz>clock_timestamp()`, *a.LeaseUntil).Scan(&live); err != nil {
		return err
	}
	if !live {
		return failure(409, "STALE_LEASE", "Control authorization window expired before commit")
	}
	r.FinishedAt = time.Now().UTC().Truncate(time.Microsecond)
	data := jsonBytes(r)
	if len(data) > 16384 {
		return invalid("KV receipt exceeds16KiB")
	}
	if _, err = tx.ExecContext(work, `INSERT INTO exec_runner_receipts VALUES($1,$2,$3,$4,$5,$6,now())`, r.CommandID, r.AttemptID, r.FencingToken, r.Result, data, hash(data)); err != nil {
		return err
	}
	if err = audit(work, tx, creator, project, "execution.kv."+strings.TrimPrefix(task.Type, "kv."), a.ID, "worker", map[string]any{"key": task.KV.Key, "result": r.Result, "command_id": a.CommandID}); err != nil {
		return err
	}
	if err = applyReceipt(work, tx, a, r, false); err != nil {
		return err
	}
	if err = settle(work, tx, x.ID); err != nil {
		return err
	}
	return tx.Commit()
}
