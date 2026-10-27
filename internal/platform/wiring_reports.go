package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

// ApplyWiringCaptureTx publishes immutable observations while the shared
// Execution settles its original receipt. This does not confirm G2 or Mapping.
func (s *Store) ApplyWiringCaptureTx(ctx context.Context, tx *sql.Tx, attempt, command string, fence int64) error {
	if !s.WiringEnabled {
		return apiError(409, "WIRING_DISABLED", "Wiring report consumer disabled")
	}
	var c WiringCommand
	var commandRaw, evaluationRaw []byte
	var sha, owner string
	var stopped time.Time
	e := tx.QueryRowContext(ctx, `SELECT c.command,p.raw_sha256,p.evaluation,x.created_by,p.stopped_at FROM validation_wiring_commands c JOIN validation_wiring_capture_records p ON p.command_id=c.command_id JOIN exec_executions x ON x.id=c.execution_id WHERE c.attempt_id=$1 AND c.command_id=$2 AND c.fencing_token=$3`, attempt, command, fence).Scan(&commandRaw, &sha, &evaluationRaw, &owner, &stopped)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(commandRaw, &c); e != nil {
		return e
	}
	var result wiring.Result
	if e = json.Unmarshal(evaluationRaw, &result); e != nil {
		return e
	}
	if e = lockKey(ctx, tx, "wiring-reports:"+c.BatchID+":"+c.AssetID); e != nil {
		return e
	}
	duration, policy := time.Duration(c.GateTTLSeconds)*time.Second, c.PolicyHash
	if duration < time.Second || duration > 24*time.Hour || len(policy) != 64 {
		return wiring.ErrCaptureRejected
	}
	until := stopped.Add(duration)
	var topologyDeadline time.Time
	if e = tx.QueryRowContext(ctx, `SELECT t.valid_until FROM validation_wiring_batch_targets b JOIN pci_topology_snapshots t ON t.id=b.topology_id WHERE b.batch_id=$1 AND b.asset_id=$2`, c.BatchID, c.AssetID).Scan(&topologyDeadline); e != nil {
		return e
	}
	if topologyDeadline.Before(until) {
		until = topologyDeadline
	}
	parent := c.ParentID
	for step := 2; step <= 3; step++ {
		verdict := result.Step2
		if step == 3 {
			if result.Step2 != "PASS" {
				break
			}
			verdict = result.Step3
		}
		var existing string
		e = tx.QueryRowContext(ctx, `SELECT id FROM validation_wiring_step_reports WHERE detail->>'command_id'=$1 AND step=$2`, command, step).Scan(&existing)
		if e == nil {
			parent = existing
			continue
		}
		if e != sql.ErrNoRows {
			return e
		}
		var version int64
		if e = tx.QueryRowContext(ctx, `SELECT coalesce(max(version),0)+1 FROM validation_wiring_step_reports WHERE batch_id=$1 AND asset_id=$2 AND step=$3`, c.BatchID, c.AssetID, step).Scan(&version); e != nil {
			return e
		}
		detail := WiringStepDetail{Version: "keel.wiring-step/v1", Step: step, Verdict: verdict, TopologyID: "", CommandID: command, RawSHA256: sha, HostOrigin: c.HostOrigin, SwitchOrigin: c.Binding.Origin, Findings: result.Findings}
		id := RandomID()
		_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_step_reports(id,batch_id,asset_id,step,version,parent_id,source_hash,verdict,detail,created_by,created_at,valid_until,policy_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, id, c.BatchID, c.AssetID, step, version, parent, sha, verdict, networkJSON(detail), owner, time.Now().UTC(), until, policy)
		if e != nil {
			return e
		}
		parent = id
	}
	if !until.After(time.Now()) {
		return nil
	}
	return recordWiringIssuesTx(ctx, tx, &c, result, owner, stopped, sha)
}

func wiringReportPolicyTx(ctx context.Context, tx *sql.Tx, project string) (time.Duration, string, error) {
	values, e := catalogDefaultsTx(ctx, tx, project, "gate.evidence_ttl")
	if e != nil {
		return 0, "", e
	}
	duration, e := time.ParseDuration(values["gate.evidence_ttl"])
	if e != nil || duration < time.Second || duration > 24*time.Hour {
		return 0, "", apiError(409, "WIRING_POLICY_INVALID", "Gate evidence TTL must be positive and at most 24 hours")
	}
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT coalesce(jsonb_agg(jsonb_build_object('id',p.id,'updated_at',p.updated_at,'value',p.default_value) ORDER BY p.id),'[]'::jsonb) FROM param_keys p WHERE p.scope_key IN('global',$1) AND p.key='gate.evidence_ttl' AND NOT EXISTS(SELECT 1 FROM param_keys override WHERE override.key=p.key AND override.scope_key=$1 AND p.scope_key='global')`, "project:"+project).Scan(&raw)
	return duration, digest(string(raw)), e
}
