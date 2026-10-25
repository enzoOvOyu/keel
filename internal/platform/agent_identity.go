package platform

import (
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
)

// ErrAgentConnectionBusy preserves the existing live connection until its lease expires.
var ErrAgentConnectionBusy = errors.New("Agent connection is still active")

var ErrAgentDenied = errors.New("Agent identity or authorization is unavailable")
var agentVersionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]{0,63}$`)

type AgentEnrollment struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	MachineID    string    `json:"machine_id"`
	Serial       string    `json:"serial_number"`
	AssetVersion int64     `json:"asset_version"`
	ExpiresAt    time.Time `json:"expires_at"`
}
type AgentCredential struct {
	ID              string     `json:"id"`
	ProjectID       string     `json:"project_id"`
	MachineID       string     `json:"machine_id"`
	Serial          string     `json:"serial_number"`
	AssetVersion    int64      `json:"asset_version"`
	AuthorityID     string     `json:"authority_id"`
	CertificateHash string     `json:"certificate_hash"`
	CertificatePEM  string     `json:"certificate_pem"`
	AgentVersion    string     `json:"agent_version"`
	ExpiresAt       time.Time  `json:"expires_at"`
	RevokedAt       *time.Time `json:"revoked_at"`
	StateVersion    int64      `json:"state_version"`
}

func agentTokenHash(token string) string { return digest("keel-agent-enrollment-v1:" + token) }
func agentTarget(ctx context.Context, tx *sql.Tx, project, machine string) (string, int64, error) {
	var serial, state, assetState string
	var version int64
	e := tx.QueryRowContext(ctx, `SELECT p.state,m.serial_number,a.state,a.version FROM projects p JOIN project_assets a ON a.project_id=p.id JOIN machines m ON m.id=a.machine_id WHERE p.id=$1 AND m.id=$2 FOR SHARE OF p,a,m`, project, machine).Scan(&state, &serial, &assetState, &version)
	if errors.Is(e, sql.ErrNoRows) {
		return "", 0, ErrAgentDenied
	}
	if e != nil {
		return "", 0, e
	}
	if state == "delivered" || state == "archived" || assetState != "provisioned" {
		return "", 0, ErrAgentDenied
	}
	return serial, version, nil
}

// IssueAgentEnrollment is an owner-only CLI action; runtime SQL roles cannot
// insert grants. The raw secret is returned once for a protected local file.
func (s *Store) IssueAgentEnrollment(ctx context.Context, project, machine, authority, reason string, ttl time.Duration) (*AgentEnrollment, string, error) {
	if !sshTrustIDPattern.MatchString(project) || !sshTrustIDPattern.MatchString(machine) || len(authority) != 64 || len(reason) < 1 || len(reason) > 500 || strings.ContainsAny(reason, "\x00\r\n") || ttl < time.Minute || ttl > 15*time.Minute {
		return nil, "", ErrAgentDenied
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, "", e
	}
	defer tx.Rollback()
	serial, version, e := agentTarget(ctx, tx, project, machine)
	if e != nil {
		return nil, "", e
	}
	var active bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_credentials WHERE machine_id=$1 AND revoked_at IS NULL)`, machine).Scan(&active); e != nil {
		return nil, "", e
	}
	if active {
		return nil, "", ErrAgentDenied
	}
	token, e := randomToken()
	if e != nil {
		return nil, "", e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	out := &AgentEnrollment{ID: RandomID(), ProjectID: project, MachineID: machine, Serial: serial, AssetVersion: version, ExpiresAt: now.Add(ttl)}
	_, e = tx.ExecContext(ctx, `INSERT INTO agent_enrollments(id,project_id,machine_id,asset_version,serial_number,authority_id,token_hash,created_at,expires_at,reason)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, out.ID, project, machine, version, serial, authority, agentTokenHash(token), now, out.ExpiresAt, reason)
	if e != nil {
		return nil, "", e
	}
	if e = appendAudit(ctx, tx, "", project, "agent.enrollment.issued", out.ID, "cli", map[string]any{"machine_id": machine, "asset_version": version, "expires_at": out.ExpiresAt, "authority_id": authority, "reason": reason, "operator_uid": os.Geteuid()}); e != nil {
		return nil, "", e
	}
	if e = tx.Commit(); e != nil {
		return nil, "", e
	}
	return out, token, nil
}
func (s *Store) CancelAgentEnrollment(ctx context.Context, project, id, reason string) error {
	if !sshTrustIDPattern.MatchString(project) || !sshTrustIDPattern.MatchString(id) || len(reason) < 1 || len(reason) > 500 || strings.ContainsAny(reason, "\x00\r\n") {
		return ErrAgentDenied
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var consumed, revoked sql.NullTime
	e = tx.QueryRowContext(ctx, `SELECT consumed_at,revoked_at FROM agent_enrollments WHERE project_id=$1 AND id=$2 FOR UPDATE`, project, id).Scan(&consumed, &revoked)
	if e != nil || consumed.Valid {
		return ErrAgentDenied
	}
	if revoked.Valid {
		return tx.Commit()
	}
	if _, e = tx.ExecContext(ctx, `UPDATE agent_enrollments SET revoked_at=now() WHERE id=$1`, id); e != nil {
		return e
	}
	if e = appendAudit(ctx, tx, "", project, "agent.enrollment.cancelled", id, "cli", map[string]any{"reason": reason, "operator_uid": os.Geteuid()}); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) EnrollAgent(ctx context.Context, a *agentpki.Authority, project, machine, serial, version, token string, csr []byte) (*AgentCredential, error) {
	if a == nil || !sshTrustIDPattern.MatchString(project) || !sshTrustIDPattern.MatchString(machine) || !validToken(token) || !agentVersionPattern.MatchString(version) || len(serial) < 1 || len(serial) > 128 {
		return nil, ErrAgentDenied
	}
	if _, e := agentpki.CSR(csr); e != nil {
		return nil, ErrAgentDenied
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var grant, expectedSerial, authority string
	var assetVersion int64
	var expiry time.Time
	var consumed, revoked sql.NullTime
	e = tx.QueryRowContext(ctx, `SELECT id,serial_number,asset_version,authority_id,expires_at,consumed_at,revoked_at FROM agent_enrollments WHERE token_hash=$1 AND project_id=$2 AND machine_id=$3 FOR UPDATE`, agentTokenHash(token), project, machine).Scan(&grant, &expectedSerial, &assetVersion, &authority, &expiry, &consumed, &revoked)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrAgentDenied
	}
	if e != nil {
		return nil, e
	}
	if consumed.Valid || revoked.Valid || !expiry.After(time.Now()) || authority != a.ID || serial != expectedSerial {
		return nil, ErrAgentDenied
	}
	currentSerial, currentVersion, e := agentTarget(ctx, tx, project, machine)
	if e != nil {
		return nil, e
	}
	if currentSerial != serial || currentVersion != assetVersion {
		return nil, ErrAgentDenied
	}
	now := time.Now().UTC().Truncate(time.Second)
	id := RandomID()
	public, expires, e := a.Issue(csr, project, machine, id, now)
	if e != nil {
		return nil, ErrAgentDenied
	}
	certificate, e := agentpki.Certificate(public)
	if e != nil {
		return nil, ErrAgentDenied
	}
	out := &AgentCredential{ID: id, ProjectID: project, MachineID: machine, Serial: serial, AssetVersion: assetVersion, AuthorityID: authority, CertificatePEM: string(public), CertificateHash: agentpki.Hash(certificate.Raw), AgentVersion: version, ExpiresAt: expires, StateVersion: 1}
	_, e = tx.ExecContext(ctx, `INSERT INTO agent_credentials(id,enrollment_id,project_id,machine_id,asset_version,serial_number,authority_id,certificate_pem,certificate_hash,agent_version,created_at,expires_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, id, grant, project, machine, assetVersion, serial, authority, string(public), agentpki.Hash(certificate.Raw), version, now, expires)
	if e != nil {
		return nil, ErrAgentDenied
	}
	if _, e = tx.ExecContext(ctx, `UPDATE agent_enrollments SET consumed_at=$2,certificate_id=$3 WHERE id=$1`, grant, time.Now().UTC(), id); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, "", project, "agent.enrolled", id, "agent", map[string]any{"machine_id": machine, "enrollment_id": grant, "asset_version": assetVersion, "authority_id": authority, "expires_at": expires, "declared_version": version}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return out, nil
}
func agentCredential(ctx context.Context, tx *sql.Tx, cert *x509.Certificate, authority string) (*AgentCredential, error) {
	if cert == nil || time.Now().Before(cert.NotBefore) || !time.Now().Before(cert.NotAfter) {
		return nil, ErrAgentDenied
	}
	out := &AgentCredential{}
	e := tx.QueryRowContext(ctx, `SELECT id,project_id,machine_id,serial_number,asset_version,authority_id,certificate_pem,agent_version,expires_at,revoked_at,state_version FROM agent_credentials WHERE certificate_hash=$1 FOR SHARE`, agentpki.Hash(cert.Raw)).Scan(&out.ID, &out.ProjectID, &out.MachineID, &out.Serial, &out.AssetVersion, &out.AuthorityID, &out.CertificatePEM, &out.AgentVersion, &out.ExpiresAt, &out.RevokedAt, &out.StateVersion)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrAgentDenied
	}
	if e != nil {
		return nil, e
	}
	if out.RevokedAt != nil || out.AuthorityID != authority || !out.ExpiresAt.After(time.Now()) || len(cert.URIs) != 1 || cert.URIs[0].String() != agentpki.URI(out.ProjectID, out.MachineID, out.ID).String() {
		return nil, ErrAgentDenied
	}
	out.CertificateHash = agentpki.Hash(cert.Raw)
	serial, version, e := agentTarget(ctx, tx, out.ProjectID, out.MachineID)
	if e != nil {
		return nil, e
	}
	if serial != out.Serial || version != out.AssetVersion {
		return nil, ErrAgentDenied
	}
	return out, nil
}
func (s *Store) CheckAgentCertificate(ctx context.Context, cert *x509.Certificate, authority string) (*AgentCredential, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	out, e := agentCredential(ctx, tx, cert, authority)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) OpenAgentConnection(ctx context.Context, cert *x509.Certificate, authority, project, machine, serial, version, boot string) (string, error) {
	if !sshTrustIDPattern.MatchString(boot) {
		return "", ErrAgentDenied
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	out, e := agentCredential(ctx, tx, cert, authority)
	if e != nil {
		return "", e
	}
	if out.ProjectID != project || out.MachineID != machine || out.Serial != serial || out.AgentVersion != version {
		return "", ErrAgentDenied
	}
	if e = lockKey(ctx, tx, "agent.connection:"+out.ID); e != nil {
		return "", e
	}
	var recent bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_connections WHERE credential_id=$1 AND (connected_at>now()-interval '1 second' OR (disconnected_at IS NULL AND last_seen_at>now()-interval '30 seconds')))`, out.ID).Scan(&recent); e != nil {
		return "", e
	}
	if recent {
		return "", ErrAgentConnectionBusy
	}
	if _, e = tx.ExecContext(ctx, `UPDATE agent_connections SET disconnected_at=now() WHERE credential_id=$1 AND disconnected_at IS NULL`, out.ID); e != nil {
		return "", e
	}
	id := RandomID()
	if _, e = tx.ExecContext(ctx, `INSERT INTO agent_connections(id,credential_id,boot_id,agent_version,connected_at,last_seen_at)VALUES($1,$2,$3,$4,now(),now())`, id, out.ID, boot, version); e != nil {
		return "", e
	}
	if e = appendAudit(ctx, tx, "", project, "agent.connected", id, "agent", map[string]any{"credential_id": out.ID, "machine_id": machine}); e != nil {
		return "", e
	}
	return id, tx.Commit()
}
func (s *Store) AgentHeartbeat(ctx context.Context, cert *x509.Certificate, authority, connection string, sequence uint64) error {
	if sequence == 0 || sequence > 1<<62 {
		return ErrAgentDenied
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	out, e := agentCredential(ctx, tx, cert, authority)
	if e != nil {
		return e
	}
	result, e := tx.ExecContext(ctx, `UPDATE agent_connections SET sequence=$3,last_seen_at=now() WHERE id=$1 AND credential_id=$2 AND disconnected_at IS NULL AND sequence=$3-1 AND last_seen_at>now()-interval '30 seconds'`, connection, out.ID, int64(sequence))
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil || n != 1 {
		return ErrAgentDenied
	}
	return tx.Commit()
}
func (s *Store) CloseAgentConnection(ctx context.Context, id string) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE agent_connections SET disconnected_at=now() WHERE id=$1 AND disconnected_at IS NULL`, id)
	return e
}
func (s *Store) RevokeAgentCredential(ctx context.Context, project, id string, expected int64, reason string) error {
	if !sshTrustIDPattern.MatchString(project) || !sshTrustIDPattern.MatchString(id) || expected < 1 || len(reason) < 1 || len(reason) > 500 || strings.ContainsAny(reason, "\x00\r\n") {
		return ErrAgentDenied
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var revoked sql.NullTime
	var version int64
	var previous string
	e = tx.QueryRowContext(ctx, `SELECT revoked_at,state_version,revocation_reason FROM agent_credentials WHERE id=$1 AND project_id=$2 FOR UPDATE`, id, project).Scan(&revoked, &version, &previous)
	if e != nil {
		return ErrAgentDenied
	}
	if revoked.Valid {
		if version == expected+1 && reason == previous {
			return tx.Commit()
		}
		return errVersion
	}
	if version != expected {
		return errVersion
	}
	if _, e = tx.ExecContext(ctx, `UPDATE agent_credentials SET revoked_at=now(),revocation_reason=$2,state_version=state_version+1 WHERE id=$1`, id, reason); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE agent_connections SET disconnected_at=now() WHERE credential_id=$1 AND disconnected_at IS NULL`, id); e != nil {
		return e
	}
	if e = appendAudit(ctx, tx, "", project, "agent.revoked", id, "cli", map[string]any{"reason": reason, "operator_uid": os.Geteuid()}); e != nil {
		return e
	}
	return tx.Commit()
}
