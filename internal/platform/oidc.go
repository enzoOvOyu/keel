package platform

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

func (s *Store) RecordOIDCFailure(ctx context.Context, provider, ip, trace string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if len(provider) > 32 {
		provider = "unknown"
	}
	if err = appendAudit(ctx, tx, "", "", "auth.login.failed", "", trace, map[string]any{"method": "oidc", "provider": provider, "source_ip": ip, "result": "invalid_credentials"}); err != nil {
		return err
	}
	return tx.Commit()
}

// OIDCPreSession rejects account switching without logout. The callback repeats
// this check under a row lock, so a concurrent local login cannot fix a session.
func (s *Store) OIDCPreSession(ctx context.Context, token string) error {
	if !validToken(token) {
		return ErrUnauthenticated
	}
	var account sql.NullString
	var expiry time.Time
	var revoked sql.NullTime
	err := s.DB.QueryRowContext(ctx, "SELECT user_id,expires_at,revoked_at FROM sessions WHERE id=$1", TokenHash(token)).Scan(&account, &expiry, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUnauthenticated
	}
	if err != nil {
		return err
	}
	if revoked.Valid || !expiry.After(time.Now()) {
		return ErrUnauthenticated
	}
	if account.Valid {
		return apiError(409, "ALREADY_AUTHENTICATED", "Sign out before changing accounts")
	}
	return nil
}

// LoginOIDC may only be called after verified issuer/audience/signature/nonce,
// one-use state, PKCE and explicit operator subject binding. The resulting
// session uses exactly the current local user's roles and security version.
func (s *Store) LoginOIDC(ctx context.Context, oldToken, username, provider, sourceIP, trace string) (string, *AuthSession, error) {
	if !validToken(oldToken) || !usernamePattern.MatchString(username) {
		return "", nil, ErrCredentials
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback()
	var account sql.NullString
	var expiry time.Time
	var revoked sql.NullTime
	err = tx.QueryRowContext(ctx, "SELECT user_id,expires_at,revoked_at FROM sessions WHERE id=$1 FOR UPDATE", TokenHash(oldToken)).Scan(&account, &expiry, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil, ErrCredentials
	}
	if err != nil {
		return "", nil, err
	}
	now := time.Now().UTC()
	if account.Valid || revoked.Valid || !expiry.After(now) {
		return "", nil, ErrCredentials
	}
	var u UserSummary
	var version int
	err = tx.QueryRowContext(ctx, "SELECT id,username,display_name,is_admin,security_version FROM users WHERE username=$1 AND enabled FOR SHARE", username).Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsAdmin, &version)
	if errors.Is(err, sql.ErrNoRows) {
		if err = appendAudit(ctx, tx, "", "", "auth.oidc.failed", digest("account:"+username), trace, map[string]any{"provider": provider, "source_ip": sourceIP, "result": "invalid_credentials"}); err != nil {
			return "", nil, err
		}
		if err = tx.Commit(); err != nil {
			return "", nil, err
		}
		return "", nil, ErrCredentials
	}
	if err != nil {
		return "", nil, err
	}
	token, err := randomToken()
	if err != nil {
		return "", nil, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE sessions SET revoked_at=$2 WHERE id=$1", TokenHash(oldToken), now); err != nil {
		return "", nil, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO sessions(id,user_id,csrf_hash,security_version,issued_at,last_seen_at,expires_at) VALUES($1,$2,$3,$4,$5,$5,$6)", TokenHash(token), u.ID, digest(CSRFToken(token)), version, now, now.Add(AbsoluteTimeout)); err != nil {
		return "", nil, err
	}
	if err = appendAudit(ctx, tx, u.ID, "", "auth.login.succeeded", u.ID, trace, map[string]any{"method": "oidc", "provider": provider, "source_ip": sourceIP}); err != nil {
		return "", nil, err
	}
	if err = tx.Commit(); err != nil {
		return "", nil, err
	}
	return token, &AuthSession{User: u, ID: TokenHash(token), CSRF: CSRFToken(token), ExpiresAt: now.Add(IdleTimeout), SecurityVersion: version}, nil
}
