package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/precollect"
)

type GateWaiverScope struct {
	ProjectID           string               `json:"project_id"`
	AssetID             string               `json:"asset_id"`
	Gate                string               `json:"gate"`
	EvidenceID          string               `json:"evidence_id"`
	ScopeHash           string               `json:"scope_hash"`
	RevisionFingerprint string               `json:"revision_fingerprint"`
	PolicyID            string               `json:"policy_id"`
	PolicyHash          string               `json:"policy_hash"`
	SourceHash          string               `json:"source_hash"`
	Findings            []precollect.Finding `json:"findings"`
	ValidUntil          *time.Time           `json:"valid_until"`
	CanApprove          bool                 `json:"can_approve"`
	Blockers            []string             `json:"blockers"`
}
type GateWaiverInput struct {
	SourceHash      string    `json:"source_hash"`
	Reason          string    `json:"reason"`
	RemediationPlan string    `json:"remediation_plan"`
	ValidUntil      time.Time `json:"valid_until"`
}
type GateWaiver struct {
	ID                  string               `json:"id"`
	ProjectID           string               `json:"project_id"`
	AssetID             string               `json:"asset_id"`
	Gate                string               `json:"gate"`
	EvidenceID          string               `json:"evidence_id"`
	SourceHash          string               `json:"source_hash"`
	ScopeHash           string               `json:"scope_hash"`
	RevisionFingerprint string               `json:"revision_fingerprint"`
	PolicyID            string               `json:"policy_id"`
	PolicyHash          string               `json:"policy_hash"`
	Findings            []precollect.Finding `json:"findings"`
	Reason              string               `json:"reason"`
	RemediationPlan     string               `json:"remediation_plan"`
	ApprovedBy          string               `json:"approved_by"`
	ApprovedAt          time.Time            `json:"approved_at"`
	ValidUntil          time.Time            `json:"valid_until"`
	RevokedAt           *time.Time           `json:"revoked_at"`
}

const gateWaiverFields = `w.id,w.project_id,w.asset_id,w.gate,w.evidence_id,w.source_hash,w.scope_hash,w.revision_fingerprint,w.policy_id,w.policy_hash,w.findings,w.reason,w.remediation_plan,w.approved_by,w.approved_at,w.valid_until,(SELECT created_at FROM gate_waiver_revocations WHERE waiver_id=w.id)`

func scanGateWaiver(row scanner) (*GateWaiver, error) {
	var out GateWaiver
	var raw []byte
	if e := row.Scan(&out.ID, &out.ProjectID, &out.AssetID, &out.Gate, &out.EvidenceID, &out.SourceHash, &out.ScopeHash, &out.RevisionFingerprint, &out.PolicyID, &out.PolicyHash, &raw, &out.Reason, &out.RemediationPlan, &out.ApprovedBy, &out.ApprovedAt, &out.ValidUntil, &out.RevokedAt); e != nil {
		return nil, e
	}
	if e := json.Unmarshal(raw, &out.Findings); e != nil {
		return nil, e
	}
	return &out, nil
}
func (s *Store) gateWaiverScopeTx(ctx context.Context, tx *sql.Tx, project, asset string) (*GateWaiverScope, error) {
	out := &GateWaiverScope{ProjectID: project, AssetID: asset, Gate: "G1", Findings: []precollect.Finding{}, Blockers: []string{}, CanApprove: true}
	block := func(reason string) { out.CanApprove = false; out.Blockers = append(out.Blockers, reason) }
	current, e := s.currentG1Tx(ctx, tx, project, asset)
	if e != nil {
		return nil, e
	}
	if current.Evidence == nil {
		block("G1_EVIDENCE_REQUIRED")
		return out, nil
	}
	proof := current.Evidence
	out.EvidenceID, out.ScopeHash, out.RevisionFingerprint, out.ValidUntil = proof.ID, proof.ScopeHash, proof.RevisionFingerprint, &proof.ValidUntil
	out.Findings = append(out.Findings, proof.Evaluation.Findings...)
	var origin, state, projectState string
	if e = tx.QueryRowContext(ctx, `SELECT r.origin,a.state,p.state FROM validation_precollect_records r JOIN project_assets a ON a.id=$1 AND a.project_id=$2 JOIN projects p ON p.id=a.project_id WHERE r.id=$3`, asset, project, proof.PrecollectID).Scan(&origin, &state, &projectState); e != nil {
		return nil, e
	}
	if origin != "real" || proof.Validity != "fresh" {
		block("CURRENT_REAL_G1_REQUIRED")
	}
	if state == "maintained" || state == "planned" || state == "provisioning" || state == "failed" || projectState == "archived" || projectState == "delivered" {
		block("GATE_WAIVER_STAGE_BLOCKED")
	}
	if proof.Verdict != "BLOCKED" || len(out.Findings) == 0 {
		block("G1_MISSING_FIELD_WAIVER_ONLY")
	}
	var fieldsRaw []byte
	e = tx.QueryRowContext(ctx, `SELECT policy_id,policy_hash,fields FROM keel_g1_waiver_policy($1)`, project).Scan(&out.PolicyID, &out.PolicyHash, &fieldsRaw)
	if errors.Is(e, sql.ErrNoRows) {
		block("G1_WAIVER_POLICY_REQUIRED")
	} else if e != nil {
		return nil, e
	} else {
		var fields []string
		if json.Unmarshal(fieldsRaw, &fields) != nil {
			return nil, apiError(409, "G1_WAIVER_POLICY_INVALID", "gate.g1.waiver_fields must be a JSON array of exact supported field names")
		}
		allowed := map[string]bool{}
		for _, field := range fields {
			if !gateWaiverFieldSupported(field) || allowed[field] {
				return nil, apiError(409, "G1_WAIVER_POLICY_INVALID", "Waiver policy includes duplicate or unsupported fields")
			}
			allowed[field] = true
		}
		for _, finding := range out.Findings {
			if finding.Code != "PRECOLLECT_FIELD_MISSING" || finding.Verdict != "BLOCKED" || !allowed[finding.Field] {
				block("G1_FINDING_NOT_WAIVABLE:" + finding.Field)
			}
		}
	}
	out.SourceHash = digest(string(networkJSON(map[string]any{"project_id": project, "asset_id": asset, "gate": "G1", "evidence_id": out.EvidenceID, "scope_hash": out.ScopeHash, "revision_fingerprint": out.RevisionFingerprint, "policy_id": out.PolicyID, "policy_hash": out.PolicyHash, "findings": out.Findings, "valid_until": out.ValidUntil})))
	return out, nil
}
func gateWaiverFieldSupported(field string) bool {
	switch field {
	case "nic.dom", "nic.driver_version", "gpu.hbm_ecc_observation", "system.os_release":
		return true
	}
	return false
}
func (s *Store) GateWaiverScope(ctx context.Context, a *AuthSession, project, asset string) (*GateWaiverScope, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	out, e := s.gateWaiverScopeTx(ctx, tx, project, asset)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) ApproveGateWaiver(ctx context.Context, a *AuthSession, project, asset string, in GateWaiverInput, key, trace string) (*GateWaiver, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	in.RemediationPlan = strings.TrimSpace(in.RemediationPlan)
	if len(in.SourceHash) != 64 || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 || utf8.RuneCountInString(in.RemediationPlan) < 3 || utf8.RuneCountInString(in.RemediationPlan) > 4000 || key == "" || len(key) > 128 {
		return nil, apiError(400, "VALIDATION_ERROR", "Current waiver scope, bounded reason, remediation, deadline and Idempotency-Key required")
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
	identity := digest("gate.waive:" + project + ":" + asset + ":" + a.User.ID + ":" + key)
	hash := digest(string(networkJSON(in)))
	if e = lockKey(ctx, tx, "gate-waiver-request:"+identity); e != nil {
		return nil, e
	}
	prior, e := priorWiringHandshakeTx(ctx, tx, identity, hash)
	if e != nil {
		return nil, e
	}
	if prior != "" {
		out, e := scanGateWaiver(tx.QueryRowContext(ctx, `SELECT `+gateWaiverFields+` FROM gate_waivers w WHERE w.id=$1 AND w.project_id=$2 AND w.asset_id=$3`, prior, project, asset))
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	scope, e := s.gateWaiverScopeTx(ctx, tx, project, asset)
	if e != nil {
		return nil, e
	}
	if scope.SourceHash != in.SourceHash {
		return nil, apiError(409, "GATE_WAIVER_SCOPE_CHANGED", "Refresh the current evidence and waiver policy")
	}
	if !scope.CanApprove || scope.ValidUntil == nil {
		return nil, apiError(409, "GATE_WAIVER_BLOCKED", "Only current real policy-permitted missing fields may be waived")
	}
	now := time.Now().UTC()
	if !in.ValidUntil.After(now) || in.ValidUntil.After(*scope.ValidUntil) {
		return nil, apiError(400, "VALIDATION_ERROR", "Waiver deadline must remain within original evidence validity")
	}
	id := RandomID()
	_, e = tx.ExecContext(ctx, `INSERT INTO gate_waivers(id,project_id,asset_id,gate,evidence_id,source_hash,scope_hash,revision_fingerprint,policy_id,policy_hash,findings,reason,remediation_plan,approved_by,approved_at,valid_until)VALUES($1,$2,$3,'G1',$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, id, project, asset, scope.EvidenceID, scope.SourceHash, scope.ScopeHash, scope.RevisionFingerprint, scope.PolicyID, scope.PolicyHash, networkJSON(scope.Findings), in.Reason, in.RemediationPlan, a.User.ID, now, in.ValidUntil)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "gate.waive", id, trace, map[string]any{"gate": "G1", "source_hash": scope.SourceHash, "reason": in.Reason, "remediation_plan": in.RemediationPlan, "valid_until": in.ValidUntil}); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at)VALUES($1,$2,$3,$4,clock_timestamp())`, identity, a.User.ID, hash, networkJSON(map[string]string{"id": id})); e != nil {
		return nil, e
	}
	out, e := scanGateWaiver(tx.QueryRowContext(ctx, `SELECT `+gateWaiverFields+` FROM gate_waivers w WHERE w.id=$1`, id))
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) RevokeGateWaiver(ctx context.Context, a *AuthSession, project, asset, id, reason, trace string) error {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) < 3 || utf8.RuneCountInString(reason) > 1000 {
		return apiError(400, "VALIDATION_ERROR", "Bounded revocation reason required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return e
	}
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return e
	}
	if role != "Admin" && role != "DL" {
		return ErrForbidden
	}
	var exists bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM gate_waivers WHERE id=$1 AND project_id=$2 AND asset_id=$3)`, id, project, asset).Scan(&exists); e != nil {
		return e
	}
	if !exists {
		return ErrNotFound
	}
	changed, e := tx.ExecContext(ctx, `INSERT INTO gate_waiver_revocations(id,waiver_id,revoked_by,reason,created_at)VALUES($1,$2,$3,$4,clock_timestamp())ON CONFLICT(waiver_id)DO NOTHING`, RandomID(), id, a.User.ID, reason)
	if e != nil {
		return e
	}
	if n, e := changed.RowsAffected(); e != nil {
		return e
	} else if n == 0 {
		var original string
		if e = tx.QueryRowContext(ctx, `SELECT reason FROM gate_waiver_revocations WHERE waiver_id=$1`, id).Scan(&original); e != nil {
			return e
		}
		if original != reason {
			return apiError(409, "GATE_WAIVER_ALREADY_REVOKED", "Waiver was already revoked with its immutable original reason")
		}
		return tx.Commit()
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "gate.waiver.revoke", id, trace, map[string]any{"reason": reason}); e != nil {
		return e
	}
	return tx.Commit()
}

type GateWaiverPage struct {
	Items    []GateWaiver `json:"items"`
	Total    int          `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

func (s *Store) ListGateWaivers(ctx context.Context, a *AuthSession, project, asset string, page Pagination) (*GateWaiverPage, error) {
	if e := page.Validate(); e != nil {
		return nil, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	out := &GateWaiverPage{Items: []GateWaiver{}, Page: page.Page, PageSize: page.PageSize}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM gate_waivers WHERE project_id=$1 AND asset_id=$2`, project, asset).Scan(&out.Total); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+gateWaiverFields+` FROM gate_waivers w WHERE w.project_id=$1 AND w.asset_id=$2 ORDER BY w.approved_at DESC,w.id DESC LIMIT $3 OFFSET $4`, project, asset, page.PageSize, page.Offset())
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		item, e := scanGateWaiver(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out.Items = append(out.Items, *item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

type GateWaiverRevocationInput struct {
	Reason string `json:"reason"`
}
