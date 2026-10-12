package platform

import (
	"context"
	"database/sql"
	"errors"
	"net"
)

func (s *Store) ResetPassword(ctx context.Context, username, password string) error {
	hash, e := HashPassword(password)
	if e != nil {
		return e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var id string
	e = tx.QueryRowContext(ctx, "SELECT id FROM users WHERE username=$1 FOR UPDATE", NormalizeUsername(username)).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return errors.New("user not found")
	}
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE users SET password_hash=$2,security_version=security_version+1 WHERE id=$1", id, hash); e != nil {
		return e
	}
	if e = appendAudit(ctx, tx, "", "", "user.password_reset", id, "cli", map[string]any{"source": "server_cli", "sessions_revoked": true}); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) UnlockIP(ctx context.Context, ip string) error {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return errors.New("--ip must be an IP address")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	key := digest("ip:" + parsed.String())
	if e = lockKey(ctx, tx, "login:"+key); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "DELETE FROM login_throttles WHERE id=$1", key); e != nil {
		return e
	}
	if e = appendAudit(ctx, tx, "", "", "auth.ip_unlock", key, "cli", map[string]any{"source": "server_cli", "source_ip": parsed.String()}); e != nil {
		return e
	}
	return tx.Commit()
}
