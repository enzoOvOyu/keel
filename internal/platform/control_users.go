package platform

import (
	"context"
	"database/sql"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"net"
	"strings"
	"time"
	"unicode/utf8"
)

type CreateUserInput struct {
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
	IsAdmin     bool   `json:"is_admin"`
}
type UpdateUserInput struct {
	Enabled *bool `json:"enabled"`
	Version int   `json:"version"`
}
type ResetPasswordInput struct {
	Password string `json:"password"`
	Version  int    `json:"version"`
}
type UnlockUserInput struct {
	Version int    `json:"version"`
	IP      string `json:"ip,omitempty"`
}

func (s *Store) ListUsers(ctx context.Context, a *AuthSession, p Pagination, q string) (Page[UserAccount], error) {
	out := NewPage[UserAccount](p)
	if !a.User.IsAdmin {
		return out, ErrForbidden
	}
	if e := p.Validate(); e != nil {
		return out, e
	}
	q = strings.TrimSpace(q)
	if utf8.RuneCountInString(q) > 128 {
		return out, apiError(400, "VALIDATION_ERROR", "Search exceeds 128 characters")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(q) + "%"
	filter := "(username ILIKE $1 ESCAPE '!' OR display_name ILIKE $1 ESCAPE '!')"
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE "+filter, pattern).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT "+accountFields+" FROM users WHERE "+filter+" ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3", pattern, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		u, e := scanAccount(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *u)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func (s *Store) ControlCreateUser(ctx context.Context, a *AuthSession, input CreateUserInput, trace string) (*UserAccount, error) {
	if !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	name := NormalizeUsername(input.Username)
	display := strings.TrimSpace(input.DisplayName)
	if !usernamePattern.MatchString(name) || utf8.RuneCountInString(display) < 1 || utf8.RuneCountInString(display) > 120 {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid username or display name")
	}
	hash, e := s.hashForRequest(input.Password)
	if e != nil {
		return nil, e
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	u := &UserAccount{ID: RandomID(), Username: name, DisplayName: display, IsAdmin: input.IsAdmin, Enabled: true, Version: 1, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	_, e = tx.ExecContext(ctx, "INSERT INTO users(id,username,display_name,password_hash,is_admin,enabled,security_version,created_at) VALUES($1,$2,$3,$4,$5,true,1,$6)", u.ID, name, display, hash, u.IsAdmin, u.CreatedAt)
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			return nil, apiError(409, "USERNAME_EXISTS", "Username already exists")
		}
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, "", "user.create", u.ID, trace, map[string]any{"is_admin": u.IsAdmin, "before_version": 0, "after_version": 1}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return u, nil
}
func (s *Store) ControlUpdateUser(ctx context.Context, a *AuthSession, id string, input UpdateUserInput, trace string) (*UserAccount, error) {
	if !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	if input.Enabled == nil {
		return nil, apiError(400, "VALIDATION_ERROR", "enabled is required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	u, e := accountForUpdate(ctx, tx, id, input.Version)
	if e != nil {
		return nil, e
	}
	if !*input.Enabled {
		if e = protectDisablingUser(ctx, tx, u); e != nil {
			return nil, e
		}
	}
	previous := u.Enabled
	if _, e = tx.ExecContext(ctx, "UPDATE users SET enabled=$2,security_version=security_version+1 WHERE id=$1", id, *input.Enabled); e != nil {
		return nil, e
	}
	u.Enabled = *input.Enabled
	u.Version++
	if e = appendAudit(ctx, tx, a.User.ID, "", "user.status", id, trace, map[string]any{"before_enabled": previous, "after_enabled": u.Enabled, "before_version": input.Version, "after_version": u.Version, "sessions_revoked": true}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return u, nil
}
func (s *Store) ControlResetPassword(ctx context.Context, a *AuthSession, id string, input ResetPasswordInput, trace string) (*UserAccount, error) {
	if !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	hash, e := s.hashForRequest(input.Password)
	if e != nil {
		return nil, e
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	u, e := accountForUpdate(ctx, tx, id, input.Version)
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE users SET password_hash=$2,security_version=security_version+1 WHERE id=$1", id, hash); e != nil {
		return nil, e
	}
	u.Version++
	if e = appendAudit(ctx, tx, a.User.ID, "", "user.password_reset", id, trace, map[string]any{"before_version": input.Version, "after_version": u.Version, "sessions_revoked": true}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return u, nil
}
func (s *Store) ControlUnlockUser(ctx context.Context, a *AuthSession, id string, input UnlockUserInput, trace string) (*UserAccount, error) {
	if !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	var name string
	e := s.DB.QueryRowContext(ctx, "SELECT username FROM users WHERE id=$1", id).Scan(&name)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(404, "USER_NOT_FOUND", "Account not found")
	}
	if e != nil {
		return nil, e
	}
	keys := []string{digest("account:" + name)}
	ip := ""
	if input.IP != "" {
		parsed := net.ParseIP(input.IP)
		if parsed == nil {
			return nil, apiError(400, "VALIDATION_ERROR", "ip must be a valid IP address")
		}
		ip = parsed.String()
		keys = append(keys, digest("ip:"+ip))
	}
	// Account names are immutable. Acquire throttle locks before user/session rows,
	// matching Login's lock order even when an administrator unlocks their own account.
	tx, e := s.beginControl(ctx, a, keys...)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	u, e := accountForUpdate(ctx, tx, id, input.Version)
	if e != nil {
		return nil, e
	}
	for _, key := range keys {
		if _, e = tx.ExecContext(ctx, "DELETE FROM login_throttles WHERE id=$1", key); e != nil {
			return nil, e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE users SET security_version=security_version+1 WHERE id=$1", id); e != nil {
		return nil, e
	}
	u.Version++
	metadata := map[string]any{"scope": "account", "before_version": input.Version, "after_version": u.Version, "sessions_revoked": true}
	if ip != "" {
		metadata["scope"] = "account_and_explicit_ip"
		metadata["source_ip"] = ip
	}
	if e = appendAudit(ctx, tx, a.User.ID, "", "user.unlock", id, trace, metadata); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return u, nil
}
