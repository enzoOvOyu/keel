package platform

import (
	"context"
	"crypto/x509"
	"database/sql"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// ActiveAgentIdentityTx freezes current identity with the same transaction that
// admits an execution. A heartbeat is never hardware or SSH trust evidence.
func (s *Store) ActiveAgentIdentityTx(ctx context.Context, tx *sql.Tx, project, machine string) (*AgentCredential, error) {
	serial, version, e := agentTarget(ctx, tx, project, machine)
	if e != nil {
		return nil, e
	}
	out := &AgentCredential{}
	e = tx.QueryRowContext(ctx, `SELECT c.id,c.project_id,c.machine_id,c.serial_number,c.asset_version,c.authority_id,c.certificate_pem,c.certificate_hash,c.agent_version,`+agentCredentialExpirySQL+`,c.revoked_at,c.state_version FROM agent_credentials c WHERE c.project_id=$1 AND c.machine_id=$2 AND c.revoked_at IS NULL AND `+agentCredentialExpirySQL+`>now() FOR SHARE OF c`, project, machine).Scan(&out.ID, &out.ProjectID, &out.MachineID, &out.Serial, &out.AssetVersion, &out.AuthorityID, &out.CertificatePEM, &out.CertificateHash, &out.AgentVersion, &out.ExpiresAt, &out.RevokedAt, &out.StateVersion)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrAgentDenied
	}
	if e != nil {
		return nil, e
	}
	if out.Serial != serial || out.AssetVersion != version {
		return nil, ErrAgentDenied
	}
	return out, nil
}
func (s *Store) ActiveAgentIdentity(ctx context.Context, project, machine string) (*AgentCredential, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	out, e := s.ActiveAgentIdentityTx(ctx, tx, project, machine)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

// ActiveAgentTaskBinding is available to the API/owner CLI without a local
// transport session. Do still requires the worker's authenticated live stream.
func (s *Store) ActiveAgentTaskBinding(ctx context.Context, project, machine string) (*AgentCredential, string, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, "", e
	}
	defer tx.Rollback()
	out, e := s.ActiveAgentIdentityTx(ctx, tx, project, machine)
	if e != nil {
		return nil, "", e
	}
	connection, e := s.ActiveAgentTaskConnectionTx(ctx, tx, out.ID)
	if e != nil {
		return nil, "", e
	}
	return out, connection, tx.Commit()
}

// ActiveAgentTaskConnectionTx checks liveness inside the identity/admission
// transaction, avoiding a second connection and inconsistent credential reads.
// This metadata permits admission only; it never gives a worker a task stream.
func (s *Store) ActiveAgentTaskConnectionTx(ctx context.Context, tx *sql.Tx, credential string) (string, error) {
	var connection string
	e := tx.QueryRowContext(ctx, `SELECT id FROM agent_connections WHERE credential_id=$1 AND disconnected_at IS NULL AND last_seen_at>now()-interval '30 seconds' AND task_session_id IS NOT NULL AND task_disconnected_at IS NULL FOR SHARE`, credential).Scan(&connection)
	if errors.Is(e, sql.ErrNoRows) {
		return "", ErrAgentDenied
	}
	if e != nil {
		return "", e
	}
	return connection, nil
}
func (s *Store) VerifyAgentTaskConnection(ctx context.Context, cert *x509.Certificate, authority, connection string) (*AgentCredential, error) {
	if !sshTrustIDPattern.MatchString(connection) {
		return nil, ErrAgentDenied
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	out, e := agentCredential(ctx, tx, cert, authority)
	if e != nil {
		return nil, e
	}
	var exists bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_connections WHERE id=$1 AND credential_id=$2 AND certificate_hash=$3 AND disconnected_at IS NULL AND last_seen_at>now()-interval '30 seconds')`, connection, out.ID, agentpki.Hash(cert.Raw)).Scan(&exists)
	if e != nil {
		return nil, e
	}
	if !exists {
		return nil, ErrAgentDenied
	}
	return out, tx.Commit()
}
func (s *Store) OpenAgentTaskSession(ctx context.Context, cert *x509.Certificate, authority, connection, session string) (*AgentCredential, error) {
	if !sshTrustIDPattern.MatchString(connection) || !sshTrustIDPattern.MatchString(session) {
		return nil, ErrAgentDenied
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	out, e := agentCredential(ctx, tx, cert, authority)
	if e != nil {
		return nil, e
	}
	result, e := tx.ExecContext(ctx, `UPDATE agent_connections SET task_session_id=$3,task_connected_at=now(),task_disconnected_at=NULL WHERE id=$1 AND credential_id=$2 AND certificate_hash=$4 AND disconnected_at IS NULL AND last_seen_at>now()-interval '30 seconds' AND (task_session_id IS NULL OR task_disconnected_at IS NOT NULL)`, connection, out.ID, session, agentpki.Hash(cert.Raw))
	if e != nil {
		return nil, e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return nil, e
	}
	if n != 1 {
		return nil, ErrAgentConnectionBusy
	}
	return out, tx.Commit()
}
func (s *Store) CloseAgentTaskSession(ctx context.Context, connection, session string) error {
	_, e := s.DB.ExecContext(ctx, `UPDATE agent_connections SET task_disconnected_at=now() WHERE id=$1 AND task_session_id=$2 AND task_disconnected_at IS NULL`, connection, session)
	return e
}

// A transport adapter does not grant execution authority. The existing attempt,
// lease, submitter, project and immutable command snapshot must still agree.
func (s *Store) AgentRequestAllowed(ctx context.Context, request taskrunner.Request, credential *AgentCredential) error {
	c := request.Command
	if request.Validate() != nil || credential == nil || c.ProjectID != credential.ProjectID || c.MachineID != credential.MachineID || (c.Operation != "host.identity" && c.Operation != "host.wait" && c.Operation != "script.run" && c.Operation != "host.cache.inventory") {
		return taskrunner.ErrRejected
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if request.Action == "run" && c.Operation == "host.cache.inventory" && !s.TaskCacheInventoryEnabled {
		return taskrunner.ErrRejected
	}
	var valid bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id JOIN users u ON u.id=x.created_by JOIN projects p ON p.id=x.project_id WHERE a.id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND a.runner='agent' AND x.mode='agent' AND x.project_id=$4 AND p.state NOT IN('delivered','archived') AND (($5='run' AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=x.project_id AND m.role IN('Admin','DE','DL','SRE'))) AND a.state='running' AND a.lease_until>now() AND x.desired_state='running') OR ($5 IN('query','cancel') AND a.state IN('running','unknown'))) AND EXISTS(SELECT 1 FROM jsonb_array_elements(x.snapshot->'flow'->'definition'->'tasks') task WHERE task->>'id'=a.task_id AND task->>'type'=$9 AND (task->>'timeout_ms')::bigint=$10) AND COALESCE((x.parameters->>'agent_wait_ms')::bigint,0)=$11 AND COALESCE(x.parameters->>'cache_cursor','')=$12 AND COALESCE(x.parameters->>'cache_manifest_sha256','')=$13 AND EXISTS(SELECT 1 FROM jsonb_array_elements(x.snapshot->'targets') t WHERE t->>'id'=a.target_id AND t->>'resource_key'=$6 AND t->'agent'->>'host_key'=$7 AND t->'agent'->>'config_hash'=$8))`, c.AttemptID, c.CommandID, c.FencingToken, c.ProjectID, request.Action, c.MachineID, credential.ID, credential.CertificateHash, c.Operation, c.TimeoutMS, c.WaitMS, c.CacheCursor, c.CacheManifestSHA256).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return taskrunner.ErrRejected
	}
	if c.Operation == "script.run" {
		if e = s.agentScriptRequestTx(ctx, tx, request); e != nil {
			return e
		}
	}
	return tx.Commit()
}

// CertificateHash is derived from DER; PEM formatting is never an identity.
func AgentCertificateBinding(c *AgentCredential) taskrunner.Binding {
	if c == nil {
		return taskrunner.Binding{}
	}
	hash := c.CertificateHash
	if hash == "" {
		if cert, e := agentpki.Certificate([]byte(c.CertificatePEM)); e == nil {
			hash = agentpki.Hash(cert.Raw)
		}
	}
	return taskrunner.Binding{HostKey: c.ID, ConfigHash: hash}
}

func (s *Store) AgentRequestExpiry(ctx context.Context, r taskrunner.Request) (time.Time, error) {
	deadline, _ := ctx.Deadline()
	if deadline.IsZero() {
		deadline = time.Now().Add(5 * time.Second)
	}
	if r.Action == "run" {
		var lease time.Time
		e := s.DB.QueryRowContext(ctx, `SELECT lease_until FROM exec_task_attempts WHERE id=$1 AND command_id=$2 AND fencing_token=$3 AND state='running' AND lease_until>now()`, r.Command.AttemptID, r.Command.CommandID, r.Command.FencingToken).Scan(&lease)
		if e != nil {
			return time.Time{}, taskrunner.ErrUnknown
		}
		if r.Command.Operation == "script.run" {
			if until := time.UnixMicro(r.Command.AuthorizationExpiresAt); until.Before(deadline) {
				deadline = until
			}
		}
		if lease.Before(deadline) {
			deadline = lease
		}
	}
	return deadline, nil
}
