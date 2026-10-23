package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"time"
)

type ZTPMockKeaPlan struct {
	BatchRef   string    `json:"batch_ref"`
	StageRef   string    `json:"stage_ref"`
	StagedHash string    `json:"staged_hash,omitempty"`
	ActiveHash string    `json:"active_hash,omitempty"`
	Version    int64     `json:"version"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type ZTPMockKeaCommitInput struct {
	BatchRef        string `json:"batch_ref"`
	StageRef        string `json:"stage_ref"`
	StagedHash      string `json:"staged_hash"`
	ExpectedVersion int64  `json:"expected_version"`
}

// StageZTPMockKeaPlan only records validated candidate options. In particular,
// it never sends Kea a config-set or changes the active simulated DORA result.
func (s *Store) StageZTPMockKeaPlan(ctx context.Context, a *AuthSession, project *ProjectSummary, input fabricboot.DHCPOptions, trace string) (*ZTPMockKeaPlan, error) {
	input.ProjectID = project.ID
	mock := fabricboot.NewMockKea()
	hash, err := mock.Stage(input)
	if err != nil {
		return nil, apiError(400, "ZTP_KEA_CONFIG_INVALID", err.Error())
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	var out ZTPMockKeaPlan
	err = tx.QueryRowContext(ctx, `INSERT INTO ztp_mock_kea_plans(project_id,batch_ref,stage_ref,staged_config,staged_hash,updated_by,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7)
 ON CONFLICT(project_id,batch_ref,stage_ref) DO UPDATE SET staged_config=excluded.staged_config,staged_hash=excluded.staged_hash,version=ztp_mock_kea_plans.version+1,updated_by=excluded.updated_by,updated_at=excluded.updated_at
 RETURNING batch_ref,stage_ref,coalesce(staged_hash,''),coalesce(active_hash,''),version,updated_at`, project.ID, input.BatchRef, input.StageRef, raw, hash, a.User.ID, now).Scan(&out.BatchRef, &out.StageRef, &out.StagedHash, &out.ActiveHash, &out.Version, &out.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "ztp.mock_kea.stage", input.BatchRef+":"+input.StageRef, trace, map[string]any{"staged_hash": hash, "version": out.Version}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) CommitZTPMockKeaPlan(ctx context.Context, a *AuthSession, project *ProjectSummary, input ZTPMockKeaCommitInput, trace string) (*ZTPMockKeaPlan, error) {
	if input.StagedHash == "" || input.ExpectedVersion < 1 {
		return nil, apiError(400, "ZTP_KEA_COMMIT_INVALID", "Staged hash and expected version are required")
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	var stagedHash string
	var stagedConfig []byte
	var version int64
	err = tx.QueryRowContext(ctx, `SELECT coalesce(staged_hash,''),staged_config,version FROM ztp_mock_kea_plans WHERE project_id=$1 AND batch_ref=$2 AND stage_ref=$3 FOR UPDATE`, project.ID, input.BatchRef, input.StageRef).Scan(&stagedHash, &stagedConfig, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if version != input.ExpectedVersion || stagedHash != input.StagedHash || len(stagedConfig) == 0 {
		return nil, apiError(409, "ZTP_KEA_STAGE_CONFLICT", "Staged Kea configuration changed")
	}
	var cfg fabricboot.DHCPOptions
	if err = json.Unmarshal(stagedConfig, &cfg); err != nil {
		return nil, err
	}
	if cfg.ProjectID != project.ID || cfg.BatchRef != input.BatchRef || cfg.StageRef != input.StageRef {
		return nil, apiError(409, "ZTP_KEA_SCOPE_MISMATCH", "Staged Kea configuration scope changed")
	}
	mock := fabricboot.NewMockKea()
	recomputed, validationErr := mock.Stage(cfg)
	if validationErr != nil || recomputed != stagedHash {
		return nil, apiError(409, "ZTP_KEA_STAGE_INVALID", "Staged Kea configuration failed validation")
	}
	// A committed simulated offer must never advertise an unapproved or changed
	// bootstrap artifact. Device fetch rechecks this gate after commit as well.
	if _, err = approvedZTPSimArtifactTx(ctx, tx, project.ID, &cfg, "239"); err != nil {
		return nil, err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	out := &ZTPMockKeaPlan{BatchRef: input.BatchRef, StageRef: input.StageRef, ActiveHash: stagedHash, Version: version + 1, UpdatedAt: now}
	_, err = tx.ExecContext(ctx, `UPDATE ztp_mock_kea_plans SET active_config=staged_config,active_hash=staged_hash,staged_config=NULL,staged_hash=NULL,version=$4,updated_by=$5,updated_at=$6 WHERE project_id=$1 AND batch_ref=$2 AND stage_ref=$3`, project.ID, input.BatchRef, input.StageRef, out.Version, a.User.ID, now)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "ztp.mock_kea.commit", input.BatchRef+":"+input.StageRef, trace, map[string]any{"active_hash": out.ActiveHash, "version": out.Version}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) SimulateZTPDORA(ctx context.Context, a *AuthSession, project *ProjectSummary, req fabricboot.DORARequest) (*fabricboot.DORAResult, error) {
	req.ProjectID = project.ID
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	result, err := simulateZTPDORATx(ctx, tx, project.ID, req)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func simulateZTPDORATx(ctx context.Context, tx *sql.Tx, projectID string, req fabricboot.DORARequest) (*fabricboot.DORAResult, error) {
	req.ProjectID = projectID
	var err error
	var raw []byte
	var activeHash string
	err = tx.QueryRowContext(ctx, `SELECT active_config,coalesce(active_hash,'') FROM ztp_mock_kea_plans WHERE project_id=$1 AND batch_ref=$2 AND stage_ref=$3`, projectID, req.BatchRef, req.StageRef).Scan(&raw, &activeHash)
	if errors.Is(err, sql.ErrNoRows) {
		return &fabricboot.DORAResult{Outcome: "no_offer", Reason: "no_active_plan"}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return &fabricboot.DORAResult{Outcome: "no_offer", Reason: "no_active_plan"}, nil
	}
	var cfg fabricboot.DHCPOptions
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.ProjectID != projectID || cfg.BatchRef != req.BatchRef || cfg.StageRef != req.StageRef {
		return nil, apiError(409, "ZTP_KEA_SCOPE_MISMATCH", "Active Kea configuration scope changed")
	}
	mock := fabricboot.NewMockKea()
	digest, err := mock.Stage(cfg)
	if err != nil || digest != activeHash {
		return nil, apiError(409, "ZTP_KEA_ACTIVE_INVALID", "Active Kea configuration failed validation")
	}
	if err = mock.Commit(cfg.ProjectID, cfg.BatchRef, cfg.StageRef, digest); err != nil {
		return nil, err
	}
	result, err := mock.DORA(req)
	if err != nil {
		return nil, apiError(400, "ZTP_DORA_INVALID", err.Error())
	}
	if result.Outcome != "ack" {
		return &result, nil
	}
	// A retired or altered script invalidates an otherwise active mock offer.
	// Keep the persisted active snapshot for audit, but never simulate an ACK.
	if _, err = approvedZTPSimArtifactReadTx(ctx, tx, projectID, &cfg, "239"); err != nil {
		var policy *Error
		if errors.As(err, &policy) {
			return &fabricboot.DORAResult{Outcome: "no_offer", Reason: "bootstrap_artifact_unavailable"}, nil
		}
		return nil, err
	}
	return &result, nil
}
