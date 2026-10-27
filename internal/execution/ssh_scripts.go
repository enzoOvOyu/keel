package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"time"
)

type SSHScriptCreateInput struct {
	PreferCache  bool     `json:"prefer_cache,omitempty"`
	ResultFormat string   `json:"result_format,omitempty"`
	MachineIDs   []string `json:"machine_ids"`
	MediaID      string   `json:"media_id"`
	TimeoutMS    int      `json:"timeout_ms"`
}

func (s *Service) CreateSSHScript(ctx context.Context, a *Actor, project string, in SSHScriptCreateInput, key, trace string) (*Execution, error) {
	if s.MediaStore == nil || !s.MediaStore.SSHScriptsEnabled {
		return nil, failure(409, "SSH_SCRIPTS_DISABLED", "Approved SSH scripts are not enabled")
	}
	if !opaqueID.MatchString(in.MediaID) {
		return nil, invalid("An approved script media ID is required")
	}
	return s.createSSH(ctx, a, project, SSHCreateInput{MachineIDs: in.MachineIDs, Operation: "script.run", ScriptMediaID: in.MediaID, TimeoutMS: in.TimeoutMS, ScriptResultFormat: in.ResultFormat, PreferScriptCache: in.PreferCache}, "", 0, key, trace)
}

func (s *Service) scriptFrozenTx(ctx context.Context, tx *sql.Tx, project, mode string, task Task) error {
	if task.Script == nil {
		if task.Type == "script.run" {
			return failure(409, "TASK_SCRIPT_SNAPSHOT_REQUIRED", "Script execution requires an immutable approved artifact snapshot")
		}
		return nil
	}
	if (task.ScriptResultFormat != "" && task.ScriptResultFormat != "json") || (mode != "ssh" && mode != "agent") || task.Type != "script.run" || task.Retry != 0 || task.Idempotent || s.MediaStore == nil {
		return failure(409, "TASK_SCRIPT_DISABLED", "Script adapter or frozen execution policy is unavailable")
	}
	if mode == "ssh" && !s.MediaStore.SSHScriptsEnabled || mode == "agent" && !s.MediaStore.AgentScriptsEnabled {
		return failure(409, "TASK_SCRIPT_DISABLED", "Approved script transport is disabled")
	}
	_, _, err := s.MediaStore.ResolveTaskScriptTx(ctx, tx, project, task.Script.MediaID, task.Script, false)
	return err
}

func (s *Service) scriptHeartbeatTx(ctx context.Context, tx *sql.Tx, c *Claim) bool {
	if c.Task.Script == nil && c.Task.Artifact == nil {
		return false
	}
	if c.Task.Artifact != nil && s.artifactFrozenTx(ctx, tx, c.Target.ProjectID, c.Task) != nil {
		return true
	}
	if s.scriptFrozenTx(ctx, tx, c.Target.ProjectID, c.Target.Mode, c.Task) != nil {
		return true
	}
	var allowed bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_executions x JOIN users u ON u.id=x.created_by JOIN projects p ON p.id=x.project_id WHERE x.id=$1 AND u.enabled AND p.state NOT IN('archived','delivered') AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=x.project_id AND m.user_id=u.id AND m.role IN('Admin','DE','DL','SRE'))))`, c.Attempt.ExecutionID).Scan(&allowed)
	return err != nil || !allowed
}

// The attempt row serializes consumption. Once committed, an orphan dispatch
// can only be queried, even if transport IO never got a chance to start.
func (s *Service) commitScriptDispatchTx(ctx context.Context, tx *sql.Tx, c *Claim) (taskrunner.TaskCommand, error) {
	return s.commitBoundedDispatchTx(ctx, tx, c, "script.dispatch_committed")
}
func (s *Service) commitBoundedDispatchTx(ctx context.Context, tx *sql.Tx, c *Claim, kind string) (taskrunner.TaskCommand, error) {
	command := sshCommand(c)
	var until, now time.Time
	err := tx.QueryRowContext(ctx, `SELECT a.lease_until,clock_timestamp() FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id WHERE a.id=$1 AND a.execution_id=$2 AND a.command_id=$3 AND a.fencing_token=$4 AND a.lease_owner=$5 AND a.state='running' AND a.lease_until>clock_timestamp() AND x.desired_state='running' FOR UPDATE OF a FOR SHARE OF x`, c.Attempt.ID, c.Attempt.ExecutionID, c.Attempt.CommandID, c.Attempt.FencingToken, c.Attempt.LeaseOwner).Scan(&until, &now)
	if err != nil {
		return command, err
	}
	var consumed bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_outbox WHERE execution_id=$1 AND event_type=$3 AND payload->>'attempt_id'=$2)`, c.Attempt.ExecutionID, c.Attempt.ID, kind).Scan(&consumed); err != nil {
		return command, err
	}
	if consumed {
		return command, taskrunner.ErrUnknown
	}
	var claims int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND fencing_token=$2 AND (mode='exclusive' OR $5) AND state='held' AND expires_at>clock_timestamp() AND resource_id IN($3,$4)`, c.Attempt.ID, c.Attempt.FencingToken, "device:machine:"+c.Target.ResourceKey, c.Target.Mode+":machine:"+c.Target.ResourceKey, c.Task.Type == "host.precollect").Scan(&claims); err != nil {
		return command, err
	}
	if claims != 2 {
		return command, taskrunner.ErrRejected
	}
	deadline := now.Add(time.Duration(c.Task.TimeoutMS) * time.Millisecond)
	leaseDeadline := until.Add(-100 * time.Millisecond)
	// Read-only precollection can span renewed shared leases. Its initial
	// authorization is still bounded by the task timeout; writes retain the
	// stricter original lease deadline. Heartbeats cancel revoked captures.
	if c.Task.Type != "host.precollect" && leaseDeadline.Before(deadline) {
		deadline = leaseDeadline
	}
	if !deadline.After(now) {
		return command, taskrunner.ErrRejected
	}
	command.AuthorizationExpiresAt = deadline.UnixMicro()
	if command.Validate() != nil {
		return command, taskrunner.ErrRejected
	}
	err = event(ctx, tx, c.Attempt.ExecutionID, kind, map[string]any{"attempt_id": c.Attempt.ID, "command": command, "authorized_at": now, "lease_until": until})
	return command, err
}

func originalScriptCommandTx(ctx context.Context, tx *sql.Tx, c *Claim) (taskrunner.TaskCommand, error) {
	return originalDispatchCommandTx(ctx, tx, c, "script.dispatch_committed")
}
func originalDispatchCommandTx(ctx context.Context, tx *sql.Tx, c *Claim, kind string) (taskrunner.TaskCommand, error) {
	base := sshCommand(c)
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM exec_outbox WHERE execution_id=$1 AND event_type=$3 AND payload->>'attempt_id'=$2 ORDER BY sequence LIMIT 2`, c.Attempt.ExecutionID, c.Attempt.ID, kind)
	if err != nil {
		return base, err
	}
	defer rows.Close()
	count := 0
	var original taskrunner.TaskCommand
	var authorizedAt, leaseUntil time.Time
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return base, err
		}
		var saved struct {
			Command      taskrunner.TaskCommand `json:"command"`
			AuthorizedAt time.Time              `json:"authorized_at"`
			LeaseUntil   time.Time              `json:"lease_until"`
		}
		if json.Unmarshal(raw, &saved) != nil {
			return base, taskrunner.ErrUnknown
		}
		original = saved.Command
		authorizedAt = saved.AuthorizedAt
		leaseUntil = saved.LeaseUntil
		count++
	}
	if err = rows.Err(); err != nil {
		return base, err
	}
	base.AuthorizationExpiresAt = original.AuthorizationExpiresAt
	expiry := time.UnixMicro(original.AuthorizationExpiresAt)
	if count != 1 || base != original || original.Validate() != nil || !expiry.After(authorizedAt) || expiry.After(authorizedAt.Add(time.Duration(c.Task.TimeoutMS)*time.Millisecond)) || c.Task.Type != "host.precollect" && expiry.After(leaseUntil.Add(-100*time.Millisecond)) {
		return base, taskrunner.ErrUnknown
	}
	return original, nil
}
