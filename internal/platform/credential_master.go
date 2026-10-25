package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/credentialvault"
)

type CredentialMasterInspection struct {
	KeyID           string `json:"key_id"`
	RegisteredKeyID string `json:"registered_key_id,omitempty"`
	Epoch           int64  `json:"epoch"`
	Keys            int    `json:"keys"`
	Verified        bool   `json:"verified"`
}
type CredentialMasterRewrapInput struct {
	ID                  string
	ExpectedSourceKeyID string
	ExpectedKeys        int
	Reason              string
}
type CredentialMasterRewrap struct {
	ID               string    `json:"id"`
	SourceKeyID      string    `json:"source_key_id"`
	TargetKeyID      string    `json:"target_key_id"`
	Keys             int       `json:"keys"`
	Epoch            int64     `json:"epoch"`
	Reason           string    `json:"reason"`
	CreatedAt        time.Time `json:"created_at"`
	AlreadyCommitted bool      `json:"already_committed"`
}
type wrappedDomainKey struct {
	id       string
	identity credentialvault.Identity
	sealed   []byte
}

func requireDatabaseOwner(ctx context.Context, tx *sql.Tx) error {
	var allowed bool
	if err := tx.QueryRowContext(ctx, `SELECT r.rolsuper OR pg_has_role(current_user,d.datdba,'MEMBER') FROM pg_roles r JOIN pg_database d ON d.datname=current_database() WHERE r.rolname=current_user`).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return ErrForbidden
	}
	return nil
}
func wrappedDomainKeys(ctx context.Context, tx *sql.Tx) ([]wrappedDomainKey, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id,project_id,credential_ref,credential_version,public_key,expires_at,ciphertext FROM ssh_domain_keys ORDER BY id LIMIT 10001`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []wrappedDomainKey{}
	for rows.Next() {
		var row wrappedDomainKey
		i := &row.identity
		if err = rows.Scan(&row.id, &i.ProjectID, &i.Ref, &i.Version, &i.PublicKey, &i.ExpiresAt, &row.sealed); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > 10000 {
		return nil, apiError(409, "CREDENTIAL_MASTER_LIMIT", "Owner maintenance supports at most 10000 stored domain keys per atomic rewrap")
	}
	return out, nil
}
func masterGeneration(ctx context.Context, tx *sql.Tx) (string, int64, error) {
	var id string
	var epoch int64
	err := tx.QueryRowContext(ctx, "SELECT active_key_id,epoch FROM credential_master_state WHERE singleton=true").Scan(&id, &epoch)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	return id, epoch, err
}

// InspectCredentialMaster authenticates all stored identities, including expired
// and revoked history, without producing signers or changing any record.
func (s *Store) InspectCredentialMaster(ctx context.Context, vault *credentialvault.Vault) (*CredentialMasterInspection, error) {
	if vault == nil || !sha256Pattern.MatchString(vault.KeyID()) {
		return nil, credentialvault.ErrUnavailable
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireDatabaseOwner(ctx, tx); err != nil {
		return nil, err
	}
	registered, epoch, err := masterGeneration(ctx, tx)
	if err != nil {
		return nil, err
	}
	if registered != "" && registered != vault.KeyID() {
		return nil, credentialvault.ErrUnavailable
	}
	rows, err := wrappedDomainKeys(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if err = vault.CheckSealed(row.identity, row.sealed); err != nil {
			return nil, credentialvault.ErrUnavailable
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &CredentialMasterInspection{KeyID: vault.KeyID(), RegisteredKeyID: registered, Epoch: epoch, Keys: len(rows), Verified: true}, nil
}

// RewrapCredentialMaster changes storage custody atomically, not the SSH key,
// expiry, revocation state or execution snapshots. The caller retains both
// private master files and must arrange a maintenance window for runtime rollout.
func (s *Store) RewrapCredentialMaster(ctx context.Context, previous, next *credentialvault.Vault, in CredentialMasterRewrapInput) (*CredentialMasterRewrap, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if previous == nil || next == nil || !modelIDPattern.MatchString(in.ID) || !sha256Pattern.MatchString(in.ExpectedSourceKeyID) || previous.KeyID() != in.ExpectedSourceKeyID || !sha256Pattern.MatchString(next.KeyID()) || next.KeyID() == previous.KeyID() || in.ExpectedKeys < 0 || in.ExpectedKeys > 10000 || !utf8.ValidString(in.Reason) || utf8.RuneCountInString(in.Reason) < 1 || utf8.RuneCountInString(in.Reason) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "Distinct protected masters, rotation ID, exact source fingerprint/count and reason are required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireDatabaseOwner(ctx, tx); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, "SET LOCAL lock_timeout='5s'"); err != nil {
		return nil, err
	}
	if err = lockKey(ctx, tx, "ssh.master.lifecycle"); err != nil {
		return nil, err
	}
	out := &CredentialMasterRewrap{}
	err = tx.QueryRowContext(ctx, `SELECT id,source_key_id,target_key_id,key_count,epoch,reason,created_at FROM credential_master_rewraps WHERE id=$1`, in.ID).Scan(&out.ID, &out.SourceKeyID, &out.TargetKeyID, &out.Keys, &out.Epoch, &out.Reason, &out.CreatedAt)
	if err == nil {
		if out.SourceKeyID != previous.KeyID() || out.TargetKeyID != next.KeyID() || out.Keys != in.ExpectedKeys || out.Reason != in.Reason {
			return nil, apiError(409, "IDEMPOTENCY_CONFLICT", "Rotation ID is already bound to different maintenance inputs")
		}
		out.AlreadyCommitted = true
		return out, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	// EXCLUSIVE conflicts with key-install/revocation row-lock transactions but
	// permits normal SELECT signing lookups. No partial generation is committed.
	if _, err = tx.ExecContext(ctx, "LOCK TABLE ssh_domain_keys IN EXCLUSIVE MODE"); err != nil {
		return nil, err
	}
	registered, epoch, err := masterGeneration(ctx, tx)
	if err != nil {
		return nil, err
	}
	if registered != "" && registered != previous.KeyID() {
		return nil, credentialvault.ErrUnavailable
	}
	var reused bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM credential_master_rewraps WHERE source_key_id=$1 OR target_key_id=$1)", next.KeyID()).Scan(&reused); err != nil {
		return nil, err
	}
	if reused {
		return nil, apiError(409, "CREDENTIAL_MASTER_REUSED", "Choose a newly generated master; previously registered custody keys cannot be reused")
	}
	rows, err := wrappedDomainKeys(ctx, tx)
	if err != nil {
		return nil, err
	}
	if len(rows) != in.ExpectedKeys {
		return nil, apiError(409, "CREDENTIAL_MASTER_COUNT_CHANGED", "Stored key count changed; inspect current custody before retrying")
	}
	// Authenticate and prepare every row before recording any custody change.
	sealed := make([][]byte, len(rows))
	for index, row := range rows {
		sealed[index], err = previous.Rewrap(next, row.identity, row.sealed)
		if err != nil {
			return nil, credentialvault.ErrUnavailable
		}
	}
	out = &CredentialMasterRewrap{ID: in.ID, SourceKeyID: previous.KeyID(), TargetKeyID: next.KeyID(), Keys: len(rows), Epoch: epoch + 1, Reason: in.Reason, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credential_master_rewraps(id,source_key_id,target_key_id,key_count,epoch,reason,created_at)VALUES($1,$2,$3,$4,$5,$6,$7)`, out.ID, out.SourceKeyID, out.TargetKeyID, out.Keys, out.Epoch, out.Reason, out.CreatedAt); err != nil {
		return nil, err
	}
	for index, row := range rows {
		before, after := sha256.Sum256(row.sealed), sha256.Sum256(sealed[index])
		if _, err = tx.ExecContext(ctx, `INSERT INTO credential_master_rewrap_entries(rotation_id,domain_key_id,previous_cipher_hash,next_cipher_hash)VALUES($1,$2,$3,$4)`, out.ID, row.id, hex.EncodeToString(before[:]), hex.EncodeToString(after[:])); err != nil {
			return nil, err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE ssh_domain_keys SET ciphertext=$1 WHERE id=$2", sealed[index], row.id); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO credential_master_state(singleton,active_key_id,epoch,updated_at)VALUES(true,$1,$2,$3) ON CONFLICT(singleton) DO UPDATE SET active_key_id=excluded.active_key_id,epoch=excluded.epoch,updated_at=excluded.updated_at`, out.TargetKeyID, out.Epoch, out.CreatedAt); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, "", "", "credential.master.rewrapped", out.ID, "cli", map[string]any{"source_key_id": out.SourceKeyID, "target_key_id": out.TargetKeyID, "key_count": out.Keys, "epoch": out.Epoch, "reason": out.Reason}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
