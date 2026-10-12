package platform

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

type ChangePasswordInput struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func checkThrottleLocked(ctx context.Context, tx *sql.Tx, keys []string, now time.Time) error {
	for _, key := range keys {
		var until sql.NullTime
		e := tx.QueryRowContext(ctx, "SELECT locked_until FROM login_throttles WHERE id=$1", key).Scan(&until)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if until.Valid && until.Time.After(now) {
			return &Error{Status: 429, Code: "RATE_LIMITED", Message: "Too many password attempts; retry later", RetryAfter: int(math.Ceil(until.Time.Sub(now).Seconds()))}
		}
	}
	return nil
}
func recordPasswordFailure(ctx context.Context, tx *sql.Tx, keys []string, now time.Time) error {
	for _, key := range keys {
		_, e := tx.ExecContext(ctx, `INSERT INTO login_throttles(id,failures,window_started_at,locked_until) VALUES($1,1,$2,NULL)
 ON CONFLICT(id) DO UPDATE SET failures=CASE WHEN login_throttles.window_started_at<=$3 THEN 1 ELSE login_throttles.failures+1 END,
 window_started_at=CASE WHEN login_throttles.window_started_at<=$3 THEN $2 ELSE login_throttles.window_started_at END,
 locked_until=CASE WHEN login_throttles.window_started_at>$3 AND login_throttles.failures+1 >= $4 THEN $5::timestamptz ELSE NULL END`, key, now, now.Add(-LockoutWindow), MaxLoginFailures, now.Add(LockoutWindow))
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) ChangeOwnPassword(ctx context.Context, a *AuthSession, input ChangePasswordInput, ip, trace string) error {
	if len(input.CurrentPassword) < 1 || len(input.CurrentPassword) > 1024 || len(input.NewPassword) < 12 || len(input.NewPassword) > 1024 {
		return apiError(400, "VALIDATION_ERROR", "Invalid current or new password length")
	}
	select {
	case s.passwordSlots <- struct{}{}:
		defer func() { <-s.passwordSlots }()
	default:
		return apiError(503, "AUTH_BUSY", "Authentication is busy; retry shortly")
	}
	keys := []string{digest("account:" + a.User.Username), digest("ip:" + ip)}
	tx, e := s.beginControl(ctx, a, keys...)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if e = checkThrottleLocked(ctx, tx, keys, now); e != nil {
		return e
	}
	var currentHash string
	var version int
	e = tx.QueryRowContext(ctx, "SELECT password_hash,security_version FROM users WHERE id=$1 FOR UPDATE", a.User.ID).Scan(&currentHash, &version)
	if e != nil {
		return e
	}
	if !VerifyPassword(currentHash, input.CurrentPassword) {
		if e = recordPasswordFailure(ctx, tx, keys, now); e != nil {
			return e
		}
		if e = appendAudit(ctx, tx, a.User.ID, "", "auth.password_change.failed", a.User.ID, trace, map[string]any{"result": "invalid_current_password", "source_ip": ip}); e != nil {
			return e
		}
		if e = tx.Commit(); e != nil {
			return e
		}
		return apiError(403, "CURRENT_PASSWORD_INVALID", "Current password is incorrect")
	}
	hash, e := HashPassword(input.NewPassword)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE users SET password_hash=$2,security_version=security_version+1 WHERE id=$1", a.User.ID, hash); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM login_throttles WHERE id=$1", digest("account:"+a.User.Username)); e != nil {
		return e
	}
	if e = appendAudit(ctx, tx, a.User.ID, "", "auth.password_change", a.User.ID, trace, map[string]any{"before_version": version, "after_version": version + 1, "sessions_revoked": true}); e != nil {
		return e
	}
	return tx.Commit()
}
