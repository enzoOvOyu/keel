package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

type MappingPublishInput struct {
	ScopeHash string `json:"scope_hash"`
	Reason    string `json:"reason"`
}
type MappingRevision struct {
	ID                 string           `json:"id"`
	AssessmentID       string           `json:"assessment_id"`
	Version            int64            `json:"version"`
	PreviousID         string           `json:"previous_id"`
	Candidate          MappingCandidate `json:"candidate"`
	ConfirmedBy        string           `json:"confirmed_by"`
	CreatedAt          time.Time        `json:"created_at"`
	Reason             string           `json:"reason"`
	EffectiveVerdict   string           `json:"effective_verdict"`
	InvalidationReason string           `json:"invalidation_reason"`
}
type MappingPublication struct {
	AssessmentID string           `json:"assessment_id"`
	Candidate    MappingCandidate `json:"candidate"`
	Revision     *MappingRevision `json:"revision"`
}

// A rejected candidate is an immutable assessment, not a mapping revision or
// a gate PASS. The client submits only a preview hash, never invented bindings.
func (s *Store) PublishMapping(ctx context.Context, a *AuthSession, project, asset string, in MappingPublishInput, key, trace string) (*MappingPublication, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if !s.WiringEnabled {
		return nil, apiError(409, "WIRING_DISABLED", "Mapping publication requires enabled G2 consumers")
	}
	if len(in.ScopeHash) != 64 || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact mapping preview, reason and Idempotency-Key required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return nil, e
	}
	if role != "Admin" && role != "DL" {
		return nil, ErrForbidden
	}
	var state string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, project).Scan(&state); e != nil {
		return nil, e
	}
	if state == "archived" || state == "delivered" {
		return nil, apiError(409, "PROJECT_READ_ONLY", "Project is read-only")
	}
	identity := digest("mapping.publish:" + project + ":" + asset + ":" + a.User.ID + ":" + key)
	requestHash := digest(string(networkJSON(in)))
	if e = lockKey(ctx, tx, identity); e != nil {
		return nil, e
	}
	prior, e := priorWiringHandshakeTx(ctx, tx, identity, requestHash)
	if e != nil {
		return nil, e
	}
	if prior != "" {
		out, e := s.mappingPublicationTx(ctx, tx, project, asset, prior)
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	if e = lockKey(ctx, tx, "mapping:"+project+":"+asset); e != nil {
		return nil, e
	}
	candidate, e := s.mappingCandidateTx(ctx, tx, project, asset, true)
	if e != nil {
		return nil, e
	}
	if candidate.ScopeHash != in.ScopeHash {
		return nil, apiError(409, "MAPPING_SCOPE_CHANGED", "Mapping dependencies changed; refresh the exact preview")
	}
	now := time.Now().UTC()
	assessment := RandomID()
	if _, e = tx.ExecContext(ctx, `INSERT INTO mapping_assessments(id,project_id,asset_id,machine_id,scope_hash,candidate,created_by,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, assessment, project, asset, candidate.MachineID, candidate.ScopeHash, networkJSON(candidate), a.User.ID, in.Reason, now); e != nil {
		return nil, e
	}
	out := &MappingPublication{AssessmentID: assessment, Candidate: *candidate}
	if candidate.Ready && candidate.Verdict == "PASS" {
		if candidate.ValidUntil == nil || !candidate.ValidUntil.After(now) {
			return nil, apiError(409, "MAPPING_EVIDENCE_EXPIRED", "Refresh the expired mapping sources")
		}
		var previous string
		var version int64
		e = tx.QueryRowContext(ctx, `SELECT id,version FROM mapping_revisions WHERE project_id=$1 AND asset_id=$2 ORDER BY version DESC LIMIT 1`, project, asset).Scan(&previous, &version)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		id := RandomID()
		if _, e = tx.ExecContext(ctx, `INSERT INTO mapping_revisions(id,assessment_id,project_id,asset_id,machine_id,version,previous_id,confirmed_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, id, assessment, project, asset, candidate.MachineID, version+1, nullBOMString(previous), a.User.ID, now); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE project_assets SET state='mapped',updated_at=clock_timestamp() WHERE id=$1 AND project_id=$2 AND state='lldp'`, asset, project); e != nil {
			return nil, e
		}
		out.Revision = &MappingRevision{ID: id, AssessmentID: assessment, Version: version + 1, PreviousID: previous, Candidate: *candidate, ConfirmedBy: a.User.ID, CreatedAt: now, Reason: in.Reason, EffectiveVerdict: "PASS"}
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "mapping.assess", assessment, trace, map[string]any{"asset_id": asset, "scope_hash": candidate.ScopeHash, "verdict": candidate.Verdict, "coverage": candidate.Coverage, "reason": in.Reason, "published": out.Revision != nil}); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,clock_timestamp())`, identity, a.User.ID, requestHash, networkJSON(map[string]string{"id": assessment})); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

const mappingRevisionFields = `r.id,r.assessment_id,r.version,coalesce(r.previous_id,''),a.candidate,r.confirmed_by,r.created_at,a.reason`

func scanMappingRevision(row scanner) (*MappingRevision, error) {
	var out MappingRevision
	var raw []byte
	e := row.Scan(&out.ID, &out.AssessmentID, &out.Version, &out.PreviousID, &raw, &out.ConfirmedBy, &out.CreatedAt, &out.Reason)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(raw, &out.Candidate); e != nil {
		return nil, e
	}
	out.EffectiveVerdict = "PASS"
	return &out, nil
}
func (s *Store) mappingPublicationTx(ctx context.Context, tx *sql.Tx, project, asset, id string) (*MappingPublication, error) {
	out := &MappingPublication{AssessmentID: id}
	var raw []byte
	if e := tx.QueryRowContext(ctx, `SELECT candidate FROM mapping_assessments WHERE id=$1 AND project_id=$2 AND asset_id=$3`, id, project, asset).Scan(&raw); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, e
	}
	if e := json.Unmarshal(raw, &out.Candidate); e != nil {
		return nil, e
	}
	revision, e := scanMappingRevision(tx.QueryRowContext(ctx, `SELECT `+mappingRevisionFields+` FROM mapping_revisions r JOIN mapping_assessments a ON a.id=r.assessment_id WHERE r.assessment_id=$1 AND r.project_id=$2 AND r.asset_id=$3`, id, project, asset))
	if errors.Is(e, ErrNotFound) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	if e = s.mappingValidityTx(ctx, tx, revision); e != nil {
		return nil, e
	}
	out.Revision = revision
	return out, nil
}
func (s *Store) mappingValidityTx(ctx context.Context, tx *sql.Tx, out *MappingRevision) error {
	if out.Candidate.ValidUntil == nil || !out.Candidate.ValidUntil.After(time.Now()) {
		out.EffectiveVerdict = "BLOCKED"
		out.InvalidationReason = "MAPPING_EVIDENCE_EXPIRED"
		return nil
	}
	current, e := s.mappingCandidateTx(ctx, tx, out.Candidate.ProjectID, out.Candidate.AssetID, false)
	if e != nil {
		return e
	}
	if !current.Ready || current.ScopeHash != out.Candidate.ScopeHash {
		out.EffectiveVerdict = "BLOCKED"
		out.InvalidationReason = "MAPPING_DEPENDENCIES_CHANGED"
	}
	var latest string
	if e = tx.QueryRowContext(ctx, `SELECT id FROM mapping_revisions WHERE project_id=$1 AND asset_id=$2 ORDER BY version DESC LIMIT 1`, out.Candidate.ProjectID, out.Candidate.AssetID).Scan(&latest); e != nil {
		return e
	}
	if latest != out.ID {
		out.EffectiveVerdict = "BLOCKED"
		out.InvalidationReason = "MAPPING_REVISION_SUPERSEDED"
	}
	return nil
}

func (s *Store) GetMappingRevision(ctx context.Context, a *AuthSession, project, asset, id string) (*MappingRevision, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	out, e := scanMappingRevision(tx.QueryRowContext(ctx, `SELECT `+mappingRevisionFields+` FROM mapping_revisions r JOIN mapping_assessments a ON a.id=r.assessment_id WHERE r.id=$1 AND r.project_id=$2 AND r.asset_id=$3`, id, project, asset))
	if e != nil {
		return nil, e
	}
	if e = s.mappingValidityTx(ctx, tx, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
