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

// A scan is a reviewable draft, never a published hardware expectation.
type ModelDraftSummary struct {
	ID               string          `json:"id"`
	ModelID          string          `json:"model_id"`
	ModelName        string          `json:"model_name"`
	SourceHost       string          `json:"source_host"`
	HwSpec           json.RawMessage `json:"hw_spec"`
	ExpectedTopology json.RawMessage `json:"expected_topology"`
	CapabilityMatrix json.RawMessage `json:"capability_matrix"`
	PanelLayout      json.RawMessage `json:"panel_layout"`
	BomExpect        json.RawMessage `json:"bom_expect"`
	DefaultRolePlan  json.RawMessage `json:"default_role_plan"`
	Slots            []ModelSlot     `json:"slots"`
	ContentHash      string          `json:"content_hash"`
	CreatedBy        string          `json:"created_by"`
	CreatedAt        time.Time       `json:"created_at"`
}
type ImportModelScanInput struct {
	ModelName        string         `json:"model_name"`
	SourceHost       string         `json:"source_host"`
	HwSpec           map[string]any `json:"hw_spec"`
	ExpectedTopology map[string]any `json:"expected_topology"`
	CapabilityMatrix map[string]any `json:"capability_matrix"`
	PanelLayout      map[string]any `json:"panel_layout"`
	BomExpect        map[string]any `json:"bom_expect"`
	DefaultRolePlan  map[string]any `json:"default_role_plan"`
	Slots            []ModelSlot    `json:"slots"`
}

func (in ImportModelScanInput) payload() PublishModelVersionInput {
	return normalizedModelPayload(PublishModelVersionInput{HwSpec: in.HwSpec, ExpectedTopology: in.ExpectedTopology, CapabilityMatrix: in.CapabilityMatrix, PanelLayout: in.PanelLayout, BomExpect: in.BomExpect, DefaultRolePlan: in.DefaultRolePlan, Slots: in.Slots})
}

const draftColumns = `d.id,d.model_id,m.name,d.source_host,d.hw_spec,d.expected_topology,d.capability_matrix,d.panel_layout,d.bom_expect,d.default_role_plan,d.slots,d.content_hash,d.created_by,d.created_at`

func scanModelDraft(row scanner) (ModelDraftSummary, error) {
	var d ModelDraftSummary
	var slots []byte
	e := row.Scan(&d.ID, &d.ModelID, &d.ModelName, &d.SourceHost, &d.HwSpec, &d.ExpectedTopology, &d.CapabilityMatrix, &d.PanelLayout, &d.BomExpect, &d.DefaultRolePlan, &slots, &d.ContentHash, &d.CreatedBy, &d.CreatedAt)
	if e != nil {
		return d, e
	}
	d.Slots = []ModelSlot{}
	e = json.Unmarshal(slots, &d.Slots)
	if d.Slots == nil {
		d.Slots = []ModelSlot{}
	}
	return d, e
}
func (s *Store) ImportModelScan(ctx context.Context, a *AuthSession, project *ProjectSummary, input ImportModelScanInput, trace string) (*ModelDraftSummary, error) {
	if strings.TrimSpace(input.SourceHost) == "" || utf8.RuneCountInString(input.SourceHost) > 120 {
		return nil, apiError(400, "VALIDATION_ERROR", "source_host must be 1..120 characters")
	}
	payload := input.payload()
	if e := validateModelPayload(payload); e != nil {
		return nil, e
	}
	hash, e := modelContentHash(payload)
	if e != nil {
		return nil, e
	}
	name := input.ModelName
	if name == "" {
		name, _ = input.HwSpec["model"].(string)
	}
	if name == "" {
		name, _ = input.HwSpec["product_name"].(string)
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 120 {
		return nil, apiError(400, "VALIDATION_ERROR", "model_name must be 1..120 characters")
	}
	kind, explicitKind := "server", false
	if value, exists := input.HwSpec["kind"]; exists {
		kind, _ = value.(string)
		explicitKind = true
	}
	switch kind {
	case "rack", "server", "compute-tray", "switch", "nic", "gpu", "psu", "bmc", "optic_module":
	default:
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid scanned model kind")
	}
	if kind == "switch" && len(payload.CapabilityMatrix) == 0 {
		return nil, apiError(400, "VALIDATION_ERROR", "Scanned switch models require a capability matrix")
	}
	scope := s.scopeKeyFor(project)
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if project != nil {
		if e = requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
			return nil, e
		}
	} else if !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	modelID := RandomID()
	// ON CONFLICT avoids continuing inside an aborted PostgreSQL transaction.
	if _, e = tx.ExecContext(ctx, `INSERT INTO device_models(id,scope_key,name,kind,state,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,'active',$5,$6,$6) ON CONFLICT(scope_key,name) DO NOTHING`, modelID, scope, name, kind, a.User.ID, now); e != nil {
		return nil, e
	}
	var state, existingKind string
	if e = tx.QueryRowContext(ctx, "SELECT id,state,kind FROM device_models WHERE scope_key=$1 AND name=$2 FOR UPDATE", scope, name).Scan(&modelID, &state, &existingKind); e != nil {
		return nil, e
	}
	if state != "active" {
		return nil, apiError(409, "MODEL_RETIRED", "Retired models cannot receive new drafts")
	}
	if explicitKind && kind != existingKind {
		return nil, apiError(409, "MODEL_KIND_MISMATCH", "The scan kind differs from the existing model identity")
	}
	if existingKind == "switch" && len(payload.CapabilityMatrix) == 0 {
		return nil, apiError(400, "VALIDATION_ERROR", "Scanned switch models require a capability matrix")
	}
	// Recompute only for comparison: legacy published hashes are never rewritten.
	current, e := latestModelPayload(ctx, tx, modelID)
	if e == nil {
		currentHash, err := modelContentHash(current)
		if err != nil {
			return nil, err
		}
		if currentHash == hash {
			return nil, apiError(409, "ALREADY_CURRENT", "The scan matches the latest published model version")
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	slots, _ := json.Marshal(payload.Slots)
	// JSON equality also recognizes older draft hashes produced before canonical
	// empty-object handling. Sorting keeps slot display order out of identity.
	equivalent := `(d.content_hash=$2 OR (d.hw_spec=$3::jsonb AND d.expected_topology=$4::jsonb AND d.capability_matrix=$5::jsonb AND d.panel_layout=$6::jsonb AND d.bom_expect=$7::jsonb AND d.default_role_plan=$8::jsonb AND coalesce((SELECT jsonb_agg(slot ORDER BY (slot->>'slot_no')::int) FROM jsonb_array_elements(CASE WHEN jsonb_typeof(d.slots)='array' THEN d.slots ELSE '[]'::jsonb END) slot),'[]'::jsonb)=$9::jsonb))`
	existing, e := scanModelDraft(tx.QueryRowContext(ctx, `SELECT `+draftColumns+` FROM device_model_drafts d JOIN device_models m ON m.id=d.model_id WHERE d.model_id=$1 AND `+equivalent+` ORDER BY d.created_at,d.id LIMIT 1`, modelID, hash, modelSpecJSON(payload.HwSpec), modelSpecJSON(payload.ExpectedTopology), modelSpecJSON(payload.CapabilityMatrix), modelSpecJSON(payload.PanelLayout), modelSpecJSON(payload.BomExpect), modelSpecJSON(payload.DefaultRolePlan), slots))
	if e == nil {
		if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "model.import.reuse", existing.ID, trace, map[string]any{"model_id": modelID, "source_host": input.SourceHost, "content_hash": existing.ContentHash}); e != nil {
			return nil, e
		}
		return &existing, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	id := RandomID()
	if _, e = tx.ExecContext(ctx, `INSERT INTO device_model_drafts(id,model_id,source_host,hw_spec,expected_topology,capability_matrix,panel_layout,bom_expect,default_role_plan,slots,content_hash,created_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, id, modelID, input.SourceHost, modelSpecJSON(payload.HwSpec), modelSpecJSON(payload.ExpectedTopology), modelSpecJSON(payload.CapabilityMatrix), modelSpecJSON(payload.PanelLayout), modelSpecJSON(payload.BomExpect), modelSpecJSON(payload.DefaultRolePlan), slots, hash, a.User.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "model.import", id, trace, map[string]any{"model_id": modelID, "model_name": name, "source_host": input.SourceHost, "content_hash": hash}); e != nil {
		return nil, e
	}
	draft := &ModelDraftSummary{ID: id, ModelID: modelID, ModelName: name, SourceHost: input.SourceHost, HwSpec: modelSpecJSON(payload.HwSpec), ExpectedTopology: modelSpecJSON(payload.ExpectedTopology), CapabilityMatrix: modelSpecJSON(payload.CapabilityMatrix), PanelLayout: modelSpecJSON(payload.PanelLayout), BomExpect: modelSpecJSON(payload.BomExpect), DefaultRolePlan: modelSpecJSON(payload.DefaultRolePlan), Slots: payload.Slots, ContentHash: hash, CreatedBy: a.User.ID, CreatedAt: now}
	return draft, tx.Commit()
}
func latestModelPayload(ctx context.Context, tx *sql.Tx, modelID string) (PublishModelVersionInput, error) {
	var in PublishModelVersionInput
	var raw [7][]byte
	e := tx.QueryRowContext(ctx, `SELECT hw_spec,expected_topology,capability_matrix,panel_layout,bom_expect,default_role_plan,coalesce((SELECT jsonb_agg(jsonb_build_object('slot_no',slot_no,'pcie_slot',pcie_slot,'expect_nic_model',expect_nic_model,'panel_port',panel_port,'default_role',default_role) ORDER BY slot_no) FROM device_model_slots WHERE model_version_id=v.id),'[]'::jsonb) FROM device_model_versions v WHERE model_id=$1 ORDER BY version DESC LIMIT 1`, modelID).Scan(&raw[0], &raw[1], &raw[2], &raw[3], &raw[4], &raw[5], &raw[6])
	if e != nil {
		return in, e
	}
	fields := []any{&in.HwSpec, &in.ExpectedTopology, &in.CapabilityMatrix, &in.PanelLayout, &in.BomExpect, &in.DefaultRolePlan, &in.Slots}
	for i, field := range fields {
		if e = json.Unmarshal(raw[i], field); e != nil {
			return in, e
		}
	}
	return in, nil
}
func (s *Store) ListModelDrafts(ctx context.Context, a *AuthSession, project *ProjectSummary, modelID string) ([]ModelDraftSummary, error) {
	if project == nil && !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if project != nil {
		if e = requireProjectMember(ctx, tx, a, project.ID); e != nil {
			return nil, e
		}
	}
	if _, e = modelForScope(ctx, tx, modelID, s.scopeKeyFor(project)); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT `+draftColumns+` FROM device_model_drafts d JOIN device_models m ON m.id=d.model_id WHERE d.model_id=$1 AND m.scope_key=$2 ORDER BY d.created_at DESC,d.id`, modelID, s.scopeKeyFor(project))
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []ModelDraftSummary{}
	for rows.Next() {
		d, err := scanModelDraft(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
func (s *Store) DeleteModelDraft(ctx context.Context, a *AuthSession, project *ProjectSummary, modelID, draftID, trace string) error {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if project != nil {
		if e = requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
			return e
		}
	} else if !a.User.IsAdmin {
		return ErrForbidden
	}
	var id string
	e = tx.QueryRowContext(ctx, `DELETE FROM device_model_drafts d USING device_models m WHERE d.id=$1 AND d.model_id=$2 AND m.id=d.model_id AND m.scope_key=$3 RETURNING d.id`, draftID, modelID, s.scopeKeyFor(project)).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return apiError(404, "MODEL_DRAFT_NOT_FOUND", "Draft not found for this model")
	}
	if e != nil {
		return e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "model.import.discard", id, trace, map[string]any{"model_id": modelID}); e != nil {
		return e
	}
	return tx.Commit()
}
