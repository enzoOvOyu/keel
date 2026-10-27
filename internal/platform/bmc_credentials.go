package platform

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
	"github.com/enzoOvOyu/keel/internal/credentialvault"
)

type DevicePassword struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type DeviceCredentialVersion struct {
	ProjectID         string    `json:"project_id"`
	BatchRef          string    `json:"credential_batch_ref"`
	Scope             string    `json:"scope"`
	Version           string    `json:"credential_version"`
	ExpiresAt         time.Time `json:"expires_at"`
	WrappingKeyID     string    `json:"wrapping_key_id"`
	VerificationState string    `json:"verification_state"`
}
type bmcCredentialProvider struct {
	db    *sql.DB
	vault *credentialvault.Vault
}

func (p *bmcCredentialProvider) Resolve(ctx context.Context, target bmc.Target) (bmc.Credentials, error) {
	if p == nil || p.db == nil || p.vault == nil || target.Validate() != nil {
		return bmc.Credentials{}, credentialvault.ErrUnavailable
	}
	identity := credentialvault.SecretIdentity{ProjectID: target.ProjectID, Ref: target.CredentialBatchRef, Version: target.CredentialVersion, Scope: "bmc"}
	var sealed []byte
	var keyID string
	var revoked bool
	e := p.db.QueryRowContext(ctx, `SELECT v.expires_at,v.wrapping_key_id,v.ciphertext,EXISTS(SELECT 1 FROM device_credential_revocations r WHERE (r.project_id,r.credential_batch_ref,r.scope,r.credential_version)=(v.project_id,v.credential_batch_ref,v.scope,v.credential_version)) FROM device_credential_versions v WHERE v.project_id=$1 AND v.credential_batch_ref=$2 AND v.scope='bmc' AND v.credential_version=$3`, identity.ProjectID, identity.Ref, identity.Version).Scan(&identity.ExpiresAt, &keyID, &sealed, &revoked)
	if e != nil || revoked || keyID != p.vault.KeyID() {
		return bmc.Credentials{}, credentialvault.ErrUnavailable
	}
	if !time.Now().Before(identity.ExpiresAt) {
		return bmc.Credentials{}, bmc.ErrCredentialExpired
	}
	plain, e := p.vault.OpenSecret(identity, sealed)
	if e != nil {
		return bmc.Credentials{}, credentialvault.ErrUnavailable
	}
	defer clear(plain)
	var value DevicePassword
	if json.Unmarshal(plain, &value) != nil || !validDevicePassword(value) {
		return bmc.Credentials{}, credentialvault.ErrUnavailable
	}
	return bmc.Credentials{Username: value.Username, Password: value.Password, ExpiresAt: identity.ExpiresAt, BatchRef: identity.Ref, Version: identity.Version}, nil
}
func validDevicePassword(value DevicePassword) bool {
	return len(value.Username) > 0 && len(value.Username) <= 256 && !strings.ContainsAny(value.Username, ":\r\n\x00") && len(value.Password) > 0 && len(value.Password) <= 4096 && !strings.ContainsRune(value.Password, 0)
}
func (s *Store) ConfigureBMCInventory(path, masterFile string) error {
	vault, e := credentialvault.Load(masterFile)
	if e != nil {
		return e
	}
	suite, e := bmc.LoadSuite(path, &bmcCredentialProvider{db: s.DB, vault: vault})
	if e != nil {
		return e
	}
	if s.BMCInventory != nil {
		s.BMCInventory.Close()
	}
	s.BMCInventory = suite
	return nil
}

// ImportDeviceCredential is a server-owner operation, never a browser secret
// endpoint. A CAS updates the batch head; old exact versions remain available
// until their independent expiry/revocation. Full-batch verification stays pending.
func (s *Store) ImportDeviceCredential(ctx context.Context, vault *credentialvault.Vault, identity credentialvault.SecretIdentity, value DevicePassword, expectedCurrent, reason string) (*DeviceCredentialVersion, error) {
	reason = strings.TrimSpace(reason)
	if vault == nil || !validDevicePassword(value) || len(reason) < 3 || len(reason) > 1000 {
		return nil, apiError(400, "DEVICE_CREDENTIAL_INVALID", "Protected credential, identity and reason required")
	}
	identity.ExpiresAt = identity.ExpiresAt.UTC().Truncate(time.Microsecond)
	plain, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	defer clear(plain)
	sealed, e := vault.SealSecret(identity, plain)
	if e != nil {
		return nil, credentialvault.ErrUnavailable
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "device-credential:"+identity.ProjectID+":"+identity.Ref+":"+identity.Scope); e != nil {
		return nil, e
	}
	var vaultPath string
	e = tx.QueryRowContext(ctx, `SELECT vault_path FROM bom_credential_batches WHERE project_id=$1 AND credential_batch_ref=$2 AND scope=$3 FOR SHARE`, identity.ProjectID, identity.Ref, identity.Scope).Scan(&vaultPath)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	var current string
	e = tx.QueryRowContext(ctx, `SELECT credential_version FROM device_credential_heads WHERE project_id=$1 AND credential_batch_ref=$2 AND scope=$3 FOR UPDATE`, identity.ProjectID, identity.Ref, identity.Scope).Scan(&current)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	out := &DeviceCredentialVersion{ProjectID: identity.ProjectID, BatchRef: identity.Ref, Scope: identity.Scope, Version: identity.Version, ExpiresAt: identity.ExpiresAt, WrappingKeyID: vault.KeyID(), VerificationState: "pending"}
	var oldSealed []byte
	var oldExpiry time.Time
	var oldKey, oldPath string
	e = tx.QueryRowContext(ctx, `SELECT ciphertext,expires_at,wrapping_key_id,vault_path FROM device_credential_versions WHERE project_id=$1 AND credential_batch_ref=$2 AND scope=$3 AND credential_version=$4`, identity.ProjectID, identity.Ref, identity.Scope, identity.Version).Scan(&oldSealed, &oldExpiry, &oldKey, &oldPath)
	if e == nil {
		previous, e := vault.OpenSecret(identity, oldSealed)
		if e != nil || !oldExpiry.Equal(identity.ExpiresAt) || oldKey != vault.KeyID() || oldPath != vaultPath || !bytes.Equal(previous, plain) {
			clear(previous)
			return nil, apiError(409, "DEVICE_CREDENTIAL_VERSION_CONFLICT", "Existing credential version cannot be replaced")
		}
		clear(previous)
		if current != identity.Version {
			return nil, apiError(409, "DEVICE_CREDENTIAL_HEAD_CHANGED", "Historical credential cannot replace the current batch head")
		}
		if e = tx.QueryRowContext(ctx, `SELECT verification_state FROM device_credential_heads WHERE project_id=$1 AND credential_batch_ref=$2 AND scope=$3`, identity.ProjectID, identity.Ref, identity.Scope).Scan(&out.VerificationState); e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if current != expectedCurrent {
		return nil, apiError(409, "DEVICE_CREDENTIAL_HEAD_CHANGED", "Expected current credential version differs")
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO device_credential_versions(project_id,credential_batch_ref,scope,credential_version,expires_at,vault_path,wrapping_key_id,ciphertext,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now())`, identity.ProjectID, identity.Ref, identity.Scope, identity.Version, identity.ExpiresAt, vaultPath, vault.KeyID(), sealed, reason)
	if e != nil {
		return nil, e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO device_credential_heads(project_id,credential_batch_ref,scope,credential_version,state_version,verification_state,updated_at) VALUES($1,$2,$3,$4,1,'pending',now()) ON CONFLICT(project_id,credential_batch_ref,scope) DO UPDATE SET credential_version=excluded.credential_version,state_version=device_credential_heads.state_version+1,verification_state='pending',updated_at=now()`, identity.ProjectID, identity.Ref, identity.Scope, identity.Version)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, "", identity.ProjectID, "credential.batch.rotate", identity.Ref, "cli", map[string]any{"scope": identity.Scope, "credential_version": identity.Version, "previous_version": current, "expires_at": identity.ExpiresAt, "wrapping_key_id": vault.KeyID(), "verification_state": "pending", "reason": reason}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) RevokeDeviceCredential(ctx context.Context, project, batch, scope, version, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 || len(reason) > 1000 {
		return apiError(400, "DEVICE_CREDENTIAL_REASON_REQUIRED", "Revocation reason required")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "device-credential:"+project+":"+batch+":"+scope); e != nil {
		return e
	}
	result, e := tx.ExecContext(ctx, `INSERT INTO device_credential_revocations(project_id,credential_batch_ref,scope,credential_version,reason,revoked_at) SELECT project_id,credential_batch_ref,scope,credential_version,$5,now() FROM device_credential_versions WHERE project_id=$1 AND credential_batch_ref=$2 AND scope=$3 AND credential_version=$4 ON CONFLICT DO NOTHING`, project, batch, scope, version, reason)
	if e != nil {
		return e
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		var exists bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM device_credential_revocations WHERE project_id=$1 AND credential_batch_ref=$2 AND scope=$3 AND credential_version=$4)`, project, batch, scope, version).Scan(&exists); e != nil {
			return e
		}
		if !exists {
			return ErrNotFound
		}
		return tx.Commit()
	}
	if e = appendAudit(ctx, tx, "", project, "credential.batch.revoke", batch, "cli", map[string]any{"scope": scope, "credential_version": version, "reason": reason}); e != nil {
		return e
	}
	return tx.Commit()
}
