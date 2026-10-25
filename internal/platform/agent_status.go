package platform

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type AgentRenewalSummary struct {
	PredecessorHash string    `json:"predecessor_hash"`
	CertificateHash string    `json:"certificate_hash"`
	IssuedAt        time.Time `json:"issued_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}
type AgentConnectionSummary struct {
	ID                 string     `json:"id"`
	CertificateHash    string     `json:"certificate_hash"`
	ConnectedAt        time.Time  `json:"connected_at"`
	LastSeenAt         time.Time  `json:"last_seen_at"`
	DisconnectedAt     *time.Time `json:"disconnected_at,omitempty"`
	HeartbeatConnected bool       `json:"heartbeat_connected"`
	TasksConnected     bool       `json:"tasks_connected"`
}
type AgentCredentialSummary struct {
	ID                      string                   `json:"id"`
	ProjectID               string                   `json:"project_id"`
	MachineID               string                   `json:"machine_id"`
	Serial                  string                   `json:"serial_number"`
	AssetVersion            int64                    `json:"asset_version"`
	AgentVersion            string                   `json:"agent_version"`
	OriginalCertificateHash string                   `json:"original_certificate_hash"`
	InitialExpiresAt        time.Time                `json:"initial_expires_at"`
	EffectiveExpiresAt      time.Time                `json:"effective_expires_at"`
	CreatedAt               time.Time                `json:"created_at"`
	RevokedAt               *time.Time               `json:"revoked_at,omitempty"`
	StateVersion            int64                    `json:"state_version"`
	State                   string                   `json:"state"`
	BindingCurrent          bool                     `json:"binding_current"`
	RenewalsTotal           int                      `json:"renewals_total"`
	RecentRenewals          []AgentRenewalSummary    `json:"recent_renewals"`
	RecentConnections       []AgentConnectionSummary `json:"recent_connections"`
}

// AgentCredentialStatus is an owner-side recovery read, never an enrollment or
// remote-execution capability. No token, key, PEM or connection session handle
// is returned. Scope and evidence are read in one repeatable-read transaction.
func (s *Store) AgentCredentialStatus(ctx context.Context, project, id string) (*AgentCredentialSummary, error) {
	if !sshTrustIDPattern.MatchString(project) || !sshTrustIDPattern.MatchString(id) {
		return nil, ErrAgentDenied
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	out := &AgentCredentialSummary{RecentRenewals: []AgentRenewalSummary{}, RecentConnections: []AgentConnectionSummary{}}
	var unexpired bool
	e = tx.QueryRowContext(ctx, `SELECT c.id,c.project_id,c.machine_id,c.serial_number,c.asset_version,c.agent_version,c.certificate_hash,c.expires_at,`+agentCredentialExpirySQL+`,c.created_at,c.revoked_at,c.state_version,`+agentCredentialExpirySQL+`>now(),EXISTS(SELECT 1 FROM project_assets a JOIN machines m ON m.id=a.machine_id JOIN projects p ON p.id=a.project_id WHERE a.project_id=c.project_id AND a.machine_id=c.machine_id AND a.state='provisioned' AND a.version=c.asset_version AND m.serial_number=c.serial_number AND p.state NOT IN('delivered','archived')) FROM agent_credentials c WHERE c.project_id=$1 AND c.id=$2`, project, id).Scan(&out.ID, &out.ProjectID, &out.MachineID, &out.Serial, &out.AssetVersion, &out.AgentVersion, &out.OriginalCertificateHash, &out.InitialExpiresAt, &out.EffectiveExpiresAt, &out.CreatedAt, &out.RevokedAt, &out.StateVersion, &unexpired, &out.BindingCurrent)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrAgentDenied
	}
	if e != nil {
		return nil, e
	}
	switch {
	case out.RevokedAt != nil:
		out.State = "revoked"
	case !out.BindingCurrent:
		out.State = "binding_invalid"
	case !unexpired:
		out.State = "expired"
	default:
		out.State = "active"
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_certificate_renewals WHERE credential_id=$1`, id).Scan(&out.RenewalsTotal); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT predecessor_hash,certificate_hash,issued_at,expires_at FROM agent_certificate_renewals WHERE credential_id=$1 ORDER BY issued_at DESC,certificate_hash LIMIT 10`, id)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var r AgentRenewalSummary
		if e = rows.Scan(&r.PredecessorHash, &r.CertificateHash, &r.IssuedAt, &r.ExpiresAt); e != nil {
			rows.Close()
			return nil, e
		}
		out.RecentRenewals = append(out.RecentRenewals, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT a.id,a.certificate_hash,a.connected_at,a.last_seen_at,a.disconnected_at,a.disconnected_at IS NULL AND a.last_seen_at>now()-interval '30 seconds' AND ((a.certificate_hash=c.certificate_hash AND c.expires_at>now()) OR EXISTS(SELECT 1 FROM agent_certificate_renewals r WHERE r.credential_id=c.id AND r.certificate_hash=a.certificate_hash AND r.expires_at>now())),a.task_session_id IS NOT NULL AND a.task_disconnected_at IS NULL FROM agent_connections a JOIN agent_credentials c ON c.id=a.credential_id WHERE a.credential_id=$1 ORDER BY a.connected_at DESC,a.id DESC LIMIT 5`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var c AgentConnectionSummary
		if e = rows.Scan(&c.ID, &c.CertificateHash, &c.ConnectedAt, &c.LastSeenAt, &c.DisconnectedAt, &c.HeartbeatConnected, &c.TasksConnected); e != nil {
			return nil, e
		}
		c.HeartbeatConnected = c.HeartbeatConnected && out.State == "active"
		c.TasksConnected = c.TasksConnected && c.HeartbeatConnected
		out.RecentConnections = append(out.RecentConnections, c)
	}
	return out, rows.Err()
}
