package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"strconv"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func (s *Service) runProvisionFirstSSH(ctx context.Context, c *Claim) (bool, error) {
	if active, err := s.runProvisionKeyRepair(ctx, c); active || err != nil {
		return false, err
	}
	// Ordinary boot/preflight polling does not load the multi-MiB report or open
	// any SSH connection. Only an installed-system identity enters this branch.
	var state string
	var ready bool
	if err := s.DB.QueryRowContext(ctx, `SELECT state,EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$2 AND event_type='first_boot.identity_reported') AND (NOT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$2 AND event_type='first_boot.ssh_started') OR EXISTS(SELECT 1 FROM provision_installation_events q WHERE q.installation_id=$2 AND q.event_type='first_boot.ssh_retry_requested' AND NOT EXISTS(SELECT 1 FROM provision_installation_events c WHERE c.installation_id=q.installation_id AND c.event_type='first_boot.ssh_retry_consumed' AND c.payload->>'retry_id'=q.payload->>'retry_id')) OR EXISTS(SELECT 1 FROM provision_installation_events q WHERE q.installation_id=$2 AND q.event_type='first_boot.ssh_verification_requested' AND NOT EXISTS(SELECT 1 FROM provision_installation_events c WHERE c.installation_id=q.installation_id AND c.event_type='first_boot.ssh_verification_consumed' AND c.payload->>'verification_id'=q.payload->>'verification_id')) OR COALESCE((SELECT max(id) FROM provision_installation_events WHERE installation_id=$2 AND event_type IN('first_boot.ssh_started','first_boot.ssh_retry_consumed','first_boot.ssh_verification_consumed')),0)>COALESCE((SELECT max(id) FROM provision_installation_events WHERE installation_id=$2 AND event_type='first_boot.ssh_repair_required'),0)) FROM provision_installations WHERE project_id=$1 AND id=$2`, c.Target.ProjectID, c.Target.ProvisionInstallationID).Scan(&state, &ready); err != nil {
		return false, err
	}
	if state != "awaiting_first_boot" || !ready {
		return false, nil
	}
	repair := func(reason string) {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.ProvisionStore.RecordProvisionFirstSSHRepair(cleanup, c.Target.ProjectID, c.Target.ProvisionInstallationID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken, reason)
	}
	work, err := s.ProvisionStore.PrepareProvisionFirstSSH(ctx, c.Target.ProjectID, c.Target.ProvisionInstallationID, c.Attempt.ExecutionID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	if err != nil {
		return false, err
	}
	if work == nil {
		// The durable dispatch may have outlived its transport or a failed
		// repair-event write. Surface that ambiguity once for explicit query;
		// never replay run merely because the prior outcome is absent.
		repair("verification_unknown")
		return false, nil
	}
	runner, ok := s.SSHRunner.(interface {
		VerifyProvisionIdentity(context.Context, taskrunner.TaskCommand, taskrunner.ProvisionSSHPlan) (taskrunner.ProvisionSSHProof, error)
	})
	if !ok {
		repair("runner_unavailable")
		return false, nil
	}
	var proof taskrunner.ProvisionSSHProof
	if work.Mode == "query" {
		query, available := s.SSHRunner.(interface {
			QueryProvisionIdentity(context.Context, taskrunner.TaskCommand, taskrunner.ProvisionSSHPlan) (taskrunner.ProvisionSSHProof, error)
		})
		if !available {
			repair("runner_unavailable")
			return false, nil
		}
		proof, err = query.QueryProvisionIdentity(ctx, work.Command, work.Plan)
	} else {
		proof, err = runner.VerifyProvisionIdentity(ctx, work.Command, work.Plan)
	}
	if err != nil {
		repair("verification_unknown")
		return false, nil
	}
	if proof.Observation().State == "failed" {
		if work.Mode == "query" {
			if err = s.ProvisionStore.RecordProvisionFirstSSHFailure(ctx, c.Target.ProjectID, c.Target.ProvisionInstallationID, proof); err != nil {
				repair("finalization_rejected")
				return false, nil
			}
		}
		repair("verification_unknown")
		return false, nil
	}
	if _, err = s.ProvisionStore.FinalizeProvisionFirstSSH(ctx, c.Target.ProjectID, c.Target.ProvisionInstallationID, proof); err != nil {
		repair("finalization_rejected")
		return false, nil
	}
	// Registration and its immutable native success receipt were one commit.
	// Complete applies that proof to this same attempt and releases its lease.
	return true, s.Complete(ctx, c)
}

func registeredInstallationReceiptTarget(ctx context.Context, tx *sql.Tx, target *Target, a Attempt, r Receipt, x *platform.ProvisionInstallation) error {
	if x.ExecutionID != a.ExecutionID || x.RequestID != target.ProvisionRequestID || x.SnapshotSHA256 != target.ProvisionSnapshotSHA256 || x.PermitIssuedAt == nil || x.AttemptID == nil || *x.AttemptID != a.ID || x.CommandID == nil || *x.CommandID != a.CommandID || x.FencingToken == nil || *x.FencingToken != a.FencingToken || r.CommandID != a.CommandID || r.AttemptID != a.ID || r.FencingToken != a.FencingToken || r.Result != "success" || r.Output["schema"] != "keel.provision-installation-proof/v1" || r.Output["installation_id"] != x.ID || r.Output["request_id"] != x.RequestID || r.Output["snapshot_sha256"] != x.SnapshotSHA256 || r.Output["installation_performed"] != true {
		return conflict("Registered installation success requires its persisted native SSH proof")
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_verified' ORDER BY id LIMIT 1`, x.ID).Scan(&raw); err != nil {
		return err
	}
	var ev struct {
		NativeCommandID       string                       `json:"native_command_id"`
		VerificationCommandID string                       `json:"verification_command_id"`
		VerificationID        string                       `json:"verification_id"`
		Command               taskrunner.TaskCommand       `json:"command"`
		Observation           taskrunner.Observation       `json:"observation"`
		BindingSHA256         string                       `json:"binding_sha256"`
		HostFingerprint       string                       `json:"host_fingerprint"`
		Transport             string                       `json:"verified_transport"`
		Credential            taskrunner.InstallCredential `json:"domain_credential"`
		AssetVersion          int64                        `json:"asset_version"`
	}
	if json.Unmarshal(raw, &ev) != nil || ev.Command.ProjectID != x.ProjectID || ev.Command.MachineID != x.MachineID || ev.Command.AttemptID != a.ID || ev.NativeCommandID != a.CommandID || ev.VerificationCommandID != ev.Command.CommandID || ev.Command.FencingToken != a.FencingToken || ev.Command.Operation != "host.identity" || ev.Observation.Validate(ev.Command) != nil || ev.Observation.State != "success" || ev.Transport != "ssh" || !taskrunner.IsValidHostKey(ev.HostFingerprint) || len(ev.BindingSHA256) != 64 {
		return conflict("Registered installation SSH proof differs from native attempt")
	}
	if ev.Command.CommandID != a.CommandID {
		var bound bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_verification_consumed' AND payload->>'native_command_id'=$2 AND payload->>'verification_command_id'=$3 AND payload->>'verification_id'=$4 AND payload->>'attempt_id'=$5 AND payload->>'fencing_token'=$6)`, x.ID, a.CommandID, ev.Command.CommandID, ev.VerificationID, a.ID, strconv.FormatInt(a.FencingToken, 10)).Scan(&bound); err != nil {
			return err
		}
		if !bound {
			return conflict("Replacement SSH probe has no immutable native-attempt authorization")
		}
	}
	if r.Output["first_ssh_proof_sha256"] != platform.ProvisionFirstSSHTranscriptSHA256(raw) {
		return conflict("Registered installation SSH transcript commitment differs")
	}
	// Registration and this native receipt were committed together before
	// Complete. After a worker crash, the reaper may quarantine this same
	// attempt; authorized Reconcile must consume the existing proof without
	// reviving its lease, rerunning SSH or admitting a new installation.
	var current bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(
 SELECT 1 FROM exec_task_attempts a
 JOIN exec_resource_leases r ON r.attempt_id=a.id
 JOIN exec_executions e ON e.id=a.execution_id
 JOIN provision_requests p ON p.id=$6
 JOIN project_assets asset ON asset.id=p.asset_id
 JOIN machines m ON m.id=asset.machine_id
 WHERE a.id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND a.execution_id=$4
 AND a.runner='provision' AND e.mode='provision' AND e.project_id=$7
 AND r.resource_id=$5 AND r.mode='exclusive' AND r.fencing_token=a.fencing_token
 AND ((a.state='running' AND a.lease_until>now() AND r.state='held' AND r.expires_at>now()
       AND e.state IN('running','dispatching') AND e.desired_state='running')
   OR (a.state='unknown' AND r.state='reconciling' AND e.state='reconciling'))
 AND asset.project_id=e.project_id AND asset.machine_id=$8 AND asset.state='provisioned'
 AND asset.version=$9 AND m.ssh_trust='established' AND m.ssh_trust_source='ssh'
 AND m.host_key_fingerprint=$10 AND m.ssh_domain_credential=$11::jsonb
 FOR SHARE OF r,asset,m
 )`, a.ID, a.CommandID, a.FencingToken, a.ExecutionID, "device:machine:"+x.MachineID, x.RequestID, x.ProjectID, x.MachineID, ev.AssetVersion, ev.HostFingerprint, jsonBytes(ev.Credential)).Scan(&current); err != nil {
		return err
	}
	if !current {
		return conflict("Registered installation no longer owns the native lease or projected identity")
	}
	return nil
}
