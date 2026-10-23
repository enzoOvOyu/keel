package platform

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

func (s *Store) CreateUser(ctx context.Context, username, displayName, password string, admin, initial bool) (*UserSummary, error) {
	username = NormalizeUsername(username)
	displayName = strings.TrimSpace(displayName)
	if !usernamePattern.MatchString(username) || utf8.RuneCountInString(displayName) < 1 || utf8.RuneCountInString(displayName) > 120 {
		return nil, errors.New("invalid username or display name")
	}
	hash, e := HashPassword(password)
	if e != nil {
		return nil, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	var count int
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE is_admin=true").Scan(&count); e != nil {
		return nil, e
	}
	if initial && count != 0 {
		return nil, errors.New("administrator was already initialized")
	}
	if !initial && count == 0 {
		return nil, errors.New("initialize the first administrator with init-admin")
	}
	if initial {
		admin = true
	}
	u := &UserSummary{ID: RandomID(), Username: username, DisplayName: displayName, IsAdmin: admin}
	_, e = tx.ExecContext(ctx, "INSERT INTO users(id,username,display_name,password_hash,is_admin,enabled,security_version,created_at) VALUES($1,$2,$3,$4,$5,true,1,$6)", u.ID, username, displayName, hash, admin, time.Now().UTC())
	if e != nil {
		return nil, errors.New("could not create user (username may already exist)")
	}
	action := "user.create"
	if initial {
		action = "user.init_admin"
	}
	if e = appendAudit(ctx, tx, "", "", action, u.ID, "cli", map[string]any{"source": "server_cli", "is_admin": admin}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return u, nil
}
func (s *Store) DisableUser(ctx context.Context, username string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return e
	}
	var id string
	var admin, enabled bool
	e = tx.QueryRowContext(ctx, "SELECT id,is_admin,enabled FROM users WHERE username=$1 FOR UPDATE", NormalizeUsername(username)).Scan(&id, &admin, &enabled)
	if errors.Is(e, sql.ErrNoRows) {
		return errors.New("user not found")
	}
	if e != nil {
		return e
	}
	if e = protectDisablingUser(ctx, tx, &UserAccount{ID: id, IsAdmin: admin, Enabled: enabled}); e != nil {
		return e
	}

	if _, e = tx.ExecContext(ctx, "UPDATE users SET enabled=false,security_version=security_version+1 WHERE id=$1", id); e != nil {
		return e
	}
	if e = appendAudit(ctx, tx, "", "", "user.disable", id, "cli", map[string]any{"source": "server_cli"}); e != nil {
		return e
	}
	return tx.Commit()
}

var validRoles = map[string]bool{"Admin": true, "DE": true, "DL": true, "NE": true, "SRE": true, "Viewer": true, "NDE": true, "NH": true}

func (s *Store) SetMembership(ctx context.Context, username, projectID, role string, revoke bool) error {
	if !revoke && !validRoles[role] {
		return errors.New("role must be Admin, DE, DL, NE, NDE, NH, SRE, or Viewer")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return e
	}
	var id string
	var enabled bool
	var userVersion int
	e = tx.QueryRowContext(ctx, "SELECT id,enabled,security_version FROM users WHERE username=$1 FOR UPDATE", NormalizeUsername(username)).Scan(&id, &enabled, &userVersion)
	if errors.Is(e, sql.ErrNoRows) {
		return errors.New("user not found")
	}
	if e != nil {
		return e
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1)", projectID).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return errors.New("project not found")
	}
	var oldRole string
	var oldVersion int
	e = tx.QueryRowContext(ctx, "SELECT role,version FROM memberships WHERE project_id=$1 AND user_id=$2 FOR UPDATE", projectID, id).Scan(&oldRole, &oldVersion)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if oldVersion == 0 && !enabled {
		return errors.New("enable the account before assigning membership")
	}
	if revoke || role != "Admin" {
		if e = protectMemberRemoval(ctx, tx, projectID, id, oldRole, enabled); e != nil {
			return e
		}
	}
	action := "membership.grant"
	if revoke {
		action = "membership.revoke"
		_, e = tx.ExecContext(ctx, "DELETE FROM memberships WHERE user_id=$1 AND project_id=$2", id, projectID)
	} else {
		_, e = tx.ExecContext(ctx, "INSERT INTO memberships(id,user_id,project_id,role,version,created_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(user_id,project_id) DO UPDATE SET role=EXCLUDED.role,version=memberships.version+1", RandomID(), id, projectID, role, userVersion+1, time.Now().UTC())
	}
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE users SET security_version=security_version+1 WHERE id=$1", id); e != nil {
		return e
	}
	after := oldVersion + 1
	if oldVersion == 0 {
		after = userVersion + 1
	}
	if revoke {
		after = 0
	}
	if e = appendAudit(ctx, tx, "", projectID, action, id, "cli", map[string]any{"source": "server_cli", "before_role": oldRole, "after_role": role, "before_version": oldVersion, "after_version": after, "sessions_revoked": true}); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) UnlockUser(ctx context.Context, username string) error {
	username = NormalizeUsername(username)
	if !usernamePattern.MatchString(username) {
		return errors.New("invalid username")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return e
	}
	key := digest("account:" + username)
	if e = lockKey(ctx, tx, "login:"+key); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM login_throttles WHERE id=$1", key); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE users SET security_version=security_version+1 WHERE username=$1", username); e != nil {
		return e
	}
	if e = appendAudit(ctx, tx, "", "", "user.unlock", key, "cli", map[string]any{"scope": "account", "source": "server_cli"}); e != nil {
		return e
	}
	return tx.Commit()
}
