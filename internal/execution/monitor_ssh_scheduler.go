package execution

import (
	"context"
	"database/sql"
	"errors"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// Periodic fallback is D1/D2 only. A known stopped failure waits for the next
// policy interval; an unknown command is never retried by this scheduler.
func (s *Service) TickMonitorAgentless(ctx context.Context, limit int) (int, error) {
	if s.MediaStore == nil || !s.MediaStore.MonitorEnabled || s.MediaStore.MediaObjects == nil || s.SSHRunner == nil {
		return 0, nil
	}
	if limit < 1 || limit > 32 {
		return 0, invalid("One to 32 Agentless scheduler candidates required")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT asset.project_id,asset.id,asset.machine_id,policy.id,policy.created_by FROM project_assets asset JOIN projects project ON project.id=asset.project_id JOIN machines machine ON machine.id=asset.machine_id JOIN LATERAL(SELECT id,created_by,policy FROM monitor_policies WHERE project_id=asset.project_id AND stage=CASE WHEN asset.state IN('collected','lldp','mapped') THEN 'M2' WHEN asset.state='configured' THEN 'M3' WHEN asset.state IN('netverified','tested') THEN 'M4' ELSE 'M1' END ORDER BY version DESC LIMIT 1) policy ON true WHERE project.state NOT IN('archived','delivered') AND asset.state IN('provisioned','trust_established','collected','lldp','mapped','configured','netverified','tested') AND machine.ssh_trust='established' AND machine.ssh_trust_source='ssh' AND NOT EXISTS(SELECT 1 FROM agent_credentials identity JOIN agent_connections connection ON connection.credential_id=identity.id WHERE identity.project_id=asset.project_id AND identity.machine_id=asset.machine_id AND identity.revoked_at IS NULL AND identity.expires_at>clock_timestamp() AND connection.disconnected_at IS NULL AND connection.last_seen_at>clock_timestamp()-interval '30 seconds') AND NOT EXISTS(SELECT 1 FROM monitor_ssh_requests request JOIN exec_executions execution ON execution.id=request.execution_id WHERE request.project_id=asset.project_id AND request.asset_id=asset.id AND (request.created_at>clock_timestamp()-make_interval(secs=>coalesce((policy.policy->>'agentless_seconds')::integer,600)) OR execution.state NOT IN('success','failed','killed','warning') OR EXISTS(SELECT 1 FROM exec_task_attempts attempt WHERE attempt.execution_id=execution.id AND attempt.state='unknown'))) AND NOT EXISTS(SELECT 1 FROM exec_resource_leases lease WHERE lease.resource_id IN('device:machine:'||asset.machine_id,'ssh:machine:'||asset.machine_id) AND lease.state IN('held','reconciling')) ORDER BY (SELECT max(request.created_at) FROM monitor_ssh_requests request WHERE request.project_id=asset.project_id AND request.asset_id=asset.id) NULLS FIRST,asset.project_id,asset.id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	type target struct{ project, asset, machine, policy, owner string }
	targets := []target{}
	for rows.Next() {
		var item target
		if err = rows.Scan(&item.project, &item.asset, &item.machine, &item.policy, &item.owner); err != nil {
			rows.Close()
			return 0, err
		}
		targets = append(targets, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	created := 0
	for _, target := range targets {
		actor := &Actor{User: platform.UserSummary{ID: target.owner}}
		input := MonitorSSHInput{AssetID: target.asset, NetworkOnly: true, Reason: "Periodic degraded D1/D2 snapshot"}
		_, err = s.createSSH(ctx, actor, target.project, SSHCreateInput{Monitor: &input, monitorDelegation: &monitorSSHDelegation{PolicyID: target.policy}, MachineIDs: []string{target.machine}, Operation: "host.monitor", TimeoutMS: 30000}, "", 0, platform.RandomID(), "monitor-agentless-scheduler")
		if err != nil {
			var business *platform.Error
			if errors.Is(err, platform.ErrForbidden) || errors.Is(err, platform.ErrUnauthenticated) || errors.Is(err, platform.ErrNotFound) || errors.As(err, &business) && business.Status < 500 {
				continue
			}
			return created, err
		}
		created++
	}
	return created, nil
}

// Explicit saved monitor policies delegate low-frequency D1/D2 polling to their
// current author. No browser session is manufactured for background work.
type monitorSSHDelegation struct{ PolicyID string }

func monitorSSHDelegateTx(ctx context.Context, tx *sql.Tx, actor *Actor, project string, input SSHCreateInput) error {
	if actor == nil || input.Monitor == nil || !input.Monitor.NetworkOnly || input.Operation != "host.monitor" || len(input.MachineIDs) != 1 || input.monitorDelegation == nil {
		return platform.ErrForbidden
	}
	var enabled bool
	err := tx.QueryRowContext(ctx, `SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE`, actor.User.ID).Scan(&enabled, &actor.User.IsAdmin)
	if err != nil {
		return err
	}
	if !enabled {
		return platform.ErrForbidden
	}
	var valid bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM monitor_policies policy JOIN project_assets asset ON asset.project_id=policy.project_id JOIN projects project ON project.id=asset.project_id WHERE policy.id=$1 AND policy.created_by=$2 AND policy.project_id=$3 AND asset.id=$4 AND asset.machine_id=$5 AND project.state NOT IN('archived','delivered') AND asset.state IN('provisioned','trust_established','collected','lldp','mapped','configured','netverified','tested') AND policy.stage=CASE WHEN asset.state IN('collected','lldp','mapped') THEN 'M2' WHEN asset.state='configured' THEN 'M3' WHEN asset.state IN('netverified','tested') THEN 'M4' ELSE 'M1' END AND NOT EXISTS(SELECT 1 FROM monitor_policies newer WHERE newer.project_id=policy.project_id AND newer.stage=policy.stage AND newer.version>policy.version) AND ($6 OR EXISTS(SELECT 1 FROM memberships membership WHERE membership.project_id=policy.project_id AND membership.user_id=policy.created_by AND membership.role IN('Admin','DE','DL','NE','SRE'))))`, input.monitorDelegation.PolicyID, actor.User.ID, project, input.Monitor.AssetID, input.MachineIDs[0], actor.User.IsAdmin).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return platform.ErrForbidden
	}
	return nil
}
