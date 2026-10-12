package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Device model library (WP-1.12, FR-14). Models are stable identities with
// immutable published versions. Expectation data only: no runtime state here.

type DeviceModelSummary struct {
	ID        string    `json:"id"`
	ScopeKey  string    `json:"scope_key"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	State     string    `json:"state"`
	Version   int64     `json:"latest_version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DeviceModelVersionDetail struct {
	ID               string          `json:"id"`
	ModelID          string          `json:"model_id"`
	Version          int64           `json:"version"`
	HwSpec           json.RawMessage `json:"hw_spec"`
	ExpectedTopology json.RawMessage `json:"expected_topology"`
	PanelLayout      json.RawMessage `json:"panel_layout"`
	BomExpect        json.RawMessage `json:"bom_expect"`
	DefaultRolePlan  json.RawMessage `json:"default_role_plan"`
	ContentHash      string          `json:"content_hash"`
	PublishedAt      time.Time       `json:"published_at"`
	Slots            []ModelSlot     `json:"slots"`
}

type ModelSlot struct {
	SlotNo        int    `json:"slot_no"`
	PcieSlot      string `json:"pcie_slot"`
	ExpectNicModel string `json:"expect_nic_model"`
	PanelPort     string `json:"panel_port"`
	DefaultRole   string `json:"default_role"`
}

type CreateDeviceModelInput struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

type PublishModelVersionInput struct {
	HwSpec           map[string]any `json:"hw_spec"`
	ExpectedTopology map[string]any `json:"expected_topology"`
	PanelLayout      map[string]any `json:"panel_layout"`
	BomExpect        map[string]any `json:"bom_expect"`
	DefaultRolePlan  map[string]any `json:"default_role_plan"`
	Slots            []ModelSlot    `json:"slots"`
}

const modelJSONLimit = 512 * 1024

func (s *Store) scopeKeyFor(project *ProjectSummary) string {
	if project == nil {
		return "global"
	}
	return "project:" + project.ID
}

// ListDeviceModels lists models visible to the project scope: project-scoped
// models plus global shared models. Retired models remain readable (F-14.1).
func (s *Store) ListDeviceModels(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[DeviceModelSummary], error) {
	out := NewPage[DeviceModelSummary](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	scope := s.scopeKeyFor(project)
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if project != nil {
		if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
			return out, e
		}
	}
	filter := "scope_key = $1 OR scope_key = 'global'"
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM device_models WHERE "+filter, scope).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT m.id,m.scope_key,m.name,m.kind,m.state,m.created_at,m.updated_at,
		coalesce((SELECT max(v.version) FROM device_model_versions v WHERE v.model_id=m.id),0)
		FROM device_models m WHERE `+filter+` ORDER BY m.scope_key,m.name LIMIT $2 OFFSET $3`, scope, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var m DeviceModelSummary
		if e = rows.Scan(&m.ID, &m.ScopeKey, &m.Name, &m.Kind, &m.State, &m.CreatedAt, &m.UpdatedAt, &m.Version); e != nil {
			return out, e
		}
		out.Items = append(out.Items, m)
	}
	return out, rows.Err()
}

// CreateDeviceModel creates a stable model identity. F-14.2: the (scope, name)
// pair is unique at the API and database level.
func (s *Store) CreateDeviceModel(ctx context.Context, a *AuthSession, project *ProjectSummary, input CreateDeviceModelInput, trace string) (*DeviceModelSummary, error) {
	name := strings.TrimSpace(input.Name)
	if utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > 120 {
		return nil, apiError(400, "VALIDATION_ERROR", "Model name must be 1-120 characters")
	}
	if input.Kind != "rack" && input.Kind != "server" && input.Kind != "compute-tray" {
		return nil, apiError(400, "VALIDATION_ERROR", "Model kind must be rack, server or compute-tray")
	}
	// Only platform administrators maintain the global scope.
	scope := s.scopeKeyFor(project)
	if scope == "global" && !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	now := time.Now().UTC().Truncate(time.Microsecond)
	m := &DeviceModelSummary{ID: RandomID(), ScopeKey: scope, Name: name, Kind: input.Kind, State: "active", CreatedAt: now, UpdatedAt: now}
	if _, e = tx.ExecContext(ctx, "INSERT INTO device_models(id,scope_key,name,kind,state,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,'active',$5,$6,$6)", m.ID, scope, name, input.Kind, a.User.ID, m.CreatedAt); e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			return nil, apiError(409, "MODEL_EXISTS", "A model with this name already exists in the scope")
		}
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "model.create", m.ID, trace, map[string]any{"scope": scope, "kind": input.Kind, "before_version": 0, "after_version": 1}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return m, nil
}

// RetireDeviceModel retires a model: existing references stay readable, new
// references are rejected. Deletion stays blocked by foreign keys.
func (s *Store) RetireDeviceModel(ctx context.Context, a *AuthSession, project *ProjectSummary, modelID string, trace string) (*DeviceModelSummary, error) {
	scope := s.scopeKeyFor(project)
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if project != nil {
		if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
			return nil, e
		}
	} else if !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	m, e := modelForScope(ctx, tx, modelID, scope)
	if e != nil {
		return nil, e
	}
	if m.State == "retired" {
		return nil, apiError(409, "MODEL_RETIRED", "Model is already retired")
	}
	if _, e = tx.ExecContext(ctx, "UPDATE device_models SET state='retired', updated_at=$2 WHERE id=$1 AND state='active'", modelID, time.Now().UTC()); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "model.retire", modelID, trace, map[string]any{"before_state": "active", "after_state": "retired"}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	m.State = "retired"
	return m, nil
}

// PublishModelVersion publishes an immutable version. Once published, neither
// the version row nor its slots can be modified; corrections require a new
// version. Retired models cannot publish new versions.
func (s *Store) PublishModelVersion(ctx context.Context, a *AuthSession, project *ProjectSummary, modelID string, input PublishModelVersionInput, trace string) (*DeviceModelVersionDetail, error) {
	scope := s.scopeKeyFor(project)
	if e := validateModelPayload(input); e != nil {
		return nil, e
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if project != nil {
		if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
			return nil, e
		}
	} else if !a.User.IsAdmin {
		return nil, ErrForbidden
	}
	m, e := modelForScope(ctx, tx, modelID, scope)
	if e != nil {
		return nil, e
	}
	if m.State == "retired" {
		return nil, apiError(409, "MODEL_RETIRED", "Retired models cannot publish new versions")
	}
	var next int64
	if e = tx.QueryRowContext(ctx, "SELECT coalesce(max(version),0)+1 FROM device_model_versions WHERE model_id=$1", modelID).Scan(&next); e != nil {
		return nil, e
	}
	hash, e := modelContentHash(input)
	if e != nil {
		return nil, e
	}
	v := &DeviceModelVersionDetail{ID: RandomID(), ModelID: modelID, Version: next, HwSpec: modelSpecJSON(input.HwSpec), ExpectedTopology: modelSpecJSON(input.ExpectedTopology), PanelLayout: modelSpecJSON(input.PanelLayout), BomExpect: modelSpecJSON(input.BomExpect), DefaultRolePlan: modelSpecJSON(input.DefaultRolePlan), ContentHash: hash, PublishedAt: time.Now().UTC().Truncate(time.Microsecond), Slots: input.Slots}
	if _, e = tx.ExecContext(ctx, `INSERT INTO device_model_versions(id,model_id,version,hw_spec,expected_topology,capability_matrix,panel_layout,bom_expect,default_role_plan,content_hash,published_at)
		VALUES($1,$2,$3,$4,$5,'{}'::jsonb,$6,$7,$8,$9,$10)`,
		v.ID, modelID, next, v.HwSpec, v.ExpectedTopology, v.PanelLayout, v.BomExpect, v.DefaultRolePlan, hash, v.PublishedAt); e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			return nil, apiError(409, "MODEL_VERSION_EXISTS", "This model version already exists")
		}
		return nil, e
	}
	for _, slot := range input.Slots {
		if _, e = tx.ExecContext(ctx, "INSERT INTO device_model_slots(id,model_version_id,slot_no,pcie_slot,expect_nic_model,panel_port,default_role) VALUES($1,$2,$3,$4,$5,$6,$7)", RandomID(), v.ID, slot.SlotNo, slot.PcieSlot, slot.ExpectNicModel, slot.PanelPort, slot.DefaultRole); e != nil {
			return nil, e
		}
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "model.version.publish", v.ID, trace, map[string]any{"model_id": modelID, "version": next, "content_hash": hash}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return v, nil
}

func validateModelPayload(input PublishModelVersionInput) error {
	total := 0
	for name, value := range map[string]map[string]any{"hw_spec": input.HwSpec, "expected_topology": input.ExpectedTopology, "panel_layout": input.PanelLayout, "bom_expect": input.BomExpect, "default_role_plan": input.DefaultRolePlan} {
		if value == nil {
			continue
		}
		encoded, e := json.Marshal(value)
		if e != nil {
			return apiError(400, "VALIDATION_ERROR", "Model payload must be valid JSON: "+name)
		}
		total += len(encoded)
	}
	if total > modelJSONLimit {
		return apiError(400, "PAYLOAD_TOO_LARGE", "Model specification exceeds 512 KiB")
	}
	seen := map[int]bool{}
	for _, slot := range input.Slots {
		if slot.SlotNo < 1 || slot.SlotNo > 1024 {
			return apiError(400, "VALIDATION_ERROR", "Slot numbers must be 1-1024")
		}
		if seen[slot.SlotNo] {
			return apiError(400, "VALIDATION_ERROR", "Duplicate slot numbers are not allowed")
		}
		seen[slot.SlotNo] = true
		for text, field := range map[string]string{slot.PcieSlot: "pcie_slot", slot.ExpectNicModel: "expect_nic_model", slot.PanelPort: "panel_port"} {
			if utf8.RuneCountInString(strings.TrimSpace(text)) < 1 || utf8.RuneCountInString(text) > 120 {
				return apiError(400, "VALIDATION_ERROR", "Slot "+field+" must be 1-120 characters")
			}
		}
		if slot.DefaultRole != "management" && slot.DefaultRole != "storage" && slot.DefaultRole != "compute" && slot.DefaultRole != "oob" && slot.DefaultRole != "unused" {
			return apiError(400, "VALIDATION_ERROR", "Slot default role must be management, storage, compute, oob or unused")
		}
	}
	return nil
}

func modelContentHash(input PublishModelVersionInput) (string, error) {
	payload := map[string]any{"hw_spec": input.HwSpec, "expected_topology": input.ExpectedTopology, "panel_layout": input.PanelLayout, "bom_expect": input.BomExpect, "default_role_plan": input.DefaultRolePlan, "slots": input.Slots}
	encoded, e := json.Marshal(payload)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func modelSpecJSON(value map[string]any) json.RawMessage {
	if value == nil {
		return json.RawMessage("{}")
	}
	encoded, e := json.Marshal(value)
	if e != nil {
		return json.RawMessage("{}")
	}
	return encoded
}

func projectIDOf(project *ProjectSummary) string {
	if project == nil {
		return ""
	}
	return project.ID
}


// requireProjectMember accepts any of the six project roles; the caller has
// already resolved the project record, so this only re-checks membership.
func requireProjectMember(ctx context.Context, tx *sql.Tx, a *AuthSession, projectID string) error {
	if a.User.IsAdmin {
		return nil
	}
	var role string
	e := tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2", projectID, a.User.ID).Scan(&role)
	if errors.Is(e, sql.ErrNoRows) {
		return ErrNotFound
	}
	return e
}


// ListModelVersions returns the immutable published history of a model.
// Cross-scope or non-member access returns 404 without leaking existence.
func (s *Store) ListModelVersions(ctx context.Context, a *AuthSession, project *ProjectSummary, modelID string, p Pagination) (Page[DeviceModelVersionDetail], error) {
	out := NewPage[DeviceModelVersionDetail](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	scope := s.scopeKeyFor(project)
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if project != nil {
		if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
			return out, e
		}
	}
	if _, e = modelForScope(ctx, tx, modelID, scope); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM device_model_versions WHERE model_id=$1", modelID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,model_id,version,hw_spec,expected_topology,panel_layout,bom_expect,default_role_plan,content_hash,published_at
		FROM device_model_versions WHERE model_id=$1 ORDER BY version DESC LIMIT $2 OFFSET $3`, modelID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var v DeviceModelVersionDetail
		if e = rows.Scan(&v.ID, &v.ModelID, &v.Version, &v.HwSpec, &v.ExpectedTopology, &v.PanelLayout, &v.BomExpect, &v.DefaultRolePlan, &v.ContentHash, &v.PublishedAt); e != nil {
			return out, e
		}
		v.Slots = []ModelSlot{}
		out.Items = append(out.Items, v)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	slotRows, e := tx.QueryContext(ctx, "SELECT slot_no,pcie_slot,expect_nic_model,panel_port,default_role FROM device_model_slots WHERE model_version_id=ANY($1) ORDER BY slot_no", versionIDs(out.Items))
	if e != nil {
		return out, e
	}
	defer slotRows.Close()
	index := map[string]int{}
	for i, v := range out.Items {
		index[v.ID] = i
	}
	for slotRows.Next() {
		var versionID string
		var slot ModelSlot
		if e = slotRows.Scan(&versionID, &slot.SlotNo, &slot.PcieSlot, &slot.ExpectNicModel, &slot.PanelPort, &slot.DefaultRole); e != nil {
			return out, e
		}
		if i, ok := index[versionID]; ok {
			out.Items[i].Slots = append(out.Items[i].Slots, slot)
		}
	}
	return out, slotRows.Err()
}

func versionIDs(items []DeviceModelVersionDetail) []string {
	ids := make([]string, len(items))
	for i, v := range items {
		ids[i] = v.ID
	}
	return ids
}

var modelIDPattern = regexp.MustCompile(`^[a-f0-9]{32}$`)

func modelForScope(ctx context.Context, tx *sql.Tx, modelID, scope string) (*DeviceModelSummary, error) {
	if !modelIDPattern.MatchString(modelID) {
		return nil, apiError(404, "MODEL_NOT_FOUND", "Model not found")
	}
	var m DeviceModelSummary
	e := tx.QueryRowContext(ctx, "SELECT id,scope_key,name,kind,state,created_at,updated_at FROM device_models WHERE id=$1", modelID).
		Scan(&m.ID, &m.ScopeKey, &m.Name, &m.Kind, &m.State, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(404, "MODEL_NOT_FOUND", "Model not found")
	}
	if e != nil {
		return nil, e
	}
	if m.ScopeKey != scope && m.ScopeKey != "global" {
		return nil, apiError(404, "MODEL_NOT_FOUND", "Model not found")
	}
	return &m, nil
}
