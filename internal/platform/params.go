package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"regexp"
	"time"
	"unicode/utf8"
)

// Parameter catalog (WP-1.11, FR-1.9). Typed parameter keys whose version-type
// option lists sync from approved, non-retired media in the same scope.

type ParamKeySummary struct {
	ID              string          `json:"id"`
	ScopeKey        string          `json:"scope_key"`
	Key             string          `json:"key"`
	ValueType       string          `json:"value_type"`
	Options         json.RawMessage `json:"options"`
	DefaultValue    string          `json:"default_value"`
	SourceMediaType string          `json:"source_media_type"`
	Deps            json.RawMessage `json:"deps"`
	State           string          `json:"state"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type UpsertParamKeyInput struct {
	Key             string         `json:"key"`
	ValueType       string         `json:"value_type"`
	Options         []string       `json:"options"`
	DefaultValue    string         `json:"default_value"`
	SourceMediaType string         `json:"source_media_type"`
	Deps            map[string]any `json:"deps"`
}

var paramKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,118}$`)

const paramOptionsLimit = 256

func validateParamInput(input UpsertParamKeyInput) error {
	if !paramKeyPattern.MatchString(input.Key) {
		return apiError(400, "VALIDATION_ERROR", "Parameter keys are lowercase dot-separated identifiers")
	}
	if input.ValueType != "string" && input.ValueType != "enum" && input.ValueType != "version" {
		return apiError(400, "VALIDATION_ERROR", "Parameter type must be string, enum or version")
	}
	seen := map[string]bool{}
	for _, option := range input.Options {
		if option == "" || utf8.RuneCountInString(option) > 120 || seen[option] {
			return apiError(400, "VALIDATION_ERROR", "Options must be unique, non-empty and at most 120 characters")
		}
		seen[option] = true
	}
	if len(input.Options) > paramOptionsLimit {
		return apiError(400, "VALIDATION_ERROR", "Option lists are limited to 256 entries")
	}
	if utf8.RuneCountInString(input.DefaultValue) > 120 {
		return apiError(400, "VALIDATION_ERROR", "Default values are limited to 120 characters")
	}
	if input.DefaultValue != "" && len(input.Options) > 0 {
		found := false
		for _, option := range input.Options {
			if option == input.DefaultValue {
				found = true
				break
			}
		}
		if !found {
			return apiError(400, "VALIDATION_ERROR", "The default value must appear in the option list")
		}
	}
	if input.SourceMediaType != "" && input.SourceMediaType != "driver" && input.SourceMediaType != "firmware" && input.SourceMediaType != "os" && input.SourceMediaType != "nccl" && input.SourceMediaType != "tool" && input.SourceMediaType != "config" {
		return apiError(400, "VALIDATION_ERROR", "source_media_type must be a registered media type")
	}
	// version-typed keys must source from media; free string/enum lists stay manual.
	if input.ValueType == "version" && input.SourceMediaType == "" {
		return apiError(400, "VALIDATION_ERROR", "version-typed parameters must declare source_media_type")
	}
	if input.Deps != nil {
		encoded, e := json.Marshal(input.Deps)
		if e != nil || len(encoded) > mediaDepsLimit {
			return apiError(400, "VALIDATION_ERROR", "Dependency map must be valid JSON within 64 KiB")
		}
	}
	return nil
}

func (s *Store) ListParamKeys(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[ParamKeySummary], error) {
	out := NewPage[ParamKeySummary](p)
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
	if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM param_keys WHERE (scope_key=$1 OR scope_key='global') AND state='active'", scope).Scan(&out.Total); e != nil {
		return out, e
	}
	// Version-typed options are derived at read time from approved media, so a
	// read-only transaction stays valid and approvals reflect immediately.
	rows, e := tx.QueryContext(ctx, `SELECT k.id,k.scope_key,k.key,k.value_type,
			CASE WHEN k.value_type='version' THEN coalesce((SELECT jsonb_agg(m.version ORDER BY m.version) FROM media_items m
				WHERE m.scope_key=k.scope_key AND m.type=k.source_media_type AND m.state='approved'), '[]'::jsonb) ELSE k.options END,
			k.default_value,k.source_media_type,k.deps,k.state,k.created_at,k.updated_at
		FROM param_keys k WHERE (k.scope_key=$1 OR k.scope_key='global') AND k.state='active'
		ORDER BY k.scope_key DESC,k.key LIMIT $2 OFFSET $3`, scope, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var k ParamKeySummary
		if e = rows.Scan(&k.ID, &k.ScopeKey, &k.Key, &k.ValueType, &k.Options, &k.DefaultValue, &k.SourceMediaType, &k.Deps, &k.State, &k.CreatedAt, &k.UpdatedAt); e != nil {
			return out, e
		}
		out.Items = append(out.Items, k)
	}
	return out, rows.Err()
}

// UpsertParamKey registers or updates a parameter key. Updating a key with an
// active default keeps options in sync; a retired key cannot be resurrected.
func (s *Store) UpsertParamKey(ctx context.Context, a *AuthSession, project *ProjectSummary, input UpsertParamKeyInput, trace string) (*ParamKeySummary, error) {
	if e := validateParamInput(input); e != nil {
		return nil, e
	}
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
	options := input.Options
	if input.ValueType == "version" {
		// Version options come only from approved media; manual lists are rejected.
		synced, e := approvedMediaVersions(ctx, tx, scope, input.SourceMediaType)
		if e != nil {
			return nil, e
		}
		if input.DefaultValue != "" {
			found := false
			for _, version := range synced {
				if version == input.DefaultValue {
					found = true
					break
				}
			}
			if !found {
				return nil, apiError(400, "VALIDATION_ERROR", "The default value must be an approved media version")
			}
		}
		options = synced
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	encoded, _ := json.Marshal(options)
	var id string
	e = tx.QueryRowContext(ctx, "SELECT id FROM param_keys WHERE scope_key=$1 AND key=$2", scope, input.Key).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		id = RandomID()
		if _, e = tx.ExecContext(ctx, "INSERT INTO param_keys(id,scope_key,key,value_type,options,default_value,source_media_type,deps,state,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'active',$9,$10,$10)",
			id, scope, input.Key, input.ValueType, encoded, input.DefaultValue, input.SourceMediaType, modelSpecJSON(input.Deps), a.User.ID, now); e != nil {
			var pg *pgconn.PgError
			if errors.As(e, &pg) && pg.Code == "23505" {
				return nil, apiError(409, "PARAM_KEY_EXISTS", "This parameter key already exists in the scope")
			}
			return nil, e
		}
		if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "param.upsert", id, trace, map[string]any{"key": input.Key, "value_type": input.ValueType, "created": true}); e != nil {
			return nil, e
		}
	} else if e != nil {
		return nil, e
	} else {
		var state string
		if e = tx.QueryRowContext(ctx, "SELECT state FROM param_keys WHERE id=$1", id).Scan(&state); e != nil {
			return nil, e
		}
		if state == "retired" {
			return nil, apiError(409, "PARAM_KEY_RETIRED", "Retired parameter keys cannot be updated")
		}
		if _, e = tx.ExecContext(ctx, "UPDATE param_keys SET value_type=$2, options=$3, default_value=$4, source_media_type=$5, deps=$6, updated_at=$7 WHERE id=$1",
			id, input.ValueType, encoded, input.DefaultValue, input.SourceMediaType, modelSpecJSON(input.Deps), now); e != nil {
			return nil, e
		}
		if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "param.upsert", id, trace, map[string]any{"key": input.Key, "value_type": input.ValueType, "created": false}); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &ParamKeySummary{ID: id, ScopeKey: scope, Key: input.Key, ValueType: input.ValueType, Options: encoded, DefaultValue: input.DefaultValue, SourceMediaType: input.SourceMediaType, Deps: modelSpecJSON(input.Deps), State: "active", CreatedAt: now, UpdatedAt: now}, nil
}

// RetireParamKey blocks new references while keeping the definition readable.
func (s *Store) RetireParamKey(ctx context.Context, a *AuthSession, project *ProjectSummary, keyID string, trace string) (*ParamKeySummary, error) {
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
	k, e := paramKeyForScope(ctx, tx, keyID, scope)
	if e != nil {
		return nil, e
	}
	if k.State == "retired" {
		return nil, apiError(409, "PARAM_KEY_RETIRED", "Parameter key is already retired")
	}
	if _, e = tx.ExecContext(ctx, "UPDATE param_keys SET state='retired', updated_at=$2 WHERE id=$1", keyID, time.Now().UTC()); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "param.retire", keyID, trace, map[string]any{"before_state": "active", "after_state": "retired"}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	k.State = "retired"
	return k, nil
}

// CheckParamValue validates a chosen value against a key: the value must be
// present in the option list (for version keys: approved media versions).
func (s *Store) CheckParamValue(ctx context.Context, a *AuthSession, project *ProjectSummary, keyName, value string) (map[string]any, error) {
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
	scope := s.scopeKeyFor(project)
	var valueType, defaultValue, sourceType string
	var options json.RawMessage
	e = tx.QueryRowContext(ctx, `SELECT value_type,
			CASE WHEN value_type='version' THEN coalesce((SELECT jsonb_agg(m.version ORDER BY m.version) FROM media_items m
				WHERE m.scope_key=param_keys.scope_key AND m.type=param_keys.source_media_type AND m.state='approved'), '[]'::jsonb) ELSE options END,
			default_value,source_media_type
		FROM param_keys WHERE (scope_key=$1 OR scope_key='global') AND key=$2 AND state='active' ORDER BY scope_key DESC LIMIT 1`, scope, keyName).Scan(&valueType, &options, &defaultValue, &sourceType)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(404, "PARAM_KEY_NOT_FOUND", "Parameter key not found")
	}
	if e != nil {
		return nil, e
	}
	var list []string
	if e := json.Unmarshal(options, &list); e != nil {
		return nil, e
	}
	allowed := false
	for _, option := range list {
		if option == value {
			allowed = true
			break
		}
	}
	if !allowed {
		return nil, apiError(422, "PARAM_VALUE_REJECTED", "Value is not in the approved option list for "+keyName)
	}
	return map[string]any{"key": keyName, "value": value, "value_type": valueType, "source_media_type": sourceType, "valid": true}, nil
}

func approvedMediaVersions(ctx context.Context, tx *sql.Tx, scope, mediaType string) ([]string, error) {
	rows, e := tx.QueryContext(ctx, "SELECT version FROM media_items WHERE scope_key=$1 AND type=$2 AND state='approved' ORDER BY version", scope, mediaType)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var version string
		if e = rows.Scan(&version); e != nil {
			return nil, e
		}
		out = append(out, version)
	}
	return out, rows.Err()
}

func paramKeyForScope(ctx context.Context, tx *sql.Tx, keyID, scope string) (*ParamKeySummary, error) {
	if !modelIDPattern.MatchString(keyID) {
		return nil, apiError(404, "PARAM_KEY_NOT_FOUND", "Parameter key not found")
	}
	var k ParamKeySummary
	e := tx.QueryRowContext(ctx, "SELECT id,scope_key,key,value_type,options,default_value,source_media_type,deps,state,created_at,updated_at FROM param_keys WHERE id=$1", keyID).
		Scan(&k.ID, &k.ScopeKey, &k.Key, &k.ValueType, &k.Options, &k.DefaultValue, &k.SourceMediaType, &k.Deps, &k.State, &k.CreatedAt, &k.UpdatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(404, "PARAM_KEY_NOT_FOUND", "Parameter key not found")
	}
	if e != nil {
		return nil, e
	}
	if k.ScopeKey != scope && k.ScopeKey != "global" {
		return nil, apiError(404, "PARAM_KEY_NOT_FOUND", "Parameter key not found")
	}
	return &k, nil
}
