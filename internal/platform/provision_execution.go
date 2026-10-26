package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// LoadProvisionRequestTx pins the request used by the shared execution kernel.
// Its caller authorizes the actor before taking this domain row lock.
func LoadProvisionRequestTx(ctx context.Context, tx *sql.Tx, project, id string) (*ProvisionRequest, error) {
	return scanProvisionRequest(tx.QueryRowContext(ctx, "SELECT "+provisionRequestColumns+" FROM provision_requests WHERE project_id=$1 AND id=$2 FOR SHARE", project, id))
}

// ValidateProvisionRequestMetadataTx does not hash objects or execute hardware.
// It guards admission/dispatch against current BOM, approval and key revocation.
func ValidateProvisionRequestMetadataTx(ctx context.Context, tx *sql.Tx, x *ProvisionRequest, now time.Time) error {
	if !x.ExpiresAt.After(now) || x.ApprovedBy == nil {
		return apiError(409, "PROVISION_REQUEST_EXPIRED", "Unexpired independently approved preparation required")
	}
	var approver bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN projects p ON p.id=$2 WHERE u.id=$1 AND u.enabled AND p.state NOT IN('archived','delivered') AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.project_id=p.id AND m.user_id=u.id AND m.role IN('Admin','DL'))))`, *x.ApprovedBy, x.ProjectID).Scan(&approver); err != nil {
		return err
	}
	if !approver {
		return apiError(409, "PROVISION_APPROVAL_REVOKED", "Approver no longer has authority")
	}
	var assetVersion int64
	var model, state, serial, hostname, trust string
	if err := tx.QueryRowContext(ctx, `SELECT a.version,COALESCE(a.model_version_id,''),a.state,m.serial_number,a.hostname,m.ssh_trust FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1 AND a.project_id=$2 AND a.machine_id=$3 FOR SHARE OF a,m`, x.AssetID, x.ProjectID, x.MachineID).Scan(&assetVersion, &model, &state, &serial, &hostname, &trust); err != nil {
		return err
	}
	expected := x.Snapshot.Target
	if assetVersion != expected.AssetVersion || model != expected.ModelVersionID || serial != expected.Serial || hostname != expected.Hostname || state == "provisioning" || ((!x.Reinstall) && (state == "provisioned" || trust == "established")) {
		return apiError(409, "PROVISION_TARGET_CHANGED", "Frozen asset identity or reinstall admission changed")
	}
	var mac string
	if err := tx.QueryRowContext(ctx, "SELECT mac_address FROM physical_ports WHERE id=$1 AND machine_id=$2 FOR SHARE", x.Snapshot.BootPort.ID, x.MachineID).Scan(&mac); err != nil {
		return err
	}
	if mac != x.Snapshot.BootPort.MAC {
		return apiError(409, "PROVISION_TARGET_CHANGED", "Frozen boot port identity changed")
	}
	if _, err := ReadSSHInstallCredential(ctx, tx, x.ProjectID, x.Snapshot.DomainKey, now.Add(time.Minute)); err != nil {
		return err
	}
	// The signed immutable role closure is fully re-read by the actual preparation task.
	for _, artifact := range x.Snapshot.Media.Artifacts {
		current, err := mediaDependencyRecord(ctx, tx, artifact.MediaID)
		if err != nil {
			return err
		}
		var objectVersion string
		if err = tx.QueryRowContext(ctx, "SELECT object_version FROM media_artifacts WHERE media_id=$1 FOR SHARE", artifact.MediaID).Scan(&objectVersion); err != nil {
			return err
		}
		if current.State != "approved" || current.Sha256 != artifact.SHA256 || objectVersion != artifact.ObjectVersion {
			return apiError(409, "PROVISION_MEDIA_CHANGED", "Frozen boot artifact is no longer approved with matching bytes")
		}
	}
	return nil
}
func (s *Store) VerifyProvisionRequestTx(ctx context.Context, tx *sql.Tx, a *AuthSession, x *ProvisionRequest) (*ProvisionPreview, error) {
	if err := requireProvisionAuthority(ctx, tx, a, x.ProjectID, false); err != nil {
		return nil, err
	}
	if err := ValidateProvisionRequestMetadataTx(ctx, tx, x, time.Now()); err != nil {
		return nil, err
	}
	frozen := x.Snapshot
	input := ProvisionPreviewInput{AssetVersion: frozen.Target.AssetVersion, BootPortID: frozen.BootPort.ID, MediaID: frozen.Media.RootID, DomainKeyID: frozen.DomainKey.ID}
	plan, err := s.freezeProvisionUntilTx(ctx, tx, a, &ProjectSummary{ID: x.ProjectID}, x.AssetID, input, x.ID, x.ExpiresAt.Add(time.Minute))
	if err != nil {
		return nil, err
	}
	if plan.SnapshotSHA256 != x.SnapshotSHA256 {
		return nil, apiError(409, "PROVISION_SNAPSHOT_CHANGED", "Current source differs from the approved installation preparation")
	}
	return plan, nil
}

// ApplyProvisionPreparationReceiptTx changes preparation evidence only. It never
// updates project_assets.state, machines.ssh_trust, DHCP, boot settings or keys.
func ApplyProvisionPreparationReceiptTx(ctx context.Context, tx *sql.Tx, executionID, requestID, snapshot, result string, at time.Time) error {
	x, err := scanProvisionRequest(tx.QueryRowContext(ctx, "SELECT "+provisionRequestColumns+" FROM provision_requests WHERE id=$1 FOR UPDATE", requestID))
	if err != nil {
		return err
	}
	if x.ExecutionID == nil || *x.ExecutionID != executionID || x.SnapshotSHA256 != snapshot {
		return apiError(409, "PROVISION_RECEIPT_MISMATCH", "Preparation receipt differs from frozen request")
	}
	state := "failed"
	if result == "success" {
		state = "prepared"
	} else if result == "cancelled" {
		state = "canceled"
	}
	if x.State == state {
		return nil
	}
	if x.State != "preparing" {
		return apiError(409, "PROVISION_REQUEST_STATE_CHANGED", "Request is not preparing")
	}
	_, err = tx.ExecContext(ctx, "UPDATE provision_requests SET state=$2,state_version=state_version+1,updated_at=$3 WHERE id=$1", requestID, state, at)
	if err != nil {
		return err
	}
	return appendAudit(ctx, tx, "", x.ProjectID, "provision.preparation.completed", requestID, "worker", map[string]any{"execution_id": executionID, "snapshot_sha256": snapshot, "result": result, "installation_performed": false})
}
func ProvisionSnapshotValid(x *ProvisionRequest) bool {
	copy := x.Snapshot
	copy.SnapshotSHA256 = ""
	raw, err := json.Marshal(copy)
	return err == nil && digest(string(raw)) == x.SnapshotSHA256 && x.Snapshot.SnapshotSHA256 == x.SnapshotSHA256 && x.ProjectID == x.Snapshot.Target.ProjectID && x.MachineID == x.Snapshot.Target.MachineID && x.AssetID == x.Snapshot.Target.AssetID
}
