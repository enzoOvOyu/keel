package platform

import (
	"context"
	"time"
)

type IssueEscalationNotice struct {
	RequestID   string    `json:"request_id"`
	IssueID     string    `json:"issue_id"`
	ProjectID   string    `json:"project_id"`
	ExecutionID string    `json:"execution_id"`
	Title       string    `json:"title"`
	CreatedAt   time.Time `json:"created_at"`
}

func (s *Store) IssueEscalationInbox(ctx context.Context, a *AuthSession, project string, p Pagination) (Page[IssueEscalationNotice], error) {
	out := NewPage[IssueEscalationNotice](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	if project != "" && !validEntityID(project) {
		return out, ErrNotFound
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if project != "" {
		if _, e = issueRoleTx(ctx, tx, a, project); e != nil {
			return out, e
		}
	}
	where := ` FROM (SELECT request_id,issue_id,project_id,execution_id,recipient_ids,created_at FROM issue_escalations UNION ALL SELECT request_id,issue_id,project_id,execution_id,recipient_ids,created_at FROM topology_issue_escalations) n JOIN issues i ON i.id=n.issue_id WHERE ($3='' OR n.project_id=$3) AND ($2 OR (n.recipient_ids @> jsonb_build_array($1::text) AND EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=$1 AND m.project_id=n.project_id AND m.role='DL')))`
	if e = tx.QueryRowContext(ctx, "SELECT count(*)"+where, a.User.ID, a.User.IsAdmin, project).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT n.request_id,n.issue_id,n.project_id,n.execution_id,i.title,n.created_at"+where+" ORDER BY n.created_at DESC,n.request_id DESC LIMIT $4 OFFSET $5", a.User.ID, a.User.IsAdmin, project, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v IssueEscalationNotice
		if e = rows.Scan(&v.RequestID, &v.IssueID, &v.ProjectID, &v.ExecutionID, &v.Title, &v.CreatedAt); e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
