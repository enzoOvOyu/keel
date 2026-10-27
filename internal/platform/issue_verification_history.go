package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

type IssueVerificationStatus struct {
	Escalated           bool                     `json:"escalated"`
	RecipientIDs        []string                 `json:"recipient_ids"`
	Request             IssueVerificationRequest `json:"request"`
	ExecutionState      string                   `json:"execution_state"`
	Verdict             string                   `json:"verdict"`
	Code                string                   `json:"code"`
	EvidenceID          string                   `json:"evidence_id"`
	Finding             *fabricmon.Finding       `json:"finding"`
	ValidUntil          *time.Time               `json:"valid_until"`
	AppliedIssueVersion *int                     `json:"applied_issue_version"`
	Stale               bool                     `json:"stale"`
	CanClose            bool                     `json:"can_close"`
}

func (s *Store) IssueVerifications(ctx context.Context, a *AuthSession, id string, p Pagination) (Page[IssueVerificationStatus], error) {
	out := NewPage[IssueVerificationStatus](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	item, e := scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1", id))
	if errors.Is(e, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	role, e := issueRoleTx(ctx, tx, a, item.ProjectID)
	if e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM issue_verification_requests WHERE issue_id=$1", id).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT q.id,q.issue_id,q.project_id,q.execution_id,q.issue_version,q.scope_hash,q.snapshot,q.reason,q.created_by,q.created_at,x.state,coalesce(r.verdict,''),coalesce(r.code,''),coalesce(r.evidence_id,''),coalesce(r.finding,'null'),r.valid_until,r.applied_issue_version,EXISTS(SELECT 1 FROM issue_escalations e WHERE e.request_id=q.id),coalesce((SELECT e.recipient_ids FROM issue_escalations e WHERE e.request_id=q.id),'[]') FROM issue_verification_requests q JOIN exec_executions x ON x.id=q.execution_id LEFT JOIN issue_verification_results r ON r.request_id=q.id WHERE q.issue_id=$1 ORDER BY q.created_at DESC,q.id DESC LIMIT $2 OFFSET $3`, id, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v IssueVerificationStatus
		var snapshot, finding, recipients []byte
		q := &v.Request
		if e = rows.Scan(&q.ID, &q.IssueID, &q.ProjectID, &q.ExecutionID, &q.IssueVersion, &q.ScopeHash, &snapshot, &q.Reason, &q.CreatedBy, &q.CreatedAt, &v.ExecutionState, &v.Verdict, &v.Code, &v.EvidenceID, &finding, &v.ValidUntil, &v.AppliedIssueVersion, &v.Escalated, &recipients); e != nil {
			rows.Close()
			return out, e
		}
		if e = json.Unmarshal(recipients, &v.RecipientIDs); e != nil {
			rows.Close()
			return out, e
		}
		if e = json.Unmarshal(snapshot, &q.Snapshot); e != nil {
			rows.Close()
			return out, e
		}
		if e = json.Unmarshal(finding, &v.Finding); e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for i := range out.Items {
		v := &out.Items[i]
		current, err := s.issueVerificationScopeMatchesTx(ctx, tx, &v.Request, item)
		if err != nil {
			return out, err
		}
		v.Stale = !current || v.ValidUntil != nil && !v.ValidUntil.After(time.Now())
		v.CanClose = v.Verdict == "PASS" && v.ExecutionState == "success" && !v.Stale && v.AppliedIssueVersion != nil && *v.AppliedIssueVersion == item.Version && item.State == "verify" && canConfirmIssueVerification(item, role, a.User.ID)
	}
	return out, tx.Commit()
}
func canConfirmIssueVerification(item *Issue, role, actor string) bool {
	return role == "Admin" || role == "DL" || role == "NE" && item.AssigneeID == actor
}
func (s *Store) issueVerificationCloseEvidenceTx(ctx context.Context, tx *sql.Tx, item *Issue) (*IssueVerificationRequest, error) {
	if item.Source == "g2" {
		return s.wiringCloseEvidenceTx(ctx, tx, item)
	}
	if item.Source == "topology" && item.Category == "T7" {
		return s.topologyCloseEvidenceTx(ctx, tx, item)
	}
	var id, execution string
	var expiry time.Time
	e := tx.QueryRowContext(ctx, `SELECT q.id,q.execution_id,r.valid_until FROM issue_verification_results r JOIN issue_verification_requests q ON q.id=r.request_id JOIN exec_executions x ON x.id=q.execution_id WHERE q.issue_id=$1 AND r.verdict='PASS' AND r.applied_issue_version=$2 AND x.state='success' ORDER BY r.evaluated_at DESC,q.id DESC LIMIT 1`, item.ID, item.Version).Scan(&id, &execution, &expiry)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "ISSUE_REVALIDATION_REQUIRED", "Current successful scoped verification is required")
	}
	if e != nil {
		return nil, e
	}
	if !expiry.After(time.Now()) {
		return nil, apiError(409, "ISSUE_VERIFICATION_EVIDENCE_EXPIRED", "Scoped verification evidence has expired")
	}
	request, e := loadIssueVerificationTx(ctx, tx, id, execution)
	if e != nil {
		return nil, e
	}
	current, e := s.issueVerificationScopeMatchesTx(ctx, tx, request, item)
	if e != nil {
		return nil, e
	}
	if !current {
		return nil, apiError(409, "ISSUE_VERIFICATION_SCOPE_CHANGED", "Verification authority, identity, BOM, policy or repair scope changed")
	}
	return request, nil
}
func (s *Store) issueVerificationTransitionsTx(ctx context.Context, tx *sql.Tx, a *AuthSession, item *Issue, role string) error {
	if !canConfirmIssueVerification(item, role, a.User.ID) {
		return nil
	}
	if item.State == "in_repair" && s.FabricIssueVerificationEnabled && item.Source == "fabric" {
		scope, e := s.issueVerificationScopeTx(ctx, tx, a, item, role)
		if e != nil {
			return e
		}
		if scope.ExecutionAvailable {
			item.AllowedTransitions = append(item.AllowedTransitions, "verify")
		}
	}
	if item.State == "verify" {
		_, e := s.issueVerificationCloseEvidenceTx(ctx, tx, item)
		if e == nil {
			item.AllowedTransitions = append(item.AllowedTransitions, "close")
		} else {
			var business *Error
			if !errors.As(e, &business) || business.Status != 409 {
				return e
			}
		}
		if !slices.Contains(item.AllowedTransitions, "repair") {
			item.AllowedTransitions = append(item.AllowedTransitions, "repair")
		}
	}
	return nil
}
func issueVerificationRepairAllowedTx(ctx context.Context, tx *sql.Tx, item *Issue) error {
	var active bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM issue_verification_requests q JOIN exec_executions x ON x.id=q.execution_id WHERE q.issue_id=$1 AND (x.state NOT IN('success','failed','killed','warning') OR EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_resource_leases l ON l.attempt_id=a.id WHERE a.execution_id=x.id AND l.state IN('held','reconciling')))) OR EXISTS(SELECT 1 FROM validation_wiring_issue_bindings b JOIN validation_wiring_run_targets target ON target.asset_id=b.asset_id JOIN exec_executions x ON x.id=target.execution_id WHERE b.issue_id=$1 AND EXISTS(SELECT 1 FROM jsonb_array_elements(target.scope#>'{expectation,ports}') port WHERE port->>'physical_port_id'=b.physical_port_id) AND (x.state NOT IN('success','failed','killed','warning') OR EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_resource_leases l ON l.attempt_id=a.id WHERE a.execution_id=x.id AND l.state IN('held','reconciling'))))`, item.ID).Scan(&active)
	if e != nil {
		return e
	}
	if active {
		return apiError(409, "ISSUE_VERIFICATION_ACTIVE", "Finish or reconcile the accepted verification before recording another repair")
	}
	return nil
}
