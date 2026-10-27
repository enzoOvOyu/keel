package platform

import (
	"context"
	"database/sql"
)

// Hash only public trust references; secret material never enters repair plans.
func repairHostIdentityTx(ctx context.Context, tx *sql.Tx, machine, project string) (string, error) {
	var raw []byte
	if e := tx.QueryRowContext(ctx, `SELECT jsonb_build_object('trust',machine.ssh_trust,'source',machine.ssh_trust_source,'ssh',(SELECT jsonb_build_object('id',request.id,'state',request.state,'host_key',request.host_key,'config_hash',request.config_hash,'credential_ref',request.credential_ref,'credential_version',request.credential_version,'expires_at',request.credential_expires_at) FROM ssh_trust_requests request WHERE request.machine_id=machine.id AND request.project_id=$2 ORDER BY request.created_at DESC,request.id DESC LIMIT 1),'agent',(SELECT jsonb_agg(jsonb_build_object('id',identity.id,'expires_at',identity.expires_at) ORDER BY identity.id) FROM agent_credentials identity WHERE identity.machine_id=machine.id AND identity.project_id=$2 AND identity.revoked_at IS NULL AND identity.expires_at>clock_timestamp())) FROM machines machine WHERE machine.id=$1`, machine, project).Scan(&raw); e != nil {
		return "", e
	}
	return digest(string(raw)), nil
}
