package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const BMCVerificationTargetLimit = 200

type BMCCredentialBatch struct {
	BatchRef          string     `json:"credential_batch_ref"`
	CurrentVersion    string     `json:"credential_version"`
	ExpiresAt         *time.Time `json:"expires_at"`
	VerificationState string     `json:"verification_state"`
	Revoked           bool       `json:"revoked"`
}
type BMCCredentialVerificationTarget struct {
	AssetID            string `json:"asset_id"`
	MachineID          string `json:"machine_id"`
	AssetVersion       int64  `json:"asset_version"`
	Serial             string `json:"serial"`
	Hostname           string `json:"hostname"`
	BOMPackageID       string `json:"bom_package_id"`
	AdapterBindingHash string `json:"adapter_binding_hash"`
	Origin             string `json:"origin"`
	Code               string `json:"code"`
}
type BMCCredentialVerificationScope struct {
	ProjectID           string                            `json:"project_id"`
	BatchRef            string                            `json:"credential_batch_ref"`
	CredentialVersion   string                            `json:"credential_version"`
	CredentialExpiresAt time.Time                         `json:"credential_expires_at"`
	ScopeHash           string                            `json:"scope_hash"`
	TargetCount         int                               `json:"target_count"`
	Ready               bool                              `json:"ready"`
	Code                string                            `json:"code"`
	Targets             []BMCCredentialVerificationTarget `json:"targets"`
}
type BMCCredentialVerification struct {
	ID                string                              `json:"id"`
	ProjectID         string                              `json:"project_id"`
	BatchRef          string                              `json:"credential_batch_ref"`
	CredentialVersion string                              `json:"credential_version"`
	ScopeHash         string                              `json:"scope_hash"`
	Scope             BMCCredentialVerificationScope      `json:"scope"`
	CreatedBy         string                              `json:"created_by"`
	CreatedAt         time.Time                           `json:"created_at"`
	ExpiresAt         time.Time                           `json:"expires_at"`
	Outcome           string                              `json:"outcome"`
	Code              string                              `json:"code"`
	ResultID          *string                             `json:"result_id"`
	FinishedAt        *time.Time                          `json:"finished_at"`
	Targets           []BMCCredentialVerificationProgress `json:"targets"`
}
type BMCCredentialVerificationProgress struct {
	AssetID        string `json:"asset_id"`
	MachineID      string `json:"machine_id"`
	ExecutionID    string `json:"execution_id"`
	CollectionID   string `json:"collection_id"`
	ExecutionState string `json:"execution_state"`
	Outcome        string `json:"outcome"`
	Code           string `json:"code"`
	EvidenceHash   string `json:"evidence_hash"`
	Origin         string `json:"origin"`
}

// Current membership is checked by the caller in the same transaction. Domain
// work is always admitted by execution.Service; this method only freezes facts.
func (s *Store) BMCCredentialVerificationScopeTx(ctx context.Context, tx *sql.Tx, project, batch string, lock bool) (*BMCCredentialVerificationScope, error) {
	out := &BMCCredentialVerificationScope{ProjectID: project, BatchRef: batch, Targets: []BMCCredentialVerificationTarget{}, Ready: true, Code: "BMC_VERIFICATION_READY"}
	from := ` FROM project_assets a JOIN machines m ON m.id=a.machine_id JOIN bom_items item ON item.item_uid=a.machine_id AND item.status<>'retired' JOIN LATERAL (SELECT v.package_id,v.row_snapshot#>>'{values,credential_batch_ref}' AS batch_ref FROM bom_item_revisions v JOIN bom_import_packages p ON p.id=v.package_id AND p.project_id=v.project_id AND p.state='committed' WHERE v.project_id=a.project_id AND v.item_uid=a.machine_id ORDER BY p.committed_at DESC,p.id DESC LIMIT 1) b ON b.batch_ref=$2 WHERE a.project_id=$1`
	if e := tx.QueryRowContext(ctx, `SELECT count(*)`+from, project, batch).Scan(&out.TargetCount); e != nil {
		return nil, e
	}
	query := `SELECT a.id,a.machine_id,a.version,m.serial_number,a.hostname,b.package_id` + from + ` ORDER BY a.id LIMIT 201`
	if lock {
		query += ` FOR SHARE OF a,m`
	}
	rows, e := tx.QueryContext(ctx, query, project, batch)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var t BMCCredentialVerificationTarget
		if e = rows.Scan(&t.AssetID, &t.MachineID, &t.AssetVersion, &t.Serial, &t.Hostname, &t.BOMPackageID); e != nil {
			rows.Close()
			return nil, e
		}
		out.Targets = append(out.Targets, t)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	var revoked bool
	headQuery := `SELECT h.credential_version,v.expires_at,EXISTS(SELECT 1 FROM device_credential_revocations r WHERE (r.project_id,r.credential_batch_ref,r.scope,r.credential_version)=(v.project_id,v.credential_batch_ref,v.scope,v.credential_version)) FROM device_credential_heads h JOIN device_credential_versions v USING(project_id,credential_batch_ref,scope,credential_version) WHERE h.project_id=$1 AND h.credential_batch_ref=$2 AND h.scope='bmc'`
	if lock {
		headQuery += ` FOR SHARE OF h`
	}
	e = tx.QueryRowContext(ctx, headQuery, project, batch).Scan(&out.CredentialVersion, &out.CredentialExpiresAt, &revoked)
	if errors.Is(e, sql.ErrNoRows) {
		out.Ready = false
		out.Code = "BMC_CREDENTIAL_REQUIRED"
	} else if e != nil {
		return nil, e
	} else if revoked || !out.CredentialExpiresAt.After(time.Now()) {
		out.Ready = false
		out.Code = "BMC_CREDENTIAL_UNAVAILABLE"
	}
	if len(out.Targets) == 0 {
		out.Ready = false
		out.Code = "BMC_VERIFICATION_EMPTY_SCOPE"
	}
	if out.TargetCount > BMCVerificationTargetLimit {
		out.Ready = false
		out.Code = "BMC_VERIFICATION_SCOPE_TOO_LARGE"
	}
	for i := range out.Targets {
		t := &out.Targets[i]
		t.Code = "BMC_TARGET_READY"
		if s.BMCInventory == nil {
			t.Code = "BMC_TARGET_UNCONFIGURED"
		} else if selected, e := s.BMCInventory.Selection(project, t.MachineID); e != nil || selected.CredentialBatchRef != batch {
			t.Code = "BMC_TARGET_UNCONFIGURED"
		} else {
			t.AdapterBindingHash = selected.BindingHash
			t.Origin = selected.Origin
			if selected.Hostname == "" {
				t.Code = "BMC_CREDENTIAL_NOT_EXERCISED"
			}
		}
		if t.Code != "BMC_TARGET_READY" {
			out.Ready = false
			if out.Code == "BMC_VERIFICATION_READY" {
				out.Code = t.Code
			}
		}
	}
	if out.TargetCount <= BMCVerificationTargetLimit && out.TargetCount != len(out.Targets) {
		out.Ready = false
		out.Code = "BMC_VERIFICATION_SCOPE_CHANGED"
	}
	if !s.BMCInventoryEnabled {
		out.Ready = false
		out.Code = "BMC_INVENTORY_DISABLED"
	}
	// The hash also freezes adapter readiness; a changed binding requires a fresh preview.
	encoded, _ := json.Marshal(struct {
		Project, Batch, Version string
		Expires                 time.Time
		Targets                 []BMCCredentialVerificationTarget
	}{project, batch, out.CredentialVersion, out.CredentialExpiresAt, out.Targets})
	out.ScopeHash = digest(string(encoded))
	return out, nil
}
func (s *Store) ListBMCCredentialBatches(ctx context.Context, a *AuthSession, project string, page Pagination) (Page[BMCCredentialBatch], error) {
	result := NewPage[BMCCredentialBatch](page)
	if e := page.Validate(); e != nil {
		return result, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return result, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return result, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bom_credential_batches WHERE project_id=$1 AND scope='bmc'`, project).Scan(&result.Total); e != nil {
		return result, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT b.credential_batch_ref,coalesce(h.credential_version,''),v.expires_at,CASE WHEN keel_bmc_credential_verified(b.project_id,b.credential_batch_ref,h.credential_version) THEN 'verified' ELSE 'pending' END,coalesce((SELECT q.scope FROM bmc_credential_verification_results proof JOIN bmc_credential_verifications q ON q.id=proof.verification_id WHERE proof.id=h.verification_receipt_id),'{}'::jsonb),coalesce(EXISTS(SELECT 1 FROM device_credential_revocations r WHERE (r.project_id,r.credential_batch_ref,r.scope,r.credential_version)=(h.project_id,h.credential_batch_ref,h.scope,h.credential_version)),false) FROM bom_credential_batches b LEFT JOIN device_credential_heads h ON h.project_id=b.project_id AND h.credential_batch_ref=b.credential_batch_ref AND h.scope='bmc' LEFT JOIN device_credential_versions v ON v.project_id=h.project_id AND v.credential_batch_ref=h.credential_batch_ref AND v.scope=h.scope AND v.credential_version=h.credential_version WHERE b.project_id=$1 AND b.scope='bmc' ORDER BY b.credential_batch_ref LIMIT $2 OFFSET $3`, project, page.PageSize, page.Offset())
	if e != nil {
		return result, e
	}
	defer rows.Close()
	out := []BMCCredentialBatch{}
	for rows.Next() {
		var item BMCCredentialBatch
		var raw []byte
		if e = rows.Scan(&item.BatchRef, &item.CurrentVersion, &item.ExpiresAt, &item.VerificationState, &raw, &item.Revoked); e != nil {
			return result, e
		}
		if item.VerificationState == "verified" {
			var scope BMCCredentialVerificationScope
			if e = json.Unmarshal(raw, &scope); e != nil {
				return result, e
			}
			if !s.bmcVerificationBindingsCurrent(scope) {
				item.VerificationState = "pending"
			}
		}
		out = append(out, item)
	}
	result.Items = out
	return result, rows.Err()
}

func (s *Store) bmcVerificationBindingsCurrent(scope BMCCredentialVerificationScope) bool {
	if !s.BMCInventoryEnabled || s.BMCInventory == nil {
		return false
	}
	for _, t := range scope.Targets {
		selected, e := s.BMCInventory.Selection(scope.ProjectID, t.MachineID)
		if e != nil || selected.Hostname == "" || selected.CredentialBatchRef != scope.BatchRef || selected.BindingHash != t.AdapterBindingHash || selected.Origin != t.Origin {
			return false
		}
	}
	return len(scope.Targets) > 0
}
func (s *Store) PreviewBMCCredentialVerification(ctx context.Context, a *AuthSession, project, batch string) (*BMCCredentialVerificationScope, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bom_credential_batches WHERE project_id=$1 AND credential_batch_ref=$2 AND scope='bmc')`, project, batch).Scan(&exists); e != nil {
		return nil, e
	}
	if !exists {
		return nil, ErrNotFound
	}
	return s.BMCCredentialVerificationScopeTx(ctx, tx, project, batch, false)
}

// Detail is bounded to the frozen 200 members, with no raw resource bodies or credentials.
func (s *Store) BMCCredentialVerificationTx(ctx context.Context, tx *sql.Tx, project, batch, id string) (*BMCCredentialVerification, error) {
	out := &BMCCredentialVerification{Targets: []BMCCredentialVerificationProgress{}}
	var scope []byte
	e := tx.QueryRowContext(ctx, `SELECT q.id,q.project_id,q.credential_batch_ref,q.credential_version,q.scope_hash,q.scope,q.created_by,q.created_at,q.expires_at,coalesce(r.outcome,'pending'),coalesce(r.code,'BMC_VERIFICATION_PENDING'),r.id,r.finished_at FROM bmc_credential_verifications q LEFT JOIN bmc_credential_verification_results r ON r.verification_id=q.id WHERE q.id=$1 AND q.project_id=$2 AND q.credential_batch_ref=$3`, id, project, batch).Scan(&out.ID, &out.ProjectID, &out.BatchRef, &out.CredentialVersion, &out.ScopeHash, &scope, &out.CreatedBy, &out.CreatedAt, &out.ExpiresAt, &out.Outcome, &out.Code, &out.ResultID, &out.FinishedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(scope, &out.Scope); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT t.asset_id,t.machine_id,t.execution_id,t.collection_id,x.state,coalesce(r.content_hash,''),coalesce(r.origin,t.snapshot->>'origin'),coalesce(receipt.payload#>>'{output,code}',''),coalesce(receipt.payload#>>'{output,stopped}','false')='true',coalesce(receipt.payload#>>'{output,evidence_hash}','')=coalesce(r.content_hash,'!'),coalesce(r.report->>'identity_matched','false')='true',coalesce(r.state,'') FROM bmc_credential_verification_targets t JOIN exec_executions x ON x.id=t.execution_id LEFT JOIN bmc_collection_records r ON r.id=t.collection_id LEFT JOIN exec_task_attempts a ON a.execution_id=x.id AND a.fencing_token=r.fencing_token LEFT JOIN exec_runner_receipts receipt ON receipt.attempt_id=a.id AND receipt.command_id=a.command_id AND receipt.fencing_token=a.fencing_token WHERE t.verification_id=$1 ORDER BY t.asset_id`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var p BMCCredentialVerificationProgress
		var stopped, retained, matched bool
		var state string
		if e = rows.Scan(&p.AssetID, &p.MachineID, &p.ExecutionID, &p.CollectionID, &p.ExecutionState, &p.EvidenceHash, &p.Origin, &p.Code, &stopped, &retained, &matched, &state); e != nil {
			return nil, e
		}
		p.Outcome = "pending"
		if bmcVerificationTerminal(p.ExecutionState) {
			p.Outcome = "failed"
			if stopped && retained && matched && state != "failed" && (p.ExecutionState == "success" || p.ExecutionState == "failed") {
				p.Outcome = "evidence_retained"
			}
		}
		out.Targets = append(out.Targets, p)
	}
	return out, rows.Err()
}
func bmcVerificationTerminal(state string) bool {
	return state == "success" || state == "failed" || state == "killed" || state == "warning"
}
func (s *Store) GetBMCCredentialVerification(ctx context.Context, a *AuthSession, project, batch, id string) (*BMCCredentialVerification, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	return s.BMCCredentialVerificationTx(ctx, tx, project, batch, id)
}

type BMCCredentialVerificationSummary struct {
	ID                string     `json:"id"`
	CredentialVersion string     `json:"credential_version"`
	ScopeHash         string     `json:"scope_hash"`
	TargetCount       int        `json:"target_count"`
	CreatedAt         time.Time  `json:"created_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	Outcome           string     `json:"outcome"`
	Code              string     `json:"code"`
	FinishedAt        *time.Time `json:"finished_at"`
}

func (s *Store) ListBMCCredentialVerifications(ctx context.Context, a *AuthSession, project, batch string, page Pagination) (Page[BMCCredentialVerificationSummary], error) {
	out := NewPage[BMCCredentialVerificationSummary](page)
	if e := page.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bmc_credential_verifications WHERE project_id=$1 AND credential_batch_ref=$2`, project, batch).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT q.id,q.credential_version,q.scope_hash,q.target_count,q.created_at,q.expires_at,coalesce(r.outcome,'pending'),coalesce(r.code,'BMC_VERIFICATION_PENDING'),r.finished_at FROM bmc_credential_verifications q LEFT JOIN bmc_credential_verification_results r ON r.verification_id=q.id WHERE q.project_id=$1 AND q.credential_batch_ref=$2 ORDER BY q.created_at DESC,q.id DESC LIMIT $3 OFFSET $4`, project, batch, page.PageSize, page.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var item BMCCredentialVerificationSummary
		if e = rows.Scan(&item.ID, &item.CredentialVersion, &item.ScopeHash, &item.TargetCount, &item.CreatedAt, &item.ExpiresAt, &item.Outcome, &item.Code, &item.FinishedAt); e != nil {
			return out, e
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}

// Reconciliation reads retained Execution receipts only; it never dispatches or
// retries device IO. Unknown children remain pending until explicit recovery or expiry.
func (s *Store) ReconcileBMCCredentialVerifications(ctx context.Context, limit int) (int, error) {
	if !s.BMCInventoryEnabled {
		return 0, nil
	}
	if limit < 1 || limit > 20 {
		limit = 10
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT q.id,q.project_id,q.credential_batch_ref FROM bmc_credential_verifications q WHERE NOT EXISTS(SELECT 1 FROM bmc_credential_verification_results r WHERE r.verification_id=q.id) AND (q.expires_at<=statement_timestamp() OR NOT EXISTS(SELECT 1 FROM bmc_credential_verification_targets t JOIN exec_executions x ON x.id=t.execution_id WHERE t.verification_id=q.id AND x.state NOT IN('success','failed','killed','warning'))) ORDER BY q.created_at,q.id LIMIT $1`, limit)
	if e != nil {
		return 0, e
	}
	type ref struct{ id, project, batch string }
	pending := []ref{}
	for rows.Next() {
		var r ref
		if e = rows.Scan(&r.id, &r.project, &r.batch); e != nil {
			rows.Close()
			return 0, e
		}
		pending = append(pending, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	n := 0
	for _, r := range pending {
		done, e := s.finalizeBMCCredentialVerification(ctx, r.project, r.batch, r.id)
		if e != nil {
			return n, e
		}
		if done {
			n++
		}
	}
	return n, nil
}
func (s *Store) finalizeBMCCredentialVerification(ctx context.Context, project, batch, id string) (bool, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return false, e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "keel-admin-initialization"); e != nil {
		return false, e
	}
	out, e := s.BMCCredentialVerificationTx(ctx, tx, project, batch, id)
	if e != nil {
		return false, e
	}
	if out.ResultID != nil {
		return false, nil
	}
	current, e := s.BMCCredentialVerificationScopeTx(ctx, tx, project, batch, true)
	if e != nil {
		return false, e
	}
	var scopeCurrent bool
	if e = tx.QueryRowContext(ctx, `SELECT keel_bmc_verification_scope_current($1)`, id).Scan(&scopeCurrent); e != nil {
		return false, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	outcome, code := "", ""
	if !now.Before(out.ExpiresAt) {
		outcome, code = "expired", "BMC_VERIFICATION_EXPIRED"
	} else if !scopeCurrent || !current.Ready || current.ScopeHash != out.ScopeHash || !s.bmcVerificationBindingsCurrent(out.Scope) {
		outcome, code = "stale", "BMC_VERIFICATION_SCOPE_CHANGED"
	} else {
		for _, p := range out.Targets {
			if !bmcVerificationTerminal(p.ExecutionState) {
				return false, nil
			}
		}
		var evidence, real bool
		if e = tx.QueryRowContext(ctx, `SELECT keel_bmc_verification_evidence_current($1,false),keel_bmc_verification_evidence_current($1,true)`, id).Scan(&evidence, &real); e != nil {
			return false, e
		}
		outcome, code = "failed", "BMC_VERIFICATION_FAILED"
		if evidence {
			outcome, code = "simulated", "BMC_VERIFICATION_SIMULATED"
		}
		if real {
			outcome, code = "passed", "BMC_VERIFICATION_PASSED"
		}
	}
	resultID := RandomID()
	if _, e = tx.ExecContext(ctx, `INSERT INTO bmc_credential_verification_results(id,verification_id,outcome,code,targets,finished_at) VALUES($1,$2,$3,$4,$5,$6)`, resultID, id, outcome, code, networkJSON(out.Targets), now); e != nil {
		return false, e
	}
	if outcome == "passed" {
		result, e := tx.ExecContext(ctx, `UPDATE device_credential_heads SET verification_receipt_id=$1 WHERE project_id=$2 AND credential_batch_ref=$3 AND scope='bmc' AND credential_version=$4`, resultID, project, batch, out.CredentialVersion)
		if e != nil {
			return false, e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return false, e
		}
		if n != 1 {
			return false, apiError(409, "BMC_CREDENTIAL_CHANGED", "Credential head changed before verification")
		}
	}
	if e = appendAudit(ctx, tx, out.CreatedBy, project, "bmc.credential.verify.result", id, "bmc-verification:"+id, map[string]any{"outcome": outcome, "code": code, "target_count": out.Scope.TargetCount, "credential_batch_ref": batch, "credential_version": out.CredentialVersion, "scope_hash": out.ScopeHash}); e != nil {
		return false, e
	}
	return true, tx.Commit()
}
