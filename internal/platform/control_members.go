package platform

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type CreateMemberInput struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}
type UpdateMemberInput struct {
	Role    string `json:"role"`
	Version int    `json:"version"`
}
type VersionInput struct {
	Version int `json:"version"`
}

const memberFields = "m.id,u.id,u.username,u.display_name,u.is_admin,u.enabled,m.role,m.version,m.created_at"

func scanMember(row scanner) (*Member, error) {
	m := &Member{}
	e := row.Scan(&m.ID, &m.User.ID, &m.User.Username, &m.User.DisplayName, &m.User.IsAdmin, &m.Enabled, &m.Role, &m.Version, &m.CreatedAt)
	return m, e
}
func (s *Store) ListMembers(ctx context.Context, a *AuthSession, projectID string, p Pagination) (Page[Member], error) {
	out := NewPage[Member](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = requireProjectAdmin(ctx, tx, a, projectID); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM memberships WHERE project_id=$1", projectID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT "+memberFields+" FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.project_id=$1 ORDER BY m.created_at,m.id LIMIT $2 OFFSET $3", projectID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		m, e := scanMember(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *m)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func (s *Store) ControlCreateMember(ctx context.Context, a *AuthSession, projectID string, input CreateMemberInput, trace string) (*Member, error) {
	if !validRoles[input.Role] {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid project role")
	}
	name := NormalizeUsername(input.Username)
	if !usernamePattern.MatchString(name) {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid username")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectAdmin(ctx, tx, a, projectID); e != nil {
		return nil, e
	}
	u, e := scanAccount(tx.QueryRowContext(ctx, "SELECT "+accountFields+" FROM users WHERE username=$1 FOR UPDATE", name))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(404, "USER_NOT_FOUND", "Account not found")
	}
	if e != nil {
		return nil, e
	}
	if !u.Enabled {
		return nil, apiError(409, "USER_DISABLED", "Enable the account before assigning new membership")
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM memberships WHERE project_id=$1 AND user_id=$2)", projectID, u.ID).Scan(&exists); e != nil {
		return nil, e
	}
	if exists {
		return nil, apiError(409, "MEMBER_EXISTS", "Account is already a project member")
	}
	m := &Member{ID: RandomID(), User: UserSummary{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, IsAdmin: u.IsAdmin}, Enabled: u.Enabled, Role: input.Role, Version: u.Version + 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if _, e = tx.ExecContext(ctx, "INSERT INTO memberships(id,user_id,project_id,role,version,created_at) VALUES($1,$2,$3,$4,$5,$6)", m.ID, u.ID, projectID, input.Role, m.Version, m.CreatedAt); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE users SET security_version=security_version+1 WHERE id=$1", u.ID); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectID, "membership.grant", u.ID, trace, map[string]any{"after_role": input.Role, "before_version": 0, "after_version": m.Version, "sessions_revoked": true}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return m, nil
}
func protectMemberRemoval(ctx context.Context, tx *sql.Tx, projectID, userID, oldRole string, enabled bool) error {
	if oldRole != "Admin" || !enabled {
		return nil
	}
	var count int
	e := tx.QueryRowContext(ctx, "SELECT count(*) FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.project_id=$1 AND m.user_id<>$2 AND m.role='Admin' AND u.enabled", projectID, userID).Scan(&count)
	if e != nil {
		return e
	}
	if count == 0 {
		return errLastProjectAdmin
	}
	return nil
}
func (s *Store) ControlChangeMember(ctx context.Context, a *AuthSession, projectID, userID string, input UpdateMemberInput, revoke bool, trace string) (*Member, error) {
	if input.Version < 1 {
		return nil, apiError(400, "VALIDATION_ERROR", "version is required")
	}
	if !revoke && !validRoles[input.Role] {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid project role")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectAdmin(ctx, tx, a, projectID); e != nil {
		return nil, e
	}
	// All control/CLI member writes share the global control lock. Lock target user
	// before membership rows to stay compatible with execution/session write order.
	var enabled bool
	e = tx.QueryRowContext(ctx, "SELECT enabled FROM users WHERE id=$1 FOR UPDATE", userID).Scan(&enabled)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(404, "MEMBER_NOT_FOUND", "Project member not found")
	}
	if e != nil {
		return nil, e
	}
	m, e := scanMember(tx.QueryRowContext(ctx, "SELECT "+memberFields+" FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.project_id=$1 AND m.user_id=$2 FOR UPDATE OF m", projectID, userID))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(404, "MEMBER_NOT_FOUND", "Project member not found")
	}
	if e != nil {
		return nil, e
	}
	if m.Version != input.Version {
		return nil, errVersion
	}
	oldRole := m.Role
	if revoke || input.Role != "Admin" {
		if e = protectMemberRemoval(ctx, tx, projectID, userID, oldRole, enabled); e != nil {
			return nil, e
		}
	}
	action := "membership.change"
	if revoke {
		action = "membership.revoke"
		_, e = tx.ExecContext(ctx, "DELETE FROM memberships WHERE id=$1", m.ID)
	} else {
		_, e = tx.ExecContext(ctx, "UPDATE memberships SET role=$2,version=version+1 WHERE id=$1", m.ID, input.Role)
		m.Role = input.Role
		m.Version++
	}
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE users SET security_version=security_version+1 WHERE id=$1", userID); e != nil {
		return nil, e
	}
	afterRole := input.Role
	afterVersion := m.Version
	if revoke {
		afterRole = ""
		afterVersion = 0
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectID, action, userID, trace, map[string]any{"before_role": oldRole, "after_role": afterRole, "before_version": input.Version, "after_version": afterVersion, "sessions_revoked": true}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return m, nil
}
