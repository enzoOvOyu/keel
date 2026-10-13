package platform

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// ConfigSet engine (WP-3.1, FR-5.1). All configuration is data: a ConfigSet
// carries keys, a schema, a scope and a Go-style template whose
// version-class variables reference parameter-catalog placeholders
// ({{param.<key>}}) resolved from the frozen parameter snapshot at apply
// time — never free-typed versions. Drafts become approved immutable
// revisions through the single publish path (the creator cannot approve
// their own draft, aligned with the media self-approval block); rendering
// runs per machine against the frozen snapshot and diffs against the
// newest previously applied revision. Applying requires a valid mapping
// revision for the target (G3 evidence, FR-5 precondition).

var paramPlaceholder = regexp.MustCompile(`\{\{\s*param\.([A-Za-z0-9_.]+)\s*\}\}`)

type ConfigSet struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Version    int        `json:"version"`
	Status     string     `json:"status"`
	Scope      string     `json:"scope"`
	ModelName  string     `json:"model_name"`
	Template   string     `json:"template"`
	Schema     string     `json:"schema"`
	Keys       string     `json:"keys"`
	CreatedBy  string     `json:"created_by"`
	CreatedAt  time.Time  `json:"created_at"`
	ApprovedBy string     `json:"approved_by,omitempty"`
	ApprovedAt *time.Time `json:"approved_at,omitempty"`
}

type CreateConfigSetInput struct {
	Name      string `json:"name"`
	Scope     string `json:"scope"`
	ModelName string `json:"model_name"`
	Template  string `json:"template"`
	Schema    string `json:"schema"`
	Keys      string `json:"keys"`
}

// CreateConfigSet opens a draft revision.
func (s *Store) CreateConfigSet(ctx context.Context, a *AuthSession, project *ProjectSummary, input CreateConfigSetInput, trace string) (*ConfigSet, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	// Authority before validation: an unauthorized caller learns nothing.
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	if len(input.Name) < 1 || len(input.Name) > 120 || !strings.Contains(input.Template, "{{") {
		return nil, apiError(400, "VALIDATION_ERROR", "name and a template with placeholders are required")
	}
	if input.Scope == "" {
		input.Scope = "machine"
	}
	if input.Schema == "" {
		input.Schema = "{}"
	}
	if input.Keys == "" {
		input.Keys = "{}"
	}
	if input.Scope != "machine" && input.Scope != "model" && input.Scope != "project" {
		return nil, apiError(400, "VALIDATION_ERROR", "scope must be machine, model or project")
	}
	var version int
	if e = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0)+1 FROM config_sets WHERE project_id=$1 AND name=$2", project.ID, input.Name).Scan(&version); e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	set := ConfigSet{ID: RandomID(), Name: input.Name, Version: version, Status: "draft", Scope: input.Scope, ModelName: input.ModelName, Template: input.Template, Schema: input.Schema, Keys: input.Keys, CreatedBy: a.User.ID, CreatedAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO config_sets(id,project_id,name,version,status,scope,model_name,template,schema,keys,created_by,created_at)
		VALUES($1,$2,$3,$4,'draft',$5,$6,$7,$8,$9,$10,$11)`,
		set.ID, project.ID, set.Name, set.Version, set.Scope, set.ModelName, set.Template, set.Schema, set.Keys, a.User.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "configset.create", set.ID, trace, map[string]any{"name": set.Name, "version": set.Version}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &set, nil
}

// ApproveConfigSet publishes the draft as an immutable approved revision;
// the creator cannot approve their own draft and retired sets stay retired.
func (s *Store) ApproveConfigSet(ctx context.Context, a *AuthSession, project *ProjectSummary, configSetID, trace string) (*ConfigSet, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var createdBy string
	var status string
	if e = tx.QueryRowContext(ctx, "SELECT created_by,status FROM config_sets WHERE id=$1 AND project_id=$2", configSetID, project.ID).Scan(&createdBy, &status); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "config set not found")
		}
		return nil, e
	}
	if status != "draft" {
		return nil, apiError(409, "VALIDATION_ERROR", "only drafts can be approved")
	}
	if createdBy == a.User.ID {
		return nil, apiError(409, "SELF_APPROVAL", "the draft creator cannot approve their own config set")
	}
	now := time.Now().UTC()
	if _, e = tx.ExecContext(ctx, "UPDATE config_sets SET status='approved',approved_by=$2,approved_at=$3 WHERE id=$1", configSetID, a.User.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "configset.approve", configSetID, trace, nil); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.getConfigSet(ctx, a, project, configSetID)
}

func (s *Store) getConfigSet(ctx context.Context, a *AuthSession, project *ProjectSummary, configSetID string) (*ConfigSet, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	set, e := scanConfigSet(tx.QueryRowContext(ctx, `SELECT id,name,version,status,scope,model_name,template,schema,keys,created_by,created_at,approved_by,approved_at
		FROM config_sets WHERE id=$1 AND project_id=$2`, configSetID, project.ID))
	if e != nil {
		return nil, e
	}
	return set, nil
}

func scanConfigSet(row *sql.Row) (*ConfigSet, error) {
	var set ConfigSet
	var approvedBy sql.NullString
	var approvedAt sql.NullTime
	e := row.Scan(&set.ID, &set.Name, &set.Version, &set.Status, &set.Scope, &set.ModelName, &set.Template, &set.Schema, &set.Keys, &set.CreatedBy, &set.CreatedAt, &approvedBy, &approvedAt)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(404, "NOT_FOUND", "config set not found")
		}
		return nil, e
	}
	if approvedBy.Valid {
		set.ApprovedBy, set.ApprovedAt = approvedBy.String, &approvedAt.Time
	}
	return &set, nil
}

type RenderInput struct {
	ConfigSetID string         `json:"config_set_id"`
	MachineID   string         `json:"machine_id"`
	Params      map[string]any `json:"params"`
}

type RenderResult struct {
	Rendered string `json:"rendered"`
	Diff     string `json:"diff"`
	Applied  bool   `json:"applied"`
}

// RenderConfigSet resolves {{param.<key>}} placeholders against the frozen
// parameters and, on apply=true, records the per-machine application with
// the diff against the newest prior apply. The target must hold a valid
// mapping revision (G3 evidence, the FR-5 precondition for batch config).
func (s *Store) RenderConfigSet(ctx context.Context, a *AuthSession, project *ProjectSummary, input RenderInput, apply bool, trace string) (*RenderResult, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	set, e := scanConfigSet(tx.QueryRowContext(ctx, `SELECT id,name,version,status,scope,model_name,template,schema,keys,created_by,created_at,approved_by,approved_at
		FROM config_sets WHERE id=$1 AND project_id=$2`, input.ConfigSetID, project.ID))
	if e != nil {
		return nil, e
	}
	if set.Status != "approved" {
		return nil, apiError(409, "VALIDATION_ERROR", "only approved config sets render")
	}
	var hostname string
	if e = tx.QueryRowContext(ctx, "SELECT hostname FROM project_assets WHERE project_id=$1 AND machine_id=$2", project.ID, input.MachineID).Scan(&hostname); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(409, "VALIDATION_ERROR", "machine not bound to this project")
		}
		return nil, e
	}
	// FR-5 precondition: applying batch config needs valid G3 evidence (a
	// published mapping revision for the target); previews stay read-only
	// and render regardless.
	if apply {
		var mapped bool
		if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM mapping_revisions WHERE project_id=$1 AND machine_id=$2)", project.ID, input.MachineID).Scan(&mapped); e != nil {
			return nil, e
		}
		if !mapped {
			return nil, apiError(409, "GATE_EVIDENCE", "batch config requires a published mapping (G3) for the target")
		}
	}
	// Resolve placeholders; missing parameters are a hard error (never
	// render a half-resolved config).
	rendered := set.Template
	for _, match := range paramPlaceholder.FindAllStringSubmatch(set.Template, -1) {
		key := match[1]
		value, ok := resolveParamPath(input.Params, key)
		if !ok {
			return nil, apiError(409, "VALIDATION_ERROR", fmt.Sprintf("parameter %q is missing from the frozen snapshot", key))
		}
		rendered = strings.ReplaceAll(rendered, match[0], fmt.Sprintf("%v", value))
	}
	// Diff against the newest prior apply of the same kind for this
	// set+machine: the generic template render is its own kind, so it never
	// diffs against one of the four renderer outputs.
	var prior sql.NullString
	_ = tx.QueryRowContext(ctx, `SELECT rendered FROM config_set_applies WHERE config_set_id=$1 AND machine_id=$2 AND kind='template' ORDER BY applied_at DESC LIMIT 1`,
		input.ConfigSetID, input.MachineID).Scan(&prior)
	diff := ""
	if prior.Valid && prior.String != rendered {
		diff = unifiedDiff(prior.String, rendered)
	} else if !prior.Valid {
		diff = "new"
	}
	result := RenderResult{Rendered: rendered, Diff: diff, Applied: false}
	if apply {
		now := time.Now().UTC()
		if _, e = tx.ExecContext(ctx, `INSERT INTO config_set_applies(id,project_id,config_set_id,machine_id,rendered,diff,applied_by,applied_at,kind)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,'template')`,
			RandomID(), project.ID, input.ConfigSetID, input.MachineID, rendered, diff, a.User.ID, now); e != nil {
			return nil, e
		}
		if e = appendAudit(ctx, tx, a.User.ID, project.ID, "configset.apply", input.ConfigSetID, trace, map[string]any{"machine": input.MachineID, "version": set.Version}); e != nil {
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
		result.Applied = true
	}
	return &result, nil
}

// resolveParamPath walks a dotted path into the nested parameter snapshot.
func resolveParamPath(params map[string]any, key string) (any, bool) {
	current := params
	parts := strings.Split(key, ".")
	for i, part := range parts {
		if i == len(parts)-1 {
			value, ok := current[part]
			return value, ok
		}
		next, ok := current[part].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return nil, false
}

// unifiedDiff renders a minimal line-level diff (preview-grade).
func unifiedDiff(before, after string) string {
	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	var out []string
	for i := 0; i < len(beforeLines) || i < len(afterLines); i++ {
		switch {
		case i < len(beforeLines) && i < len(afterLines):
			if beforeLines[i] != afterLines[i] {
				out = append(out, "- "+beforeLines[i], "+ "+afterLines[i])
			}
		case i < len(beforeLines):
			out = append(out, "- "+beforeLines[i])
		default:
			out = append(out, "+ "+afterLines[i])
		}
	}
	return strings.Join(out, "\n")
}

// ListConfigSets pages the project's config sets newest-first.
func (s *Store) ListConfigSets(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[ConfigSet], error) {
	out := NewPage[ConfigSet](p)
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
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM config_sets WHERE project_id=$1", project.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,name,version,status,scope,model_name,template,schema,keys,created_by,created_at,approved_by,approved_at
		FROM config_sets WHERE project_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, project.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var set ConfigSet
		var approvedBy sql.NullString
		var approvedAt sql.NullTime
		if e = rows.Scan(&set.ID, &set.Name, &set.Version, &set.Status, &set.Scope, &set.ModelName, &set.Template, &set.Schema, &set.Keys, &set.CreatedBy, &set.CreatedAt, &approvedBy, &approvedAt); e != nil {
			return out, e
		}
		if approvedBy.Valid {
			set.ApprovedBy, set.ApprovedAt = approvedBy.String, &approvedAt.Time
		}
		out.Items = append(out.Items, set)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.Items == nil {
		out.Items = []ConfigSet{}
	}
	return out, nil
}
