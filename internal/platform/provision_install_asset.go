package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"time"
)

type provisionActivatedAsset struct {
	AssetID         string `json:"asset_id"`
	SourceVersion   int64  `json:"source_version"`
	ActiveVersion   int64  `json:"active_version"`
	PreviousState   string `json:"previous_state"`
	PreviousTrust   string `json:"previous_ssh_trust"`
	PreviousHostPin string `json:"previous_host_fingerprint"`
}

// ActivateProvisionAssetForWriteTx is called inside the one-time permit
// transaction, after the owning exclusive execution and immutable source were
// revalidated. Failure rolls back both the asset transition and the permit.
// Old proven domain identity is retained as historical binding, never cleared.
func ActivateProvisionAssetForWriteTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest) (map[string]any, error) {
	if x == nil || request == nil || x.PermitIssuedAt != nil || x.State != "storage_approved" || x.RequestID != request.ID || x.MachineID != request.MachineID || x.ProjectID != request.ProjectID || request.Snapshot.Target.AssetVersion < 1 || request.Snapshot.Target.AssetVersion == math.MaxInt64 {
		return nil, ErrProvisionBootDenied
	}
	asset := provisionActivatedAsset{AssetID: request.AssetID, SourceVersion: request.Snapshot.Target.AssetVersion}
	var version int64
	if err := tx.QueryRowContext(ctx, `SELECT a.version,a.state,m.ssh_trust,m.host_key_fingerprint FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1 AND a.project_id=$2 AND a.machine_id=$3 FOR UPDATE OF a,m`, request.AssetID, x.ProjectID, x.MachineID).Scan(&version, &asset.PreviousState, &asset.PreviousTrust, &asset.PreviousHostPin); err != nil {
		return nil, err
	}
	if version != asset.SourceVersion || asset.PreviousState == "provisioning" || (!request.Reinstall && (asset.PreviousState == "provisioned" || asset.PreviousTrust == "established")) {
		return nil, ErrProvisionBootDenied
	}
	if err := tx.QueryRowContext(ctx, `UPDATE project_assets SET state='provisioning',version=version+1,updated_at=now() WHERE id=$1 AND version=$2 RETURNING version`, asset.AssetID, asset.SourceVersion).Scan(&asset.ActiveVersion); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE machines SET ssh_trust='unverified',trust_updated_at=now() WHERE id=$1`, x.MachineID); err != nil {
		return nil, err
	}
	return map[string]any{"asset": asset}, nil
}

func validateProvisionInstallationMetadataTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation, request *ProvisionRequest, now time.Time) error {
	if x.PermitIssuedAt == nil {
		return ValidateProvisionRequestMetadataTx(ctx, tx, request, now)
	}
	switch x.State {
	case "installing", "awaiting_first_boot", "unknown":
	default:
		return ErrProvisionBootDenied
	}
	var raw []byte
	if err := tx.QueryRowContext(ctx, `SELECT payload->'asset' FROM provision_installation_events WHERE installation_id=$1 AND event_type='write.permit_issued' ORDER BY id DESC LIMIT 1`, x.ID).Scan(&raw); err != nil {
		return err
	}
	var asset provisionActivatedAsset
	if json.Unmarshal(raw, &asset) != nil || asset.AssetID != request.AssetID || asset.SourceVersion != request.Snapshot.Target.AssetVersion || asset.SourceVersion < 1 || asset.SourceVersion == math.MaxInt64 || asset.ActiveVersion != asset.SourceVersion+1 {
		return ErrProvisionBootDenied
	}
	return validateProvisionRequestMetadataTx(ctx, tx, request, now, asset.ActiveVersion, true)
}
