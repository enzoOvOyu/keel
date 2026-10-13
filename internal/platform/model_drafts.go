package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// Model import drafts: a scan of one live server produces a draft that a human
// reviews and publishes through PublishModelVersion. Drafts never auto-publish;
// re-importing an identical inventory returns the existing draft (idempotent),
// and a draft whose content already matches the latest published version is
// rejected as ALREADY_CURRENT so the library never accumulates stale copies.

type ModelDraftSummary struct {
	ID              string          `json:"id"`
	ModelID         string          `json:"model_id"`
	ModelName       string          `json:"model_name"`
	SourceHost      string          `json:"source_host"`
	HwSpec          json.RawMessage `json:"hw_spec"`
	ExpectedTopology json.RawMessage `json:"expected_topology"`
	PanelLayout     json.RawMessage `json:"panel_layout"`
	BomExpect       json.RawMessage `json:"bom_expect"`
	DefaultRolePlan json.RawMessage `json:"default_role_plan"`
	Slots           []ModelSlot    `json:"slots"`
	ContentHash     string          `json:"content_hash"`
	CreatedBy       string          `json:"created_by"`
	CreatedAt       time.Time       `json:"created_at"`
}

// ImportModelScanInput is flat: scan fields plus the same payload keys a
// manual publish uses, so the scanner output can be submitted verbatim.
type ImportModelScanInput struct {
	ModelName        string         `json:"model_name"`
	SourceHost       string         `json:"source_host"`
	HwSpec           map[string]any `json:"hw_spec"`
	ExpectedTopology map[string]any `json:"expected_topology"`
	PanelLayout      map[string]any `json:"panel_layout"`
	BomExpect        map[string]any `json:"bom_expect"`
	DefaultRolePlan  map[string]any `json:"default_role_plan"`
	Slots            []ModelSlot    `json:"slots"`
}

func (in ImportModelScanInput) payload() PublishModelVersionInput {
	return PublishModelVersionInput{HwSpec: in.HwSpec, ExpectedTopology: in.ExpectedTopology, PanelLayout: in.PanelLayout, BomExpect: in.BomExpect, DefaultRolePlan: in.DefaultRolePlan, Slots: in.Slots}
}

// ImportModelScan creates or reuses a draft from one server's collected
// inventory. Idempotency is three-way: an identical published version short-
// circuits with ALREADY_CURRENT; an identical draft returns the existing draft;
// otherwise a new draft is inserted.
func (s *Store) ImportModelScan(ctx context.Context, a *AuthSession, project *ProjectSummary, input ImportModelScanInput, trace string) (*ModelDraftSummary, error) {
	if input.SourceHost == "" {
		return nil, apiError(400, "VALIDATION_ERROR", "source_host is required")
	}
	if len(input.SourceHost) > 120 {
		return nil, apiError(400, "VALIDATION_ERROR", "source_host too long")
	}
	scope := s.scopeKeyFor(project)
	if e := validateModelPayload(input.payload()); e != nil {
		return nil, e
	}
	hash, e := modelContentHash(input.payload())
	if e != nil {
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

	// Resolve the target model: find an active same-name model, or create one.
	name := input.ModelName
	if name == "" {
		if v, _ := input.HwSpec["model"].(string); v != "" {
			name = v
		} else if v, _ := input.HwSpec["product_name"].(string); v != "" {
			name = v
		}
	}
	if name == "" {
		return nil, apiError(400, "VALIDATION_ERROR", "model_name is required when the inventory carries no model field")
	}
	var modelID string
	if e = tx.QueryRowContext(ctx, "SELECT id FROM device_models WHERE scope_key=$1 AND name=$2 AND state='active'", scope, name).Scan(&modelID); e != nil {
		if !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		modelID = RandomID()
		now := time.Now().UTC()
		kind := "server"
		if v, _ := input.HwSpec["kind"].(string); v == "rack" || v == "compute-tray" {
			kind = v
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO device_models(id,scope_key,name,kind,state,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,'active',$5,$6,$6)", modelID, scope, name, kind, a.User.ID, now); e != nil {
			var pg *pgconn.PgError
			if errors.As(e, &pg) && pg.Code == "23505" {
				// Concurrent import created the model; resolve to it.
				if e2 := tx.QueryRowContext(ctx, "SELECT id FROM device_models WHERE scope_key=$1 AND name=$2", scope, name).Scan(&modelID); e2 != nil {
					return nil, e2
				}
			} else {
				return nil, e
			}
		}
	}

	// Idempotency 1: the newest published version already has this content.
	var publishedHash string
	if e = tx.QueryRowContext(ctx, "SELECT content_hash FROM device_model_versions WHERE model_id=$1 ORDER BY version DESC LIMIT 1", modelID).Scan(&publishedHash); e == nil {
		if publishedHash == hash {
			return nil, apiError(409, "ALREADY_CURRENT", "The scanned inventory matches the latest published version of this model")
		}
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}

	// Idempotency 2: an identical draft already exists — return it.
	var existing ModelDraftSummary
	if e = tx.QueryRowContext(ctx, `SELECT d.id,d.source_host,d.hw_spec,d.expected_topology,d.panel_layout,d.bom_expect,d.default_role_plan,d.content_hash,d.created_by,d.created_at
		FROM device_model_drafts d WHERE d.model_id=$1 AND d.content_hash=$2`, modelID, hash).
		Scan(&existing.ID, &existing.SourceHost, &existing.HwSpec, &existing.ExpectedTopology, &existing.PanelLayout, &existing.BomExpect, &existing.DefaultRolePlan, &existing.ContentHash, &existing.CreatedBy, &existing.CreatedAt); e == nil {
		existing.ModelID = modelID
		if e2 := tx.QueryRowContext(ctx, "SELECT name FROM device_models WHERE id=$1", modelID).Scan(&existing.ModelName); e2 != nil {
			return nil, e2
		}
		if e2 := appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "model.import.reuse", existing.ID, trace, map[string]any{"model_id": modelID, "source_host": input.SourceHost, "content_hash": hash, "outcome": "existing_draft"}); e2 != nil {
			return nil, e2
        }
		if e2 := tx.Commit(); e2 != nil {
			return nil, e2
		}
		return &existing, nil
	} else if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}

	draftID := RandomID()
	now := time.Now().UTC()
	slotsJSON, e := json.Marshal(input.Slots)
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO device_model_drafts(id,model_id,source_host,hw_spec,expected_topology,panel_layout,bom_expect,default_role_plan,content_hash,created_by,created_at,slots)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		draftID, modelID, input.SourceHost, modelSpecJSON(input.HwSpec), modelSpecJSON(input.ExpectedTopology), modelSpecJSON(input.PanelLayout), modelSpecJSON(input.BomExpect), modelSpecJSON(input.DefaultRolePlan), hash, a.User.ID, now, slotsJSON); e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			// Raced with a concurrent import of the same inventory.
			if e2 := tx.QueryRowContext(ctx, `SELECT d.id,d.source_host,d.hw_spec,d.expected_topology,d.panel_layout,d.bom_expect,d.default_role_plan,d.content_hash,d.created_by,d.created_at
				FROM device_model_drafts d WHERE d.model_id=$1 AND d.content_hash=$2`, modelID, hash).
				Scan(&existing.ID, &existing.SourceHost, &existing.HwSpec, &existing.ExpectedTopology, &existing.PanelLayout, &existing.BomExpect, &existing.DefaultRolePlan, &existing.ContentHash, &existing.CreatedBy, &existing.CreatedAt); e2 != nil {
				return nil, e2
			}
			existing.ModelID = modelID
			if e2 := tx.QueryRowContext(ctx, "SELECT name FROM device_models WHERE id=$1", modelID).Scan(&existing.ModelName); e2 != nil {
				return nil, e2
			}
			return &existing, nil
		}
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "model.import", draftID, trace, map[string]any{"model_id": modelID, "model_name": name, "source_host": input.SourceHost, "content_hash": hash}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &ModelDraftSummary{
		ID: draftID, ModelID: modelID, ModelName: name, SourceHost: input.SourceHost,
		HwSpec: modelSpecJSON(input.HwSpec), ExpectedTopology: modelSpecJSON(input.ExpectedTopology),
		PanelLayout: modelSpecJSON(input.PanelLayout), BomExpect: modelSpecJSON(input.BomExpect),
		DefaultRolePlan: modelSpecJSON(input.DefaultRolePlan), ContentHash: hash, CreatedBy: a.User.ID, CreatedAt: now,
	}, nil
}

// ListModelDrafts returns drafts for one model, newest first.
func (s *Store) ListModelDrafts(ctx context.Context, a *AuthSession, project *ProjectSummary, modelID string) ([]ModelDraftSummary, error) {
	scope := s.scopeKeyFor(project)
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if project != nil {
		if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
			return nil, e
		}
	}
	if _, e = modelForScope(ctx, tx, modelID, scope); e != nil {
		return nil, e
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT d.id,d.model_id,m.name,d.source_host,d.hw_spec,d.expected_topology,d.panel_layout,d.bom_expect,d.default_role_plan,d.content_hash,d.created_by,d.created_at,d.slots
		FROM device_model_drafts d JOIN device_models m ON m.id=d.model_id
		WHERE d.model_id=$1 AND m.scope_key=$2 ORDER BY d.created_at DESC`, modelID, scope)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []ModelDraftSummary
	for rows.Next() {
		var item ModelDraftSummary
		var slotsRaw []byte
		if e = rows.Scan(&item.ID, &item.ModelID, &item.ModelName, &item.SourceHost, &item.HwSpec, &item.ExpectedTopology, &item.PanelLayout, &item.BomExpect, &item.DefaultRolePlan, &item.ContentHash, &item.CreatedBy, &item.CreatedAt, &slotsRaw); e != nil {
			return nil, e
		}
		item.Slots = []ModelSlot{}
		_ = json.Unmarshal(slotsRaw, &item.Slots)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []ModelDraftSummary{}
	}
	return out, nil
}

// DeleteModelDraft removes an unreviewed draft. Published versions are
// immutable and are never touched by this path.
func (s *Store) DeleteModelDraft(ctx context.Context, a *AuthSession, project *ProjectSummary, draftID string, trace string) error {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if project != nil {
		if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
			return e
		}
	} else if !a.User.IsAdmin {
		return ErrForbidden
	}
	var discardedID string
	if e = tx.QueryRowContext(ctx, `DELETE FROM device_model_drafts WHERE id=$1 AND model_id IN (SELECT id FROM device_models WHERE scope_key=$2) RETURNING id`, draftID, s.scopeKeyFor(project)).Scan(&discardedID); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return apiError(404, "NOT_FOUND", "Draft not found")
		}
		return e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "model.import.discard", draftID, trace, map[string]any{"draft_id": draftID}); e != nil {
		return e
	}
	return tx.Commit()
}

var _ = http.StatusOK
