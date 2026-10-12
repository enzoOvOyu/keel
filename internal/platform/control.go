package platform

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"time"
)

type Pagination struct {
	Page     int
	PageSize int
}

func (p Pagination) Validate() error {
	if p.Page < 1 || p.Page > 1000000 || p.PageSize < 1 || p.PageSize > 100 {
		return apiError(400, "VALIDATION_ERROR", "page must be 1..1000000 and page_size 1..100")
	}
	return nil
}
func (p Pagination) Offset() int { return (p.Page - 1) * p.PageSize }

type Page[T any] struct {
	Items    []T `json:"items"`
	Total    int `json:"total"`
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

func NewPage[T any](p Pagination) Page[T] {
	return Page[T]{Items: []T{}, Page: p.Page, PageSize: p.PageSize}
}

type UserAccount struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	IsAdmin     bool      `json:"is_admin"`
	Enabled     bool      `json:"enabled"`
	Version     int       `json:"version"`
	CreatedAt   time.Time `json:"created_at"`
}
type Member struct {
	ID        string      `json:"id"`
	User      UserSummary `json:"user"`
	Enabled   bool        `json:"enabled"`
	Role      string      `json:"role"`
	Version   int         `json:"version"`
	CreatedAt time.Time   `json:"created_at"`
}

var errVersion = apiError(409, "VERSION_CONFLICT", "The record changed; refresh before trying again")
var errLastPlatformAdmin = apiError(409, "LAST_PLATFORM_ADMIN", "Cannot remove the last active platform administrator")
var errLastProjectAdmin = apiError(409, "LAST_PROJECT_ADMIN", "Assign another active project administrator before removing this access")

// RecheckSessionTx pins both identity and session until the caller's transaction
// commits. All writers acquire user -> session -> domain rows in that order.
func RecheckSessionTx(ctx context.Context, tx *sql.Tx, a *AuthSession) error {
	if a == nil {
		return ErrUnauthenticated
	}
	var enabled, admin bool
	var version int
	e := tx.QueryRowContext(ctx, "SELECT enabled,is_admin,security_version FROM users WHERE id=$1 FOR SHARE", a.User.ID).Scan(&enabled, &admin, &version)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrUnauthenticated
	}
	if e != nil {
		return e
	}
	if !enabled || version != a.SecurityVersion || admin != a.User.IsAdmin {
		return ErrUnauthenticated
	}
	var id string
	now := time.Now().UTC()
	e = tx.QueryRowContext(ctx, "SELECT id FROM sessions WHERE id=$1 AND user_id=$2 AND security_version=$3 AND revoked_at IS NULL AND expires_at>$4 AND last_seen_at>$5 FOR SHARE", a.ID, a.User.ID, a.SecurityVersion, now, now.Add(-IdleTimeout)).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrUnauthenticated
	}
	return e
}
func (s *Store) beginControl(ctx context.Context, a *AuthSession, throttleKeys ...string) (*sql.Tx, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*sql.Tx, error) { tx.Rollback(); return nil, e }
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return fail(e)
	}
	sort.Strings(throttleKeys)
	for _, key := range throttleKeys {
		if e = lockKey(ctx, tx, "login:"+key); e != nil {
			return fail(e)
		}
	}
	if e = RecheckSessionTx(ctx, tx, a); e != nil {
		return fail(e)
	}
	return tx, nil
}
func (s *Store) beginControlRead(ctx context.Context, a *AuthSession) (*sql.Tx, error) {
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return nil, e
	}
	var ok bool
	now := time.Now().UTC()
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN sessions s ON s.user_id=u.id WHERE u.id=$1 AND u.enabled AND u.is_admin=$2 AND u.security_version=$3 AND s.id=$4 AND s.security_version=u.security_version AND s.revoked_at IS NULL AND s.expires_at>$5 AND s.last_seen_at>$6)`, a.User.ID, a.User.IsAdmin, a.SecurityVersion, a.ID, now, now.Add(-IdleTimeout)).Scan(&ok)
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	if !ok {
		tx.Rollback()
		return nil, ErrUnauthenticated
	}
	return tx, nil
}
func requireProjectAdmin(ctx context.Context, tx *sql.Tx, a *AuthSession, projectID string) error {
	var exists bool
	if e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1)", projectID).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return ErrNotFound
	}
	if a.User.IsAdmin {
		return nil
	}
	var role string
	e := tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2", projectID, a.User.ID).Scan(&role)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	if e != nil {
		return e
	}
	if role != "Admin" {
		return ErrForbidden
	}
	return nil
}

type scanner interface{ Scan(...any) error }

func scanAccount(row scanner) (*UserAccount, error) {
	u := &UserAccount{}
	e := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsAdmin, &u.Enabled, &u.Version, &u.CreatedAt)
	return u, e
}

const accountFields = "id,username,display_name,is_admin,enabled,security_version,created_at"

func accountForUpdate(ctx context.Context, tx *sql.Tx, id string, version int) (*UserAccount, error) {
	if version < 1 {
		return nil, apiError(400, "VALIDATION_ERROR", "version is required")
	}
	u, e := scanAccount(tx.QueryRowContext(ctx, "SELECT "+accountFields+" FROM users WHERE id=$1 FOR UPDATE", id))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(404, "USER_NOT_FOUND", "Account not found")
	}
	if e != nil {
		return nil, e
	}
	if u.Version != version {
		return nil, errVersion
	}
	return u, nil
}
func protectDisablingUser(ctx context.Context, tx *sql.Tx, u *UserAccount) error {
	if !u.Enabled {
		return nil
	}
	if u.IsAdmin {
		var n int
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM users WHERE enabled AND is_admin AND id<>$1", u.ID).Scan(&n); e != nil {
			return e
		}
		if n == 0 {
			return errLastPlatformAdmin
		}
	}
	var stranded bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=$1 AND m.role='Admin' AND NOT EXISTS(SELECT 1 FROM memberships other JOIN users u ON u.id=other.user_id WHERE other.project_id=m.project_id AND other.user_id<>$1 AND other.role='Admin' AND u.enabled))`, u.ID).Scan(&stranded)
	if e != nil {
		return e
	}
	if stranded {
		return errLastProjectAdmin
	}
	return nil
}
func (s *Store) hashForRequest(password string) (string, error) {
	if len(password) < 12 || len(password) > 1024 {
		return "", apiError(400, "VALIDATION_ERROR", "New password must contain 12 to 1024 bytes")
	}
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
	default:
		return "", apiError(503, "AUTH_BUSY", "Authentication is busy; retry shortly")
	}
	return HashPassword(password)
}
