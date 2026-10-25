package platform

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/credentialvault"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func (s *Store) ConfigureSSHRunner(targetFile, masterKeyFile string) error {
	vault, err := credentialvault.Load(masterKeyFile)
	if err != nil {
		return err
	}
	var provider taskrunner.CredentialProvider
	var credentials *sql.DB
	if vault != nil {
		// Admission often holds a business transaction. Resolve through a separate
		// bounded pool so simultaneous admissions cannot exhaust their own pool
		// while waiting for a second connection to read a credential.
		credentials, err = sql.Open("pgx", s.dsn)
		if err != nil {
			return credentialvault.ErrUnavailable
		}
		credentials.SetMaxOpenConns(4)
		credentials.SetMaxIdleConns(1)
		credentials.SetConnMaxLifetime(30 * time.Minute)
		provider = &credentialvault.Provider{DB: credentials, Vault: vault}
	}
	runner, err := taskrunner.LoadWithCredentials(targetFile, provider)
	if err != nil {
		if credentials != nil {
			credentials.Close()
		}
		return err
	}
	if s.SSHRunner != nil {
		s.SSHRunner.Close()
	}
	if s.credentialDB != nil {
		s.credentialDB.Close()
	}
	s.credentialDB = credentials
	s.SSHRunner = runner
	return nil
}

// SSHDomainKey is safe operator metadata. Ciphertext and private material are
// deliberately absent, including from CLI output and audit records.
type SSHDomainKey struct {
	ID string `json:"id"`
	credentialvault.Identity
	CreatedAt        time.Time  `json:"created_at"`
	RevokedAt        *time.Time `json:"revoked_at,omitempty"`
	RevocationReason string     `json:"revocation_reason,omitempty"`
	StateVersion     int64      `json:"state_version"`
}

const sshDomainColumns = `id,project_id,credential_ref,credential_version,public_key,expires_at,created_at,revoked_at,revocation_reason,state_version`

// ProjectSSHDomainKeys exposes only public identities to current project
// operators, including after project closure so retirement evidence remains visible.
func (s *Store) ProjectSSHDomainKeys(ctx context.Context, a *AuthSession, project string, p Pagination) (Page[SSHDomainKey], error) {
	out := NewPage[SSHDomainKey](p)
	if err := p.Validate(); err != nil {
		return out, err
	}
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project); err != nil {
		return out, err
	}
	if !a.User.IsAdmin {
		var role string
		if err = tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2`, project, a.User.ID).Scan(&role); err != nil {
			return out, err
		}
		if role != "Admin" && role != "DL" && role != "SRE" {
			return out, ErrForbidden
		}
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM ssh_domain_keys WHERE project_id=$1`, project).Scan(&out.Total); err != nil {
		return out, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+sshDomainColumns+` FROM ssh_domain_keys WHERE project_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, project, p.PageSize, p.Offset())
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		x, err := scanSSHDomainKey(rows)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, *x)
	}
	return out, rows.Err()
}

func scanSSHDomainKey(row sshTrustScanner) (*SSHDomainKey, error) {
	x := &SSHDomainKey{}
	err := row.Scan(&x.ID, &x.ProjectID, &x.Ref, &x.Version, &x.PublicKey, &x.ExpiresAt, &x.CreatedAt, &x.RevokedAt, &x.RevocationReason, &x.StateVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return x, err
}

// These operations are server-only owner CLI operations. Runtime roles receive
// SELECT only; there is intentionally no browser secret import/export endpoint.
func (s *Store) CreateSSHDomainKey(ctx context.Context, vault *credentialvault.Vault, i credentialvault.Identity) (*SSHDomainKey, error) {
	i.ExpiresAt = i.ExpiresAt.UTC().Truncate(time.Microsecond)
	if !i.Valid() || !i.ExpiresAt.After(time.Now().Add(5*time.Minute)) {
		return nil, apiError(400, "VALIDATION_ERROR", "Project, credential reference, version and expiry more than five minutes away are required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = lockKey(ctx, tx, "ssh.domain.key:"+i.ProjectID+":"+i.Ref+":"+i.Version); err != nil {
		return nil, err
	}
	var state string
	err = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, i.ProjectID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if state == "archived" || state == "delivered" {
		return nil, apiError(409, "SSH_TRUST_PROJECT_CLOSED", "New keys are not permitted for a closed project")
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ssh_domain_keys WHERE project_id=$1 AND credential_ref=$2 AND credential_version=$3)`, i.ProjectID, i.Ref, i.Version).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return nil, apiError(409, "CREDENTIAL_VERSION_EXISTS", "Credential versions cannot be replaced or reused")
	}
	identity, sealed, err := vault.Generate(i)
	if err != nil {
		return nil, err
	}
	x := &SSHDomainKey{ID: RandomID(), Identity: identity, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), StateVersion: 1}
	_, err = tx.ExecContext(ctx, `INSERT INTO ssh_domain_keys(id,project_id,credential_ref,credential_version,public_key,expires_at,created_at,ciphertext)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, x.ID, x.ProjectID, x.Ref, x.Version, x.PublicKey, x.ExpiresAt, x.CreatedAt, sealed)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, "", x.ProjectID, "credential.ssh.created", x.ID, "cli", map[string]any{"credential_ref": x.Ref, "credential_version": x.Version, "expires_at": x.ExpiresAt}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return x, nil
}

func (s *Store) ListSSHDomainKeys(ctx context.Context, project string, limit, offset int) ([]SSHDomainKey, error) {
	if !sshTrustIDPattern.MatchString(project) || limit < 1 || limit > 100 || offset < 0 {
		return nil, apiError(400, "VALIDATION_ERROR", "A project and pagination limit 1..100 are required")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+sshDomainColumns+` FROM ssh_domain_keys WHERE project_id=$1 ORDER BY created_at DESC,id LIMIT $2 OFFSET $3`, project, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []SSHDomainKey{}
	for rows.Next() {
		x, err := scanSSHDomainKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *x)
	}
	return items, rows.Err()
}

func (s *Store) RevokeSSHDomainKey(ctx context.Context, project, id string, version int64, reason string) (*SSHDomainKey, error) {
	reason = strings.TrimSpace(reason)
	if !sshTrustIDPattern.MatchString(project) || !sshTrustIDPattern.MatchString(id) || version < 1 || utf8.RuneCountInString(reason) < 1 || utf8.RuneCountInString(reason) > 500 || strings.ContainsAny(reason, "\x00\r\n") {
		return nil, apiError(400, "VALIDATION_ERROR", "Project, key id, expected version and reason are required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, err := scanSSHDomainKey(tx.QueryRowContext(ctx, `SELECT `+sshDomainColumns+` FROM ssh_domain_keys WHERE project_id=$1 AND id=$2 FOR UPDATE`, project, id))
	if err != nil {
		return nil, err
	}
	// An exact repeated revocation returns its existing evidence, without another
	// state transition or audit event. Conflicting retries still fail.
	if x.RevokedAt != nil {
		if x.StateVersion == version+1 && x.RevocationReason == reason {
			return x, nil
		}
		return nil, errVersion
	}
	if x.StateVersion != version {
		return nil, errVersion
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	x.RevokedAt = &now
	x.RevocationReason = reason
	x.StateVersion++
	_, err = tx.ExecContext(ctx, `UPDATE ssh_domain_keys SET revoked_at=$3,revocation_reason=$4,state_version=state_version+1 WHERE project_id=$1 AND id=$2`, project, id, now, reason)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, "", project, "credential.ssh.revoked", id, "cli", map[string]any{"credential_ref": x.Ref, "credential_version": x.Version, "reason": reason}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return x, nil
}
