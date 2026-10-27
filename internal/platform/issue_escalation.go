package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// A single escalation is emitted for each consecutive failure streak. Missing
// evidence is BLOCKED and does not manufacture another failed verification.
func issueVerificationEscalationTx(ctx context.Context, tx *sql.Tx, request *IssueVerificationRequest) error {
	var failures int
	e := tx.QueryRowContext(ctx, `SELECT count(*) FROM (SELECT r.verdict FROM issue_verification_results r JOIN issue_verification_requests q ON q.id=r.request_id WHERE q.issue_id=$1 AND r.verdict IN('PASS','FAIL') ORDER BY r.evaluated_at DESC,q.created_at DESC,q.id DESC LIMIT 2) v WHERE verdict='FAIL'`, request.IssueID).Scan(&failures)
	if e != nil || failures < 2 {
		return e
	}
	var already bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM issue_escalations e WHERE e.issue_id=$1 AND e.created_at>coalesce((SELECT max(r.evaluated_at) FROM issue_verification_results r JOIN issue_verification_requests q ON q.id=r.request_id WHERE q.issue_id=$1 AND r.verdict='PASS'),'-infinity'::timestamptz))`, request.IssueID).Scan(&already)
	if e != nil || already {
		return e
	}
	rows, e := tx.QueryContext(ctx, `SELECT u.id FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.project_id=$1 AND m.role='DL' AND u.enabled ORDER BY u.id`, request.ProjectID)
	if e != nil {
		return e
	}
	recipients := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
		recipients = append(recipients, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	raw, e := json.Marshal(recipients)
	if e != nil {
		return e
	}
	at := time.Now().UTC()
	if _, e = tx.ExecContext(ctx, `INSERT INTO issue_escalations(request_id,issue_id,project_id,execution_id,recipient_ids,failed_verifications,created_at) VALUES($1,$2,$3,$4,$5,2,$6)`, request.ID, request.IssueID, request.ProjectID, request.ExecutionID, raw, at); e != nil {
		return e
	}
	payload, e := json.Marshal(map[string]any{"issue_id": request.IssueID, "request_id": request.ID, "failed_verifications": 2, "recipient_count": len(recipients), "recommendation": "review_evidence_and_consider_replacement"})
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO exec_outbox(id,execution_id,aggregate_version,event_type,payload,created_at) SELECT $1,id,state_version,'issue.escalated',$3,$4 FROM exec_executions WHERE id=$2`, RandomID(), request.ExecutionID, payload, at); e != nil {
		return e
	}
	return appendAudit(ctx, tx, request.CreatedBy, request.ProjectID, "issue.escalated", request.IssueID, "worker", map[string]any{"request_id": request.ID, "failed_verifications": 2, "recipient_count": len(recipients), "notification": "durable_in_app_and_outbox"})
}
