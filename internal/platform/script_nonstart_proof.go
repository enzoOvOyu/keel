package platform

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// Shared by worker and Agent broker; transport cannot manufacture authority to
// settle a missing receipt. The human request is bound to the original command,
// unknown attempt version, current requester role and a five-minute lifetime.
func ScriptNonstartProofAllowedTx(ctx context.Context, tx *sql.Tx, c taskrunner.TaskCommand) (bool, error) {
	if c.Validate() != nil || c.Operation != "script.run" {
		return false, nil
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return false, err
	}
	var allowed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id JOIN projects p ON p.id=x.project_id JOIN exec_outbox o ON o.execution_id=x.id AND o.event_type='script.nonstart_requested' JOIN users u ON u.id=o.payload->>'requested_by' WHERE a.id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND a.state='unknown' AND x.mode IN('ssh','agent') AND x.project_id=$4 AND p.state NOT IN('archived','delivered') AND o.payload->'command'=$5::jsonb AND o.payload->>'attempt_id'=a.id AND o.payload->>'attempt_version'=a.state_version::text AND o.created_at>clock_timestamp()-interval '5 minutes' AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=x.project_id AND m.user_id=u.id AND m.role IN('Admin','DL','SRE'))))`, c.AttemptID, c.CommandID, c.FencingToken, c.ProjectID, raw).Scan(&allowed)
	return allowed, err
}
