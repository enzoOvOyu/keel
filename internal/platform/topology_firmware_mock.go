package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type TopologyFirmwareInput struct {
	IssueVersion   int    `json:"issue_version"`
	MediaID        string `json:"media_id"`
	Action         string `json:"action"`
	Runbook        string `json:"runbook"`
	RecoveryPlan   string `json:"recovery_plan"`
	Reason         string `json:"reason"`
	RebootApproved bool   `json:"reboot_approved"`
}
type TopologyFirmwareApproval struct {
	ScopeHash string `json:"scope_hash"`
	Reason    string `json:"reason"`
}
type TopologyFirmwarePlan struct {
	HostTrustHash  string       `json:"host_trust_hash"`
	ID             string       `json:"id"`
	IssueID        string       `json:"issue_id"`
	ProjectID      string       `json:"project_id"`
	IssueVersion   int          `json:"issue_version"`
	AssetID        string       `json:"asset_id"`
	AssetVersion   int64        `json:"asset_version"`
	MachineID      string       `json:"machine_id"`
	Serial         string       `json:"serial"`
	ModelVersionID string       `json:"model_version_id"`
	ModelHash      string       `json:"model_hash"`
	Action         string       `json:"action"`
	Artifact       TaskArtifact `json:"artifact"`
	Runbook        string       `json:"runbook"`
	RecoveryPlan   string       `json:"recovery_plan"`
	Reason         string       `json:"reason"`
	RebootApproved bool         `json:"reboot_approved"`
	ScopeHash      string       `json:"scope_hash"`
	CreatedBy      string       `json:"created_by"`
	CreatedAt      time.Time    `json:"created_at"`
	ValidUntil     time.Time    `json:"valid_until"`
	ApprovedBy     string       `json:"approved_by"`
	ExecutionID    string       `json:"execution_id"`
	Origin         string       `json:"origin"`
}
type TopologyFirmwareWorkspace struct {
	Enabled    bool                   `json:"enabled"`
	CanPropose bool                   `json:"can_propose"`
	CanApprove bool                   `json:"can_approve"`
	Plans      []TopologyFirmwarePlan `json:"plans"`
	Media      []MediaItemSummary     `json:"media"`
}

func topologyFirmwareText(v string) bool {
	n := utf8.RuneCountInString(strings.TrimSpace(v))
	return n >= 3 && n <= 2000
}
func firmwareCompatibility(media *MediaItemSummary, model, arch, action string) bool {
	if media.Type != "firmware" || media.State != "approved" || !media.ArtifactStored || media.ArtifactQuarantined || media.Arch != arch && media.Arch != "noarch" {
		return false
	}
	matrix, e := decodeCompatibility(media.Deps)
	if e != nil || matrix == nil {
		return false
	}
	return slices.Contains(matrix.Requires["device_model_version"], model) && slices.Contains(matrix.Requires["repair_action"], action)
}
func (s *Store) topologyFirmwarePrepareTx(ctx context.Context, tx *sql.Tx, a *AuthSession, issue string, in TopologyFirmwareInput) (*TopologyFirmwarePlan, error) {
	if !s.TopologyFirmwareMockEnabled {
		return nil, apiError(409, "TOPOLOGY_FIRMWARE_MOCK_DISABLED", "P2 firmware simulation is disabled")
	}
	item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1 FOR UPDATE`, issue))
	if e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, item.ProjectID)
	if e != nil {
		return nil, e
	}
	if !canConfirmIssueVerification(item, role, a.User.ID) {
		return nil, ErrForbidden
	}
	if item.Source != "topology" || item.Category != "T7" || item.State != "in_repair" || item.Version != in.IssueVersion {
		return nil, apiError(409, "TOPOLOGY_FIRMWARE_SCOPE_CHANGED", "Current T7 repair and issue version required")
	}
	out := &TopologyFirmwarePlan{IssueID: issue, ProjectID: item.ProjectID, IssueVersion: item.Version, Action: in.Action, Runbook: in.Runbook, RecoveryPlan: in.RecoveryPlan, Reason: in.Reason, RebootApproved: in.RebootApproved, CreatedBy: a.User.ID, Origin: "sim"}
	var arch, assetState, projectState string
	e = tx.QueryRowContext(ctx, `SELECT asset.id,asset.version,asset.machine_id,machine.serial_number,asset.model_version_id,model.content_hash,coalesce(model.hw_spec->>'cpu_arch',''),asset.state,project.state FROM topology_issue_bindings discovery JOIN project_assets asset ON asset.id=discovery.asset_id JOIN machines machine ON machine.id=asset.machine_id JOIN device_model_versions model ON model.id=asset.model_version_id JOIN projects project ON project.id=asset.project_id WHERE discovery.issue_id=$1 AND asset.project_id=$2 AND machine.ssh_trust='established' AND machine.ssh_trust_source='ssh'`, issue, item.ProjectID).Scan(&out.AssetID, &out.AssetVersion, &out.MachineID, &out.Serial, &out.ModelVersionID, &out.ModelHash, &arch, &assetState, &projectState)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "TOPOLOGY_FIRMWARE_IDENTITY_REQUIRED", "Trusted host and bound independent model required")
	}
	if e != nil {
		return nil, e
	}
	if assetState == "maintained" || projectState == "archived" || projectState == "delivered" || !slices.Contains([]string{"amd64", "arm64"}, arch) {
		return nil, apiError(409, "TOPOLOGY_FIRMWARE_CHANNEL_BLOCKED", "Active delivery host with known architecture required")
	}
	out.HostTrustHash, e = repairHostIdentityTx(ctx, tx, out.MachineID, item.ProjectID)
	if e != nil {
		return nil, e
	}
	media, e := mediaForScopeLocked(ctx, tx, in.MediaID, s.scopeKeyFor(&ProjectSummary{ID: item.ProjectID}), true)
	if e != nil {
		return nil, e
	}
	if !firmwareCompatibility(media, out.ModelVersionID, arch, in.Action) {
		return nil, apiError(409, "TOPOLOGY_FIRMWARE_COMPATIBILITY_REQUIRED", "Approved firmware must explicitly declare device_model_version and repair_action compatibility")
	}
	artifact, e := s.ResolveTaskArtifactTx(ctx, tx, item.ProjectID, media.ID, nil)
	if e != nil {
		return nil, e
	}
	out.Artifact = *artifact
	if e = checkPinnedMediaDependenciesWithLock(ctx, tx, media.ID, s, true); e != nil {
		return nil, e
	}
	out.ScopeHash = digest(string(networkJSON(out)))
	return out, nil
}
func (s *Store) CreateTopologyFirmwarePlan(ctx context.Context, a *AuthSession, issue string, in TopologyFirmwareInput, key, trace string) (*TopologyFirmwarePlan, error) {
	if !s.TopologyFirmwareMockEnabled {
		return nil, apiError(409, "TOPOLOGY_FIRMWARE_MOCK_DISABLED", "P2 firmware simulation is disabled")
	}
	if in.IssueVersion < 1 || !validEntityID(in.MediaID) || !slices.Contains([]string{"flash_bios", "update_nvswitch"}, in.Action) || !topologyFirmwareText(in.Runbook) || !topologyFirmwareText(in.RecoveryPlan) || !topologyFirmwareText(in.Reason) || !in.RebootApproved || key == "" || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact firmware, runbook, recovery plan, reboot consent and reason required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	identity := digest("topology.firmware.plan:" + issue + ":" + a.User.ID + ":" + key)
	requestHash := digest(string(networkJSON(in)))
	if e = lockKey(ctx, tx, identity); e != nil {
		return nil, e
	}
	prior, e := priorWiringHandshakeTx(ctx, tx, identity, requestHash)
	if e != nil {
		return nil, e
	}
	if prior != "" {
		out, e := loadTopologyFirmwarePlanTx(ctx, tx, prior)
		if e != nil {
			return nil, e
		}
		if _, e = issueRoleTx(ctx, tx, a, out.ProjectID); e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	out, e := s.topologyFirmwarePrepareTx(ctx, tx, a, issue, in)
	if e != nil {
		return nil, e
	}
	out.ID = RandomID()
	out.CreatedAt = time.Now().UTC().Truncate(time.Microsecond)
	out.ValidUntil = out.CreatedAt.Add(30 * time.Minute)
	if _, e = tx.ExecContext(ctx, `INSERT INTO topology_firmware_mock_plans(id,issue_id,project_id,created_by,snapshot,scope_hash,created_at,valid_until)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, out.ID, issue, out.ProjectID, a.User.ID, networkJSON(out), out.ScopeHash, out.CreatedAt, out.ValidUntil); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at)VALUES($1,$2,$3,$4,clock_timestamp())`, identity, a.User.ID, requestHash, networkJSON(map[string]string{"id": out.ID})); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, out.ProjectID, "topology.firmware.mock.plan", out.ID, trace, map[string]any{"issue_id": issue, "scope_hash": out.ScopeHash, "origin": "sim"}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func loadTopologyFirmwarePlanTx(ctx context.Context, tx *sql.Tx, id string) (*TopologyFirmwarePlan, error) {
	var raw []byte
	var approved, execution string
	e := tx.QueryRowContext(ctx, `SELECT plan.snapshot,coalesce(approval.approved_by,''),coalesce(run.execution_id,'') FROM topology_firmware_mock_plans plan LEFT JOIN topology_firmware_mock_approvals approval ON approval.plan_id=plan.id LEFT JOIN topology_firmware_mock_runs run ON run.plan_id=plan.id WHERE plan.id=$1`, id).Scan(&raw, &approved, &execution)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	out := &TopologyFirmwarePlan{}
	if e = json.Unmarshal(raw, out); e != nil {
		return nil, e
	}
	out.ApprovedBy = approved
	out.ExecutionID = execution
	return out, nil
}
func (s *Store) TopologyFirmwareCurrentTx(ctx context.Context, tx *sql.Tx, id string) (*TopologyFirmwarePlan, error) {
	plan, e := loadTopologyFirmwarePlanTx(ctx, tx, id)
	if e != nil {
		return nil, e
	}
	if !plan.ValidUntil.After(time.Now()) {
		return nil, apiError(409, "TOPOLOGY_FIRMWARE_PLAN_EXPIRED", "Firmware simulation plan expired")
	}
	var enabled, admin bool
	if e = tx.QueryRowContext(ctx, `SELECT enabled,is_admin FROM users WHERE id=$1`, plan.CreatedBy).Scan(&enabled, &admin); e != nil {
		return nil, e
	}
	if !enabled {
		return nil, ErrForbidden
	}
	actor := &AuthSession{User: UserSummary{ID: plan.CreatedBy, IsAdmin: admin}}
	current, e := s.topologyFirmwarePrepareTx(ctx, tx, actor, plan.IssueID, TopologyFirmwareInput{IssueVersion: plan.IssueVersion, MediaID: plan.Artifact.MediaID, Action: plan.Action, Runbook: plan.Runbook, RecoveryPlan: plan.RecoveryPlan, Reason: plan.Reason, RebootApproved: plan.RebootApproved})
	if e != nil {
		return nil, e
	}
	if current.ScopeHash != plan.ScopeHash {
		return nil, apiError(409, "TOPOLOGY_FIRMWARE_SCOPE_CHANGED", "Model, artifact, repair scope or authority changed")
	}
	if plan.ApprovedBy != "" {
		var allowed bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users person WHERE person.id=$1 AND person.enabled AND (person.is_admin OR EXISTS(SELECT 1 FROM memberships membership WHERE membership.user_id=person.id AND membership.project_id=$2 AND membership.role IN('Admin','DL'))))`, plan.ApprovedBy, plan.ProjectID).Scan(&allowed); e != nil {
			return nil, e
		}
		if !allowed {
			return nil, ErrForbidden
		}
	}
	return plan, nil
}
func (s *Store) ApproveTopologyFirmwarePlan(ctx context.Context, a *AuthSession, issue, id string, in TopologyFirmwareApproval, trace string) (*TopologyFirmwarePlan, error) {
	if len(in.ScopeHash) != 64 || !topologyFirmwareText(in.Reason) {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact scope digest and approval reason required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	plan, e := s.TopologyFirmwareCurrentTx(ctx, tx, id)
	if e != nil {
		return nil, e
	}
	if plan.IssueID != issue {
		return nil, ErrNotFound
	}
	role, e := issueRoleTx(ctx, tx, a, plan.ProjectID)
	if e != nil {
		return nil, e
	}
	if !slices.Contains([]string{"Admin", "DL"}, role) || a.User.ID == plan.CreatedBy {
		return nil, ErrForbidden
	}
	if plan.ScopeHash != in.ScopeHash || plan.ExecutionID != "" || plan.ApprovedBy != "" {
		return nil, apiError(409, "TOPOLOGY_FIRMWARE_SCOPE_CHANGED", "Refresh the unapproved exact firmware plan")
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO topology_firmware_mock_approvals(plan_id,scope_hash,approved_by,reason)VALUES($1,$2,$3,$4)`, id, in.ScopeHash, a.User.ID, in.Reason); e != nil {
		return nil, e
	}
	plan.ApprovedBy = a.User.ID
	if e = appendAudit(ctx, tx, a.User.ID, plan.ProjectID, "topology.firmware.mock.approve", id, trace, map[string]any{"scope_hash": plan.ScopeHash, "origin": "sim"}); e != nil {
		return nil, e
	}
	return plan, tx.Commit()
}

func (s *Store) TopologyFirmwareWorkspace(ctx context.Context, a *AuthSession, issue string) (*TopologyFirmwareWorkspace, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1`, issue))
	if e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, item.ProjectID)
	if e != nil {
		return nil, e
	}
	if item.Source != "topology" || item.Category != "T7" {
		return nil, apiError(409, "TOPOLOGY_ISSUE_REQUIRED", "T7 topology issue required")
	}
	out := &TopologyFirmwareWorkspace{Enabled: s.TopologyFirmwareMockEnabled, CanPropose: canConfirmIssueVerification(item, role, a.User.ID) && item.State == "in_repair", CanApprove: role == "Admin" || role == "DL", Plans: []TopologyFirmwarePlan{}, Media: []MediaItemSummary{}}
	rows, e := tx.QueryContext(ctx, `SELECT id FROM topology_firmware_mock_plans WHERE issue_id=$1 ORDER BY created_at DESC,id DESC LIMIT 20`, issue)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for _, id := range ids {
		plan, e := loadTopologyFirmwarePlanTx(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		out.Plans = append(out.Plans, *plan)
	}
	// The picker only exposes independently approved stored firmware. Compatibility
	// remains authoritative at proposal/approval/dispatch, never in the browser.
	rows, e = tx.QueryContext(ctx, `SELECT id FROM media_items WHERE state='approved' AND type='firmware' AND scope_key IN('global',$1) ORDER BY name,version,id LIMIT 200`, s.scopeKeyFor(&ProjectSummary{ID: item.ProjectID}))
	if e != nil {
		return nil, e
	}
	ids = nil
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for _, id := range ids {
		media, e := mediaForScopeLocked(ctx, tx, id, s.scopeKeyFor(&ProjectSummary{ID: item.ProjectID}), false)
		if e != nil {
			return nil, e
		}
		if media.ArtifactStored && !media.ArtifactQuarantined {
			out.Media = append(out.Media, *media)
		}
	}
	return out, tx.Commit()
}
func (s *Store) TopologyFirmwareResourcesTx(ctx context.Context, tx *sql.Tx, id string) ([]string, error) {
	plan, e := s.TopologyFirmwareCurrentTx(ctx, tx, id)
	if e != nil {
		return nil, e
	}
	if plan.ApprovedBy == "" {
		return nil, ErrForbidden
	}
	return []string{"agent:machine:" + plan.MachineID, "device:machine:" + plan.MachineID, "ssh:machine:" + plan.MachineID}, nil
}
func (s *Store) AttachTopologyFirmwareRunTx(ctx context.Context, tx *sql.Tx, a *AuthSession, id, execution string) error {
	plan, e := s.TopologyFirmwareCurrentTx(ctx, tx, id)
	if e != nil {
		return e
	}
	item, e := scanIssue(tx.QueryRowContext(ctx, `SELECT `+issueColumns+` FROM issues WHERE id=$1`, plan.IssueID))
	if e != nil {
		return e
	}
	role, e := issueRoleTx(ctx, tx, a, plan.ProjectID)
	if e != nil {
		return e
	}
	if !canConfirmIssueVerification(item, role, a.User.ID) || plan.ApprovedBy == "" {
		return ErrForbidden
	}
	if plan.ExecutionID != "" {
		return apiError(409, "TOPOLOGY_FIRMWARE_ALREADY_EXECUTED", "An accepted mock command is never repeated")
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO topology_firmware_mock_runs(plan_id,execution_id)VALUES($1,$2)`, id, execution)
	return e
}
func (s *Store) RecordTopologyFirmwareMockTx(ctx context.Context, tx *sql.Tx, id, execution, attempt, command string, fence int64) (map[string]any, error) {
	plan, e := s.TopologyFirmwareCurrentTx(ctx, tx, id)
	if e != nil {
		return nil, e
	}
	if plan.ExecutionID != execution || plan.ApprovedBy == "" {
		return nil, ErrForbidden
	}
	output := map[string]any{"mode": "sim", "origin": "sim", "hardware_evidence": false, "operation": "issue.repair", "plan_id": id, "scope_hash": plan.ScopeHash, "issue_id": plan.IssueID, "action": plan.Action, "media_id": plan.Artifact.MediaID, "artifact_sha256": plan.Artifact.SHA256, "requested_version": plan.Artifact.Version, "mock_readback_version": plan.Artifact.Version, "requires_new_precollect_and_topology": true}
	_, e = tx.ExecContext(ctx, `INSERT INTO topology_firmware_mock_receipts(command_id,attempt_id,fencing_token,plan_id,execution_id,output)VALUES($1,$2,$3,$4,$5,$6)`, command, attempt, fence, id, execution, networkJSON(output))
	return output, e
}
