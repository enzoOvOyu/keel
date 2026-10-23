package platform

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/ent/user"
	"math"
	"os"
	osuser "os/user"
	"regexp"
	"sort"
	"strings"
	"time"
)

type UserSummary struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}
type AuthSession struct {
	User            UserSummary
	ID              string
	CSRF            string
	ExpiresAt       time.Time
	SecurityVersion int
}
type SessionResponse struct {
	User        UserSummary      `json:"user"`
	Permissions []string         `json:"permissions"`
	Projects    []ProjectSummary `json:"projects"`
	CSRFToken   string           `json:"csrf_token"`
	ExpiresAt   time.Time        `json:"expires_at"`
}
type LoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Method   string `json:"method"`
}

var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func NormalizeUsername(v string) string { return strings.ToLower(strings.TrimSpace(v)) }
func (s *Store) HasAdministrator(ctx context.Context) (bool, error) {
	return s.Ent.User.Query().Where(user.IsAdmin(true), user.Enabled(true)).Exist(ctx)
}
func (s *Store) NewPreSession(ctx context.Context) (string, time.Time, error) {
	token, e := randomToken()
	if e != nil {
		return "", time.Time{}, e
	}
	now := time.Now().UTC()
	expiry := now.Add(PreSessionTimeout)
	_, e = s.DB.ExecContext(ctx, "INSERT INTO sessions (id,csrf_hash,security_version,issued_at,last_seen_at,expires_at) VALUES($1,$2,0,$3,$3,$4)", TokenHash(token), digest(CSRFToken(token)), now, expiry)
	return token, expiry, e
}
func (s *Store) CSRFSession(ctx context.Context, token string) (time.Time, error) {
	if !validToken(token) {
		return time.Time{}, ErrUnauthenticated
	}
	var userID sql.NullString
	var revoked sql.NullTime
	var securityVersion int
	var last, expiry time.Time
	e := s.DB.QueryRowContext(ctx, "SELECT user_id,security_version,last_seen_at,expires_at,revoked_at FROM sessions WHERE id=$1", TokenHash(token)).Scan(&userID, &securityVersion, &last, &expiry, &revoked)
	if errors.Is(e, sql.ErrNoRows) {
		return time.Time{}, ErrUnauthenticated
	}
	if e != nil {
		return time.Time{}, e
	}
	now := time.Now().UTC()
	if revoked.Valid || !expiry.After(now) || (!userID.Valid && !last.Add(PreSessionTimeout).After(now)) {
		return time.Time{}, ErrUnauthenticated
	}
	if userID.Valid {
		a, e := s.Authenticate(ctx, token)
		if e != nil {
			return time.Time{}, e
		}
		return a.ExpiresAt, nil
	}
	return expiry, nil
}
func (s *Store) Authenticate(ctx context.Context, token string) (*AuthSession, error) {
	if !validToken(token) {
		return nil, ErrUnauthenticated
	}
	now := time.Now().UTC()
	a := &AuthSession{ID: TokenHash(token), CSRF: CSRFToken(token)}
	e := s.DB.QueryRowContext(ctx, `UPDATE sessions AS s SET last_seen_at=$2 FROM users AS u
 WHERE s.id=$1 AND s.user_id=u.id AND u.enabled=true AND s.security_version=u.security_version
 AND s.revoked_at IS NULL AND s.expires_at>$2 AND s.last_seen_at>$3
 RETURNING u.id,u.username,u.display_name,u.is_admin,u.security_version,LEAST(s.expires_at,$4)`,
		a.ID, now, now.Add(-IdleTimeout), now.Add(IdleTimeout)).Scan(&a.User.ID, &a.User.Username, &a.User.DisplayName, &a.User.IsAdmin, &a.SecurityVersion, &a.ExpiresAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrUnauthenticated
	}
	if e != nil {
		return nil, e
	}
	return a, nil
}
func (s *Store) CheckCSRF(ctx context.Context, token, csrf string) error {
	if !validToken(token) || !validToken(csrf) {
		return apiError(403, "CSRF_INVALID", "Invalid CSRF token")
	}
	var want string
	e := s.DB.QueryRowContext(ctx, "SELECT csrf_hash FROM sessions WHERE id=$1 AND revoked_at IS NULL AND expires_at>$2", TokenHash(token), time.Now().UTC()).Scan(&want)
	if errors.Is(e, sql.ErrNoRows) {
		return apiError(403, "CSRF_INVALID", "Invalid CSRF token")
	}
	if e != nil {
		return e
	}
	if subtle.ConstantTimeCompare([]byte(want), []byte(digest(csrf))) != 1 {
		return apiError(403, "CSRF_INVALID", "Invalid CSRF token")
	}
	return nil
}
func (s *Store) SessionResponse(ctx context.Context, a *AuthSession) (*SessionResponse, error) {
	projects, e := s.ListProjects(ctx, a)
	if e != nil {
		return nil, e
	}
	permissions := []string{"project:read", "account:password"}
	projectAdmin := a.User.IsAdmin
	for _, p := range projects {
		if p.Role == "Admin" {
			projectAdmin = true
		}
	}
	if projectAdmin {
		permissions = append(permissions, "project:members:read", "project:members:manage", "project:audit:read")
	}
	if a.User.IsAdmin {
		permissions = append(permissions, "project:create", "user:read", "user:manage", "audit:read")
	}
	// Permissions are a capability union for navigation. Each execution route
	// must still authorize its own project rather than trusting this union.
	readExecution := a.User.IsAdmin || len(projects) > 0
	publishExecution, createExecution, reconcileExecution := a.User.IsAdmin, a.User.IsAdmin, a.User.IsAdmin
	for _, p := range projects {
		switch p.Role {
		case "Admin", "SRE":
			publishExecution = true
			createExecution = true
			reconcileExecution = true
		case "DL":
			createExecution = true
			reconcileExecution = true
		case "DE":
			createExecution = true
		}
	}
	if readExecution {
		permissions = append(permissions, "sim:target:read", "flow:read", "execution:read")
	}
	// Model and media libraries share the delivery scope: any member reads,
	// project administrators manage. Platform administrators manage globally.
	if projectAdmin || a.User.IsAdmin {
		permissions = append(permissions, "model:manage")
	}
	if projectAdmin || len(projects) > 0 || a.User.IsAdmin {
		permissions = append(permissions, "model:read")
	}
	if publishExecution {
		permissions = append(permissions, "sim:target:register", "flow:publish")
	}
	if createExecution {
		permissions = append(permissions, "execution:create", "execution:control")
	}
	if reconcileExecution {
		permissions = append(permissions, "execution:reconcile")
	}
	// This union is for navigation only; mutation authorization uses the target
	// project's current membership inside the transaction.
	fabric := map[string]bool{}
	if a.User.IsAdmin {
		for _, permission := range fabricPermissions("Admin") {
			fabric[permission] = true
		}
	}
	for _, project := range projects {
		for _, permission := range fabricPermissions(project.Role) {
			fabric[permission] = true
		}
	}
	for _, permission := range []string{"fabric:read", "fabric:plan", "fabric:collect", "fabric:template:edit"} {
		if fabric[permission] {
			permissions = append(permissions, permission)
		}
	}
	return &SessionResponse{User: a.User, Permissions: permissions, Projects: projects, CSRFToken: a.CSRF, ExpiresAt: a.ExpiresAt}, nil
}
func lockKey(ctx context.Context, tx *sql.Tx, key string) error {
	h := sha256.Sum256([]byte(key))
	_, e := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", int64(binary.BigEndian.Uint64(h[:8])))
	return e
}
func appendAudit(ctx context.Context, tx *sql.Tx, actor, project, action, target, trace string, metadata map[string]any) error {
	if trace == "cli" {
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["operator_uid"] = os.Geteuid()
		if u, e := osuser.Current(); e == nil {
			metadata["operator_os_user"] = u.Username
		}
		trace = "cli:" + RandomID()
	}
	data, e := json.Marshal(metadata)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO audits(id,actor_id,project_id,action,target_id,trace_id,metadata,created_at) VALUES($1,NULLIF($2,''),NULLIF($3,''),$4,$5,$6,$7,$8)", RandomID(), actor, project, action, target, trace, data, time.Now().UTC())
	return e
}
func (s *Store) Login(ctx context.Context, token, csrf string, input LoginInput, sourceIP, trace string) (string, *AuthSession, error) {
	if input.Method != "local" || len(input.Username) < 1 || len(input.Username) > 128 || len(input.Password) < 1 || len(input.Password) > 1024 {
		return "", nil, apiError(400, "VALIDATION_ERROR", "Invalid login request")
	}
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
	default:
		return "", nil, apiError(503, "AUTH_BUSY", "Authentication is busy; retry shortly")
	}
	if !validToken(token) || !validToken(csrf) {
		return "", nil, apiError(403, "CSRF_INVALID", "Invalid CSRF token")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", nil, e
	}
	defer tx.Rollback()
	var oldUser sql.NullString
	var oldCSRF string
	var expires time.Time
	var revoked sql.NullTime
	e = tx.QueryRowContext(ctx, "SELECT user_id,csrf_hash,expires_at,revoked_at FROM sessions WHERE id=$1 FOR UPDATE", TokenHash(token)).Scan(&oldUser, &oldCSRF, &expires, &revoked)
	if errors.Is(e, sql.ErrNoRows) {
		return "", nil, apiError(403, "CSRF_INVALID", "Invalid CSRF token")
	}
	if e != nil {
		return "", nil, e
	}
	now := time.Now().UTC()
	if revoked.Valid || !expires.After(now) || subtle.ConstantTimeCompare([]byte(oldCSRF), []byte(digest(csrf))) != 1 {
		return "", nil, apiError(403, "CSRF_INVALID", "Invalid CSRF token")
	}
	if oldUser.Valid {
		return "", nil, apiError(409, "ALREADY_AUTHENTICATED", "Sign out before changing accounts")
	}
	name := NormalizeUsername(input.Username)
	keys := []string{digest("account:" + name), digest("ip:" + sourceIP)}
	sort.Strings(keys)
	for _, key := range keys {
		if e = lockKey(ctx, tx, "login:"+key); e != nil {
			return "", nil, e
		}
		var until sql.NullTime
		e = tx.QueryRowContext(ctx, "SELECT locked_until FROM login_throttles WHERE id=$1", key).Scan(&until)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return "", nil, e
		}
		if until.Valid && until.Time.After(now) {
			return "", nil, &Error{Status: 429, Code: "RATE_LIMITED", Message: "Too many login attempts; retry later", RetryAfter: int(math.Ceil(until.Time.Sub(now).Seconds()))}
		}
	}
	var u UserSummary
	var hash string
	var enabled bool
	var securityVersion int
	e = tx.QueryRowContext(ctx, "SELECT id,username,display_name,is_admin,password_hash,enabled,security_version FROM users WHERE username=$1 FOR UPDATE", name).Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsAdmin, &hash, &enabled, &securityVersion)
	found := e == nil
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return "", nil, e
	}
	if !found {
		hash = s.dummyHash
	}
	valid := VerifyPassword(hash, input.Password)
	if !found || !enabled || !valid {
		for _, key := range keys {
			_, e = tx.ExecContext(ctx, `INSERT INTO login_throttles(id,failures,window_started_at,locked_until) VALUES($1,1,$2,NULL)
    ON CONFLICT(id) DO UPDATE SET failures=CASE WHEN login_throttles.window_started_at<=$3 THEN 1 ELSE login_throttles.failures+1 END,
    window_started_at=CASE WHEN login_throttles.window_started_at<=$3 THEN $2 ELSE login_throttles.window_started_at END,
    locked_until=CASE WHEN login_throttles.window_started_at>$3 AND login_throttles.failures+1 >= $4 THEN $5::timestamptz ELSE NULL END`,
				key, now, now.Add(-LockoutWindow), MaxLoginFailures, now.Add(LockoutWindow))
			if e != nil {
				return "", nil, e
			}
		}
		if e = appendAudit(ctx, tx, "", "", "auth.login.failed", digest("account:"+name), trace, map[string]any{"source_ip": sourceIP, "result": "invalid_credentials"}); e != nil {
			return "", nil, e
		}
		if e = tx.Commit(); e != nil {
			return "", nil, e
		}
		return "", nil, ErrCredentials
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM login_throttles WHERE id=$1", digest("account:"+name)); e != nil {
		return "", nil, e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=$2 WHERE id=$1", TokenHash(token), now); e != nil {
		return "", nil, e
	}
	newToken, e := randomToken()
	if e != nil {
		return "", nil, e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO sessions(id,user_id,csrf_hash,security_version,issued_at,last_seen_at,expires_at) VALUES($1,$2,$3,$4,$5,$5,$6)", TokenHash(newToken), u.ID, digest(CSRFToken(newToken)), securityVersion, now, now.Add(AbsoluteTimeout))
	if e != nil {
		return "", nil, e
	}
	if e = appendAudit(ctx, tx, u.ID, "", "auth.login.succeeded", u.ID, trace, map[string]any{"source_ip": sourceIP}); e != nil {
		return "", nil, e
	}
	if e = tx.Commit(); e != nil {
		return "", nil, e
	}
	return newToken, &AuthSession{User: u, ID: TokenHash(newToken), CSRF: CSRFToken(newToken), ExpiresAt: now.Add(IdleTimeout), SecurityVersion: securityVersion}, nil
}
func (s *Store) Logout(ctx context.Context, a *AuthSession, trace string) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=$2 WHERE id=$1 AND revoked_at IS NULL", a.ID, time.Now().UTC()); e != nil {
		return e
	}
	if e = appendAudit(ctx, tx, a.User.ID, "", "auth.logout", a.User.ID, trace, map[string]any{}); e != nil {
		return e
	}
	return tx.Commit()
}
