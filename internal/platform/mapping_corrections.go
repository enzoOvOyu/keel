package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Corrections change planned logical labels only. Physical identities and GPU
// relations must be repaired at their independent source and verified again.
type MappingNameChange struct {
	PhysicalPortID string `json:"physical_port_id"`
	LogicalName    string `json:"logical_name"`
}
type MappingCorrectionInput struct {
	ScopeHash string              `json:"scope_hash"`
	Changes   []MappingNameChange `json:"changes"`
	Reason    string              `json:"reason"`
}
type MappingCorrectionDecisionInput struct {
	Approve bool   `json:"approve"`
	Reason  string `json:"reason"`
}
type MappingCorrectionDecision struct {
	Approved   bool      `json:"approved"`
	ApprovedBy string    `json:"approved_by"`
	Reason     string    `json:"reason"`
	CreatedAt  time.Time `json:"created_at"`
}
type MappingCorrection struct {
	ID              string                     `json:"id"`
	ProjectID       string                     `json:"project_id"`
	AssetID         string                     `json:"asset_id"`
	BaseScopeHash   string                     `json:"base_scope_hash"`
	SourceScopeHash string                     `json:"source_scope_hash"`
	SourceCandidate MappingCandidate           `json:"source_candidate"`
	Changes         []MappingNameChange        `json:"changes"`
	RequestedBy     string                     `json:"requested_by"`
	Reason          string                     `json:"reason"`
	CreatedAt       time.Time                  `json:"created_at"`
	Decision        *MappingCorrectionDecision `json:"decision"`
	EffectiveState  string                     `json:"effective_state"`
}
type MappingCorrectionPage struct {
	Items    []MappingCorrection `json:"items"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

func applyMappingNames(source *MappingCandidate, changes []MappingNameChange, id string) (*MappingCandidate, error) {
	// Own the slices; a proposal cannot mutate an existing immutable candidate.
	var out MappingCandidate
	if e := json.Unmarshal(networkJSON(source), &out); e != nil {
		return nil, e
	}
	names := map[string]string{}
	for _, change := range changes {
		if _, exists := names[change.PhysicalPortID]; exists || !mappingName.MatchString(change.LogicalName) {
			return nil, apiError(400, "MAPPING_INVALID_CORRECTION", "Unique physical ports and valid logical names are required")
		}
		names[change.PhysicalPortID] = change.LogicalName
	}
	found := map[string]bool{}
	unique := map[string]bool{}
	for i := range out.Bindings {
		b := &out.Bindings[i]
		if name, ok := names[b.Plan.PhysicalPortID]; ok {
			b.LogicalName = name
			found[b.Plan.PhysicalPortID] = true
		}
		if !mappingName.MatchString(b.LogicalName) || unique[b.LogicalName] {
			return nil, apiError(400, "MAPPING_INVALID_CORRECTION", "All enabled ports must retain unique valid logical names")
		}
		unique[b.LogicalName] = true
	}
	if len(found) != len(names) || len(names) == 0 {
		return nil, apiError(400, "MAPPING_INVALID_CORRECTION", "Correction contains unknown ports or no changes")
	}
	out.Conflicts = []MappingConflict{}
	for _, c := range source.Conflicts {
		if c.Code != "MAPPING_LOGICAL_NAME_CONFLICT" && c.Code != "MAPPING_BOM_NAME_INVALID" {
			out.Conflicts = append(out.Conflicts, c)
		}
	}
	out.Ready = len(out.Conflicts) == 0
	out.Verdict = "BLOCKED"
	if out.Ready {
		out.Verdict = "PASS"
	}
	out.CorrectionID = id
	out.seal()
	return &out, nil
}

func (s *Store) mappingCandidateTx(ctx context.Context, tx *sql.Tx, project, asset string, lock bool) (*MappingCandidate, error) {
	base, e := s.mappingBaseCandidateTx(ctx, tx, project, asset, lock)
	if e != nil {
		return nil, e
	}
	var id string
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT c.id,c.changes FROM mapping_corrections c JOIN mapping_correction_decisions d ON d.correction_id=c.id AND d.approved WHERE c.project_id=$1 AND c.asset_id=$2 AND c.base_scope_hash=$3 ORDER BY d.created_at DESC,c.id DESC LIMIT 1`, project, asset, base.ScopeHash).Scan(&id, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return base, nil
	}
	if e != nil {
		return nil, e
	}
	var changes []MappingNameChange
	if e = json.Unmarshal(raw, &changes); e != nil {
		return nil, e
	}
	return applyMappingNames(base, changes, id)
}

// Mutations share the asset publication lock, so approval and publication do
// not race each other. Approval never creates a revision or renews evidence.
func (s *Store) mappingCorrectionWriteTx(ctx context.Context, a *AuthSession, project, asset string) (*sql.Tx, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*sql.Tx, error) { tx.Rollback(); return nil, e }
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return fail(e)
	}
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return fail(e)
	}
	if role != "Admin" && role != "DL" {
		return fail(ErrForbidden)
	}
	var state string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, project).Scan(&state); e != nil {
		return fail(e)
	}
	if state == "archived" || state == "delivered" {
		return fail(apiError(409, "PROJECT_READ_ONLY", "Project is read-only"))
	}
	if !s.WiringEnabled {
		return fail(apiError(409, "WIRING_DISABLED", "Mapping corrections require enabled G2 consumers"))
	}
	if e = lockKey(ctx, tx, "mapping:"+project+":"+asset); e != nil {
		return fail(e)
	}
	return tx, nil
}
func mappingCorrectionReason(reason, key string) (string, error) {
	reason = strings.TrimSpace(reason)
	if utf8.RuneCountInString(reason) < 3 || utf8.RuneCountInString(reason) > 1000 || strings.TrimSpace(key) == "" || len(key) > 128 {
		return "", apiError(400, "VALIDATION_ERROR", "Reason and Idempotency-Key required")
	}
	return reason, nil
}
func (s *Store) RequestMappingCorrection(ctx context.Context, a *AuthSession, project, asset string, in MappingCorrectionInput, key, trace string) (*MappingCorrection, error) {
	var e error
	in.Reason, e = mappingCorrectionReason(in.Reason, key)
	if e != nil {
		return nil, e
	}
	if len(in.ScopeHash) != 64 || len(in.Changes) == 0 || len(in.Changes) > 512 {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact preview and 1–512 logical name changes required")
	}
	tx, e := s.mappingCorrectionWriteTx(ctx, a, project, asset)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	identity := digest("mapping.correct:" + project + ":" + asset + ":" + a.User.ID + ":" + key)
	requestHash := digest(string(networkJSON(in)))
	prior, e := priorWiringHandshakeTx(ctx, tx, identity, requestHash)
	if e != nil {
		return nil, e
	}
	if prior != "" {
		out, e := mappingCorrectionTx(ctx, tx, project, asset, prior)
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	current, e := s.mappingCandidateTx(ctx, tx, project, asset, true)
	if e != nil {
		return nil, e
	}
	if current.ScopeHash != in.ScopeHash {
		return nil, apiError(409, "MAPPING_SCOPE_CHANGED", "Refresh the exact current preview before proposing corrections")
	}
	id := RandomID()
	proposed, e := applyMappingNames(current, in.Changes, id)
	if e != nil {
		return nil, e
	}
	if !proposed.Ready || proposed.ValidUntil == nil || !proposed.ValidUntil.After(time.Now()) {
		return nil, apiError(409, "MAPPING_PHYSICAL_EVIDENCE_REQUIRED", "Logical corrections cannot waive missing, expired or conflicting physical evidence")
	}
	// Store the complete resulting name plan, retaining earlier approved names.
	changes := make([]MappingNameChange, 0, len(proposed.Bindings))
	changed := false
	for i, b := range proposed.Bindings {
		changes = append(changes, MappingNameChange{b.Plan.PhysicalPortID, b.LogicalName})
		if b.LogicalName != current.Bindings[i].LogicalName {
			changed = true
		}
	}
	if !changed {
		return nil, apiError(400, "MAPPING_CORRECTION_UNCHANGED", "At least one logical name must change")
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].PhysicalPortID < changes[j].PhysicalPortID })
	_, e = tx.ExecContext(ctx, `INSERT INTO mapping_corrections(id,project_id,asset_id,base_scope_hash,source_scope_hash,source_candidate,changes,requested_by,reason,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,clock_timestamp())`, id, project, asset, current.BaseScopeHash, current.ScopeHash, networkJSON(current), networkJSON(changes), a.User.ID, in.Reason)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "mapping.correction.request", id, trace, map[string]any{"asset_id": asset, "source_scope_hash": current.ScopeHash, "changes": changes, "reason": in.Reason}); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,clock_timestamp())`, identity, a.User.ID, requestHash, networkJSON(map[string]string{"id": id})); e != nil {
		return nil, e
	}
	out, e := mappingCorrectionTx(ctx, tx, project, asset, id)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) DecideMappingCorrection(ctx context.Context, a *AuthSession, project, asset, id string, in MappingCorrectionDecisionInput, key, trace string) (*MappingCorrection, error) {
	var e error
	in.Reason, e = mappingCorrectionReason(in.Reason, key)
	if e != nil {
		return nil, e
	}
	tx, e := s.mappingCorrectionWriteTx(ctx, a, project, asset)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	identity := digest("mapping.correct.decide:" + project + ":" + asset + ":" + id + ":" + a.User.ID + ":" + key)
	requestHash := digest(string(networkJSON(in)))
	prior, e := priorWiringHandshakeTx(ctx, tx, identity, requestHash)
	if e != nil {
		return nil, e
	}
	if prior != "" {
		out, e := mappingCorrectionTx(ctx, tx, project, asset, prior)
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	proposal, e := mappingCorrectionTx(ctx, tx, project, asset, id)
	if e != nil {
		return nil, e
	}
	if proposal.Decision != nil {
		return nil, apiError(409, "MAPPING_CORRECTION_DECIDED", "This correction already has an immutable decision")
	}
	if in.Approve {
		current, e := s.mappingCandidateTx(ctx, tx, project, asset, true)
		if e != nil {
			return nil, e
		}
		if current.ScopeHash != proposal.SourceScopeHash || current.BaseScopeHash != proposal.BaseScopeHash {
			return nil, apiError(409, "MAPPING_SCOPE_CHANGED", "Correction sources or previous approvals changed; create a new proposal")
		}
		proposed, e := applyMappingNames(current, proposal.Changes, id)
		if e != nil {
			return nil, e
		}
		if !proposed.Ready || proposed.ValidUntil == nil || !proposed.ValidUntil.After(time.Now()) {
			return nil, apiError(409, "MAPPING_PHYSICAL_EVIDENCE_REQUIRED", "Approval cannot renew or replace physical evidence")
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO mapping_correction_decisions(correction_id,approved,approved_by,reason,created_at) VALUES($1,$2,$3,$4,clock_timestamp())`, id, in.Approve, a.User.ID, in.Reason)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "mapping.correction.decide", id, trace, map[string]any{"asset_id": asset, "approved": in.Approve, "reason": in.Reason}); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO idempotencies(id,actor_id,request_hash,response,created_at) VALUES($1,$2,$3,$4,clock_timestamp())`, identity, a.User.ID, requestHash, networkJSON(map[string]string{"id": id})); e != nil {
		return nil, e
	}
	out, e := mappingCorrectionTx(ctx, tx, project, asset, id)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

const mappingCorrectionFields = `c.id,c.project_id,c.asset_id,c.base_scope_hash,c.source_scope_hash,c.source_candidate,c.changes,c.requested_by,c.reason,c.created_at,d.approved,d.approved_by,d.reason,d.created_at`

func scanMappingCorrection(row scanner) (*MappingCorrection, error) {
	var out MappingCorrection
	var source, changes []byte
	var approved sql.NullBool
	var by, reason sql.NullString
	var at sql.NullTime
	e := row.Scan(&out.ID, &out.ProjectID, &out.AssetID, &out.BaseScopeHash, &out.SourceScopeHash, &source, &changes, &out.RequestedBy, &out.Reason, &out.CreatedAt, &approved, &by, &reason, &at)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(source, &out.SourceCandidate); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(changes, &out.Changes); e != nil {
		return nil, e
	}
	out.EffectiveState = "pending"
	if approved.Valid {
		out.Decision = &MappingCorrectionDecision{approved.Bool, by.String, reason.String, at.Time}
		out.EffectiveState = "rejected"
		if approved.Bool {
			out.EffectiveState = "approved"
		}
	}
	return &out, nil
}
func mappingCorrectionTx(ctx context.Context, tx *sql.Tx, project, asset, id string) (*MappingCorrection, error) {
	return scanMappingCorrection(tx.QueryRowContext(ctx, `SELECT `+mappingCorrectionFields+` FROM mapping_corrections c LEFT JOIN mapping_correction_decisions d ON d.correction_id=c.id WHERE c.id=$1 AND c.project_id=$2 AND c.asset_id=$3`, id, project, asset))
}
func (s *Store) ListMappingCorrections(ctx context.Context, a *AuthSession, project, asset string, page Pagination) (*MappingCorrectionPage, error) {
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
	out := &MappingCorrectionPage{Items: []MappingCorrection{}, Page: page.Page, PageSize: page.PageSize}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM mapping_corrections WHERE project_id=$1 AND asset_id=$2`, project, asset).Scan(&out.Total); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+mappingCorrectionFields+` FROM mapping_corrections c LEFT JOIN mapping_correction_decisions d ON d.correction_id=c.id WHERE c.project_id=$1 AND c.asset_id=$2 ORDER BY c.created_at DESC,c.id DESC LIMIT $3 OFFSET $4`, project, asset, page.PageSize, page.Offset())
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		item, e := scanMappingCorrection(rows)
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
	current, e := s.mappingCandidateTx(ctx, tx, project, asset, false)
	if e != nil {
		return nil, e
	}
	for i := range out.Items {
		item := &out.Items[i]
		if item.Decision != nil && !item.Decision.Approved {
			continue
		}
		if item.BaseScopeHash != current.BaseScopeHash || (item.Decision == nil && item.SourceScopeHash != current.ScopeHash) || (item.Decision != nil && current.CorrectionID != item.ID) || item.SourceCandidate.ValidUntil == nil || !item.SourceCandidate.ValidUntil.After(time.Now()) {
			item.EffectiveState = "stale"
		}
	}
	return out, tx.Commit()
}
