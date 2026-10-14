package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// WP-3.4 driver-svc (FR-5.6, F-5.3): the driver and dependency upgrade
// chain. A plan selects component targets from approved media (media://
// URIs only, F-1.8), prechecks the current->target matrix against the
// component capability matrix, then runs canary -> batched rolling ->
// read-back. Recovery follows the declared component capability: a verified
// rollback path restores the pre-upgrade backup; firmware is never assumed
// rollback-able and routes to manual handling. Distribution is accounted
// per target: a cache hit skips the transfer (F-1.7); a checksum mismatch
// retries a bounded number of times and then isolates the target (F-1.6).
// A timed-out or unknown install step never retries blindly: the target
// enters reconcile and only a queried result resolves it.

// DriverComponent is one row of the platform capability matrix. The matrix
// is platform knowledge: which components exist, what upgrading them
// requires and — critically — what recovery looks like.
type DriverComponent struct {
	Key            string         `json:"key"`
	DisplayName    string         `json:"display_name"`
	MediaType      string         `json:"media_type"`
	Rollback       string         `json:"rollback"` // rollback_verified / manual_only / none
	BackupRequired bool           `json:"backup_required"`
	CompanionRule  map[string]any `json:"companion_rule"`
	Notes          string         `json:"notes"`
}

// DriverPlan is one upgrade campaign for one project.
type DriverPlan struct {
	ID               string         `json:"id"`
	ProjectID        string         `json:"project_id"`
	Name             string         `json:"name"`
	State            string         `json:"state"`
	Components       map[string]any `json:"components"`
	Machines         []string       `json:"machines"`
	Baseline         map[string]any `json:"baseline"`
	BaselineOverride map[string]any `json:"baseline_overrides"`
	CanaryMachineID  string         `json:"canary_machine_id"`
	BatchSize        int            `json:"batch_size"`
	BackupPlan       string         `json:"backup_plan"`
	Precheck         map[string]any `json:"precheck"`
	Conclusion       string         `json:"conclusion"`
	CreatedBy        string         `json:"created_by"`
	CreatedAt        time.Time      `json:"created_at"`
	StartedAt        *time.Time     `json:"started_at,omitempty"`
	FinishedAt       *time.Time     `json:"finished_at,omitempty"`
}

// DriverTarget is one machine inside a plan.
type DriverTarget struct {
	ID              string            `json:"id"`
	PlanID          string            `json:"plan_id"`
	MachineID       string            `json:"machine_id"`
	Role            string            `json:"role"` // canary / batch
	CurrentVersions map[string]string `json:"current_versions"`
	TargetVersions  map[string]string `json:"target_versions"`
	State           string            `json:"state"`
	CacheHit        bool              `json:"cache_hit"`
	BytesTransferred int64            `json:"bytes_transferred"`
	Readback        map[string]string `json:"readback"`
	FailureClass    string            `json:"failure_class"`
	Attempt         int               `json:"attempt"`
	Reconcile       map[string]any    `json:"reconcile"`
	Evidence        map[string]any    `json:"evidence"`
	UpdatedAt       time.Time         `json:"updated_at"`
}

// DriverTransfer is one distribution event, the audit proof for F-1.6/F-1.7.
type DriverTransfer struct {
	ID        string    `json:"id"`
	PlanID    string    `json:"plan_id"`
	MachineID string    `json:"machine_id"`
	Component string    `json:"component"`
	URI       string    `json:"uri"`
	SHA256    string    `json:"sha256"`
	Size      int64     `json:"size"`
	CacheHit  bool      `json:"cache_hit"`
	Bytes     int64     `json:"bytes"`
	Checksum  string    `json:"checksum"` // "" / match / mismatch
	Retries   int       `json:"retries"`
	CreatedAt time.Time `json:"created_at"`
}

// DriverPlanDetail is the read model: the plan, its targets, the transfer
// log and the distribution accounting summary.
type DriverPlanDetail struct {
	Plan      DriverPlan       `json:"plan"`
	Targets   []DriverTarget   `json:"targets"`
	Transfers []DriverTransfer `json:"transfers"`
	Summary   map[string]any   `json:"summary"`
}

type DriverComponentInput struct {
	MediaID string `json:"media_id"`
	URI     string `json:"uri"`
}

type DriverPlanInput struct {
	Name              string                            `json:"name"`
	Components        map[string]DriverComponentInput   `json:"components"`
	Machines          []string                          `json:"machines"`
	Baseline          map[string]string                 `json:"baseline"`
	BaselineOverrides map[string]map[string]string      `json:"baseline_overrides"`
	CanaryMachineID   string                            `json:"canary_machine_id"`
	BatchSize         int                               `json:"batch_size"`
	BackupPlan        string                            `json:"backup_plan"`
}

// DriverTargetResult is one machine's report from the mock execution
// channel (ADR-14): what happened, what was read back and which media the
// machine already caches (the heartbeat manifest per ADR-07).
type DriverTargetResult struct {
	MachineID string            `json:"machine_id"`
	Outcome  string            `json:"outcome"` // success / failed / timeout / unknown / reconciled
	Readback map[string]string `json:"readback"`
	Cache    []string          `json:"cache"`
	Corrupt  []string          `json:"corrupt"`
}

type DriverAdvanceInput struct {
	Results []DriverTargetResult `json:"results"`
}

// ListDriverComponents returns the platform capability matrix.
func (s *Store) ListDriverComponents(ctx context.Context, a *AuthSession) ([]DriverComponent, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT key,display_name,media_type,rollback,backup_required,companion_rule,notes
		FROM driver_components ORDER BY key`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []DriverComponent{}
	for rows.Next() {
		var (
			c       DriverComponent
			ruleRaw []byte
		)
		if e = rows.Scan(&c.Key, &c.DisplayName, &c.MediaType, &c.Rollback, &c.BackupRequired, &ruleRaw, &c.Notes); e != nil {
			return nil, e
		}
		c.CompanionRule = map[string]any{}
		if len(ruleRaw) > 0 {
			_ = json.Unmarshal(ruleRaw, &c.CompanionRule)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CreateDriverUpgradePlan validates the component selections against the
// media library (F-1.8: approved media:// references only; local paths are
// rejected) and stores the campaign as a draft.
func (s *Store) CreateDriverUpgradePlan(ctx context.Context, a *AuthSession, project *ProjectSummary, input DriverPlanInput, trace string) (*DriverPlan, error) {
	if strings.TrimSpace(input.Name) == "" {
		return nil, apiError(400, "VALIDATION_ERROR", "plan name is required")
	}
	if len(input.Components) == 0 || len(input.Components) > 8 {
		return nil, apiError(400, "VALIDATION_ERROR", "components must declare 1-8 entries")
	}
	if len(input.Machines) == 0 || len(input.Machines) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "machines must contain 1-500 entries")
	}
	if input.BatchSize < 1 || input.BatchSize > 64 {
		return nil, apiError(400, "VALIDATION_ERROR", "batch_size must be 1-64")
	}
	if input.CanaryMachineID == "" {
		return nil, apiError(400, "VALIDATION_ERROR", "canary_machine_id is required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	components, e := loadDriverComponents(ctx, tx)
	if e != nil {
		return nil, e
	}
	resolved := map[string]any{}
	targetVersions := map[string]string{}
	backupNeeded := false
	for key, ref := range input.Components {
		component, ok := components[key]
		if !ok {
			return nil, apiError(400, "VALIDATION_ERROR", fmt.Sprintf("unknown driver component %q", key))
		}
		media, e := resolveDriverMedia(ctx, tx, project.ID, component, ref)
		if e != nil {
			return nil, e
		}
		resolved[key] = media
		targetVersions[key] = media["version"].(string)
		if component.BackupRequired {
			backupNeeded = true
		}
	}
	if backupNeeded && strings.TrimSpace(input.BackupPlan) == "" {
		return nil, apiError(400, "VALIDATION_ERROR", "backup_plan is required: the plan upgrades components whose recovery depends on a pre-upgrade backup")
	}
	machines, e := driverProjectMachines(ctx, tx, project.ID, input.Machines)
	if e != nil {
		return nil, e
	}
	if !driverContains(machines, input.CanaryMachineID) {
		return nil, apiError(400, "VALIDATION_ERROR", "canary_machine_id must be one of the plan machines")
	}
	baseline := map[string]any{}
	for key, value := range input.Baseline {
		baseline[key] = value
	}
	overrides := map[string]any{}
	for machineID, versions := range input.BaselineOverrides {
		if !driverContains(machines, machineID) {
			return nil, apiError(400, "VALIDATION_ERROR", "baseline override references a machine outside the plan")
		}
		entry := map[string]any{}
		for key, value := range versions {
			entry[key] = value
		}
		overrides[machineID] = entry
	}
	now := time.Now().UTC()
	plan := &DriverPlan{
		ID: RandomID(), ProjectID: project.ID, Name: strings.TrimSpace(input.Name), State: "draft",
		Components: resolved, Machines: machines, Baseline: baseline, BaselineOverride: overrides,
		CanaryMachineID: input.CanaryMachineID, BatchSize: input.BatchSize, BackupPlan: strings.TrimSpace(input.BackupPlan),
		Precheck: map[string]any{}, Conclusion: "", CreatedBy: a.User.ID, CreatedAt: now,
	}
	componentsJSON, _ := json.Marshal(resolved)
	machinesJSON, _ := json.Marshal(plan.Machines)
	baselineJSON, _ := json.Marshal(baseline)
	overridesJSON, _ := json.Marshal(overrides)
	if _, e = tx.ExecContext(ctx, `INSERT INTO driver_upgrade_plans(id,project_id,name,state,components,machines,baseline,baseline_overrides,canary_machine_id,batch_size,backup_plan,precheck,conclusion,created_by,created_at)
		VALUES($1,$2,$3,'draft',$4,$5,$6,$7,$8,$9,$10,'{}','',$11,$12)`,
		plan.ID, project.ID, plan.Name, string(componentsJSON), string(machinesJSON), string(baselineJSON),
		string(overridesJSON), plan.CanaryMachineID, plan.BatchSize, plan.BackupPlan, a.User.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "driver.plan_create", plan.ID, trace, map[string]any{
		"components": len(resolved), "machines": len(plan.Machines), "canary": plan.CanaryMachineID, "batch_size": plan.BatchSize,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return plan, nil
}

// PrecheckDriverUpgradePlan evaluates the current->target matrix per
// machine: a missing baseline is blocked (never PASS on missing data,
// ADR-07), companion rules are checked against the post-upgrade stack and
// the backup plan must exist when a component needs one. The plan becomes
// prechecked only when every machine passes.
func (s *Store) PrecheckDriverUpgradePlan(ctx context.Context, a *AuthSession, project *ProjectSummary, planID, trace string) (*DriverPlanDetail, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	plan, e := loadDriverPlan(ctx, tx, project.ID, planID)
	if e != nil {
		return nil, e
	}
	if plan.State != "draft" && plan.State != "prechecked" {
		return nil, apiError(409, "PLAN_STATE", "only a draft or prechecked plan can be prechecked")
	}
	components, e := loadDriverComponents(ctx, tx)
	if e != nil {
		return nil, e
	}
	verdicts := []map[string]any{}
	blocked := false
	for _, machineID := range plan.Machines {
		verdict := driverPrecheckMachine(plan, components, machineID)
		if verdict["state"] == "blocked" {
			blocked = true
		}
		verdicts = append(verdicts, verdict)
	}
	precheck := map[string]any{"machines": verdicts, "evaluated_at": time.Now().UTC()}
	if !blocked {
		plan.State = "prechecked"
	}
	precheckJSON, _ := json.Marshal(precheck)
	if _, e = tx.ExecContext(ctx, "UPDATE driver_upgrade_plans SET state=$2, precheck=$3 WHERE id=$1", plan.ID, plan.State, string(precheckJSON)); e != nil {
		return nil, e
	}
	plan.Precheck = precheck
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "driver.precheck", plan.ID, trace, map[string]any{
		"blocked": blocked, "machines": len(plan.Machines),
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.driverPlanDetail(ctx, a, project, planID)
}

// StartDriverUpgradePlan materialises the per-machine targets and enters
// the canary phase. Batch targets stay pending until the canary verifies.
func (s *Store) StartDriverUpgradePlan(ctx context.Context, a *AuthSession, project *ProjectSummary, planID, trace string) (*DriverPlanDetail, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	plan, e := loadDriverPlan(ctx, tx, project.ID, planID)
	if e != nil {
		return nil, e
	}
	if plan.State != "prechecked" {
		return nil, apiError(409, "PLAN_STATE", "only a prechecked plan can start")
	}
	now := time.Now().UTC()
	for _, machineID := range plan.Machines {
		role := "batch"
		if machineID == plan.CanaryMachineID {
			role = "canary"
		}
		current := driverBaseline(plan, machineID)
		currentJSON, _ := json.Marshal(current)
		targets := driverTargetVersions(plan)
		targetsJSON, _ := json.Marshal(targets)
		if _, e = tx.ExecContext(ctx, `INSERT INTO driver_upgrade_targets(id,plan_id,project_id,machine_id,role,current_versions,target_versions,state,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,'pending',$8)
			ON CONFLICT(plan_id,machine_id) DO UPDATE SET role=EXCLUDED.role,current_versions=EXCLUDED.current_versions,target_versions=EXCLUDED.target_versions,state='pending',updated_at=EXCLUDED.updated_at`,
			RandomID(), plan.ID, project.ID, machineID, role, string(currentJSON), string(targetsJSON), now); e != nil {
			return nil, e
		}
	}
	if _, e = tx.ExecContext(ctx, "UPDATE driver_upgrade_plans SET state='canary_running', started_at=$2 WHERE id=$1", plan.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "driver.start", plan.ID, trace, map[string]any{
		"canary": plan.CanaryMachineID, "machines": len(plan.Machines), "batch_size": plan.BatchSize,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.driverPlanDetail(ctx, a, project, planID)
}

// AdvanceDriverUpgradePlan consumes one batch of mock execution reports.
// The batch window is enforced: during the canary phase only the canary
// machine may report; during rolling at most batch_size targets report and
// only in plan order. A timed-out or unknown report moves the target to
// reconcile — a blind retry is refused until a queried result resolves it.
func (s *Store) AdvanceDriverUpgradePlan(ctx context.Context, a *AuthSession, project *ProjectSummary, planID string, input DriverAdvanceInput, trace string) (*DriverPlanDetail, error) {
	if len(input.Results) == 0 || len(input.Results) > 64 {
		return nil, apiError(400, "VALIDATION_ERROR", "results must contain 1-64 entries")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	plan, e := loadDriverPlan(ctx, tx, project.ID, planID)
	if e != nil {
		return nil, e
	}
	if plan.State != "canary_running" && plan.State != "rolling" {
		return nil, apiError(409, "PLAN_STATE", "only a running plan advances")
	}
	targets, e := loadDriverTargets(ctx, tx, plan.ID)
	if e != nil {
		return nil, e
	}
	byMachine := map[string]*DriverTarget{}
	for i := range targets {
		byMachine[targets[i].MachineID] = &targets[i]
	}
	window, e := driverBatchWindow(plan, targets)
	if e != nil {
		return nil, e
	}
	inWindow := map[string]bool{}
	for _, machineID := range window {
		inWindow[machineID] = true
	}
	if len(input.Results) > len(window) {
		return nil, apiError(409, "BATCH_LIMIT", fmt.Sprintf("at most %d targets may report in this batch", len(window)))
	}
	for _, result := range input.Results {
		target, ok := byMachine[result.MachineID]
		if !ok {
			return nil, apiError(400, "VALIDATION_ERROR", "result references a machine outside the plan")
		}
		if !inWindow[result.MachineID] {
			if plan.State == "canary_running" {
				return nil, apiError(409, "CANARY_FIRST", "the canary must verify before any batch machine advances")
			}
			return nil, apiError(409, "OUT_OF_BATCH", "the machine is outside the current batch window")
		}
		if target.State == "verified" || target.State == "rolled_back" || target.State == "manual" {
			return nil, apiError(409, "TARGET_STATE", "the target already reached a terminal state")
		}
		if target.State == "reconcile" && result.Outcome != "reconciled" {
			return nil, apiError(409, "RECONCILE_REQUIRED", "a timed-out or unknown step must be reconciled by a queried result before any retry")
		}
		if e = driverApplyResult(ctx, tx, plan, target, result); e != nil {
			return nil, e
		}
	}
	if e = driverAdvancePlan(ctx, tx, plan, targets); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "driver.advance", plan.ID, trace, map[string]any{
		"results": len(input.Results), "state": plan.State,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.driverPlanDetail(ctx, a, project, planID)
}

// RollbackDriverUpgradeTarget restores a failed or reconciled target from
// its pre-upgrade backup. The capability matrix decides: when any plan
// component lacks a verified rollback path the call is refused and the
// target must go through manual recovery instead.
func (s *Store) RollbackDriverUpgradeTarget(ctx context.Context, a *AuthSession, project *ProjectSummary, planID, machineID, trace string) (*DriverPlanDetail, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	plan, e := loadDriverPlan(ctx, tx, project.ID, planID)
	if e != nil {
		return nil, e
	}
	targets, e := loadDriverTargets(ctx, tx, plan.ID)
	if e != nil {
		return nil, e
	}
	var target *DriverTarget
	for i := range targets {
		if targets[i].MachineID == machineID {
			target = &targets[i]
		}
	}
	if target == nil {
		return nil, apiError(404, "NOT_FOUND", "target not found in this plan")
	}
	// A target already routed to manual handling (no verified rollback
	// path) must not be auto-rolled-back: let driverRequireRollback reject
	// it with MANUAL_RECOVERY_REQUIRED rather than a generic state error.
	if target.State != "failed" && target.State != "reconcile" && target.State != "manual" {
		return nil, apiError(409, "TARGET_STATE", "only a failed, reconciling or manual target can be rolled back")
	}
	if e = driverRequireRollback(ctx, tx, plan); e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	target.State = "rolled_back"
	target.FailureClass = ""
	target.Reconcile = map[string]any{}
	target.Evidence = map[string]any{"rollback": map[string]any{"restored_from": target.CurrentVersions, "by": a.User.ID, "at": now}}
	if e = driverSaveTarget(ctx, tx, target, now); e != nil {
		return nil, e
	}
	if e = driverAdvancePlan(ctx, tx, plan, targets); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "driver.rollback", plan.ID, trace, map[string]any{"machine": machineID}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.driverPlanDetail(ctx, a, project, planID)
}

// ManualRecoverDriverUpgradeTarget records an out-of-band recovery for a
// target whose components have no verified rollback path. The note is the
// evidence, mirroring the FR-4.7 repair registration.
func (s *Store) ManualRecoverDriverUpgradeTarget(ctx context.Context, a *AuthSession, project *ProjectSummary, planID, machineID, note, trace string) (*DriverPlanDetail, error) {
	if strings.TrimSpace(note) == "" {
		return nil, apiError(400, "VALIDATION_ERROR", "a manual recovery note is required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	plan, e := loadDriverPlan(ctx, tx, project.ID, planID)
	if e != nil {
		return nil, e
	}
	targets, e := loadDriverTargets(ctx, tx, plan.ID)
	if e != nil {
		return nil, e
	}
	var target *DriverTarget
	for i := range targets {
		if targets[i].MachineID == machineID {
			target = &targets[i]
		}
	}
	if target == nil {
		return nil, apiError(404, "NOT_FOUND", "target not found in this plan")
	}
	if target.State != "failed" && target.State != "manual" && target.State != "reconcile" {
		return nil, apiError(409, "TARGET_STATE", "only a failed, manual or reconciling target can be recovered manually")
	}
	now := time.Now().UTC()
	target.State = "manual"
	target.Evidence = map[string]any{"manual_recovery": map[string]any{"note": strings.TrimSpace(note), "by": a.User.ID, "at": now}}
	if e = driverSaveTarget(ctx, tx, target, now); e != nil {
		return nil, e
	}
	if e = driverAdvancePlan(ctx, tx, plan, targets); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "driver.manual_recovery", plan.ID, trace, map[string]any{"machine": machineID}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.driverPlanDetail(ctx, a, project, planID)
}

// AbortDriverUpgradePlan stops a running campaign. Targets keep their
// recorded state; the abort is auditable.
func (s *Store) AbortDriverUpgradePlan(ctx context.Context, a *AuthSession, project *ProjectSummary, planID, trace string) (*DriverPlanDetail, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	plan, e := loadDriverPlan(ctx, tx, project.ID, planID)
	if e != nil {
		return nil, e
	}
	switch plan.State {
	case "prechecked", "canary_running", "rolling":
	default:
		return nil, apiError(409, "PLAN_STATE", "only a prechecked or running plan can be aborted")
	}
	now := time.Now().UTC()
	if _, e = tx.ExecContext(ctx, "UPDATE driver_upgrade_plans SET state='aborted', finished_at=$2 WHERE id=$1", plan.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "driver.abort", plan.ID, trace, map[string]any{"from": plan.State}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.driverPlanDetail(ctx, a, project, planID)
}

// ListDriverUpgradePlans returns the project's plans, newest first.
func (s *Store) ListDriverUpgradePlans(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[DriverPlan], error) {
	out := NewPage[DriverPlan](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return out, e
	}
	var total int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM driver_upgrade_plans WHERE project_id=$1", project.ID).Scan(&total); e != nil {
		return out, e
	}
	out.Total = total
	rows, e := tx.QueryContext(ctx, `SELECT id,project_id,name,state,components,machines,baseline,baseline_overrides,canary_machine_id,batch_size,backup_plan,precheck,conclusion,created_by,created_at,started_at,finished_at
		FROM driver_upgrade_plans WHERE project_id=$1 ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`, project.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		plan, e := scanDriverPlan(rows)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, *plan)
	}
	return out, rows.Err()
}

// GetDriverUpgradePlan returns the plan with its targets, transfer log and
// the distribution accounting summary.
func (s *Store) GetDriverUpgradePlan(ctx context.Context, a *AuthSession, project *ProjectSummary, planID string) (*DriverPlanDetail, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	if _, e = loadDriverPlan(ctx, tx, project.ID, planID); e != nil {
		return nil, e
	}
	return s.driverPlanDetail(ctx, a, project, planID)
}

// ---- internals ----

func (s *Store) driverPlanDetail(ctx context.Context, a *AuthSession, project *ProjectSummary, planID string) (*DriverPlanDetail, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	plan, e := loadDriverPlan(ctx, tx, project.ID, planID)
	if e != nil {
		return nil, e
	}
	targets, e := loadDriverTargets(ctx, tx, plan.ID)
	if e != nil {
		return nil, e
	}
	transfers, e := loadDriverTransfers(ctx, tx, plan.ID)
	if e != nil {
		return nil, e
	}
	detail := &DriverPlanDetail{Plan: *plan, Targets: targets, Transfers: transfers, Summary: driverSummary(targets, transfers)}
	return detail, nil
}

func loadDriverPlan(ctx context.Context, tx *sql.Tx, projectID, planID string) (*DriverPlan, error) {
	row := tx.QueryRowContext(ctx, `SELECT id,project_id,name,state,components,machines,baseline,baseline_overrides,canary_machine_id,batch_size,backup_plan,precheck,conclusion,created_by,created_at,started_at,finished_at
		FROM driver_upgrade_plans WHERE project_id=$1 AND id=$2`, projectID, planID)
	plan, e := scanDriverPlan(row)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "upgrade plan not found")
		}
		return nil, e
	}
	return plan, nil
}

func scanDriverPlan(row interface{ Scan(...any) error }) (*DriverPlan, error) {
	var (
		plan                                        DriverPlan
		componentsRaw, machinesRaw, baselineRaw    []byte
		overridesRaw, precheckRaw                   []byte
		startedAt, finishedAt                       sql.NullTime
	)
	if e := row.Scan(&plan.ID, &plan.ProjectID, &plan.Name, &plan.State, &componentsRaw, &machinesRaw,
		&baselineRaw, &overridesRaw, &plan.CanaryMachineID, &plan.BatchSize, &plan.BackupPlan, &precheckRaw,
		&plan.Conclusion, &plan.CreatedBy, &plan.CreatedAt, &startedAt, &finishedAt); e != nil {
		return nil, e
	}
	plan.Components = map[string]any{}
	plan.Machines = []string{}
	plan.Baseline = map[string]any{}
	plan.BaselineOverride = map[string]any{}
	plan.Precheck = map[string]any{}
	_ = json.Unmarshal(componentsRaw, &plan.Components)
	_ = json.Unmarshal(machinesRaw, &plan.Machines)
	_ = json.Unmarshal(baselineRaw, &plan.Baseline)
	_ = json.Unmarshal(overridesRaw, &plan.BaselineOverride)
	_ = json.Unmarshal(precheckRaw, &plan.Precheck)
	if startedAt.Valid {
		at := startedAt.Time
		plan.StartedAt = &at
	}
	if finishedAt.Valid {
		at := finishedAt.Time
		plan.FinishedAt = &at
	}
	return &plan, nil
}

func loadDriverComponents(ctx context.Context, tx *sql.Tx) (map[string]DriverComponent, error) {
	rows, e := tx.QueryContext(ctx, `SELECT key,display_name,media_type,rollback,backup_required,companion_rule,notes FROM driver_components ORDER BY key`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]DriverComponent{}
	for rows.Next() {
		var (
			c       DriverComponent
			ruleRaw []byte
		)
		if e = rows.Scan(&c.Key, &c.DisplayName, &c.MediaType, &c.Rollback, &c.BackupRequired, &ruleRaw, &c.Notes); e != nil {
			return nil, e
		}
		c.CompanionRule = map[string]any{}
		if len(ruleRaw) > 0 {
			_ = json.Unmarshal(ruleRaw, &c.CompanionRule)
		}
		out[c.Key] = c
	}
	return out, rows.Err()
}

// resolveDriverMedia enforces F-1.8: only approved media references are
// accepted, a media:// URI must resolve to approved media of the
// component's type, and local paths are rejected outright.
func resolveDriverMedia(ctx context.Context, tx *sql.Tx, projectID string, component DriverComponent, ref DriverComponentInput) (map[string]any, error) {
	scope := "project:" + projectID
	if ref.URI != "" && ref.MediaID != "" {
		return nil, apiError(400, "VALIDATION_ERROR", "declare either media_id or uri, not both")
	}
	if ref.URI == "" && ref.MediaID == "" {
		return nil, apiError(400, "VALIDATION_ERROR", "a media reference is required")
	}
	var (
		mediaType, name, version, sha256 string
		size                             int64
		id                               string
	)
	if ref.URI != "" {
		if !strings.HasPrefix(ref.URI, "media://") {
			return nil, apiError(400, "MEDIA_REFERENCE_REJECTED", "only media:// references are accepted; local paths are rejected")
		}
		mediaType, name, version, e := parseMediaURI(ref.URI)
		if e != nil {
			return nil, e
		}
		if mediaType != component.MediaType {
			return nil, apiError(400, "MEDIA_TYPE_MISMATCH", fmt.Sprintf("component %s distributes %s media, not %s", component.Key, component.MediaType, mediaType))
		}
		row := tx.QueryRowContext(ctx, `SELECT id,sha256,size FROM media_items
			WHERE scope_key IN('global',$1) AND type=$2 AND name=$3 AND version=$4 AND state='approved'`, scope, mediaType, name, version)
		if e := row.Scan(&id, &sha256, &size); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return nil, apiError(409, "MEDIA_NOT_APPROVED", "the referenced media version is not approved")
			}
			return nil, e
		}
		return driverMediaRef(id, mediaType, name, version, sha256, size), nil
	}
	row := tx.QueryRowContext(ctx, `SELECT id,type,name,version,sha256,size,state FROM media_items WHERE id=$1`, ref.MediaID)
	var state string
	if e := row.Scan(&id, &mediaType, &name, &version, &sha256, &size, &state); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "media item not found")
		}
		return nil, e
	}
	if state != "approved" {
		return nil, apiError(409, "MEDIA_NOT_APPROVED", "only approved media can be distributed")
	}
	if mediaType != component.MediaType {
		return nil, apiError(400, "MEDIA_TYPE_MISMATCH", fmt.Sprintf("component %s distributes %s media, not %s", component.Key, component.MediaType, mediaType))
	}
	scopeRow := tx.QueryRowContext(ctx, `SELECT scope_key FROM media_items WHERE id=$1`, ref.MediaID)
	var scopeKey string
	if e := scopeRow.Scan(&scopeKey); e != nil {
		return nil, e
	}
	if scopeKey != "global" && scopeKey != scope {
		return nil, apiError(403, "MEDIA_SCOPE", "the media item is not visible to this project")
	}
	return driverMediaRef(id, mediaType, name, version, sha256, size), nil
}

func driverMediaRef(id, mediaType, name, version, sha256 string, size int64) map[string]any {
	return map[string]any{
		"media_id": id, "type": mediaType, "name": name, "version": version,
		"uri": fmt.Sprintf("media://%s/%s@%s", mediaType, name, version), "sha256": sha256, "size": size,
	}
}

// parseMediaURI splits media://<type>/<name>@<version>.
func parseMediaURI(uri string) (mediaType, name, version string, err error) {
	rest := strings.TrimPrefix(uri, "media://")
	slash := strings.Index(rest, "/")
	at := strings.LastIndex(rest, "@")
	if slash <= 0 || at <= slash+1 || at == len(rest)-1 {
		return "", "", "", apiError(400, "MEDIA_REFERENCE_REJECTED", "media URI must look like media://<type>/<name>@<version>")
	}
	return rest[:slash], rest[slash+1 : at], rest[at+1:], nil
}

// driverProjectMachines validates that every referenced machine belongs to
// the project and returns the de-duplicated plan order.
func driverProjectMachines(ctx context.Context, tx *sql.Tx, projectID string, machineIDs []string) ([]string, error) {
	seen := map[string]bool{}
	order := []string{}
	for _, machineID := range machineIDs {
		if machineID == "" || seen[machineID] {
			return nil, apiError(400, "VALIDATION_ERROR", "machines must be unique non-empty ids")
		}
		seen[machineID] = true
		order = append(order, machineID)
	}
	var count int
	if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_assets WHERE project_id=$1 AND machine_id = ANY($2)`, projectID, order).Scan(&count); e != nil {
		return nil, e
	}
	if count != len(order) {
		return nil, apiError(400, "VALIDATION_ERROR", "every machine must be bound to the project")
	}
	return order, nil
}

func driverContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func driverBaseline(plan *DriverPlan, machineID string) map[string]string {
	current := map[string]string{}
	if override, ok := plan.BaselineOverride[machineID].(map[string]any); ok {
		for key, value := range override {
			if version, ok := value.(string); ok {
				current[key] = version
			}
		}
	}
	for key, value := range plan.Baseline {
		if _, ok := current[key]; ok {
			continue
		}
		if version, ok := value.(string); ok {
			current[key] = version
		}
	}
	return current
}

func driverTargetVersions(plan *DriverPlan) map[string]string {
	out := map[string]string{}
	for key, raw := range plan.Components {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if version, ok := entry["version"].(string); ok {
			out[key] = version
		}
	}
	return out
}

// driverPrecheckMachine evaluates one machine's current->target matrix.
// A missing baseline is blocked: missing data is never a PASS (ADR-07).
func driverPrecheckMachine(plan *DriverPlan, components map[string]DriverComponent, machineID string) map[string]any {
	reasons := []string{}
	current := driverBaseline(plan, machineID)
	targets := driverTargetVersions(plan)
	for key := range plan.Components {
		if _, ok := current[key]; !ok {
			reasons = append(reasons, "baseline_missing:"+key)
		}
	}
	for key, component := range components {
		rule, ok := component.CompanionRule["cuda_at_least"].(string)
		if !ok || rule == "" {
			continue
		}
		if _, upgrading := plan.Components[key]; !upgrading {
			continue
		}
		stack := current["cuda"]
		if target, ok := targets["cuda"]; ok {
			stack = target
		}
		if stack == "" {
			reasons = append(reasons, "companion_unverifiable:cuda")
			continue
		}
		ok, e := versionAtLeast(stack, rule)
		if e != nil {
			reasons = append(reasons, "companion_unverifiable:cuda")
			continue
		}
		if !ok {
			reasons = append(reasons, fmt.Sprintf("companion_rule:%s requires cuda >= %s, found %s", key, rule, stack))
		}
	}
	state := "pass"
	if len(reasons) > 0 {
		state = "blocked"
	}
	sort.Strings(reasons)
	return map[string]any{"machine_id": machineID, "state": state, "reasons": reasons}
}

// versionAtLeast compares dotted numeric versions; an unparsable side is an
// error so callers can refuse to pass on unknown data.
func versionAtLeast(actual, minimum string) (bool, error) {
	a, e := parseVersion(actual)
	if e != nil {
		return false, e
	}
	b, e := parseVersion(minimum)
	if e != nil {
		return false, e
	}
	for i := 0; i < len(a) || i < len(b); i++ {
		x, y := 0, 0
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return x > y, nil
		}
	}
	return true, nil
}

func parseVersion(value string) ([]int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, errors.New("empty version")
	}
	parts := strings.Split(value, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		fields := strings.FieldsFunc(part, func(r rune) bool { return r < '0' || r > '9' })
		digits := strings.Join(fields, "")
		if digits == "" {
			return nil, fmt.Errorf("unparsable version %q", value)
		}
		n, e := strconv.Atoi(digits)
		if e != nil {
			return nil, fmt.Errorf("unparsable version %q", value)
		}
		out = append(out, n)
	}
	return out, nil
}

// driverBatchWindow returns the machines that may report in the current
// batch: the canary alone during the canary phase, then the next
// batch_size actionable batch targets in plan order. A target isolated by a
// checksum mismatch is not actionable: it awaits an explicit rollback or
// manual recovery, never a blind re-report.
func driverBatchWindow(plan *DriverPlan, targets []DriverTarget) ([]string, error) {
	actionable := func(state string) bool {
		switch state {
		case "pending", "reconcile", "distributing", "installing", "verifying":
			return true
		}
		return false
	}
	if plan.State == "canary_running" {
		return []string{plan.CanaryMachineID}, nil
	}
	window := []string{}
	for _, machineID := range plan.Machines {
		if machineID == plan.CanaryMachineID {
			continue
		}
		for _, target := range targets {
			if target.MachineID != machineID {
				continue
			}
			if actionable(target.State) {
				window = append(window, machineID)
			}
		}
		if len(window) == plan.BatchSize {
			break
		}
	}
	if len(window) == 0 {
		return nil, apiError(409, "PLAN_STATE", "no actionable target remains; resolve isolated or manual targets first")
	}
	return window, nil
}

// driverApplyResult runs one target through distribute -> install ->
// read-back. Every branch records its evidence; nothing is assumed.
func driverApplyResult(ctx context.Context, tx *sql.Tx, plan *DriverPlan, target *DriverTarget, result DriverTargetResult) error {
	now := time.Now().UTC()
	switch result.Outcome {
	case "reconciled":
		// The queried truth after a timeout/unknown: verify against it and
		// never treat the query itself as a side effect.
		target.Reconcile = map[string]any{"resolved_at": now, "resolved_by": "queried"}
		target.Readback = result.Readback
		return driverVerify(ctx, tx, plan, target, now)
	case "success", "failed", "timeout", "unknown":
	default:
		return apiError(400, "VALIDATION_ERROR", "outcome must be success, failed, timeout, unknown or reconciled")
	}
	cache := map[string]bool{}
	for _, sha := range result.Cache {
		cache[sha] = true
	}
	corrupt := map[string]bool{}
	for _, key := range result.Corrupt {
		corrupt[key] = true
	}
	// Distribute every component; a cache hit skips the transfer entirely.
	keys := make([]string, 0, len(plan.Components))
	for key := range plan.Components {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hitAll := true
	for _, key := range keys {
		media := plan.Components[key].(map[string]any)
		sha, _ := media["sha256"].(string)
		size, _ := media["size"].(float64)
		uri, _ := media["uri"].(string)
		hit := cache[sha]
		bytes := int64(0)
		checksum := ""
		retries := 0
		if !hit {
			bytes = int64(size)
			if corrupt[key] {
				// A corrupt artifact retries a bounded number of times and
				// then isolates the target: the digest is never waived.
				for retries < 2 && corrupt[key] {
					retries++
				}
				checksum = "mismatch"
			} else {
				checksum = "match"
			}
		}
		if _, e := tx.ExecContext(ctx, `INSERT INTO driver_transfer_log(id,plan_id,machine_id,component,uri,sha256,size,cache_hit,bytes,checksum,retries,created_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			RandomID(), plan.ID, target.MachineID, key, uri, sha, int64(size), hit, bytes, checksum, retries, now); e != nil {
			return e
		}
		target.BytesTransferred += bytes
		if !hit {
			hitAll = false
		}
		if checksum == "mismatch" {
			target.CacheHit = false
			target.State = "failed"
			target.FailureClass = "checksum_mismatch"
			target.Evidence = map[string]any{"isolated": map[string]any{"component": key, "sha256": sha, "retries": retries, "at": now}}
			return driverSaveTarget(ctx, tx, target, now)
		}
	}
	target.CacheHit = hitAll
	if result.Outcome == "failed" {
		target.State = "failed"
		target.FailureClass = "install_failed"
		return driverRecoverOrManual(ctx, tx, plan, target, now, map[string]any{"install_failed": true})
	}
	if result.Outcome == "timeout" || result.Outcome == "unknown" {
		// Query first, retry never: the target waits for a reconciled
		// result and the attempt counter is not advanced.
		target.State = "reconcile"
		target.FailureClass = result.Outcome
		target.Reconcile = map[string]any{"reason": result.Outcome, "attempt": target.Attempt, "at": now}
		return driverSaveTarget(ctx, tx, target, now)
	}
	target.Attempt++
	target.State = "verifying"
	if e := driverSaveTarget(ctx, tx, target, now); e != nil {
		return e
	}
	target.Readback = result.Readback
	return driverVerify(ctx, tx, plan, target, now)
}

// driverVerify compares the read-back with the target versions. A mismatch
// is a failure, not a warning: silence is never a pass.
func driverVerify(ctx context.Context, tx *sql.Tx, plan *DriverPlan, target *DriverTarget, now time.Time) error {
	targets := driverTargetVersions(plan)
	mismatch := []string{}
	for key, want := range targets {
		got, ok := target.Readback[key]
		if !ok || got != want {
			mismatch = append(mismatch, key)
		}
	}
	if len(mismatch) == 0 {
		target.State = "verified"
		target.FailureClass = ""
		target.Evidence = map[string]any{"readback_match": true, "at": now}
		return driverSaveTarget(ctx, tx, target, now)
	}
	sort.Strings(mismatch)
	target.State = "failed"
	target.FailureClass = "readback_mismatch"
	return driverRecoverOrManual(ctx, tx, plan, target, now, map[string]any{"readback_mismatch": mismatch})
}

// driverRecoverOrManual applies the capability matrix to a failure: every
// component rollback_verified restores the pre-upgrade backup; any
// component without a verified path routes the target to manual handling.
func driverRecoverOrManual(ctx context.Context, tx *sql.Tx, plan *DriverPlan, target *DriverTarget, now time.Time, detail map[string]any) error {
	components, e := loadDriverComponents(ctx, tx)
	if e != nil {
		return e
	}
	manual := []string{}
	for key := range plan.Components {
		component, ok := components[key]
		if !ok || component.Rollback != "rollback_verified" {
			manual = append(manual, key)
		}
	}
	if len(manual) == 0 {
		target.State = "rolled_back"
		target.Evidence = map[string]any{"auto_rollback": map[string]any{"restored_from": target.CurrentVersions, "at": now, "detail": detail}}
		return driverSaveTarget(ctx, tx, target, now)
	}
	sort.Strings(manual)
	target.State = "manual"
	target.Evidence = map[string]any{"manual_required": map[string]any{"components": manual, "detail": detail, "at": now}}
	return driverSaveTarget(ctx, tx, target, now)
}

func driverRequireRollback(ctx context.Context, tx *sql.Tx, plan *DriverPlan) error {
	components, e := loadDriverComponents(ctx, tx)
	if e != nil {
		return e
	}
	manual := []string{}
	for key := range plan.Components {
		component, ok := components[key]
		if !ok || component.Rollback != "rollback_verified" {
			manual = append(manual, key)
		}
	}
	if len(manual) > 0 {
		sort.Strings(manual)
		return apiError(409, "MANUAL_RECOVERY_REQUIRED", "no verified rollback path for: "+strings.Join(manual, ", "))
	}
	return nil
}

func driverSaveTarget(ctx context.Context, tx *sql.Tx, target *DriverTarget, now time.Time) error {
	readbackJSON, _ := json.Marshal(target.Readback)
	reconcileJSON, _ := json.Marshal(target.Reconcile)
	evidenceJSON, _ := json.Marshal(target.Evidence)
	_, e := tx.ExecContext(ctx, `UPDATE driver_upgrade_targets SET state=$2,cache_hit=$3,bytes_transferred=$4,readback=$5,failure_class=$6,attempt=$7,reconcile=$8,evidence=$9,updated_at=$10 WHERE id=$1`,
		target.ID, target.State, target.CacheHit, target.BytesTransferred, string(readbackJSON), target.FailureClass,
		target.Attempt, string(reconcileJSON), string(evidenceJSON), now)
	return e
}

// driverAdvancePlan moves the plan after a batch of results: the canary
// gates the batches; when every target is terminal the plan completes with
// an honest conclusion (PASS only when every machine verified).
func driverAdvancePlan(ctx context.Context, tx *sql.Tx, plan *DriverPlan, targets []DriverTarget) error {
	byMachine := map[string]DriverTarget{}
	for _, target := range targets {
		byMachine[target.MachineID] = target
	}
	terminal := func(state string) bool {
		return state == "verified" || state == "rolled_back" || state == "manual"
	}
	now := time.Now().UTC()
	canary := byMachine[plan.CanaryMachineID]
	switch plan.State {
	case "canary_running":
		switch canary.State {
		case "verified":
			hasBatch := false
			for _, machineID := range plan.Machines {
				if machineID != plan.CanaryMachineID {
					hasBatch = true
				}
			}
			if hasBatch {
				plan.State = "rolling"
			} else {
				return driverCompletePlan(ctx, tx, plan, targets, now)
			}
		case "rolled_back":
			plan.State = "rolled_back"
			plan.FinishedAt = &now
		case "manual":
			plan.State = "manual_required"
			plan.FinishedAt = &now
		}
	case "rolling":
		allTerminal := true
		for _, target := range targets {
			if !terminal(target.State) {
				allTerminal = false
			}
		}
		if allTerminal {
			return driverCompletePlan(ctx, tx, plan, targets, now)
		}
	}
	_, e := tx.ExecContext(ctx, "UPDATE driver_upgrade_plans SET state=$2, finished_at=$3 WHERE id=$1", plan.ID, plan.State, nullableTime(plan.FinishedAt))
	return e
}

func driverCompletePlan(ctx context.Context, tx *sql.Tx, plan *DriverPlan, targets []DriverTarget, now time.Time) error {
	plan.State = "completed"
	plan.FinishedAt = &now
	conclusion := "PASS"
	for _, target := range targets {
		if target.State != "verified" {
			conclusion = "FAIL"
		}
	}
	plan.Conclusion = conclusion
	_, e := tx.ExecContext(ctx, "UPDATE driver_upgrade_plans SET state='completed', conclusion=$2, finished_at=$3 WHERE id=$1", plan.ID, conclusion, now)
	return e
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return *t
}

func loadDriverTargets(ctx context.Context, tx *sql.Tx, planID string) ([]DriverTarget, error) {
	rows, e := tx.QueryContext(ctx, `SELECT id,plan_id,machine_id,role,current_versions,target_versions,state,cache_hit,bytes_transferred,readback,failure_class,attempt,reconcile,evidence,updated_at
		FROM driver_upgrade_targets WHERE plan_id=$1 ORDER BY role, machine_id`, planID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []DriverTarget{}
	for rows.Next() {
		var (
			t                                        DriverTarget
			currentRaw, targetsRaw, readbackRaw      []byte
			reconcileRaw, evidenceRaw                []byte
		)
		if e = rows.Scan(&t.ID, &t.PlanID, &t.MachineID, &t.Role, &currentRaw, &targetsRaw, &t.State,
			&t.CacheHit, &t.BytesTransferred, &readbackRaw, &t.FailureClass, &t.Attempt, &reconcileRaw, &evidenceRaw, &t.UpdatedAt); e != nil {
			return nil, e
		}
		t.CurrentVersions = map[string]string{}
		t.TargetVersions = map[string]string{}
		t.Readback = map[string]string{}
		t.Reconcile = map[string]any{}
		t.Evidence = map[string]any{}
		_ = json.Unmarshal(currentRaw, &t.CurrentVersions)
		_ = json.Unmarshal(targetsRaw, &t.TargetVersions)
		_ = json.Unmarshal(readbackRaw, &t.Readback)
		_ = json.Unmarshal(reconcileRaw, &t.Reconcile)
		_ = json.Unmarshal(evidenceRaw, &t.Evidence)
		out = append(out, t)
	}
	return out, rows.Err()
}

func loadDriverTransfers(ctx context.Context, tx *sql.Tx, planID string) ([]DriverTransfer, error) {
	rows, e := tx.QueryContext(ctx, `SELECT id,plan_id,machine_id,component,uri,sha256,size,cache_hit,bytes,checksum,retries,created_at
		FROM driver_transfer_log WHERE plan_id=$1 ORDER BY created_at, id`, planID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []DriverTransfer{}
	for rows.Next() {
		var t DriverTransfer
		if e = rows.Scan(&t.ID, &t.PlanID, &t.MachineID, &t.Component, &t.URI, &t.SHA256, &t.Size,
			&t.CacheHit, &t.Bytes, &t.Checksum, &t.Retries, &t.CreatedAt); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// driverSummary is the distribution accounting: hits skip the transfer
// (F-1.7), misses move the package bytes, isolated targets stay visible.
func driverSummary(targets []DriverTarget, transfers []DriverTransfer) map[string]any {
	hits, misses, bytes, isolated := 0, 0, int64(0), 0
	for _, t := range transfers {
		if t.CacheHit {
			hits++
		} else {
			misses++
			bytes += t.Bytes
		}
	}
	verified, rolledBack, manual, failed, pending := 0, 0, 0, 0, 0
	for _, t := range targets {
		switch t.State {
		case "verified":
			verified++
		case "rolled_back":
			rolledBack++
		case "manual":
			manual++
		case "failed":
			failed++
			isolated++
		default:
			pending++
		}
	}
	return map[string]any{
		"transfer_hits": hits, "transfer_misses": misses, "bytes_transferred": bytes,
		"verified": verified, "rolled_back": rolledBack, "manual": manual, "failed": failed,
		"isolated": isolated, "pending": pending,
	}
}
