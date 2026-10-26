package platform

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// ProvisionFirstSSHWork is pinned to the original native attempt. It is never
// admitted as a second ordinary SSH execution or granted a second device lease.
type ProvisionFirstSSHWork struct {
	Mode         string                      `json:"mode"`
	RetryID      string                      `json:"retry_id,omitempty"`
	Command      taskrunner.TaskCommand      `json:"command"`
	Plan         taskrunner.ProvisionSSHPlan `json:"plan"`
	ReportSHA256 string                      `json:"report_sha256"`
}

type provisionFirstBootEvidence struct {
	Report            provisionfirstboot.Report `json:"report"`
	Signature         string                    `json:"signature"`
	ReportSHA256      string                    `json:"report_sha256"`
	BindingSHA256     string                    `json:"binding_sha256"`
	Peer              string                    `json:"peer"`
	ScopeSHA256       string                    `json:"scope_sha256"`
	HTTPAuthenticated bool                      `json:"http_authenticated"`
	DHCPMatched       bool                      `json:"dhcp_auxiliary_matched"`
}

func provisionFirstSSHPlanTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest, scope *ProvisionBootScope, until time.Time) (*ProvisionFirstSSHWork, error) {
	if x.State != "awaiting_first_boot" || x.BootID == nil || x.PermitIssuedAt == nil {
		return nil, ErrProvisionBootDenied
	}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.identity_reported' ORDER BY id LIMIT 1`, x.ID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ev provisionFirstBootEvidence
	if json.Unmarshal(raw, &ev) != nil || !ev.HTTPAuthenticated || !ev.DHCPMatched || ev.ScopeSHA256 != x.ScopeSHA256 {
		return nil, ErrProvisionBootDenied
	}
	binding, err := provisionFirstBootBindingTx(ctx, tx, x, request, scope.Scope)
	if err != nil {
		return nil, err
	}
	in := provisionfirstboot.Submission{Report: ev.Report, Signature: ev.Signature}
	// Freshness was checked at the authenticated callback. Here the immutable
	// signature is rechecked at its observation time, while authority/lease/source
	// and the approved network are checked at the current server time.
	if provisionfirstboot.VerifySubmission(*binding, in, ev.Report.ObservedAt) != nil || ev.BindingSHA256 != binding.SHA256() || digest(string(provisionFirstSSHJSON(ev.Report))) != ev.ReportSHA256 {
		return nil, ErrProvisionBootDenied
	}
	peer, err := netip.ParseAddr(ev.Peer)
	if err != nil || !scope.Scope.CheckPeer(peer) {
		return nil, ErrProvisionBootDenied
	}
	p, err := writePermitEventTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if p.FirstBootBindingSHA256 != binding.SHA256() {
		return nil, ErrProvisionBootDenied
	}
	if err = validateProvisionStorageApproverTx(ctx, tx, x); err != nil {
		return nil, err
	}
	if err = validateProvisionRuntimeInputsTx(ctx, tx, x, p.RuntimeInputSHA256); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	key, err := ReadSSHInstallCredential(ctx, tx, x.ProjectID, request.Snapshot.DomainKey, now.Add(15*time.Second))
	if err != nil {
		return nil, err
	}
	expiry := now.Add(20 * time.Second)
	if until.Before(expiry) {
		expiry = until
	}
	if x.ExpiresAt.Before(expiry) {
		expiry = x.ExpiresAt
	}
	if key.ExpiresAt.Before(expiry) {
		expiry = key.ExpiresAt
	}
	if !expiry.After(now.Add(time.Second)) {
		return nil, ErrProvisionBootDenied
	}
	return &ProvisionFirstSSHWork{Mode: "run", Command: taskrunner.TaskCommand{APIVersion: taskrunner.APIVersion, ProjectID: x.ProjectID, MachineID: x.MachineID, CommandID: *x.CommandID, AttemptID: *x.AttemptID, FencingToken: *x.FencingToken, Operation: "host.identity", TimeoutMS: 5000}, Plan: taskrunner.ProvisionSSHPlan{BindingSHA256: binding.SHA256(), Peer: peer, Clients: scope.Scope.Clients, HostKey: ev.Report.HostFingerprint, Hostname: binding.Hostname, Arch: binding.StoragePlan.Spec.Expected.Arch, Credential: *key, ExpiresAt: expiry}, ReportSHA256: ev.ReportSHA256}, nil
}

// PrepareProvisionFirstSSH durably consumes the one automatic first-SSH try
// before any network operation. A crash or lost receipt leaves an explicit
// repair item and cannot silently replay a run command on the next worker tick.
func (s *Store) PrepareProvisionFirstSSH(ctx context.Context, project, id, execution, attempt, command string, fence int64) (*ProvisionFirstSSHWork, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, request, scope, until, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if x.ExecutionID != execution || x.AttemptID == nil || *x.AttemptID != attempt || x.CommandID == nil || *x.CommandID != command || x.FencingToken == nil || *x.FencingToken != fence {
		return nil, ErrProvisionBootDenied
	}
	var already bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_started')`, x.ID).Scan(&already); err != nil {
		return nil, err
	}
	if already {
		return prepareProvisionFirstSSHRetryTx(ctx, tx, x, request, scope, until)
	}
	work, err := provisionFirstSSHPlanTx(ctx, tx, x, request, scope, until)
	if err != nil || work == nil {
		return nil, err
	}
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_started", map[string]any{"command": work.Command, "plan": work.Plan, "report_sha256": work.ReportSHA256, "mode": "run"}); err != nil {
		return nil, err
	}
	return work, tx.Commit()
}

func (s *Store) RecordProvisionFirstSSHRepair(ctx context.Context, project, id, attempt, command string, fence int64, reason string) error {
	if reason != "runner_unavailable" && reason != "verification_unknown" && reason != "finalization_rejected" {
		return ErrProvisionBootDenied
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	x, err := scanProvisionInstallation(tx.QueryRowContext(ctx, "SELECT "+provisionInstallationColumns+" FROM provision_installations WHERE id=$1 AND project_id=$2 FOR UPDATE", id, project))
	if err != nil {
		return err
	}
	if x.State != "awaiting_first_boot" || x.AttemptID == nil || *x.AttemptID != attempt || x.CommandID == nil || *x.CommandID != command || x.FencingToken == nil || *x.FencingToken != fence {
		return ErrProvisionBootDenied
	}
	var dispatchID, repairID int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(id) FILTER(WHERE event_type IN('first_boot.ssh_started','first_boot.ssh_retry_consumed')),0),COALESCE(max(id) FILTER(WHERE event_type='first_boot.ssh_repair_required'),0) FROM provision_installation_events WHERE installation_id=$1`, x.ID).Scan(&dispatchID, &repairID); err != nil {
		return err
	}
	if dispatchID == 0 {
		return ErrProvisionBootDenied
	}
	already := repairID > dispatchID
	if !already {
		if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_repair_required", map[string]any{"reason": reason, "attempt_id": attempt, "command_id": command, "fencing_token": fence, "automatic_retry": false, "resource_retained": true}); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) FinalizeProvisionFirstSSH(ctx context.Context, project, id string, proof taskrunner.ProvisionSSHProof) (*ProvisionInstallation, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, request, scope, until, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	current, err := provisionFirstSSHPlanTx(ctx, tx, x, request, scope, until)
	if err != nil || current == nil {
		return nil, ErrProvisionBootDenied
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM provision_installation_events WHERE installation_id=$1 AND event_type IN('first_boot.ssh_started','first_boot.ssh_retry_consumed') ORDER BY id DESC LIMIT 1`, x.ID).Scan(&raw); err != nil {
		return nil, err
	}
	var work ProvisionFirstSSHWork
	if json.Unmarshal(raw, &work) != nil {
		return nil, ErrProvisionBootDenied
	}
	// The time-bounded plan is frozen at dispatch. Everything else is recomputed
	// from current authorities and immutable first-boot evidence.
	current.Plan.ExpiresAt = work.Plan.ExpiresAt
	if work.Mode != "run" && work.Mode != "query" {
		return nil, ErrProvisionBootDenied
	}
	current.Mode = work.Mode
	current.RetryID = work.RetryID
	if work.Mode == "query" {
		var requester string
		if !modelIDPattern.MatchString(work.RetryID) {
			return nil, ErrProvisionBootDenied
		}
		if err = tx.QueryRowContext(ctx, `SELECT payload->>'requested_by' FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_retry_requested' AND payload->>'retry_id'=$2 ORDER BY id DESC LIMIT 1`, x.ID, work.RetryID).Scan(&requester); err != nil {
			return nil, err
		}
		if err = validateProvisionSSHRetryActorTx(ctx, tx, x.ProjectID, requester); err != nil {
			return nil, err
		}
	}
	if string(provisionFirstSSHJSON(current)) != string(provisionFirstSSHJSON(work)) || proof.Validate(work.Command, work.Plan, time.Now()) != nil {
		return nil, ErrProvisionBootDenied
	}
	var asset provisionActivatedAsset
	if err = tx.QueryRowContext(ctx, `SELECT payload->'asset' FROM provision_installation_events WHERE installation_id=$1 AND event_type='write.permit_issued' ORDER BY id LIMIT 1`, x.ID).Scan(&raw); err != nil {
		return nil, err
	}
	if json.Unmarshal(raw, &asset) != nil || asset.AssetID != request.AssetID || asset.SourceVersion != request.Snapshot.Target.AssetVersion || asset.ActiveVersion != asset.SourceVersion+1 {
		return nil, ErrProvisionBootDenied
	}
	var version int64
	var state, trust string
	if err = tx.QueryRowContext(ctx, `SELECT a.version,a.state,m.ssh_trust FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1 AND a.project_id=$2 AND a.machine_id=$3 FOR UPDATE OF a,m`, asset.AssetID, project, x.MachineID).Scan(&version, &state, &trust); err != nil {
		return nil, err
	}
	if version != asset.ActiveVersion || state != "provisioning" || trust != "unverified" {
		return nil, ErrProvisionBootDenied
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if !until.After(now) || !x.ExpiresAt.After(now) || !work.Plan.ExpiresAt.After(now) {
		return nil, ErrProvisionBootDenied
	}
	if _, err = tx.ExecContext(ctx, `UPDATE project_assets SET state='provisioned',version=version+1,updated_at=$2 WHERE id=$1`, asset.AssetID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE machines SET ssh_trust='established',ssh_trust_source='ssh',host_key_fingerprint=$2,ssh_domain_credential=$3,trust_updated_at=$4 WHERE id=$1`, x.MachineID, work.Plan.HostKey, provisionFirstSSHJSON(work.Plan.Credential), now); err != nil {
		return nil, err
	}
	observation := proof.Observation()
	verified := map[string]any{"command": work.Command, "observation": observation, "binding_sha256": work.Plan.BindingSHA256, "report_sha256": work.ReportSHA256, "host_fingerprint": work.Plan.HostKey, "domain_credential": work.Plan.Credential, "asset_version": asset.ActiveVersion + 1, "verified_transport": "ssh", "hardware_evidence": false}
	verifiedSHA := ProvisionFirstSSHTranscriptSHA256(provisionFirstSSHJSON(verified))
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_verified", verified); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE provision_installations SET state='registered',state_version=state_version+1,updated_at=$2 WHERE id=$1`, x.ID, now); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO trust_logs(id,machine_id,project_id,action,outcome,fingerprint,detail,created_by,created_at) VALUES($1,$2,$3,'trust.verify','success',$4,$5,$6,$7)`, RandomID(), x.MachineID, project, work.Plan.HostKey, provisionFirstSSHJSON(map[string]any{"installation_id": x.ID, "execution_id": x.ExecutionID, "command_id": work.Command.CommandID, "attempt_id": work.Command.AttemptID, "fencing_token": work.Command.FencingToken, "binding_sha256": work.Plan.BindingSHA256, "source": "ssh"}), x.CreatedBy, now); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, x.CreatedBy, project, "provision.first_ssh.verified", x.ID, "worker", map[string]any{"execution_id": x.ExecutionID, "binding_sha256": work.Plan.BindingSHA256, "host_fingerprint": work.Plan.HostKey, "asset_version": asset.ActiveVersion + 1}); err != nil {
		return nil, err
	}
	// Persist the native success proof in the same commit as registration. A
	// crash before Complete can be recovered from this immutable runner receipt.
	receipt := provisionRegisteredReceipt{CommandID: work.Command.CommandID, AttemptID: work.Command.AttemptID, FencingToken: work.Command.FencingToken, Result: "success", Output: map[string]any{"schema": "keel.provision-installation-proof/v1", "installation_id": x.ID, "request_id": x.RequestID, "snapshot_sha256": x.SnapshotSHA256, "installation_performed": true, "first_ssh_proof_sha256": verifiedSHA}, StartedAt: observation.StartedAt, FinishedAt: now}
	receiptRaw := provisionFirstSSHJSON(receipt)
	if _, err = tx.ExecContext(ctx, `INSERT INTO exec_runner_receipts(command_id,attempt_id,fencing_token,result,payload,digest,observed_at) VALUES($1,$2,$3,'success',$4,$5,now())`, receipt.CommandID, receipt.AttemptID, receipt.FencingToken, receiptRaw, digest(string(receiptRaw))); err != nil {
		return nil, err
	}
	x.State = "registered"
	x.StateVersion++
	x.UpdatedAt = now
	return x, tx.Commit()
}

func provisionFirstSSHJSON(v any) []byte { b, _ := json.Marshal(v); return b }

// Keep the persisted schema identical to execution.Receipt without introducing a
// platform -> execution package cycle.
type provisionRegisteredReceipt struct {
	CommandID    string         `json:"command_id"`
	AttemptID    string         `json:"attempt_id"`
	FencingToken int64          `json:"fencing_token"`
	Result       string         `json:"result"`
	Output       map[string]any `json:"output"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   time.Time      `json:"finished_at"`
}

// ProvisionFirstSSHTranscriptSHA256 canonicalizes persisted JSONB without
// changing integer fences/versions or depending on nested Go struct key order.
func ProvisionFirstSSHTranscriptSHA256(raw []byte) string {
	var v any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&v) != nil {
		return ""
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ""
	}
	return digest(string(provisionFirstSSHJSON(v)))
}

func prepareProvisionFirstSSHRetryTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest, scope *ProvisionBootScope, until time.Time) (*ProvisionFirstSSHWork, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT e.payload FROM provision_installation_events e WHERE e.installation_id=$1 AND e.event_type='first_boot.ssh_retry_requested' AND NOT EXISTS(SELECT 1 FROM provision_installation_events c WHERE c.installation_id=e.installation_id AND c.event_type='first_boot.ssh_retry_consumed' AND c.payload->>'retry_id'=e.payload->>'retry_id') ORDER BY e.id LIMIT 1`, x.ID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		// A single dispatch with no terminal/repair event is uncertain. Record that
		// evidence once; never infer that the helper did not execute and rerun it.
		var dispatchID, repairID int64
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(id) FILTER(WHERE event_type IN('first_boot.ssh_started','first_boot.ssh_retry_consumed')),0),COALESCE(max(id) FILTER(WHERE event_type='first_boot.ssh_repair_required'),0) FROM provision_installation_events WHERE installation_id=$1`, x.ID).Scan(&dispatchID, &repairID); err != nil {
			return nil, err
		}
		if dispatchID > repairID {
			if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_repair_required", map[string]any{"reason": "verification_unknown", "attempt_id": *x.AttemptID, "command_id": *x.CommandID, "fencing_token": *x.FencingToken, "automatic_retry": false, "resource_retained": true}); err != nil {
				return nil, err
			}
			return nil, tx.Commit()
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var retry struct {
		ID            string `json:"retry_id"`
		RepairEventID int64  `json:"repair_event_id"`
		AttemptID     string `json:"attempt_id"`
		CommandID     string `json:"command_id"`
		FencingToken  int64  `json:"fencing_token"`
		RequestedBy   string `json:"requested_by"`
		Mode          string `json:"mode"`
	}
	if json.Unmarshal(raw, &retry) != nil || !modelIDPattern.MatchString(retry.ID) || !modelIDPattern.MatchString(retry.RequestedBy) || retry.RepairEventID < 1 || retry.AttemptID != *x.AttemptID || retry.CommandID != *x.CommandID || retry.FencingToken != *x.FencingToken || retry.Mode != "query" {
		return nil, ErrProvisionBootDenied
	}
	var currentRepair int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(max(id),0) FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.ssh_repair_required'`, x.ID).Scan(&currentRepair); err != nil {
		return nil, err
	}
	if currentRepair != retry.RepairEventID {
		return nil, ErrProvisionBootDenied
	}
	if err = validateProvisionSSHRetryActorTx(ctx, tx, x.ProjectID, retry.RequestedBy); err != nil {
		return nil, err
	}
	work, err := provisionFirstSSHPlanTx(ctx, tx, x, request, scope, until)
	if err != nil || work == nil {
		return nil, ErrProvisionBootDenied
	}
	work.Mode = "query"
	work.RetryID = retry.ID
	if err = appendProvisionEventTx(ctx, tx, x, "first_boot.ssh_retry_consumed", map[string]any{"retry_id": retry.ID, "repair_event_id": retry.RepairEventID, "attempt_id": *x.AttemptID, "command_id": *x.CommandID, "fencing_token": *x.FencingToken, "command": work.Command, "plan": work.Plan, "report_sha256": work.ReportSHA256, "mode": "query"}); err != nil {
		return nil, err
	}
	return work, tx.Commit()
}

func validateProvisionSSHRetryActorTx(ctx context.Context, tx *sql.Tx, project, actor string) error {
	if !modelIDPattern.MatchString(actor) {
		return ErrProvisionBootDenied
	}
	var enabled, admin bool
	if err := tx.QueryRowContext(ctx, `SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE`, actor).Scan(&enabled, &admin); err != nil {
		return err
	}
	if !enabled {
		return ErrProvisionBootDenied
	}
	if !admin {
		var role string
		if err := tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE`, project, actor).Scan(&role); err != nil {
			return err
		}
		if role != "Admin" && role != "DL" && role != "SRE" {
			return ErrProvisionBootDenied
		}
	}
	return nil
}
