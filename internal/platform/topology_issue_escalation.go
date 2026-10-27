package platform

import (
	"context"
	"database/sql"
	"time"
)

func topologyRetestEscalationTx(ctx context.Context, tx *sql.Tx, a *AuthSession, id, issue, project, execution, trace string) error {
	var failures int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT verdict FROM topology_issue_retests WHERE issue_id=$1 AND verdict IN('PASS','FAIL') ORDER BY created_at DESC,id DESC LIMIT 2) v WHERE verdict='FAIL'`, issue).Scan(&failures); e != nil || failures < 2 {
		return e
	}
	var exists bool
	if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM topology_issue_escalations WHERE issue_id=$1 AND created_at>coalesce((SELECT max(created_at) FROM topology_issue_retests WHERE issue_id=$1 AND verdict='PASS'),'-infinity'::timestamptz))`, issue).Scan(&exists); e != nil || exists {
		return e
	}
	rows, e := tx.QueryContext(ctx, `SELECT u.id FROM memberships m JOIN users u ON u.id=m.user_id AND u.enabled WHERE m.project_id=$1 AND m.role='DL' ORDER BY u.id`, project)
	if e != nil {
		return e
	}
	recipients := []string{}
	for rows.Next() {
		var recipient string
		if e = rows.Scan(&recipient); e != nil {
			rows.Close()
			return e
		}
		recipients = append(recipients, recipient)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	at := time.Now().UTC()
	if _, e = tx.ExecContext(ctx, `INSERT INTO topology_issue_escalations(request_id,issue_id,project_id,execution_id,recipient_ids,failed_verifications,created_at) VALUES($1,$2,$3,$4,$5,2,$6)`, id, issue, project, execution, networkJSON(recipients), at); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_outbox(id,execution_id,aggregate_version,event_type,payload,created_at) SELECT $1,id,state_version,'issue.escalated',$3,$4 FROM exec_executions WHERE id=$2`, RandomID(), execution, networkJSON(map[string]any{"issue_id": issue, "request_id": id, "failed_verifications": 2, "recipient_count": len(recipients), "source": "topology", "recommendation": "diagnose_with_DL_and_vendor_no_automatic_flash_or_RMA"}), at); e != nil {
		return e
	}
	return appendAudit(ctx, tx, a.User.ID, project, "issue.escalated", issue, trace, map[string]any{"retest_id": id, "failed_verifications": 2, "recipient_count": len(recipients), "source": "topology", "notification": "durable_in_app_and_outbox"})
}
