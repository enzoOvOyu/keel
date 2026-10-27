package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
)

type BMCCollectionInput struct {
	Level                string `json:"level"`
	ExpectedAssetVersion int64  `json:"expected_asset_version"`
	Reason               string `json:"reason"`
}
type BMCCollectionAdmission struct {
	Target       bmc.Target `json:"target"`
	Level        string     `json:"level"`
	Origin       string     `json:"origin"`
	Hostname     string     `json:"hostname"`
	ModelVersion string     `json:"model_version"`
}

func validBMCLevel(level string) bool { return level == "L1" || level == "L2" || level == "L3" }
func (s *Store) PrepareBMCCollectionTx(ctx context.Context, tx *sql.Tx, project, asset, request string, in BMCCollectionInput) (*BMCCollectionAdmission, error) {
	if !s.BMCInventoryEnabled || s.BMCInventory == nil || s.MediaObjects == nil {
		return nil, apiError(409, "BMC_INVENTORY_DISABLED", "BMC inventory requires an enabled adapter and durable raw storage")
	}
	if !validBMCLevel(in.Level) || in.ExpectedAssetVersion < 1 || len(strings.TrimSpace(in.Reason)) < 3 || len(in.Reason) > 1000 {
		return nil, apiError(400, "BMC_COLLECTION_INVALID", "Level, exact asset version and reason required")
	}
	out := &BMCCollectionAdmission{Level: in.Level, Target: bmc.Target{RequestID: request, ProjectID: project, AssetID: asset}}
	var address string
	e := tx.QueryRowContext(ctx, `SELECT a.machine_id,a.version,a.hostname,coalesce(a.model_version_id,''),m.serial_number,m.bmc_address FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1 AND a.project_id=$2 FOR SHARE OF a,m`, asset, project).Scan(&out.Target.MachineID, &out.Target.AssetVersion, &out.Hostname, &out.ModelVersion, &out.Target.Serial, &address)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if out.Target.AssetVersion != in.ExpectedAssetVersion {
		return nil, apiError(409, "BMC_ASSET_CHANGED", "Project asset version changed")
	}
	selection, e := s.BMCInventory.Selection(project, out.Target.MachineID)
	if e != nil {
		return nil, apiError(409, "BMC_TARGET_UNCONFIGURED", "No operator-owned inventory binding for this project machine")
	}
	out.Origin = selection.Origin
	out.Target.CredentialBatchRef = selection.CredentialBatchRef
	out.Target.AdapterBindingHash = selection.BindingHash
	if e = checkBMCBOMBatchTx(ctx, tx, &out.Target); e != nil {
		return nil, e
	}
	// The operator owns routing; a supplied server BMC address, when available,
	// remains an independent pin. URLs/credentials never come from this API.
	if selection.Origin == "real" && address != "" && !sameBMCAddress(address, selection.Hostname) {
		return nil, apiError(409, "BMC_ADDRESS_MISMATCH", "Configured BMC origin differs from the registered machine address")
	}
	if selection.Origin == "sim" && selection.Hostname == "" {
		out.Target.CredentialVersion = "sim-v1"
	} else {
		e = tx.QueryRowContext(ctx, `SELECT v.credential_version FROM device_credential_heads h JOIN device_credential_versions v USING(project_id,credential_batch_ref,scope,credential_version) WHERE h.project_id=$1 AND h.credential_batch_ref=$2 AND h.scope='bmc' AND v.expires_at>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM device_credential_revocations r WHERE (r.project_id,r.credential_batch_ref,r.scope,r.credential_version)=(v.project_id,v.credential_batch_ref,v.scope,v.credential_version)) FOR SHARE OF h`, project, selection.CredentialBatchRef).Scan(&out.Target.CredentialVersion)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(409, "BMC_CREDENTIAL_REQUIRED", "A current unrevoked BMC credential batch version is required")
		}
		if e != nil {
			return nil, e
		}
	}
	if e = out.Target.Validate(); e != nil {
		return nil, e
	}
	return out, nil
}
func sameBMCAddress(address, hostname string) bool {
	a, b := net.ParseIP(address), net.ParseIP(hostname)
	if a != nil && b != nil {
		return a.Equal(b)
	}
	return strings.EqualFold(strings.TrimSuffix(address, "."), strings.TrimSuffix(hostname, "."))
}
func checkBMCBOMBatchTx(ctx context.Context, tx *sql.Tx, target *bmc.Target) error {
	var batch, packageID string
	e := tx.QueryRowContext(ctx, `SELECT p.id,coalesce(r.row_snapshot#>>'{values,credential_batch_ref}','') FROM bom_item_revisions r JOIN bom_import_packages p ON p.id=r.package_id AND p.project_id=r.project_id AND p.state='committed' WHERE r.project_id=$1 AND r.item_uid=$2 ORDER BY p.committed_at DESC,p.id DESC LIMIT 1`, target.ProjectID, target.MachineID).Scan(&packageID, &batch)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if target.BOMPackageID != "" && target.BOMPackageID != packageID {
		return apiError(409, "BMC_BOM_CHANGED", "Frozen server BOM revision changed")
	}
	if target.BOMPackageID == "" {
		target.BOMPackageID = packageID
	}
	if batch != target.CredentialBatchRef {
		return apiError(409, "BMC_BOM_CREDENTIAL_MISMATCH", "Inventory requires the current committed project BOM credential batch")
	}
	var exists bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bom_credential_batches WHERE project_id=$1 AND credential_batch_ref=$2 AND scope='bmc')`, target.ProjectID, batch).Scan(&exists)
	if e != nil {
		return e
	}
	if !exists {
		return apiError(409, "BMC_BOM_CREDENTIAL_MISMATCH", "Registered BMC credential batch is required")
	}
	return nil
}
func (s *Store) AttachBMCCollectionTx(ctx context.Context, tx *sql.Tx, a *AuthSession, execution string, admission *BMCCollectionAdmission, reason, trace string) error {
	target := admission.Target
	_, e := tx.ExecContext(ctx, `INSERT INTO bmc_collection_requests(id,project_id,asset_id,machine_id,asset_version,level,origin,credential_batch_ref,credential_version,target,execution_id,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,now())`, target.RequestID, target.ProjectID, target.AssetID, target.MachineID, target.AssetVersion, admission.Level, admission.Origin, target.CredentialBatchRef, target.CredentialVersion, networkJSON(target), execution, a.User.ID)
	if e != nil {
		return e
	}
	return appendAudit(ctx, tx, a.User.ID, target.ProjectID, "bmc.collection.request", target.RequestID, trace, map[string]any{"execution_id": execution, "asset_id": target.AssetID, "level": admission.Level, "origin": admission.Origin, "credential_batch_ref": target.CredentialBatchRef, "credential_version": target.CredentialVersion, "reason": strings.TrimSpace(reason)})
}
func (s *Store) BMCCollectionFrozenTx(ctx context.Context, tx *sql.Tx, request, execution string) (*BMCCollectionAdmission, error) {
	if !s.BMCInventoryEnabled || s.BMCInventory == nil {
		return nil, apiError(409, "BMC_INVENTORY_DISABLED", "BMC inventory adapter is disabled")
	}
	var expired bool
	if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bmc_credential_verification_targets t JOIN bmc_credential_verifications q ON q.id=t.verification_id WHERE t.collection_id=$1 AND t.execution_id=$2 AND (q.expires_at<=statement_timestamp() OR EXISTS(SELECT 1 FROM bmc_credential_verification_results r WHERE r.verification_id=q.id)))`, request, execution).Scan(&expired); e != nil {
		return nil, e
	}
	if expired {
		return nil, apiError(409, "BMC_VERIFICATION_CLOSED", "Frozen full-batch verification window is closed")
	}
	var raw []byte
	out := &BMCCollectionAdmission{}
	e := tx.QueryRowContext(ctx, `SELECT target,level,origin FROM bmc_collection_requests WHERE id=$1 AND execution_id=$2`, request, execution).Scan(&raw, &out.Level, &out.Origin)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &out.Target); e != nil {
		return nil, e
	}
	if out.Target.BOMPackageID == "" {
		return nil, apiError(409, "BMC_BOM_REQUIRED", "A frozen server BOM revision is required")
	}
	var current bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1 AND a.project_id=$2 AND a.machine_id=$3 AND a.version=$4 AND m.serial_number=$5)`, out.Target.AssetID, out.Target.ProjectID, out.Target.MachineID, out.Target.AssetVersion, out.Target.Serial).Scan(&current)
	if e != nil {
		return nil, e
	}
	if !current {
		return nil, apiError(409, "BMC_ASSET_CHANGED", "Frozen inventory asset identity changed")
	}
	selection, e := s.BMCInventory.Selection(out.Target.ProjectID, out.Target.MachineID)
	if e != nil || selection.BindingHash != out.Target.AdapterBindingHash || selection.Origin != out.Origin {
		return nil, apiError(409, "BMC_BINDING_CHANGED", "Frozen BMC endpoint or trust configuration changed")
	}
	if e = checkBMCBOMBatchTx(ctx, tx, &out.Target); e != nil {
		return nil, e
	}
	// Deliberately do not compare the current head: in-flight jobs retain their
	// exact old credential version across a batch rotation until expiry/revocation.
	return out, nil
}

// BeginBMCCollectionDispatch commits one original command before any BMC IO.
// Unknown dispatches cannot re-enter this adapter, even though it is read-only.
func (s *Store) beginBMCCollectionDispatch(ctx context.Context, request, execution, attempt, command string, fence int64) (*BMCCollectionAdmission, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	var executionID string
	e = tx.QueryRowContext(ctx, `SELECT id FROM exec_executions WHERE id=$1 AND mode='bmc' FOR UPDATE`, execution).Scan(&executionID)
	if e != nil {
		return nil, e
	}
	admission, e := s.BMCCollectionFrozenTx(ctx, tx, request, execution)
	if e != nil {
		return nil, e
	}
	var dispatched bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bmc_collection_dispatches WHERE request_id=$1)`, request).Scan(&dispatched)
	if e != nil {
		return nil, e
	}
	if dispatched {
		return nil, apiError(409, "BMC_COLLECTION_ALREADY_DISPATCHED", "Original inventory command is already dispatched; recover its receipt without recollection")
	}
	var valid bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id JOIN bmc_collection_requests q ON q.execution_id=x.id JOIN users u ON u.id=x.created_by JOIN projects p ON p.id=x.project_id JOIN exec_targets t ON t.id=a.target_id WHERE a.id=$1 AND a.execution_id=$2 AND a.command_id=$3 AND a.fencing_token=$4 AND a.runner='bmc' AND a.state='running' AND a.lease_until>clock_timestamp() AND q.id=$5 AND t.resource_key=q.machine_id AND x.desired_state='running' AND p.state NOT IN('archived','delivered') AND u.enabled AND (u.is_admin OR EXISTS(SELECT 1 FROM memberships m WHERE m.user_id=u.id AND m.project_id=x.project_id AND m.role IN('Admin','DE','DL','SRE'))) AND (SELECT count(*) FROM exec_resource_leases l WHERE l.attempt_id=a.id AND l.fencing_token=a.fencing_token AND l.state='held' AND l.expires_at>clock_timestamp() AND ((l.resource_id='device:machine:'||q.machine_id AND l.mode='shared') OR (l.resource_id='bmc:machine:'||q.machine_id AND l.mode='exclusive')))=2)`, attempt, execution, command, fence, request).Scan(&valid)
	if e != nil {
		return nil, e
	}
	if !valid {
		return nil, apiError(409, "BMC_COLLECTION_FENCE_LOST", "Current inventory command, authority and both fenced resources required")
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO bmc_collection_dispatches(request_id,execution_id,attempt_id,command_id,fencing_token,dispatched_at,deadline) VALUES($1,$2,$3,$4,$5,statement_timestamp(),statement_timestamp()+interval '15 minutes')`, request, execution, attempt, command, fence)
	if e != nil {
		return nil, e
	}
	return admission, tx.Commit()
}
func (s *Store) RunBMCCollection(ctx context.Context, request, execution, attempt, command string, fence int64) (*bmc.Snapshot, error) {
	admission, e := s.beginBMCCollectionDispatch(ctx, request, execution, attempt, command, fence)
	if e != nil {
		return nil, e
	}
	work, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	reader, e := s.BMCInventory.Reader(admission.Target)
	if e != nil {
		return nil, apiError(409, "BMC_BINDING_CHANGED", "BMC reader binding is unavailable")
	}
	if closer, ok := reader.(io.Closer); ok {
		defer closer.Close()
	}
	fallback, e := s.BMCInventory.Fallback(admission.Target)
	if e != nil {
		return nil, apiError(409, "BMC_BINDING_CHANGED", "BMC fallback binding is unavailable")
	}
	collector := bmc.Collector{Reader: reader, Fallback: fallback, Raw: s.BMCInventoryRawSink(), Config: bmc.DefaultConfig()}
	return collector.Collect(work, admission.Target, admission.Level)
}
