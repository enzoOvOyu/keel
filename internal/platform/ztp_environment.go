package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"time"
)

type ZTPEnvironmentInput struct {
	BatchRef    string `json:"batch_ref"`
	BootChannel string `json:"boot_channel"`
}
type ZTPEnvironmentReport struct {
	ID          string                   `json:"id"`
	PlanID      string                   `json:"plan_id"`
	BatchRef    string                   `json:"batch_ref"`
	BootChannel string                   `json:"boot_channel"`
	Ready       bool                     `json:"ready"`
	Checks      []fabricboot.ProbeResult `json:"checks"`
	ConfigHash  string                   `json:"config_hash"`
	CheckedAt   time.Time                `json:"checked_at"`
	ExpiresAt   time.Time                `json:"expires_at"`
}

func (s *Store) RunZTPEnvironmentCheck(ctx context.Context, a *AuthSession, p *ProjectSummary, input ZTPEnvironmentInput, trace string) (*ZTPEnvironmentReport, error) {
	if !bootRouteRef.MatchString(input.BatchRef) || (input.BootChannel != "239" && input.BootChannel != "143") {
		return nil, apiError(400, "VALIDATION_ERROR", "Batch and 239/143 channel are required")
	}
	// Authorize and bind the plan before external I/O; reauthorize and recheck the
	// binding afterward. Never hold the control lock while waiting on probes.
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:plan"); e != nil {
		tx.Rollback()
		return nil, e
	}
	plan, e := currentManagementPlanTx(ctx, tx, p.ID, input.BatchRef)
	tx.Rollback()
	planID := ""
	if plan != nil {
		planID = plan.ID
	}
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "ZTP_BATCH_PLAN_REQUIRED", "Save an approved-rule management plan for this batch first")
	}
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	report := &ZTPEnvironmentReport{ID: RandomID(), PlanID: planID, BatchRef: input.BatchRef, BootChannel: input.BootChannel, Ready: true, CheckedAt: time.Now().UTC()}
	report.Checks = s.FabricProbes.Check(ctx, input.BootChannel)
	report.ConfigHash = fabricboot.ManagementPlanDigest("not-configured")
	if s.FabricProbes != nil {
		report.ConfigHash = s.FabricProbes.Fingerprint
	}
	for _, check := range report.Checks {
		if check.State != "ready" {
			report.Ready = false
		}
	}
	report.ExpiresAt = report.CheckedAt.Add(5 * time.Minute)
	tx, e = s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:plan"); e != nil {
		return nil, e
	}
	current, e := currentManagementPlanTx(ctx, tx, p.ID, input.BatchRef)
	if e != nil {
		return nil, e
	}
	if current.ID != planID {
		return nil, apiError(409, "ZTP_ENVIRONMENT_PLAN_CHANGED", "Batch plan changed; run checks again")
	}
	raw, _ := json.Marshal(report)
	_, e = tx.ExecContext(ctx, `INSERT INTO ztp_environment_checks(id,project_id,plan_id,batch_ref,boot_channel,config_hash,ready,report,checked_by,checked_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, report.ID, p.ID, planID, report.BatchRef, report.BootChannel, report.ConfigHash, report.Ready, raw, a.User.ID, report.CheckedAt, report.ExpiresAt)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "ztp.environment.check", report.ID, trace, map[string]any{"batch_ref": report.BatchRef, "plan_id": planID, "ready": report.Ready, "config_hash": report.ConfigHash}); e != nil {
		return nil, e
	}
	return report, tx.Commit()
}
func (s *Store) LatestZTPEnvironmentCheck(ctx context.Context, a *AuthSession, p *ProjectSummary, input ZTPEnvironmentInput) (*ZTPEnvironmentReport, error) {
	if !bootRouteRef.MatchString(input.BatchRef) || (input.BootChannel != "239" && input.BootChannel != "143") {
		return nil, apiError(400, "VALIDATION_ERROR", "Batch and 239/143 channel are required")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT report FROM ztp_environment_checks WHERE project_id=$1 AND batch_ref=$2 AND boot_channel=$3 ORDER BY checked_at DESC,id DESC LIMIT 1`, p.ID, input.BatchRef, input.BootChannel).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if e != nil {
		return nil, e
	}
	var out ZTPEnvironmentReport
	if e = json.Unmarshal(raw, &out); e != nil {
		return nil, e
	}
	fingerprint := fabricboot.ManagementPlanDigest("not-configured")
	if s.FabricProbes != nil {
		fingerprint = s.FabricProbes.Fingerprint
	}
	out.Ready = out.Ready && out.ConfigHash == fingerprint && time.Now().Before(out.ExpiresAt)
	current, planErr := currentManagementPlanTx(ctx, tx, p.ID, input.BatchRef)
	if planErr != nil {
		var domain *Error
		if !errors.As(planErr, &domain) {
			return nil, planErr
		}
		out.Ready = false
	} else {
		out.Ready = out.Ready && current.ID == out.PlanID
	}
	return &out, tx.Commit()
}

func currentManagementPlanTx(ctx context.Context, tx *sql.Tx, projectID, batch string) (*ZTPManagementPlan, error) {
	var raw []byte
	e := tx.QueryRowContext(ctx, `SELECT report FROM ztp_management_plans WHERE project_id=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, projectID).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "ZTP_BATCH_PLAN_REQUIRED", "Save a management plan first")
	}
	if e != nil {
		return nil, e
	}
	var saved ZTPManagementPlan
	if e = json.Unmarshal(raw, &saved); e != nil {
		return nil, e
	}
	found := false
	for _, assignment := range saved.Assignments {
		if assignment.Batch == batch {
			found = true
		}
	}
	if !found {
		return nil, apiError(409, "ZTP_BATCH_PLAN_REQUIRED", "Batch is not in the current management plan")
	}
	current, e := managementPlanTx(ctx, tx, projectID, saved.BaselineRef)
	if e != nil {
		return nil, e
	}
	if current.ContentHash != saved.ContentHash {
		return nil, apiError(409, "ZTP_MANAGEMENT_PREVIEW_STALE", "BOM or rules changed; save a current plan before checking the environment")
	}
	return &saved, nil
}
