package platform

import (
	"context"
	"time"
)

// AgentTaskTarget is public task eligibility metadata, not enrollment material
// or evidence of host health. Only an exact, current provisioned asset is listed.
type AgentTaskTarget struct {
	MachineID           string     `json:"machine_id"`
	AssetID             string     `json:"asset_id"`
	Hostname            string     `json:"hostname"`
	SerialNumber        string     `json:"serial_number"`
	AssetVersion        int64      `json:"asset_version"`
	CredentialID        string     `json:"credential_id"`
	CredentialExpiresAt time.Time  `json:"credential_expires_at"`
	LastSeenAt          *time.Time `json:"last_seen_at,omitempty"`
	Connected           bool       `json:"connected"`
}

type AgentTaskTargetPage struct {
	Page[AgentTaskTarget]
	Enabled bool `json:"enabled"`
}

func (s *Store) ListAgentTaskTargets(ctx context.Context, actor *AuthSession, project string, p Pagination) (AgentTaskTargetPage, error) {
	out := AgentTaskTargetPage{Page: NewPage[AgentTaskTarget](p), Enabled: s.AgentTasksEnabled}
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, actor)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, actor, project); e != nil {
		return out, e
	}
	if !out.Enabled {
		return out, nil
	}
	// Count and rows use one repeatable-read snapshot, including now(). The
	// latest connection may be offline; never infer liveness from registration.
	const eligible = ` FROM agent_credentials c
		JOIN project_assets a ON a.project_id=c.project_id AND a.machine_id=c.machine_id AND a.version=c.asset_version
		JOIN machines m ON m.id=a.machine_id AND m.serial_number=c.serial_number
		JOIN projects p ON p.id=a.project_id
		WHERE c.project_id=$1 AND c.revoked_at IS NULL AND ` + agentCredentialExpirySQL + `>now()
		AND a.state IN('provisioned','trust_established','collected','lldp','mapped','configured','netverified','tested') AND p.state NOT IN('delivered','archived')`
	if e = tx.QueryRowContext(ctx, `SELECT count(*)`+eligible, project).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT a.machine_id,a.id,a.hostname,m.serial_number,a.version,c.id,`+agentCredentialExpirySQL+`,
		(SELECT last_seen_at FROM agent_connections WHERE credential_id=c.id ORDER BY last_seen_at DESC,id DESC LIMIT 1),
		EXISTS(SELECT 1 FROM agent_connections WHERE credential_id=c.id AND disconnected_at IS NULL
		AND last_seen_at>now()-interval '30 seconds' AND task_session_id IS NOT NULL AND task_disconnected_at IS NULL)`+eligible+`
		ORDER BY a.hostname,a.machine_id LIMIT $2 OFFSET $3`, project, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var item AgentTaskTarget
		if e = rows.Scan(&item.MachineID, &item.AssetID, &item.Hostname, &item.SerialNumber, &item.AssetVersion, &item.CredentialID, &item.CredentialExpiresAt, &item.LastSeenAt, &item.Connected); e != nil {
			return out, e
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}
