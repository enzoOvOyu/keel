package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

var networkPrepareGates = []string{"syntax", "semantic", "consistency", "reachability", "credentials", "system", "ports", "neighbors", "configuration", "services", "batfish", "assertions", "rehearsal"}
var networkVerifyGates = []string{"connectivity", "bgp", "nvue", "hardware", "lossless"}

type NetworkRunResult struct {
	State       string `json:"state"`
	Code        string `json:"code"`
	Source      string `json:"source"`
	RunningHash string `json:"running_hash"`
}

func (s *Store) RunNetworkTarget(ctx context.Context, project, jobID, deviceID, phase, executionID, commandID string, fence int64) (*NetworkRunResult, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	j, e := loadNetworkPush(ctx, tx, project, jobID, false)
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	var d *NetworkPushDevice
	for i := range j.Devices {
		if j.Devices[i].Device.ID == deviceID {
			d = &j.Devices[i]
			break
		}
	}
	if d == nil {
		tx.Rollback()
		return nil, ErrNotFound
	}
	var intended, snapshot []byte
	var templateHash string
	e = tx.QueryRowContext(ctx, `SELECT i.configuration,t.content_hash FROM network_intended_configs i JOIN network_template_versions t ON t.id=i.template_id WHERE i.id=$1`, d.Device.IntendedID).Scan(&intended, &templateHash)
	if e == nil {
		e = tx.QueryRowContext(ctx, `SELECT snapshot FROM network_push_devices WHERE job_id=$1 AND switch_id=$2`, jobID, deviceID).Scan(&snapshot)
	}
	tx.Rollback()
	if e != nil {
		return nil, e
	}
	client, e := s.FabricProbes.NetworkClient()
	if e != nil {
		return nil, e
	}
	op := netbaseline.Operation{ID: commandID, Phase: "prepare", ProjectID: project, DeviceID: deviceID, Serial: d.Device.Serial, FencingToken: fence, CredentialBatchRef: d.Device.CredentialBatchRef, BaselineID: j.BaselineID, TemplateHash: templateHash, Intended: intended, ApprovedVariables: d.Device.ApprovedVariables, AddressPlanID: d.Device.AddressPlanID, Addresses: d.Device.Addresses}
	if phase == "prepare" {
		evidence, err := client.Do(ctx, op)
		state, code := "PREPARED", ""
		if err == nil {
			err = netbaseline.RequireGates(evidence, networkPrepareGates...)
		}
		if err == nil && len(d.Device.Addresses) > 0 {
			err = netbaseline.RequireGates(evidence, "address-planning")
		}
		if err == nil && (len(evidence.Running) == 0 || evidence.SnapshotRef == "" || len(evidence.SnapshotRef) > 500) {
			err = errors.New("A normalized running snapshot and durable backup reference are required")
		}
		var diff []netbaseline.Difference
		if err == nil {
			diff, err = netbaseline.StructuredDiff(evidence.Running, intended)
		}
		if err != nil {
			state, code = "BLOCKED", "NETWORK_PRECHECK_BLOCKED"
		}
		if e = s.saveNetworkPreparation(ctx, j, deviceID, state, code, evidence, diff, executionID, fence); e != nil {
			return nil, e
		}
		source := ""
		if evidence != nil {
			source = evidence.EvidenceSource
		}
		return &NetworkRunResult{State: state, Code: code, Source: source}, nil
	}
	if phase != "push" {
		return nil, errors.New("unsupported network runner phase")
	}
	// Recheck live authority and approved immutable facts immediately before any
	// production write. The gateway must compare expected hash atomically with apply.
	if e = s.networkMutationAdmission(ctx, j, executionID, deviceID, fence); e != nil {
		if save := s.saveNetworkOutcome(ctx, j, deviceID, "BLOCKED", "NETWORK_MUTATION_ADMISSION_CHANGED", nil, nil, executionID, fence); save != nil {
			return nil, save
		}
		return &NetworkRunResult{State: "BLOCKED", Code: "NETWORK_MUTATION_ADMISSION_CHANGED"}, nil
	}
	op.Phase = "apply"
	op.ExpectedRunningHash = d.RunningHash
	op.RollbackRef = d.SnapshotRef
	applied, e := client.Do(ctx, op)
	var unknown *netbaseline.MutationUnknown
	if errors.As(e, &unknown) {
		s.markNetworkUnknown(j.ID, deviceID, "NETWORK_WRITE_UNKNOWN")
		return nil, e
	}
	if applied != nil && applied.State == "rejected" {
		if err := s.saveNetworkOutcome(ctx, j, deviceID, "BLOCKED", "NETWORK_APPLY_REJECTED", applied, nil, executionID, fence); err != nil {
			return nil, err
		}
		return &NetworkRunResult{State: "BLOCKED", Code: "NETWORK_APPLY_REJECTED", Source: applied.EvidenceSource}, nil
	}
	if e == nil {
		e = netbaseline.RequireGates(applied, "applied")
	}
	if e == nil && d.Prepare != nil && applied.EvidenceSource != d.Prepare.EvidenceSource {
		e = &netbaseline.MutationUnknown{Phase: "apply"}
		s.markNetworkUnknown(j.ID, deviceID, "NETWORK_EVIDENCE_SOURCE_CHANGED")
		return nil, e
	}
	if e != nil {
		return s.rollbackNetworkTarget(ctx, client, j, d, op, snapshot, applied, nil, executionID, fence)
	}
	verifyOp := op
	verifyOp.ID = RandomID()
	verifyOp.Phase = "verify"
	verified, verifyErr := client.Do(ctx, verifyOp)
	if verifyErr == nil {
		verifyErr = netbaseline.RequireGates(verified, networkVerifyGates...)
	}
	if verifyErr == nil && (len(verified.Running) == 0 || verified.EvidenceSource != applied.EvidenceSource || verified.RunningHash != d.Device.IntendedHash) {
		verifyErr = errors.New("Post-change normalized running does not match approved intended configuration")
	}
	if verifyErr != nil {
		return s.rollbackNetworkTarget(ctx, client, j, d, op, snapshot, applied, verified, executionID, fence)
	}
	if e = s.saveNetworkOutcome(ctx, j, deviceID, "VERIFIED", "", applied, verified, executionID, fence); e != nil {
		return nil, &netbaseline.MutationUnknown{Phase: "verify-persist"}
	}
	return &NetworkRunResult{State: "VERIFIED", Source: verified.EvidenceSource, RunningHash: verified.RunningHash}, nil
}
func (s *Store) rollbackNetworkTarget(ctx context.Context, client *netbaseline.Client, j *NetworkPushJob, d *NetworkPushDevice, op netbaseline.Operation, snapshot json.RawMessage, applied, verified *netbaseline.OperationResult, execution string, fence int64) (*NetworkRunResult, error) {
	if len(snapshot) == 0 || d.SnapshotRef == "" {
		s.markNetworkUnknown(j.ID, d.Device.ID, "NETWORK_ROLLBACK_SNAPSHOT_MISSING")
		return nil, &netbaseline.MutationUnknown{Phase: "rollback"}
	}
	// A cancelled/expired owner must not start a fresh rollback. Quarantine lets an
	// operator reconcile the actual device before issuing another fenced operation.
	if e := s.networkMutationAdmission(ctx, j, execution, d.Device.ID, fence); e != nil {
		s.markNetworkUnknown(j.ID, d.Device.ID, "NETWORK_ROLLBACK_AUTHORITY_LOST")
		return nil, &netbaseline.MutationUnknown{Phase: "rollback"}
	}
	op.ID = RandomID()
	op.Phase = "rollback"
	op.Intended = snapshot
	op.ExpectedRunningHash = ""
	op.RollbackRef = d.SnapshotRef
	rolled, e := client.Do(ctx, op)
	if e != nil {
		s.markNetworkUnknown(j.ID, d.Device.ID, "NETWORK_ROLLBACK_UNKNOWN")
		return nil, &netbaseline.MutationUnknown{Phase: "rollback"}
	}
	if netbaseline.RequireGates(rolled, "restored") != nil || rolled.RunningHash != d.RunningHash || len(rolled.Running) == 0 || d.Prepare == nil || rolled.EvidenceSource != d.Prepare.EvidenceSource {
		s.markNetworkUnknown(j.ID, d.Device.ID, "NETWORK_ROLLBACK_UNVERIFIED")
		return nil, &netbaseline.MutationUnknown{Phase: "rollback"}
	}
	// Persist the rollback receipt in verify_evidence so a failure retains a
	// device-backed restoration proof rather than a guessed terminal state.
	if e = s.saveNetworkOutcome(ctx, j, d.Device.ID, "ROLLED_BACK", "NETWORK_PUSH_FAILED_RESTORED", applied, rolled, execution, fence); e != nil {
		return nil, &netbaseline.MutationUnknown{Phase: "rollback-persist"}
	}
	return &NetworkRunResult{State: "ROLLED_BACK", Code: "NETWORK_PUSH_FAILED_RESTORED", Source: rolled.EvidenceSource, RunningHash: rolled.RunningHash}, nil
}
func (s *Store) markNetworkUnknown(job, device, code string) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = s.DB.ExecContext(ctx, `UPDATE network_push_devices SET state='UNKNOWN',code=$3,updated_at=now() WHERE job_id=$1 AND switch_id=$2`, job, device, code)
}
func networkFenceValid(ctx context.Context, tx *sql.Tx, execution, device string, fence int64) error {
	var valid bool
	e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id JOIN exec_targets t ON t.id=a.target_id WHERE a.execution_id=$1 AND t.resource_key=$2 AND a.fencing_token=$3 AND a.state='running' AND a.lease_until>now() AND x.desired_state IN('running','paused'))`, execution, device, fence).Scan(&valid)
	if e != nil {
		return e
	}
	if !valid {
		return apiError(409, "NETWORK_FENCE_LOST", "Execution no longer owns the live switch lease")
	}
	return nil
}
func (s *Store) networkMutationAdmission(ctx context.Context, j *NetworkPushJob, execution, device string, fence int64) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var enabled, admin bool
	var state string
	if e = tx.QueryRowContext(ctx, `SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE`, j.CreatedBy).Scan(&enabled, &admin); e != nil {
		return e
	}
	if !enabled {
		return ErrForbidden
	}
	if !admin {
		var role string
		if e = tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE user_id=$1 AND project_id=$2 FOR SHARE`, j.CreatedBy, j.ProjectID).Scan(&role); e != nil {
			return e
		}
		if role != "Admin" && role != "NDE" {
			return ErrForbidden
		}
	}
	if e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, j.ProjectID).Scan(&state); e != nil {
		return e
	}
	if state == "archived" || state == "delivered" {
		return ErrForbidden
	}
	if e = networkFenceValid(ctx, tx, execution, device, fence); e != nil {
		return e
	}
	current, e := loadNetworkPush(ctx, tx, j.ProjectID, j.ID, false)
	if e != nil {
		return e
	}
	if current.State != "EXECUTING" && current.State != "VERIFYING" {
		return apiError(409, "NETWORK_JOB_NOT_EXECUTING", "PushJob is not executing")
	}
	if e = s.reviewNetworkPushFacts(ctx, tx, current); e != nil {
		return e
	}
	if e = validNetworkSignatures(ctx, tx, current); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) saveNetworkPreparation(ctx context.Context, j *NetworkPushJob, device, state, code string, evidence *netbaseline.OperationResult, diff []netbaseline.Difference, execution string, fence int64) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = networkFenceValid(ctx, tx, execution, device, fence); e != nil {
		return e
	}
	current, e := loadNetworkPush(ctx, tx, j.ProjectID, j.ID, true)
	if e != nil {
		return e
	}
	if current.State != "PRECHECKING" {
		return errors.New("PushJob precheck no longer active")
	}
	var running any
	hash, ref := "", ""
	var rawEvidence any
	if evidence != nil {
		running = []byte(evidence.Running)
		hash = evidence.RunningHash
		ref = evidence.SnapshotRef
		copy := *evidence
		copy.Running = nil
		rawEvidence = networkJSON(copy)
	}
	if _, e = tx.ExecContext(ctx, `UPDATE network_push_devices SET state=$3,code=$4,snapshot=$5,running_hash=nullif($6,''),snapshot_ref=nullif($7,''),diff=$8,prepare_evidence=$9,updated_at=now() WHERE job_id=$1 AND switch_id=$2`, j.ID, device, state, code, running, hash, ref, networkJSON(diff), rawEvidence); e != nil {
		return e
	}
	current, e = loadNetworkPush(ctx, tx, j.ProjectID, j.ID, false)
	if e != nil {
		return e
	}
	pending, blocked := false, false
	for _, d := range current.Devices {
		pending = pending || d.State == "PENDING"
		blocked = blocked || d.State == "BLOCKED"
	}
	if !pending {
		to := "READY"
		if blocked {
			to = "BLOCKED"
		} else {
			if e = s.reviewNetworkPushFacts(ctx, tx, current); e != nil {
				to = "BLOCKED"
			} else {
				current.Critical = false
				source := ""
				for _, d := range current.Devices {
					current.Critical = current.Critical || netbaseline.HasCriticalDiff(d.Diff)
					if d.Prepare == nil || source != "" && source != d.Prepare.EvidenceSource {
						to = "BLOCKED"
						break
					}
					source = d.Prepare.EvidenceSource
				}
				current.ReviewHash = netbaseline.ContentHash(struct {
					Facts, Config, Baseline, Template string
					Canary, Concurrency               int
					Devices                           []NetworkPushDevice
				}{current.FactsHash, current.ConfigHash, current.BaselineID, current.TemplateID, current.CanaryMinSize, current.Concurrency, current.Devices})
				if _, e = tx.ExecContext(ctx, `UPDATE network_push_jobs SET review_hash=$2,critical=$3 WHERE id=$1`, j.ID, current.ReviewHash, current.Critical); e != nil {
					return e
				}
			}
		}
		if e = pushTransition(ctx, tx, current, to, map[string]any{"review_hash": current.ReviewHash, "critical": current.Critical}); e != nil {
			return e
		}
	}
	if e = appendAudit(ctx, tx, "", j.ProjectID, "network.push.precheck", j.ID, "worker", map[string]any{"device_id": device, "state": state, "code": code}); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) saveNetworkOutcome(ctx context.Context, j *NetworkPushJob, device, state, code string, applied, verified *netbaseline.OperationResult, execution string, fence int64) error {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = networkFenceValid(ctx, tx, execution, device, fence); e != nil {
		return e
	}
	current, e := loadNetworkPush(ctx, tx, j.ProjectID, j.ID, true)
	if e != nil {
		return e
	}
	var a, v any
	if applied != nil {
		copy := *applied
		copy.Running = nil
		a = networkJSON(copy)
	}
	if verified != nil {
		copy := *verified
		copy.Running = nil
		v = networkJSON(copy)
	}
	if _, e = tx.ExecContext(ctx, `UPDATE network_push_devices SET state=$3,code=$4,apply_evidence=$5,verify_evidence=$6,updated_at=now() WHERE job_id=$1 AND switch_id=$2`, j.ID, device, state, code, a, v); e != nil {
		return e
	}
	// A failed canary cancels every still-pending peer before it acquires a lease.
	if len(current.Devices) >= current.CanaryMinSize && current.Devices[0].Device.ID == device && state != "VERIFIED" {
		if _, e = tx.ExecContext(ctx, `UPDATE network_push_devices SET state='SKIPPED',code='NETWORK_CANARY_BLOCKED',updated_at=now() WHERE job_id=$1 AND ordinal>0 AND state='PREPARED'`, j.ID); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE exec_task_attempts SET state='skipped',state_version=state_version+1,finished_at=now() WHERE execution_id=$1 AND state='pending'`, execution); e != nil {
			return e
		}
	}
	current, e = loadNetworkPush(ctx, tx, j.ProjectID, j.ID, false)
	if e != nil {
		return e
	}
	done, allVerified, allRestored := true, true, true
	for _, d := range current.Devices {
		if d.State == "PREPARED" || d.State == "APPLYING" || d.State == "UNKNOWN" || d.State == "PAUSED" {
			done = false
		}
		allVerified = allVerified && d.State == "VERIFIED"
		allRestored = allRestored && (d.State == "ROLLED_BACK" || d.State == "SKIPPED")
	}
	if done {
		if allRestored {
			if e = pushTransition(ctx, tx, current, "ROLLING_BACK", map[string]any{"reason": "automatic_restore"}); e != nil {
				return e
			}
			if e = pushTransition(ctx, tx, current, "ROLLED_BACK", map[string]any{"restored": true}); e != nil {
				return e
			}
		} else {
			if e = pushTransition(ctx, tx, current, "VERIFYING", map[string]any{"readback_complete": true}); e != nil {
				return e
			}
			to := "PARTIAL"
			if allVerified {
				to = "COMPLETED"
			}
			if e = pushTransition(ctx, tx, current, to, map[string]any{"verified": allVerified}); e != nil {
				return e
			}
		}
	}
	if e = appendAudit(ctx, tx, "", j.ProjectID, "network.push.device", j.ID, "worker", map[string]any{"device_id": device, "state": state, "code": code, "fencing_token": fence}); e != nil {
		return e
	}
	return tx.Commit()
}
