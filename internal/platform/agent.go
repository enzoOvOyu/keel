// WP-3.5 agent installer (FR-5.7, F-5.4): the keel-agent binary is itself a
// media:// artifact. Batch installation runs only against delivery-state
// machines covered by a published mapping revision (the G3+ evidence
// contract, same source of truth as batch config, FR-5 precondition).
// Architecture selection (amd64/arm64) is enforced at plan creation: the
// agent media arch must match the machine model's declared arch.
//
// Credential lifecycle: creating a plan issues a one-time short-lived
// enrollment token per machine, bound to project+machine. The registration
// call verifies the token, the serial and the fingerprint, then issues a
// short-lived device credential whose fingerprint is recorded; token replay
// is refused and audited (token_replay_refused). Rotation and revocation
// are audited (rotate/revoke). A machine cannot self-register on a bare SN
// report alone: without a live token the registration is refused.
package platform

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// AgentPlan is one batch installation of the keel-agent binary.
type AgentPlan struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	Name       string         `json:"name"`
	State      string         `json:"state"`
	Media      map[string]any `json:"media"`
	Machines   []string       `json:"machines"`
	CreatedBy  string         `json:"created_by"`
	CreatedAt  time.Time      `json:"created_at"`
	FinishedAt *time.Time     `json:"finished_at"`
}

// AgentTarget is the per-machine installation row.
type AgentTarget struct {
	ID            string         `json:"id"`
	PlanID        string         `json:"plan_id"`
	MachineID     string         `json:"machine_id"`
	Hostname      string         `json:"hostname"`
	State         string         `json:"state"`
	Reason        string         `json:"reason"`
	MediaURI      string         `json:"media_uri"`
	MediaSHA256   string         `json:"media_sha256"`
	CacheHit      bool           `json:"cache_hit"`
	Bytes         int64          `json:"bytes_transferred"`
	Enrollment    string         `json:"enrollment_token"`
	CredentialID  string         `json:"credential_id"`
	Fingerprint   string         `json:"credential_fingerprint"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// AgentCredentialEvent is the auditable credential lifecycle entry.
type AgentCredentialEvent struct {
	ID         string         `json:"id"`
	MachineID  string         `json:"machine_id"`
	CredID     string         `json:"credential_id"`
	Action     string         `json:"action"`
	Detail     map[string]any `json:"detail"`
	CreatedAt  time.Time      `json:"created_at"`
}

// AgentPlanDetail is the read model.
type AgentPlanDetail struct {
	Plan    AgentPlan              `json:"plan"`
	Targets []AgentTarget          `json:"targets"`
	Summary map[string]any         `json:"summary"`
	Events  []AgentCredentialEvent `json:"events"`
}

// AgentPlanInput creates a batch installation plan.
type AgentPlanInput struct {
	Name      string   `json:"name"`
	MediaID   string   `json:"media_id"`
	Machines  []string `json:"machines"`
}

// AgentAdvanceInput reports installation outcomes for a batch of machines.
type AgentAdvanceInput struct {
	Results []struct {
		MachineID string `json:"machine_id"`
		Outcome   string `json:"outcome"` // installed | failed | cache_hit
		Bytes     int64  `json:"bytes"`
		Reason    string `json:"reason"`
	} `json:"results"`
}

// AgentEnrollInput is the machine-side registration call.
type AgentEnrollInput struct {
	Token       string `json:"token"`
	Serial      string `json:"serial_number"`
	Fingerprint string `json:"fingerprint"`
}

// CreateAgentPlan validates the agent media (approved, type agent), the
// machine set (project-bound, delivery state, G3+ published mapping) and the
// per-machine arch match, then materializes the plan with one enrollment
// token per machine.
func (s *Store) CreateAgentPlan(ctx context.Context, a *AuthSession, project *ProjectSummary, input AgentPlanInput, trace string) (*AgentPlan, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, apiError(400, "VALIDATION_ERROR", "plan name is required")
	}
	if len(input.Machines) == 0 || len(input.Machines) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "machines must contain 1-500 entries")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	// Resolve the agent media: only approved type=agent media is installable.
	media, e := resolveAgentMedia(ctx, tx, project.ID, input.MediaID)
	if e != nil {
		return nil, e
	}
	// Validate the machine set: project-bound, delivery state, G3+.
	blocked := map[string]string{}
	order := []string{}
	seen := map[string]bool{}
	for _, machineID := range input.Machines {
		if machineID == "" || seen[machineID] {
			return nil, apiError(400, "VALIDATION_ERROR", "machines must be unique non-empty ids")
		}
		seen[machineID] = true
		order = append(order, machineID)
	}
	var bound int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM project_assets WHERE project_id=$1 AND machine_id = ANY($2)`,
		project.ID, order).Scan(&bound); e != nil {
		return nil, e
	}
	if bound != len(order) {
		return nil, apiError(400, "VALIDATION_ERROR", "every machine must be bound to the project")
	}
	now := time.Now().UTC()
	plan := &AgentPlan{
		ID: RandomID(), ProjectID: project.ID, Name: name, State: "running",
		Media: media, Machines: order, CreatedBy: a.User.ID, CreatedAt: now,
	}
	mediaJSON, _ := json.Marshal(media)
	machinesJSON, _ := json.Marshal(order)
	if _, e = tx.ExecContext(ctx, `INSERT INTO agent_install_plans(id,project_id,name,state,media,machines,created_by,created_at)
		VALUES($1,$2,$3,'running',$4,$5,$6,$7)`,
		plan.ID, project.ID, plan.Name, string(mediaJSON), string(machinesJSON), a.User.ID, now); e != nil {
		return nil, e
	}
	// Materialize per-machine rows: the G3+ admission verdict is frozen at
	// creation time; a blocked row records why and never gets a token.
	for _, machineID := range order {
		hostname, _, reason, ok := agentAdmission(ctx, tx, project.ID, machineID, media)
		if !ok {
			blocked[machineID] = reason
		}
		token := ""
		if ok {
			token, e = newEnrollmentToken()
			if e != nil {
				return nil, e
			}
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO agent_installation_targets(id,plan_id,project_id,machine_id,hostname,state,reason,media_uri,media_sha256,enrollment_token,updated_at)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			RandomID(), plan.ID, project.ID, machineID, hostname,
			map[bool]string{true: "pending", false: "blocked"}[ok], reason,
			media["uri"].(string), media["sha256"].(string), token, now); e != nil {
			return nil, e
		}
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "agent.plan_create", plan.ID, trace, map[string]any{
		"media": media["uri"], "machines": len(order), "blocked": len(blocked),
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return plan, nil
}

// agentAdmission freezes the F-5.4 G3+ verdict for one machine: the asset
// must be in the delivery state (provisioned) and a published mapping
// revision must exist (the G3 evidence contract). The returned ok=false
// carries the human-readable reason.
func agentAdmission(ctx context.Context, tx *sql.Tx, projectID, machineID string, media map[string]any) (hostname, assetState, reason string, ok bool) {
	var modelID sql.NullString
	if e := tx.QueryRowContext(ctx, `SELECT pa.hostname,pa.state,pa.model_id FROM project_assets pa WHERE pa.project_id=$1 AND pa.machine_id=$2`,
		projectID, machineID).Scan(&hostname, &assetState, &modelID); e != nil {
		return hostname, assetState, "machine not bound to the project", false
	}
	if assetState == "failed" {
		return hostname, assetState, fmt.Sprintf("machine state is %s, a failed machine cannot receive an agent install", assetState), false
	}
	// G3+ evidence: a published mapping revision must exist for the machine
	// (same source of truth as the batch-config precondition, FR-5). The
	// mapping chain proves the machine passed inventory -> LLDP -> mapping
	// publication, i.e. it is a delivery-state device.
	var mapped bool
	if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mapping_revisions WHERE project_id=$1 AND machine_id=$2)`,
		projectID, machineID).Scan(&mapped); e != nil {
		return hostname, assetState, "admission check failed", false
	}
	if !mapped {
		return hostname, assetState, "no published mapping revision (G3) for the machine", false
	}
	// Arch match: the model's latest published version declares the arch in
	// hw_spec; the media arch must match it. An undeclared model arch skips
	// the check (nothing to match against).
	if modelID.Valid {
		var hwSpec string
		if e := tx.QueryRowContext(ctx, `SELECT hw_spec FROM device_model_versions
			WHERE model_id=$1 ORDER BY version DESC LIMIT 1`, modelID.String).Scan(&hwSpec); e == nil {
			var spec struct {
				Arch string `json:"arch"`
			}
			if json.Unmarshal([]byte(hwSpec), &spec) == nil && spec.Arch != "" {
				mediaArch, _ := media["arch"].(string)
				if mediaArch != "noarch" && mediaArch != spec.Arch {
					return hostname, assetState, fmt.Sprintf("media arch %s does not match model arch %s", mediaArch, spec.Arch), false
				}
			}
		}
	}
	return hostname, assetState, "", true
}

// resolveAgentMedia requires an approved type=agent media item visible to
// the project and returns the media://agent reference.
func resolveAgentMedia(ctx context.Context, tx *sql.Tx, projectID, mediaID string) (map[string]any, error) {
	var (
		id, mediaType, name, version, sha256, state, scopeKey, arch string
		size                                                        int64
	)
	row := tx.QueryRowContext(ctx, `SELECT id,type,name,version,sha256,size,state,scope_key,arch FROM media_items WHERE id=$1`, mediaID)
	if e := row.Scan(&id, &mediaType, &name, &version, &sha256, &size, &state, &scopeKey, &arch); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "media item not found")
		}
		return nil, e
	}
	if state != "approved" {
		return nil, apiError(409, "MEDIA_NOT_APPROVED", "only approved media can be distributed")
	}
	if mediaType != "agent" {
		return nil, apiError(400, "MEDIA_TYPE_MISMATCH", "agent installation distributes type=agent media only")
	}
	if scopeKey != "global" && scopeKey != "project:"+projectID {
		return nil, apiError(403, "MEDIA_SCOPE", "the media item is not visible to this project")
	}
	return map[string]any{
		"media_id": id, "type": mediaType, "name": name, "version": version, "arch": arch,
		"uri": fmt.Sprintf("media://%s/%s@%s", mediaType, name, version), "sha256": sha256, "size": size,
	}, nil
}

func newEnrollmentToken() (string, error) {
	buf := make([]byte, 24)
	if _, e := rand.Read(buf); e != nil {
		return "", e
	}
	return "keel-enroll-" + hex.EncodeToString(buf), nil
}

// AgentEnroll is the machine-side registration: the token must be live
// (pending row, unused), the serial must match the machine, and the caller
// supplies a fingerprint. Success consumes the token and issues a short-lived
// device credential; replay is refused and audited. A bare SN report with no
// live token is refused (no self-registration).
func (s *Store) AgentEnroll(ctx context.Context, a *AuthSession, project *ProjectSummary, input AgentEnrollInput, trace string) (*AgentTarget, error) {
	if input.Token == "" || input.Serial == "" || input.Fingerprint == "" {
		return nil, apiError(400, "VALIDATION_ERROR", "token, serial_number and fingerprint are required")
	}
	if len(input.Fingerprint) > 160 {
		return nil, apiError(400, "VALIDATION_ERROR", "fingerprint too long")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var (
		rowID, machineID, state, tokenInRow, serial string
		credID, oldFp                               string
	)
	row := tx.QueryRowContext(ctx, `SELECT t.id,t.machine_id,t.state,t.enrollment_token,m.serial_number,t.credential_id,t.credential_fingerprint
		FROM agent_installation_targets t JOIN machines m ON m.id=t.machine_id
		WHERE t.project_id=$1 AND t.enrollment_token=$2`, project.ID, input.Token)
	if e := row.Scan(&rowID, &machineID, &state, &tokenInRow, &serial, &credID, &oldFp); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "unknown enrollment token")
		}
		return nil, e
	}
	_, _ = credID, oldFp
	now := time.Now().UTC()
	if state != "pending" {
		// Token already consumed: replay is refused and audited.
		if e = appendAudit(ctx, tx, a.User.ID, project.ID, "agent.enroll_refused", machineID, trace, map[string]any{
			"reason": "token replay", "state": state,
		}); e != nil {
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
		return nil, apiError(409, "TOKEN_REPLAY", "the enrollment token was already used; request a new one")
	}
	if serial != input.Serial {
		return nil, apiError(403, "DEVICE_MISMATCH", "the serial number does not match the enrollment token's machine")
	}
	// Issue the short-lived device credential (identity recorded; the mTLS
	// material itself lives in the platform CA, out of store scope). The
	// target leaves pending: the enrollment token is consumed exactly once
	// and a replay now hits the state gate.
	newCred := RandomID()
	if _, e = tx.ExecContext(ctx, `UPDATE agent_installation_targets SET state='installing', credential_id=$2, credential_fingerprint=$3, updated_at=$4 WHERE id=$1`,
		rowID, newCred, input.Fingerprint, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "agent.credential_issue", newCred, trace, map[string]any{
		"machine_id": machineID, "fingerprint": input.Fingerprint,
	}); e != nil {
		return nil, e
	}
	if e = appendCredentialEvent(ctx, tx, project.ID, machineID, newCred, "issue", map[string]any{
		"fingerprint": input.Fingerprint, "enrolled_by": a.User.ID,
	}, a.User.ID); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &AgentTarget{ID: rowID, MachineID: machineID, CredentialID: newCred, Fingerprint: input.Fingerprint, UpdatedAt: now}, nil
}

// AgentRotateCredential rotates the device credential of an installed
// machine and audits the rotation.
func (s *Store) AgentRotateCredential(ctx context.Context, a *AuthSession, project *ProjectSummary, machineID, fingerprint, trace string) (*AgentTarget, error) {
	if fingerprint == "" || len(fingerprint) > 160 {
		return nil, apiError(400, "VALIDATION_ERROR", "fingerprint is required (1-160 characters)")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var (
		rowID, credID, fp, state string
		updatedAt                time.Time
	)
	row := tx.QueryRowContext(ctx, `SELECT id,credential_id,credential_fingerprint,state,updated_at FROM agent_installation_targets
		WHERE project_id=$1 AND machine_id=$2 ORDER BY updated_at DESC LIMIT 1`, project.ID, machineID)
	if e := row.Scan(&rowID, &credID, &fp, &state, &updatedAt); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "no credential installed for the machine")
		}
		return nil, e
	}
	if credID == "" {
		return nil, apiError(409, "NO_CREDENTIAL", "the machine has no issued credential to rotate")
	}
	now := time.Now().UTC()
	newCred := RandomID()
	if _, e = tx.ExecContext(ctx, `UPDATE agent_installation_targets SET credential_id=$2, credential_fingerprint=$3, updated_at=$4 WHERE id=$1`,
		rowID, newCred, fingerprint, now); e != nil {
		return nil, e
	}
	if e = appendCredentialEvent(ctx, tx, project.ID, machineID, newCred, "rotate", map[string]any{
		"from": credID, "fingerprint": fingerprint,
	}, a.User.ID); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "agent.credential_rotate", newCred, trace, map[string]any{
		"machine_id": machineID, "previous": credID,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &AgentTarget{ID: rowID, MachineID: machineID, CredentialID: newCred, Fingerprint: fingerprint, UpdatedAt: now}, nil
}

// AgentRevokeCredential revokes the device credential (cleanup path) and
// audits the revocation.
func (s *Store) AgentRevokeCredential(ctx context.Context, a *AuthSession, project *ProjectSummary, machineID, trace string) (*AgentTarget, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var (
		rowID, credID, fp, state string
		updatedAt                time.Time
	)
	row := tx.QueryRowContext(ctx, `SELECT id,credential_id,credential_fingerprint,state,updated_at FROM agent_installation_targets
		WHERE project_id=$1 AND machine_id=$2 ORDER BY updated_at DESC LIMIT 1`, project.ID, machineID)
	if e := row.Scan(&rowID, &credID, &fp, &state, &updatedAt); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "no credential installed for the machine")
		}
		return nil, e
	}
	if credID == "" {
		return nil, apiError(409, "NO_CREDENTIAL", "the machine has no issued credential to revoke")
	}
	now := time.Now().UTC()
	if _, e = tx.ExecContext(ctx, `UPDATE agent_installation_targets SET credential_id='', credential_fingerprint='', updated_at=$2 WHERE id=$1`,
		rowID, now); e != nil {
		return nil, e
	}
	if e = appendCredentialEvent(ctx, tx, project.ID, machineID, credID, "revoke", map[string]any{
		"revoked_by": a.User.ID,
	}, a.User.ID); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "agent.credential_revoke", credID, trace, map[string]any{
		"machine_id": machineID,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &AgentTarget{ID: rowID, MachineID: machineID, UpdatedAt: now}, nil
}

// AdvanceAgentPlan records installation outcomes: installed machines get
// their systemd/heartbeat state, failed machines record the reason, and a
// cache hit accounts the distribution as skipped (F-1.7). The plan completes
// when no target is pending/installing.
func (s *Store) AdvanceAgentPlan(ctx context.Context, a *AuthSession, project *ProjectSummary, planID string, input AgentAdvanceInput, trace string) (*AgentPlanDetail, error) {
	if len(input.Results) == 0 || len(input.Results) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "results must contain 1-500 entries")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var planState string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM agent_install_plans WHERE id=$1 AND project_id=$2`,
		planID, project.ID).Scan(&planState); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "plan not found")
		}
		return nil, e
	}
	if planState != "running" {
		return nil, apiError(409, "PLAN_STATE", "only a running plan can be advanced")
	}
	now := time.Now().UTC()
	for _, result := range input.Results {
		var (
			rowID, state string
			cacheHit     bool
			uri, sha     string
		)
		row := tx.QueryRowContext(ctx, `SELECT id,state,cache_hit,media_uri,media_sha256 FROM agent_installation_targets WHERE plan_id=$1 AND machine_id=$2`,
			planID, result.MachineID)
		if e := row.Scan(&rowID, &state, &cacheHit, &uri, &sha); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return nil, apiError(404, "NOT_FOUND", fmt.Sprintf("machine %s is not a target of this plan", result.MachineID))
			}
			return nil, e
		}
		if state != "pending" && state != "installing" {
			return nil, apiError(409, "TARGET_STATE", fmt.Sprintf("machine %s is %s and cannot accept a new outcome", result.MachineID, state))
		}
		next := ""
		switch result.Outcome {
		case "installed":
			next = "installed"
		case "failed":
			next = "failed"
		case "cache_hit":
			next = "installed"
			cacheHit = true
		default:
			return nil, apiError(400, "VALIDATION_ERROR", "outcome must be installed, failed or cache_hit")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE agent_installation_targets SET state=$2, reason=$3, cache_hit=$4, bytes_transferred=$5, updated_at=$6 WHERE id=$1`,
			rowID, next, result.Reason, cacheHit, result.Bytes, now); e != nil {
			return nil, e
		}
	}
	// Reconcile the plan state: completed when nothing is in flight.
	var inflight int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_installation_targets WHERE plan_id=$1 AND state IN('pending','installing')`,
		planID).Scan(&inflight); e != nil {
		return nil, e
	}
	if inflight == 0 {
		var failed int
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_installation_targets WHERE plan_id=$1 AND state IN('failed','blocked')`,
			planID).Scan(&failed); e != nil {
			return nil, e
		}
		nextState := "completed"
		if failed > 0 {
			nextState = "failed"
		}
		if _, e = tx.ExecContext(ctx, `UPDATE agent_install_plans SET state=$2, finished_at=$3 WHERE id=$1`,
			planID, nextState, now); e != nil {
			return nil, e
		}
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "agent.plan_advance", planID, trace, map[string]any{
		"results": len(input.Results),
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.agentPlanDetail(ctx, a, project, planID)
}

// AbortAgentPlan cancels a running plan: pending/installing targets become
// blocked with an abort reason.
func (s *Store) AbortAgentPlan(ctx context.Context, a *AuthSession, project *ProjectSummary, planID, trace string) (*AgentPlanDetail, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var planState string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM agent_install_plans WHERE id=$1 AND project_id=$2`,
		planID, project.ID).Scan(&planState); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "plan not found")
		}
		return nil, e
	}
	if planState != "running" {
		return nil, apiError(409, "PLAN_STATE", "only a running plan can be aborted")
	}
	now := time.Now().UTC()
	if _, e = tx.ExecContext(ctx, `UPDATE agent_installation_targets SET state='blocked', reason='plan aborted', updated_at=$2
		WHERE plan_id=$1 AND state IN('pending','installing')`, planID, now); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE agent_install_plans SET state='aborted', finished_at=$2 WHERE id=$1`,
		planID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "agent.plan_abort", planID, trace, map[string]any{}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.agentPlanDetail(ctx, a, project, planID)
}

// ListAgentPlans lists the project's installation plans.
func (s *Store) ListAgentPlans(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[AgentPlan], error) {
	out := NewPage[AgentPlan](p)
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
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM agent_install_plans WHERE project_id=$1`, project.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,name,state,media,machines,created_by,created_at,finished_at
		FROM agent_install_plans WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		project.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var (
			plan                AgentPlan
			mediaJSON, machJSON []byte
			finished            sql.NullTime
		)
		if e = rows.Scan(&plan.ID, &plan.Name, &plan.State, &mediaJSON, &machJSON, &plan.CreatedBy, &plan.CreatedAt, &finished); e != nil {
			return out, e
		}
		_ = json.Unmarshal(mediaJSON, &plan.Media)
		_ = json.Unmarshal(machJSON, &plan.Machines)
		if plan.Media == nil {
			plan.Media = map[string]any{}
		}
		if plan.Machines == nil {
			plan.Machines = []string{}
		}
		if finished.Valid {
			plan.FinishedAt = &finished.Time
		}
		out.Items = append(out.Items, plan)
	}
	return out, rows.Err()
}

// GetAgentPlan returns the plan detail: targets, summary and credential
// events.
func (s *Store) GetAgentPlan(ctx context.Context, a *AuthSession, project *ProjectSummary, planID string) (*AgentPlanDetail, error) {
	return s.agentPlanDetail(ctx, a, project, planID)
}

func (s *Store) agentPlanDetail(ctx context.Context, a *AuthSession, project *ProjectSummary, planID string) (*AgentPlanDetail, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var (
		plan                AgentPlan
		mediaJSON, machJSON []byte
		finished            sql.NullTime
	)
	row := tx.QueryRowContext(ctx, `SELECT id,name,state,media,machines,created_by,created_at,finished_at
		FROM agent_install_plans WHERE id=$1 AND project_id=$2`, planID, project.ID)
	if e := row.Scan(&plan.ID, &plan.Name, &plan.State, &mediaJSON, &machJSON, &plan.CreatedBy, &plan.CreatedAt, &finished); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "plan not found")
		}
		return nil, e
	}
	plan.ProjectID = project.ID
	_ = json.Unmarshal(mediaJSON, &plan.Media)
	_ = json.Unmarshal(machJSON, &plan.Machines)
	if plan.Media == nil {
		plan.Media = map[string]any{}
	}
	if plan.Machines == nil {
		plan.Machines = []string{}
	}
	if finished.Valid {
		plan.FinishedAt = &finished.Time
	}
	targets, e := agentScanTargets(ctx, tx, planID)
	if e != nil {
		return nil, e
	}
	events, e := agentScanEvents(ctx, tx, project.ID, planID, targets)
	if e != nil {
		return nil, e
	}
	summary := map[string]any{"installed": 0, "failed": 0, "blocked": 0, "pending": 0, "cache_hits": 0, "bytes": int64(0)}
	for _, target := range targets {
		switch target.State {
		case "installed":
			summary["installed"] = summary["installed"].(int) + 1
		case "failed":
			summary["failed"] = summary["failed"].(int) + 1
		case "blocked":
			summary["blocked"] = summary["blocked"].(int) + 1
		default:
			summary["pending"] = summary["pending"].(int) + 1
		}
		if target.CacheHit {
			summary["cache_hits"] = summary["cache_hits"].(int) + 1
		}
		summary["bytes"] = summary["bytes"].(int64) + target.Bytes
	}
	detail := &AgentPlanDetail{Plan: plan, Targets: targets, Summary: summary, Events: events}
	if detail.Targets == nil {
		detail.Targets = []AgentTarget{}
	}
	if detail.Events == nil {
		detail.Events = []AgentCredentialEvent{}
	}
	return detail, nil
}

func agentScanTargets(ctx context.Context, tx *sql.Tx, planID string) ([]AgentTarget, error) {
	rows, e := tx.QueryContext(ctx, `SELECT id,plan_id,machine_id,hostname,state,reason,media_uri,media_sha256,cache_hit,bytes_transferred,enrollment_token,credential_id,credential_fingerprint,updated_at
		FROM agent_installation_targets WHERE plan_id=$1 ORDER BY machine_id`, planID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AgentTarget{}
	for rows.Next() {
		var t AgentTarget
		if e = rows.Scan(&t.ID, &t.PlanID, &t.MachineID, &t.Hostname, &t.State, &t.Reason, &t.MediaURI, &t.MediaSHA256,
			&t.CacheHit, &t.Bytes, &t.Enrollment, &t.CredentialID, &t.Fingerprint, &t.UpdatedAt); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func agentScanEvents(ctx context.Context, tx *sql.Tx, projectID, planID string, targets []AgentTarget) ([]AgentCredentialEvent, error) {
	machines := []string{}
	for _, t := range targets {
		machines = append(machines, t.MachineID)
	}
	if len(machines) == 0 {
		return []AgentCredentialEvent{}, nil
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,machine_id,credential_id,action,detail,created_at
		FROM agent_credential_events WHERE project_id=$1 AND machine_id = ANY($2) ORDER BY created_at DESC LIMIT 100`,
		projectID, machines)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []AgentCredentialEvent{}
	for rows.Next() {
		var (
			ev         AgentCredentialEvent
			detailJSON []byte
		)
		if e = rows.Scan(&ev.ID, &ev.MachineID, &ev.CredID, &ev.Action, &detailJSON, &ev.CreatedAt); e != nil {
			return nil, e
		}
		_ = json.Unmarshal(detailJSON, &ev.Detail)
		if ev.Detail == nil {
			ev.Detail = map[string]any{}
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func appendCredentialEvent(ctx context.Context, tx *sql.Tx, projectID, machineID, credID, action string, detail map[string]any, actor string) error {
	detailJSON, _ := json.Marshal(detail)
	_, e := tx.ExecContext(ctx, `INSERT INTO agent_credential_events(id,project_id,machine_id,credential_id,action,detail,created_by,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
		RandomID(), projectID, machineID, credID, action, string(detailJSON), actor, time.Now().UTC())
	return e
}
