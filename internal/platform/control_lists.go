package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Store) ListProjectsPage(ctx context.Context, a *AuthSession, p Pagination) (Page[ProjectSummary], error) {
	out := NewPage[ProjectSummary](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	join := " FROM projects p LEFT JOIN memberships m ON m.project_id=p.id AND m.user_id=$2 WHERE ($1::boolean OR m.user_id IS NOT NULL)"
	if e = tx.QueryRowContext(ctx, "SELECT count(*)"+join, a.User.IsAdmin, a.User.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT p.id,p.name,p.slug,p.description,p.state,CASE WHEN $1::boolean THEN 'Admin' ELSE m.role END,p.created_at,p.updated_at,p.version"+join+" ORDER BY p.created_at,p.id LIMIT $3 OFFSET $4", a.User.IsAdmin, a.User.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v ProjectSummary
		if e = rows.Scan(&v.ID, &v.Name, &v.Slug, &v.Description, &v.State, &v.Role, &v.CreatedAt, &v.UpdatedAt, &v.Version); e != nil {
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	return out, tx.Commit()
}

type AuditEntry struct {
	ID        string         `json:"id"`
	ActorID   *string        `json:"actor_id"`
	ActorName *string        `json:"actor_name"`
	ProjectID *string        `json:"project_id"`
	Action    string         `json:"action"`
	TargetID  *string        `json:"target_id"`
	TraceID   string         `json:"trace_id"`
	Metadata  map[string]any `json:"metadata"`
	CreatedAt time.Time      `json:"created_at"`
}

func safeAuditMetadata(raw []byte) map[string]any {
	out := map[string]any{}
	var in map[string]any
	if json.Unmarshal(raw, &in) != nil {
		return out
	}
	for k, v := range in {
		switch k {
		case "before_version", "after_version":
			if n, ok := v.(float64); ok && n >= 0 && n <= 1e12 {
				out[k] = n
			}
		case "enabled", "is_admin", "before_enabled", "after_enabled", "sessions_revoked":
			if b, ok := v.(bool); ok {
				out[k] = b
			}
		case "role", "before_role", "after_role":
			if r, ok := v.(string); ok && (r == "" || validRoles[r]) {
				out[k] = r
			}
		case "source":
			if r, ok := v.(string); ok && (r == "server_cli" || r == "web" || r == "sim") {
				out[k] = r
			}
		case "scope":
			if r, ok := v.(string); ok && (r == "account" || r == "account_and_explicit_ip") {
				out[k] = r
			}
		case "result":
			if r, ok := v.(string); ok {
				switch r {
				case "invalid_credentials", "invalid_current_password", "success", "succeeded", "failed", "cancelled", "ok":
					out[k] = r
				}
			}
		}
	}
	return out
}
func nullableString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}
func (s *Store) ListAudits(ctx context.Context, a *AuthSession, projectID string, p Pagination, action string) (Page[AuditEntry], error) {
	out := NewPage[AuditEntry](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	action = strings.TrimSpace(action)
	if utf8.RuneCountInString(action) > 64 {
		return out, apiError(400, "VALIDATION_ERROR", "Action filter exceeds 64 characters")
	}
	if projectID == "" && !a.User.IsAdmin {
		return out, ErrForbidden
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if projectID != "" {
		if e = requireProjectAdmin(ctx, tx, a, projectID); e != nil {
			return out, e
		}
	}
	where := " WHERE ($1::text='' OR a.project_id=$1) AND ($2::text='' OR a.action=$2)"
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM audits a"+where, projectID, action).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT a.id,a.actor_id,CASE WHEN a.actor_id IS NULL AND a.trace_id LIKE 'cli:%' THEN 'Server CLI' ELSE u.display_name END,a.project_id,a.action,a.target_id,COALESCE(a.trace_id,''),a.metadata,a.created_at FROM audits a LEFT JOIN users u ON u.id=a.actor_id`+where+" ORDER BY a.created_at DESC,a.id DESC LIMIT $3 OFFSET $4", projectID, action, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v AuditEntry
		var actor, name, project, target sql.NullString
		var metadata []byte
		if e = rows.Scan(&v.ID, &actor, &name, &project, &v.Action, &target, &v.TraceID, &metadata, &v.CreatedAt); e != nil {
			return out, e
		}
		v.ActorID = nullableString(actor)
		v.ActorName = nullableString(name)
		v.ProjectID = nullableString(project)
		v.TargetID = nullableString(target)
		v.Metadata = safeAuditMetadata(metadata)
		out.Items = append(out.Items, v)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	return out, tx.Commit()
}
