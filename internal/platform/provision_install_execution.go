package platform

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediamanifest"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
)

// ValidateProvisionInstallationQueuedTx authorizes admission/dispatch only.
// It cannot issue a boot Grant or reuse the preparation execution's old lease.
func (s *Store) ValidateProvisionInstallationQueuedTx(ctx context.Context, tx *sql.Tx, project, id string) (*ProvisionInstallation, error) {
	if !s.ProvisionInstallEnabled || s.MediaObjects == nil || len(s.ProvisionImagePublicKey) != ed25519.PublicKeySize {
		return nil, apiError(503, "PROVISION_INSTALL_DISABLED", "Installation runtime and independent image trust are not configured")
	}
	x, err := LoadProvisionInstallationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if x.State != "queued" || !x.ExpiresAt.After(time.Now()) || x.AttemptID != nil {
		return nil, apiError(409, "PROVISION_INSTALL_STATE_CHANGED", "Unconsumed queued installation required")
	}
	request, err := LoadProvisionRequestTx(ctx, tx, project, x.RequestID)
	if err != nil {
		return nil, err
	}
	if request.State != "prepared" || !ProvisionSnapshotValid(request) || request.MachineID != x.MachineID || request.SnapshotSHA256 != x.SnapshotSHA256 || request.Snapshot.Media.ValidateInstallable() != nil {
		return nil, apiError(409, "PROVISION_INSTALL_SOURCE_CHANGED", "Prepared immutable installable source required")
	}
	if err = ValidateProvisionRequestMetadataTx(ctx, tx, request, time.Now()); err != nil {
		return nil, err
	}
	scope, err := LoadApprovedProvisionScopeTx(ctx, tx, project, x.ScopeID)
	if err != nil {
		return nil, err
	}
	if scope.ScopeSHA256 != x.ScopeSHA256 {
		return nil, ErrProvisionBootDenied
	}
	return x, nil
}

// VerifyProvisionInstallationSources hashes frozen sources and authenticates
// the actual initrd before activation. It deliberately does not lock attempt or
// resource rows during object IO, so the owner's heartbeat can renew its lease.
// Activation subsequently rechecks all live bindings in a short transaction.
type VerifiedProvisionInstallSources struct {
	project                 string
	installation            string
	snapshot                string
	execution               string
	verifiedAt              time.Time
	runtimeSchema           string
	runtimeInputSHA256      string
	writerToolBytesVerified bool
}

func (s *Store) VerifyProvisionInstallationSources(ctx context.Context, project, id string) (*VerifiedProvisionInstallSources, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, err := s.ValidateProvisionInstallationQueuedTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	var enabled, admin bool
	if err = tx.QueryRowContext(ctx, "SELECT enabled,is_admin FROM users WHERE id=$1 FOR SHARE", x.CreatedBy).Scan(&enabled, &admin); err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrProvisionBootDenied
	}
	actor := &AuthSession{User: UserSummary{ID: x.CreatedBy, IsAdmin: admin}}
	request, err := LoadProvisionRequestTx(ctx, tx, project, x.RequestID)
	if err != nil {
		return nil, err
	}
	if _, err = s.VerifyProvisionRequestTx(ctx, tx, actor, request); err != nil {
		return nil, err
	}
	var initrd string
	for _, a := range request.Snapshot.Media.Artifacts {
		if a.Role == "initrd" {
			initrd = a.MediaID
		}
	}
	media, err := mediaDependencyRecord(ctx, tx, initrd)
	if err != nil {
		return nil, err
	}
	file, err := s.checkedMediaFile(ctx, tx, &media)
	if err != nil {
		return nil, err
	}
	defer file.close()
	runtime, err := mediamanifest.VerifyInstallerInitrdForPlan(ctx, file.file, request.Snapshot.Media, s.ProvisionImagePublicKey)
	if err != nil {
		return nil, apiError(422, "PROVISION_INITRD_INVALID", "Actual approved initrd does not match its authenticated runtime inputs and frozen role plan")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if _, err = s.ValidateProvisionInstallationQueuedTx(ctx, tx, project, id); err != nil {
		return nil, err
	}
	return &VerifiedProvisionInstallSources{project: project, installation: id, snapshot: x.SnapshotSHA256, execution: x.ExecutionID, verifiedAt: time.Now(), runtimeSchema: runtime.Metadata.Schema, runtimeInputSHA256: runtime.Metadata.ApprovedRuntimeSHA256, writerToolBytesVerified: runtime.Metadata.Runtime.Writer != nil}, nil
}

func (s *Store) ActivateProvisionInstallation(ctx context.Context, project, id, execution, attempt, command string, fence int64, verified *VerifiedProvisionInstallSources) (*ProvisionInstallation, error) {
	if verified == nil || verified.project != project || verified.installation != id || verified.execution != execution || verified.verifiedAt.IsZero() || time.Since(verified.verifiedAt) < 0 || time.Since(verified.verifiedAt) > time.Minute {
		return nil, ErrProvisionBootDenied
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, err := s.ValidateProvisionInstallationQueuedTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if x.ExecutionID != execution || x.SnapshotSHA256 != verified.snapshot {
		return nil, ErrProvisionBootDenied
	}
	request, err := LoadProvisionRequestTx(ctx, tx, project, x.RequestID)
	if err != nil {
		return nil, err
	}
	var hardware, bom []byte
	var modelHash string
	if err = tx.QueryRowContext(ctx, "SELECT hw_spec,bom_expect,content_hash FROM device_model_versions WHERE id=$1 FOR SHARE", request.Snapshot.Target.ModelVersionID).Scan(&hardware, &bom, &modelHash); err != nil {
		return nil, err
	}
	if modelHash != request.Snapshot.Target.ModelContentHash {
		return nil, ErrProvisionBootDenied
	}
	expected, err := provisionpreflight.ExpectationFromModel(request.Snapshot.Target.Serial, request.Snapshot.BootPort.MAC, hardware, bom)
	if err != nil {
		return nil, err
	}
	if expected.NICCount == nil || expected.GPUCount == nil {
		return nil, apiError(422, "PROVISION_MODEL_EXPECTATION_REQUIRED", "Independent published NIC and GPU count expectations are required before boot")
	}
	x.AttemptID = &attempt
	x.CommandID = &command
	x.FencingToken = &fence
	x.Spec = &provisionpreflight.Spec{Schema: provisionpreflight.Schema, ProjectID: project, RequestID: x.RequestID, MachineID: x.MachineID, AttemptID: attempt, CommandID: command, FencingToken: fence, SnapshotSHA256: x.SnapshotSHA256, ExpiresAt: x.ExpiresAt, Expected: expected}
	if _, _, _, err = ValidateProvisionInstallationTx(ctx, tx, x, time.Now()); err != nil {
		return nil, err
	}
	// This short mutation follows the attempt/resource locks taken above.
	var state string
	if err = tx.QueryRowContext(ctx, "SELECT state FROM provision_installations WHERE id=$1 FOR UPDATE", id).Scan(&state); err != nil {
		return nil, err
	}
	if state != "queued" {
		return nil, ErrProvisionBootDenied
	}
	spec, err := json.Marshal(x.Spec)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	if _, err = tx.ExecContext(ctx, `UPDATE provision_installations SET state='booting',state_version=state_version+1,attempt_id=$2,command_id=$3,fencing_token=$4,spec=$5,updated_at=$6 WHERE id=$1`, id, attempt, command, fence, spec, now); err != nil {
		return nil, err
	}
	if err = appendProvisionEventTx(ctx, tx, x, "boot.activated", map[string]any{"attempt_id": attempt, "fencing_token": fence, "installation_performed": false, "runtime_schema": verified.runtimeSchema, "runtime_input_sha256": verified.runtimeInputSHA256, "writer_tool_bytes_verified": verified.writerToolBytesVerified, "disk_write_authorized": false}); err != nil {
		return nil, err
	}
	x, err = LoadProvisionInstallationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	return x, tx.Commit()
}

// StopProvisionBeforeWrite can prove a safe pre-write stop even on expiry or
// cancellation. Once a permit has ever been issued, absence of a callback is
// unknown; that branch must retain the resource for actual reconciliation.
func (s *Store) StopProvisionBeforeWrite(ctx context.Context, project, id, execution string, canceled bool, reason string) (*ProvisionInstallation, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Match normal completion lock order without requiring a still-live lease.
	var executionState string
	if err = tx.QueryRowContext(ctx, "SELECT state FROM exec_executions WHERE id=$1 AND project_id=$2 FOR SHARE", execution, project).Scan(&executionState); err != nil {
		return nil, err
	}
	x, err := scanProvisionInstallation(tx.QueryRowContext(ctx, "SELECT "+provisionInstallationColumns+" FROM provision_installations WHERE id=$1 AND project_id=$2 AND execution_id=$3 FOR UPDATE", id, project, execution))
	if err != nil {
		return nil, err
	}
	if x.PermitIssuedAt != nil {
		return nil, ErrProvisionBootDenied
	}
	if x.State == "failed" || x.State == "canceled" {
		return x, nil
	}
	switch x.State {
	case "queued", "booting", "preflight_ready", "awaiting_storage_review", "storage_approved", "unknown":
	default:
		return nil, ErrProvisionBootDenied
	}
	state := "failed"
	if canceled {
		state = "canceled"
	}
	if len(reason) == 0 || len(reason) > 120 {
		return nil, ErrProvisionBootDenied
	}
	if _, err = tx.ExecContext(ctx, `UPDATE provision_installations SET state=$2,state_version=state_version+1,failure_reason=$3,updated_at=now() WHERE id=$1`, id, state, reason); err != nil {
		return nil, err
	}
	if err = appendProvisionEventTx(ctx, tx, x, "installation.stopped_before_write", map[string]any{"state": state, "reason": reason, "installation_performed": false}); err != nil {
		return nil, err
	}
	x, err = LoadProvisionInstallationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	return x, tx.Commit()
}
