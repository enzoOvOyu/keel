package platform

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
	"github.com/enzoOvOyu/keel/internal/precollect"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// Frozen admission is inside the same Execution transaction. It grants no gate.
func (s *Store) PrepareMonitorSSHRequestTx(ctx context.Context, tx *sql.Tx, project, asset, machine, execution string, revision int64, networkOnly bool) error {
	if !s.MonitorEnabled || s.MediaObjects == nil {
		return apiError(409, "MONITOR_SSH_DISABLED", "Agentless snapshots require enabled monitoring and original artifact storage")
	}
	var state string
	if err := tx.QueryRowContext(ctx, `SELECT state FROM project_assets WHERE id=$1 AND project_id=$2 AND machine_id=$3 AND version=$4 FOR SHARE`, asset, project, machine, revision).Scan(&state); err != nil {
		return err
	}
	if !PrecollectAssetState(state) {
		return apiError(409, "MONITOR_INBAND_STOPPED", "Agentless sampling requires an active delivery asset")
	}
	if err := MonitorSSHAgentAbsentTx(ctx, tx, project, machine); err != nil {
		return err
	}
	policy, err := monitorPolicyTx(ctx, tx, project, monitorStage(state))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO monitor_ssh_requests(execution_id,machine_id,project_id,asset_id,asset_version,network_only,policy_id,interval_seconds,payload_bytes_per_second)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, execution, machine, project, asset, revision, networkOnly, policy.ID, policy.Policy.AgentlessSeconds, policy.Policy.SteadyBytesPerSecond)
	return err
}

func MonitorSSHAgentAbsentTx(ctx context.Context, tx *sql.Tx, project, machine string) error {
	var live bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_credentials identity JOIN agent_connections connection ON connection.credential_id=identity.id WHERE identity.project_id=$1 AND identity.machine_id=$2 AND identity.revoked_at IS NULL AND identity.expires_at>clock_timestamp() AND connection.disconnected_at IS NULL AND connection.last_seen_at>clock_timestamp()-interval '30 seconds')`, project, machine).Scan(&live)
	if err != nil {
		return err
	}
	if live {
		return apiError(409, "MONITOR_AGENT_AVAILABLE", "Use the current authenticated Agent telemetry channel")
	}
	return nil
}

func (s *Store) MonitorSSHProofForCommand(ctx context.Context, command taskrunner.TaskCommand) (string, string, error) {
	var id, sha string
	err := s.DB.QueryRowContext(ctx, `SELECT command_id,raw_sha256 FROM monitor_ssh_captures WHERE command_id=$1 AND attempt_id=$2 AND fencing_token=$3 AND project_id=$4 AND machine_id=$5`, command.CommandID, command.AttemptID, command.FencingToken, command.ProjectID, command.MachineID).Scan(&id, &sha)
	return id, sha, err
}

func (s *Store) RecordMonitorSSHCapture(ctx context.Context, observation taskrunner.Observation, raw []byte) (string, string, error) {
	command := observation.Command
	if !s.MonitorEnabled || s.MediaObjects == nil || command.Operation != "host.monitor" || observation.State != "success" || observation.Validate(command) != nil {
		return "", "", apiError(409, "MONITOR_SSH_CAPTURE_INVALID", "Original stopped SSH snapshot required")
	}
	snapshot, err := monitor.DecodeHostSnapshot(raw, time.Now().UTC())
	if err != nil {
		return "", "", err
	}
	sha := precollect.Hash(raw)
	if observation.Output["monitor_sha256"] != sha || snapshot.NetworkOnly != command.MonitorNetworkOnly || snapshot.StartedAt.Before(observation.StartedAt) || snapshot.CollectedAt.After(observation.FinishedAt) || snapshot.CollectedAt.After(time.UnixMicro(command.AuthorizationExpiresAt)) {
		return "", "", apiError(409, "MONITOR_SSH_CAPTURE_MISMATCH", "Snapshot differs from original bounded observation")
	}
	var execution string
	if err = s.DB.QueryRowContext(ctx, `SELECT execution_id FROM exec_task_attempts WHERE id=$1 AND command_id=$2 AND fencing_token=$3`, command.AttemptID, command.CommandID, command.FencingToken).Scan(&execution); err != nil {
		return "", "", err
	}
	key := "monitor/ssh/" + command.ProjectID + "/" + command.MachineID + "/" + command.CommandID + "/" + sha + ".json"
	version, err := s.MediaObjects.Put(ctx, key, bytes.NewReader(raw), int64(len(raw)))
	if err != nil || version == "" || version == "null" {
		return "", "", apiError(409, "MONITOR_RAW_UNAVAILABLE", "Versioned snapshot storage unavailable")
	}
	if err = s.verifyPrecollectRaw(ctx, key, version, sha, int64(len(raw))); err != nil {
		return "", "", err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	var old string
	err = tx.QueryRowContext(ctx, `SELECT raw_sha256 FROM monitor_ssh_captures WHERE command_id=$1`, command.CommandID).Scan(&old)
	if err == nil {
		if old != sha {
			return "", "", apiError(409, "MONITOR_CAPTURE_CONFLICT", "Original snapshot is immutable")
		}
		return command.CommandID, sha, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", "", err
	}
	var owned bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts attempt WHERE attempt.id=$1 AND attempt.command_id=$2 AND attempt.fencing_token=$3 AND (attempt.state='unknown' OR attempt.state='running' AND attempt.lease_until>clock_timestamp() AND (SELECT count(*) FROM exec_resource_leases lease WHERE lease.attempt_id=attempt.id AND lease.fencing_token=attempt.fencing_token AND lease.state='held' AND lease.expires_at>clock_timestamp() AND lease.resource_id IN('device:machine:'||$4,'ssh:machine:'||$4))=2))`, command.AttemptID, command.CommandID, command.FencingToken, command.MachineID).Scan(&owned)
	if err != nil {
		return "", "", err
	}
	if !owned {
		return "", "", apiError(409, "MONITOR_CAPTURE_FENCE_LOST", "Original attempt no longer owns capture admission")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO monitor_ssh_captures(command_id,attempt_id,execution_id,machine_id,project_id,fencing_token,command,raw_key,raw_version,raw_sha256,raw_size,snapshot,collected_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, command.CommandID, command.AttemptID, execution, command.MachineID, command.ProjectID, command.FencingToken, networkJSON(command), key, version, sha, len(raw), raw, snapshot.CollectedAt)
	if err != nil {
		return "", "", err
	}
	if snapshot.Truncated {
		_, err = tx.ExecContext(ctx, `INSERT INTO monitor_gaps(project_id,machine_id,frame_id,source,method,started_at,ended_at,reason)VALUES($1,$2,$3,'host','ssh',$4,$5,'snapshot_sample_limit')`, command.ProjectID, command.MachineID, command.CommandID, snapshot.StartedAt, snapshot.CollectedAt)
		if err != nil {
			return "", "", err
		}
	}
	return command.CommandID, sha, tx.Commit()
}
