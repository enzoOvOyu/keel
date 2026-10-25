package execution

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func lockSSHTrustAuthorities(ctx context.Context, tx *sql.Tx, request string) error {
	// Share locks serialize the final authorization/projection with account,
	// membership and project revocation. Stable user order avoids crossed grants.
	rows, e := tx.QueryContext(ctx, `SELECT u.id FROM users u WHERE u.id IN(SELECT created_by FROM ssh_trust_requests WHERE id=$1 UNION SELECT approved_by FROM ssh_trust_requests WHERE id=$1 UNION SELECT x.created_by FROM exec_executions x JOIN ssh_trust_requests r ON r.execution_id=x.id WHERE r.id=$1) ORDER BY u.id FOR SHARE`, request)
	if e != nil {
		return e
	}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	var project string
	if e = tx.QueryRowContext(ctx, `SELECT p.id FROM projects p JOIN ssh_trust_requests r ON r.project_id=p.id WHERE r.id=$1 FOR SHARE OF p`, request).Scan(&project); e != nil {
		return e
	}
	rows, e = tx.QueryContext(ctx, `SELECT m.user_id FROM memberships m WHERE m.project_id=$2 AND m.user_id IN(SELECT created_by FROM ssh_trust_requests WHERE id=$1 UNION SELECT approved_by FROM ssh_trust_requests WHERE id=$1 UNION SELECT x.created_by FROM exec_executions x JOIN ssh_trust_requests r ON r.execution_id=x.id WHERE r.id=$1) ORDER BY m.user_id FOR SHARE`, request, project)
	if e != nil {
		return e
	}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return e
		}
	}
	e = rows.Err()
	rows.Close()
	return e
}

// A trust request only admits a pinned read-only first connection. Installing
// the platform key/helper is an operator/PXE prerequisite, never a shell escape.
func (s *Service) sshTrustTargetTx(ctx context.Context, tx *sql.Tx, project, request, requiredState string) (Target, error) {
	if s.SSHRunner == nil {
		return Target{}, failure(503, "SSH_DISABLED", "SSH runner is not configured")
	}
	if e := lockSSHTrustAuthorities(ctx, tx, request); e != nil {
		return Target{}, e
	}
	var t Target
	t.ProjectID, t.Mode, t.SSHTrustRequestID = project, "ssh", request
	var expectedTrust, pin, configHash, state, trust, currentPin, ref, version, creator, approver string
	var expires, credentialExpires time.Time
	var assetVersion int64
	var install []byte
	e := tx.QueryRowContext(ctx, `SELECT machine_id,asset_version,expected_trust,host_key,config_hash,credential_ref,credential_version,credential_expires_at,created_by,COALESCE(approved_by,''),expires_at,state,install_key_snapshot FROM ssh_trust_requests WHERE id=$1 AND project_id=$2 FOR SHARE`, request, project).Scan(&t.ResourceKey, &assetVersion, &expectedTrust, &pin, &configHash, &ref, &version, &credentialExpires, &creator, &approver, &expires, &state, &install)
	if errors.Is(e, sql.ErrNoRows) {
		return t, platform.ErrNotFound
	}
	if e != nil {
		return t, e
	}
	t.SSHInstallKey, e = decode[*taskrunner.InstallCredential](install)
	if e != nil {
		return t, e
	}
	if state != requiredState && !(requiredState == "consumed" && state == "unknown") {
		return t, conflict("SSH trust request is not available")
	}
	if !expires.After(time.Now()) || credentialExpires.Before(expires) || approver == "" || approver == creator {
		return t, conflict("SSH trust approval expired or incomplete")
	}
	for i, user := range []string{creator, approver} {
		var allowed bool
		roles := []string{"Admin", "DL", "SRE"}
		if i == 1 {
			roles = []string{"Admin", "DL"}
		}
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN projects p ON p.id=$2 WHERE u.id=$1 AND u.enabled AND p.state NOT IN('archived','delivered') AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=p.id AND m.role=ANY($3))))`, user, project, roles).Scan(&allowed)
		if e != nil {
			return t, e
		}
		if !allowed {
			return t, conflict("SSH trust approval authority changed")
		}
	}
	var assetState, trustSource string
	e = tx.QueryRowContext(ctx, `SELECT a.hostname,a.version,COALESCE(a.model_version_id,''),a.created_at,a.state,m.ssh_trust,m.host_key_fingerprint,m.ssh_trust_source FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.project_id=$1 AND a.machine_id=$2 FOR SHARE OF a,m`, project, t.ResourceKey).Scan(&t.Name, &t.Revision, &t.ModelVersion, &t.CreatedAt, &assetState, &trust, &currentPin, &trustSource)
	if errors.Is(e, sql.ErrNoRows) {
		return t, platform.ErrNotFound
	}
	if e != nil {
		return t, e
	}
	if assetState != "provisioned" || assetVersion != t.Revision || trust != expectedTrust || (trust != "unverified" && trust != "broken" && !(trust == "established" && trustSource != "ssh")) {
		return t, conflict("SSH trust target state or asset version changed")
	}
	b, e := s.SSHRunner.Binding(project, t.ResourceKey)
	if e != nil || b.HostKey != pin || b.ConfigHash != configHash {
		return t, conflict("SSH trust binding changed")
	}
	metadata, ok := s.SSHRunner.(interface {
		CredentialMetadata(string, string) (taskrunner.CredentialMetadata, error)
	})
	if !ok {
		return t, conflict("Versioned temporary SSH credential required")
	}
	m, e := metadata.CredentialMetadata(project, t.ResourceKey)
	if e != nil || m.Ref != ref || m.Version != version || version == "" || m.ExpiresAt == nil || !m.ExpiresAt.Equal(credentialExpires) {
		return t, conflict("SSH credential version or validity changed")
	}
	t.SSH = &b
	if t.SSHInstallKey == nil {
		if err := platform.EnforceSSHDomainBinding(ctx, tx, s.SSHRunner, project, t.ResourceKey); err != nil {
			return t, err
		}
	}
	if t.SSHInstallKey != nil {
		expiry := expires
		t.SSHTrustExpiresAt = &expiry
	}
	if t.SSHInstallKey != nil {
		key, err := platform.ReadSSHInstallCredential(ctx, tx, project, *t.SSHInstallKey, expires)
		if err != nil {
			return t, err
		}
		provider, ok := s.SSHRunner.(interface {
			CheckInstallCredential(string, string, taskrunner.InstallCredential) error
		})
		if !ok || provider.CheckInstallCredential(project, t.ResourceKey, *key) != nil {
			return t, conflict("Approved installation key is unavailable")
		}
		t.SSHInstallKey = key
	}
	return t, nil
}

func (s *Service) sshTrustFrozenTx(ctx context.Context, tx *sql.Tx, t Target) error {

	current, e := s.sshTrustTargetTx(ctx, tx, t.ProjectID, t.SSHTrustRequestID, "consumed")
	if e != nil {
		return e
	}

	var executorAllowed bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ssh_trust_requests r JOIN exec_executions x ON x.id=r.execution_id JOIN users u ON u.id=x.created_by WHERE r.id=$1 AND u.enabled AND(u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=x.project_id AND m.user_id=u.id AND m.role IN('Admin','DL','SRE'))))`, t.SSHTrustRequestID).Scan(&executorAllowed)
	if e != nil {
		return e
	}
	if !executorAllowed {
		return conflict("SSH trust executor authority changed")
	}
	if t.SSH == nil || *t.SSH != *current.SSH || t.Revision != current.Revision || t.ModelVersion != current.ModelVersion || t.ResourceKey != current.ResourceKey || hash(jsonBytes(t.SSHInstallKey)) != hash(jsonBytes(current.SSHInstallKey)) {
		return conflict("Frozen SSH trust target changed")
	}
	return nil
}

// Projection runs in the same transaction as applying terminal proof and
// releasing leases. Unknown transport state never establishes host trust.
func (s *Service) applySSHTrustReceipt(ctx context.Context, tx *sql.Tx, a Attempt, r Receipt) error {
	var raw []byte
	if e := tx.QueryRowContext(ctx, "SELECT snapshot FROM exec_executions WHERE id=$1 AND mode='ssh'", a.ExecutionID).Scan(&raw); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		return e
	}
	snapshot, e := decode[Snapshot](raw)
	if e != nil {
		return e
	}
	var t Target
	for _, candidate := range snapshot.Targets {
		if candidate.ID == a.TargetID {
			t = candidate
			break
		}
	}
	if t.SSHTrustRequestID == "" {
		return nil
	}
	if t.SSH == nil {
		return conflict("SSH trust binding missing")
	}
	if e = lockSSHTrustAuthorities(ctx, tx, t.SSHTrustRequestID); e != nil {
		return e
	}
	var state, creator, approver, action, expectedTrust, pin, binding string
	var expires, credentialExpires, created time.Time
	var assetVersion int64
	e = tx.QueryRowContext(ctx, `SELECT state,created_by,COALESCE(approved_by,''),action,expected_trust,host_key,config_hash,expires_at,credential_expires_at,created_at,asset_version FROM ssh_trust_requests WHERE id=$1 AND execution_id=$2 FOR UPDATE`, t.SSHTrustRequestID, a.ExecutionID).Scan(&state, &creator, &approver, &action, &expectedTrust, &pin, &binding, &expires, &credentialExpires, &created, &assetVersion)
	if e != nil {
		return e
	}
	if state == "succeeded" || state == "failed" {
		return nil
	}
	valid := state == "consumed" || state == "unknown"
	valid = valid && r.Result == "success" && r.StartedAt.Before(expires) && !r.StartedAt.Before(created) && !r.FinishedAt.After(expires) && !r.FinishedAt.After(credentialExpires) && pin == t.SSH.HostKey && binding == t.SSH.ConfigHash
	operation := "trust.verify"
	if t.SSHInstallKey != nil {
		operation = "trust.install"
	}
	valid = valid && r.Output["mode"] == "ssh" && r.Output["source"] == "host" && r.Output["operation"] == operation
	observation, ok := r.Output["observation"].(map[string]any)
	if !ok {
		valid = false
	} else if t.SSHInstallKey != nil {
		valid = valid && observation["key_installed"] == true && observation["verified_credential_id"] == t.SSHInstallKey.ID && observation["public_key_verified"] == true
		_, err := platform.ReadSSHInstallCredential(ctx, tx, t.ProjectID, *t.SSHInstallKey, expires)
		valid = valid && err == nil
	} else {
		for _, key := range []string{"hostname", "os", "arch"} {
			value, ok := observation[key].(string)
			valid = valid && ok && value != ""
		}
	}
	var authority bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users c JOIN users a ON a.id=$2 JOIN projects p ON p.id=$3 WHERE c.id=$1 AND c.enabled AND a.enabled AND c.id<>a.id AND p.state NOT IN('archived','delivered') AND(c.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=c.id AND m.project_id=p.id AND m.role IN('Admin','DL','SRE'))) AND(a.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=a.id AND m.project_id=p.id AND m.role IN('Admin','DL'))))`, creator, approver, t.ProjectID).Scan(&authority)
	if e != nil {
		return e
	}
	valid = valid && authority
	var executorAllowed bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_executions x JOIN users u ON u.id=x.created_by WHERE x.id=$1 AND u.enabled AND(u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=x.project_id AND m.user_id=u.id AND m.role IN('Admin','DL','SRE'))))`, a.ExecutionID).Scan(&executorAllowed); e != nil {
		return e
	}
	valid = valid && executorAllowed
	if s.SSHRunner == nil {
		valid = false
	} else {
		current, err := s.SSHRunner.Binding(t.ProjectID, t.ResourceKey)
		valid = valid && err == nil && current == *t.SSH
	}
	if valid && t.SSHInstallKey == nil {
		valid = platform.EnforceSSHDomainBinding(ctx, tx, s.SSHRunner, t.ProjectID, t.ResourceKey) == nil
	}
	if valid {
		result, err := tx.ExecContext(ctx, `UPDATE machines m SET ssh_trust='established',ssh_trust_source='ssh',host_key_fingerprint=$3,trust_updated_at=now(),ssh_domain_credential=CASE WHEN $6::jsonb<>'null'::jsonb THEN $6::jsonb ELSE ssh_domain_credential END WHERE m.id=$1 AND m.ssh_trust=$4 AND (m.ssh_trust<>'established' OR m.ssh_trust_source<>'ssh') AND EXISTS(SELECT 1 FROM project_assets a WHERE a.project_id=$2 AND a.machine_id=m.id AND a.version=$5 AND a.state='provisioned')`, t.ResourceKey, t.ProjectID, pin, expectedTrust, assetVersion, jsonBytes(t.SSHInstallKey))
		if err != nil {
			return err
		}
		n, _ := result.RowsAffected()
		valid = n == 1
	}
	final := "failed"
	if valid {
		final = "succeeded"
	}
	if _, e = tx.ExecContext(ctx, "UPDATE ssh_trust_requests SET state=$2,state_version=state_version+1 WHERE id=$1", t.SSHTrustRequestID, final); e != nil {
		return e
	}
	outcome := "failure"
	if valid {
		outcome = "success"
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO trust_logs(id,machine_id,project_id,action,outcome,fingerprint,detail,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,now())`, platform.RandomID(), t.ResourceKey, t.ProjectID, action, outcome, pin, jsonBytes(map[string]any{"source": "ssh", "request_id": t.SSHTrustRequestID, "execution_id": a.ExecutionID, "command_id": a.CommandID, "receipt_fence": a.FencingToken, "key_installation": t.SSHInstallKey != nil, "installed_key": t.SSHInstallKey}), creator); e != nil {
		return e
	}
	return audit(ctx, tx, creator, t.ProjectID, "ssh.trust.proof", t.SSHTrustRequestID, "worker", map[string]any{"result": final, "execution_id": a.ExecutionID, "command_id": a.CommandID, "source": "ssh", "key_installation": t.SSHInstallKey != nil, "installed_key": t.SSHInstallKey})
}
