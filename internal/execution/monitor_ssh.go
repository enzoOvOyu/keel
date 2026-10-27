package execution

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	"github.com/enzoOvOyu/keel/internal/monitor"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/precollect"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type MonitorSSHInput struct {
	AssetID     string `json:"asset_id"`
	NetworkOnly bool   `json:"network_only"`
	Reason      string `json:"reason"`
}

func (s *Service) CreateMonitorSSHSnapshot(ctx context.Context, actor *Actor, project string, in MonitorSSHInput, key, trace string) (*Execution, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !opaqueID.MatchString(in.AssetID) || len(in.Reason) < 3 || len(in.Reason) > 1000 {
		return nil, invalid("One exact project asset and a snapshot reason required")
	}
	if s.MediaStore == nil || !s.MediaStore.MonitorEnabled || s.MediaStore.MediaObjects == nil || s.SSHRunner == nil {
		return nil, failure(409, "MONITOR_SSH_DISABLED", "Agentless snapshot adapters or retained storage unavailable")
	}
	asset, err := s.MediaStore.GetProjectAsset(ctx, actor, project, in.AssetID)
	if err != nil {
		return nil, err
	}
	return s.createSSH(ctx, actor, project, SSHCreateInput{Monitor: &in, MachineIDs: []string{asset.MachineID}, Operation: "host.monitor", TimeoutMS: 30000}, "", 0, key, trace)
}

func (s *Service) monitorSSHFrozenTx(ctx context.Context, tx *sql.Tx, target Target) error {
	if s.MediaStore == nil || !s.MediaStore.MonitorEnabled || !target.Monitor {
		return taskrunner.ErrRejected
	}
	current, err := s.sshTargetForPrecollectTx(ctx, tx, target.ProjectID, target.ResourceKey, true)
	if err != nil {
		return err
	}
	if target.SSH == nil || current.SSH == nil || *target.SSH != *current.SSH || target.Revision != current.Revision || target.ModelVersion != current.ModelVersion {
		return conflict("Frozen monitor SSH/asset identity changed")
	}
	return platform.MonitorSSHAgentAbsentTx(ctx, tx, target.ProjectID, target.ResourceKey)
}

func (s *Service) recordMonitorSSHObservation(ctx context.Context, observation taskrunner.Observation) error {
	if s.MediaStore == nil || s.SSHRunner == nil || observation.Validate(observation.Command) != nil {
		return taskrunner.ErrRejected
	}
	output := map[string]any{"operation": "host.monitor", "mode": "ssh", "transport": "ssh", "source": "host", "hardware_evidence": false, "stopped": true, "observation": observation.Output}
	if observation.State == "success" {
		id, sha, err := s.MediaStore.MonitorSSHProofForCommand(ctx, observation.Command)
		if errors.Is(err, sql.ErrNoRows) {
			var meta struct {
				Size   int64  `json:"monitor_size"`
				SHA256 string `json:"monitor_sha256"`
			}
			if json.Unmarshal(jsonBytes(observation.Output), &meta) != nil || meta.Size < 1 || meta.Size > monitor.MaxHostSnapshotBytes {
				return taskrunner.ErrRejected
			}
			raw := make([]byte, 0, meta.Size)
			for offset := int64(0); offset < meta.Size; offset += precollect.ChunkBytes {
				chunk, readErr := s.SSHRunner.Do(ctx, taskrunner.Request{Action: "read", Command: observation.Command, ChunkOffset: offset, ChunkSHA256: meta.SHA256})
				if readErr != nil {
					return readErr
				}
				var part struct {
					Offset      int64  `json:"monitor_chunk_offset"`
					Size        int64  `json:"monitor_size"`
					SHA256      string `json:"monitor_sha256"`
					ChunkSHA256 string `json:"chunk_sha256"`
					Chunk       string `json:"chunk"`
				}
				if chunk.Command != observation.Command || chunk.State != "accepted" || json.Unmarshal(jsonBytes(chunk.Output), &part) != nil {
					return taskrunner.ErrRejected
				}
				body, decodeErr := base64.StdEncoding.Strict().DecodeString(part.Chunk)
				if decodeErr != nil || int64(len(body)) != min(int64(precollect.ChunkBytes), meta.Size-offset) || part.Offset != offset || part.Size != meta.Size || part.SHA256 != meta.SHA256 || precollect.Hash(body) != part.ChunkSHA256 {
					return taskrunner.ErrRejected
				}
				raw = append(raw, body...)
			}
			if precollect.Hash(raw) != meta.SHA256 {
				return taskrunner.ErrRejected
			}
			id, sha, err = s.MediaStore.RecordMonitorSSHCapture(ctx, observation, raw)
		}
		if err != nil {
			return err
		}
		output["monitor_capture_id"] = id
		output["evidence_hash"] = sha
	}
	return s.RecordReceipt(ctx, Receipt{CommandID: observation.Command.CommandID, AttemptID: observation.Command.AttemptID, FencingToken: observation.Command.FencingToken, Result: observation.State, StartedAt: observation.StartedAt, FinishedAt: observation.FinishedAt, Output: output})
}

func (s *Service) monitorSSHReceiptAllowedTx(ctx context.Context, tx *sql.Tx, receipt Receipt) error {
	var domain bool
	err := tx.QueryRowContext(ctx, `SELECT coalesce(execution.parameters->>'monitor_kind'='snapshot',false) FROM exec_executions execution JOIN exec_task_attempts attempt ON attempt.execution_id=execution.id WHERE attempt.id=$1`, receipt.AttemptID).Scan(&domain)
	if err != nil || !domain {
		return err
	}
	if receipt.Output["stopped"] != true {
		return conflict("Monitor receipt lacks stopped proof")
	}
	if receipt.Result != "success" {
		return nil
	}
	var valid bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM monitor_ssh_captures WHERE command_id=$1 AND attempt_id=$2 AND fencing_token=$3 AND raw_sha256=$4 AND command_id=$5)`, receipt.CommandID, receipt.AttemptID, receipt.FencingToken, receipt.Output["evidence_hash"], receipt.Output["monitor_capture_id"]).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return conflict("Monitor success requires its original retained capture")
	}
	return nil
}

func (s *Service) monitorSSHHeartbeatTx(ctx context.Context, tx *sql.Tx, claim *Claim) bool {
	if !claim.Target.Monitor {
		return false
	}
	if s.monitorSSHFrozenTx(ctx, tx, claim.Target) != nil {
		return true
	}
	var allowed bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_executions execution JOIN users actor ON actor.id=execution.created_by JOIN projects project ON project.id=execution.project_id WHERE execution.id=$1 AND actor.enabled AND project.state NOT IN('archived','delivered') AND (actor.is_admin OR EXISTS(SELECT 1 FROM memberships membership WHERE membership.project_id=execution.project_id AND membership.user_id=actor.id AND membership.role IN('Admin','DE','DL','NE','SRE'))))`, claim.Attempt.ExecutionID).Scan(&allowed)
	return err != nil || !allowed
}
