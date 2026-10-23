package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"strconv"
	"time"
)

type ZTPManagementPlan struct {
	ID          string                            `json:"id,omitempty"`
	BaselineRef string                            `json:"baseline_ref"`
	BaselineID  string                            `json:"baseline_id"`
	BatchSize   int                               `json:"batch_size"`
	Rules       fabricboot.ManagementRules        `json:"rules"`
	Assignments []fabricboot.ManagementAssignment `json:"assignments"`
	SourceHash  string                            `json:"source_hash"`
	ContentHash string                            `json:"content_hash"`
	CreatedAt   *time.Time                        `json:"created_at,omitempty"`
}
type ZTPManagementPlanInput struct {
	BaselineRef  string `json:"baseline_ref"`
	ExpectedHash string `json:"expected_hash,omitempty"`
}

func managementPlanTx(ctx context.Context, tx *sql.Tx, projectID, ref string) (*ZTPManagementPlan, error) {
	out := &ZTPManagementPlan{BaselineRef: ref, BatchSize: 500}
	var limits NetworkEngineeringLimits
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT id,engineering_limits FROM network_baseline_versions WHERE project_id=$1 AND version_ref=$2 AND state='approved'`, projectID, ref).Scan(&out.BaselineID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apiError(409, "ZTP_BASELINE_NOT_APPROVED", "An approved project baseline is required")
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &limits); err != nil {
		return nil, err
	}
	if limits.Management == nil {
		return nil, apiError(409, "ZTP_MANAGEMENT_RULES_REQUIRED", "Approved baseline has no management address and batch rules")
	}
	out.Rules = *limits.Management
	var configured string
	err = tx.QueryRowContext(ctx, `SELECT default_value FROM param_keys WHERE key='fabric.batch_size' AND state='active' AND scope_key IN ($1,'global') ORDER BY CASE WHEN scope_key=$1 THEN 0 ELSE 1 END LIMIT 1`, "project:"+projectID).Scan(&configured)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if err == nil {
		out.BatchSize, err = strconv.Atoi(configured)
		if err != nil || out.BatchSize < 1 || out.BatchSize > 500 {
			return nil, apiError(409, "ZTP_BATCH_SIZE_INVALID", "fabric.batch_size must be 1 through 500")
		}
	}
	devices := []fabricboot.ManagementDevice{}
	rows, err := tx.QueryContext(ctx, `SELECT b.item_uid,coalesce(b.serial,''),coalesce(b.position->>'dc',''),coalesce(b.pod,''),coalesce(b.role,''),coalesce(b.position->>'rack',''),coalesce(b.position->>'idx','') FROM bom_items b JOIN switches s ON s.id=b.item_uid WHERE b.status<>'retired' AND EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.item_uid=b.item_uid AND r.project_id=$1) ORDER BY b.item_uid`, projectID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d fabricboot.ManagementDevice
		if err = rows.Scan(&d.ID, &d.Serial, &d.DC, &d.Pod, &d.Role, &d.Rack, &d.Index); err != nil {
			rows.Close()
			return nil, err
		}
		devices = append(devices, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	existing := []fabricboot.ManagementAssignment{}
	rows, err = tx.QueryContext(ctx, `SELECT switch_id,serial_number,hostname,host(management_ip),host(gateway),batch_ref FROM ztp_management_assignments WHERE project_id=$1 ORDER BY switch_id`, projectID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a fabricboot.ManagementAssignment
		if err = rows.Scan(&a.SwitchID, &a.Serial, &a.Hostname, &a.Address, &a.Gateway, &a.Batch); err != nil {
			rows.Close()
			return nil, err
		}
		existing = append(existing, a)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out.SourceHash = fabricboot.ManagementPlanDigest(struct {
		Devices    []fabricboot.ManagementDevice
		BaselineID string
		Rules      fabricboot.ManagementRules
		BatchSize  int
	}{devices, out.BaselineID, out.Rules, out.BatchSize})
	out.Assignments, err = fabricboot.BuildManagementPlan(out.Rules, devices, existing, out.BatchSize)
	if err != nil {
		return nil, apiError(409, "ZTP_MANAGEMENT_PLAN_BLOCKED", err.Error())
	}
	out.ContentHash = fabricboot.ManagementPlanDigest(out)
	return out, nil
}
func (s *Store) PreviewZTPManagementPlan(ctx context.Context, a *AuthSession, p *ProjectSummary, input ZTPManagementPlanInput) (*ZTPManagementPlan, error) {
	if !bomProfileVersionPattern.MatchString(input.BaselineRef) {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid baseline reference")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	out, e := managementPlanTx(ctx, tx, p.ID, input.BaselineRef)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) CommitZTPManagementPlan(ctx context.Context, a *AuthSession, p *ProjectSummary, input ZTPManagementPlanInput, trace string) (*ZTPManagementPlan, error) {
	if !bomProfileVersionPattern.MatchString(input.BaselineRef) || len(input.ExpectedHash) != 64 {
		return nil, apiError(400, "VALIDATION_ERROR", "Preview baseline and digest are required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:plan"); e != nil {
		return nil, e
	}
	out, e := managementPlanTx(ctx, tx, p.ID, input.BaselineRef)
	if e != nil {
		return nil, e
	}
	if out.ContentHash != input.ExpectedHash {
		return nil, apiError(409, "ZTP_MANAGEMENT_PREVIEW_STALE", "Baseline, BOM, parameter or allocation changed; preview again")
	}
	var old []byte
	e = tx.QueryRowContext(ctx, `SELECT report FROM ztp_management_plans WHERE project_id=$1 AND content_hash=$2`, p.ID, out.ContentHash).Scan(&old)
	if e == nil {
		var saved ZTPManagementPlan
		if e = json.Unmarshal(old, &saved); e != nil {
			return nil, e
		}
		return &saved, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	now := time.Now().UTC()
	out.ID = RandomID()
	out.CreatedAt = &now
	raw, _ := json.Marshal(out)
	_, e = tx.ExecContext(ctx, `INSERT INTO ztp_management_plans(id,project_id,baseline_id,content_hash,report,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, out.ID, p.ID, out.BaselineID, out.ContentHash, raw, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	for _, v := range out.Assignments {
		_, e = tx.ExecContext(ctx, `INSERT INTO ztp_management_assignments(project_id,switch_id,serial_number,hostname,management_ip,gateway,batch_ref,plan_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(project_id,switch_id) DO NOTHING`, p.ID, v.SwitchID, v.Serial, v.Hostname, v.Address, v.Gateway, v.Batch, out.ID)
		if e != nil {
			return nil, e
		}
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "ztp.management.plan", out.ID, trace, map[string]any{"baseline_ref": out.BaselineRef, "content_hash": out.ContentHash, "switch_count": len(out.Assignments), "batch_size": out.BatchSize}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) LatestZTPManagementPlan(ctx context.Context, a *AuthSession, p *ProjectSummary) (*ZTPManagementPlan, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT report FROM ztp_management_plans WHERE project_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, p.ID).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if e != nil {
		return nil, e
	}
	var out ZTPManagementPlan
	if e = json.Unmarshal(raw, &out); e != nil {
		return nil, e
	}
	return &out, tx.Commit()
}
