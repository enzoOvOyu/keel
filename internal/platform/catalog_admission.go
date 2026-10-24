package platform

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Compatibility is an exact, registered matrix, never a hard-coded vendor rule.
// Media matrices inherit the media's independent approval and immutable metadata.
type Compatibility struct {
	Source       string              `json:"source"`
	Version      string              `json:"version"`
	ReportRef    string              `json:"report_ref"`
	Requires     map[string][]string `json:"requires,omitempty"`
	Combinations []map[string]string `json:"combinations,omitempty"`
}
type catalogDependencies struct {
	Compatibility *Compatibility `json:"compatibility,omitempty"`
}

func decodeCompatibility(raw json.RawMessage) (*Compatibility, error) {
	if len(raw) > mediaDepsLimit {
		return nil, errors.New("dependencies exceed 64 KiB")
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var deps catalogDependencies
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(&deps); e != nil {
		return nil, errors.New("deps must contain only a documented compatibility matrix")
	}
	if e := d.Decode(new(any)); e != io.EOF {
		return nil, errors.New("invalid dependency document")
	}
	c := deps.Compatibility
	if c == nil {
		return nil, nil
	}
	if strings.TrimSpace(c.Source) == "" || strings.TrimSpace(c.Version) == "" || strings.TrimSpace(c.ReportRef) == "" || len(c.Source) > 1024 || len(c.Version) > 120 || len(c.ReportRef) > 1024 || (len(c.Requires) == 0 && len(c.Combinations) == 0) || len(c.Requires) > 32 || len(c.Combinations) > 128 {
		return nil, errors.New("compatibility requires source, version, report_ref and exact requires and/or 1..128 combinations")
	}
	for key, values := range c.Requires {
		if !paramKeyPattern.MatchString(key) || len(values) == 0 || len(values) > 256 {
			return nil, errors.New("invalid compatibility dependency")
		}
		seen := map[string]bool{}
		for _, v := range values {
			if strings.TrimSpace(v) == "" || len(v) > 120 || seen[v] {
				return nil, errors.New("compatibility values must be unique exact values")
			}
			seen[v] = true
		}
	}
	seenRows := map[string]bool{}
	for _, row := range c.Combinations {
		if len(row) == 0 || len(row) > 32 || len(row) != len(c.Combinations[0]) {
			return nil, errors.New("compatibility combinations must have the same 1..32 columns")
		}
		for key, value := range row {
			if _, ok := c.Combinations[0][key]; !ok || !paramKeyPattern.MatchString(key) || strings.TrimSpace(value) == "" || len(value) > 120 {
				return nil, errors.New("compatibility combinations require identical keys and nonempty exact string values")
			}
		}
		encoded, _ := json.Marshal(row) // encoding/json sorts map keys.
		if seenRows[string(encoded)] {
			return nil, errors.New("duplicate compatibility combination")
		}
		seenRows[string(encoded)] = true
	}
	return c, nil
}
func validateCompatibility(deps map[string]any) error {
	raw, e := json.Marshal(deps)
	if e != nil {
		return apiError(400, "VALIDATION_ERROR", "Invalid dependencies")
	}
	if _, e = decodeCompatibility(raw); e != nil {
		return apiError(400, "VALIDATION_ERROR", e.Error())
	}
	return nil
}

type CatalogMedia struct {
	ID            string          `json:"id"`
	URI           string          `json:"uri"`
	Version       string          `json:"version"`
	Arch          string          `json:"arch"`
	HwScope       string          `json:"hw_scope"`
	OsScope       string          `json:"os_scope"`
	SHA256        string          `json:"sha256"`
	ObjectVersion string          `json:"object_version"`
	Deps          json.RawMessage `json:"deps"`
}
type CatalogSelection struct {
	KeyID     string          `json:"key_id"`
	Key       string          `json:"key"`
	Value     string          `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
	Deps      json.RawMessage `json:"deps"`
	Media     *CatalogMedia   `json:"media,omitempty"`
}
type CatalogIssue struct {
	Key        string         `json:"key"`
	Code       string         `json:"code"`
	Message    string         `json:"message"`
	Dependency string         `json:"dependency,omitempty"`
	Expected   []string       `json:"expected,omitempty"`
	Matrix     *Compatibility `json:"matrix,omitempty"`
}
type CatalogValidation struct {
	Valid      bool               `json:"valid"`
	Issues     []CatalogIssue     `json:"issues"`
	Selections []CatalogSelection `json:"selections"`
}

// catalogMediaOptions is shared by catalog reads and transactional admission.
// Both global and current-project artifacts are visible. Duplicate version labels
// remain distinguishable in metadata; ambiguous selections fail admission.
func catalogMediaOptions(ctx context.Context, tx *sql.Tx, scope, mediaType, arch string, lock bool) ([]CatalogMedia, error) {
	q := `SELECT m.id,m.type,m.name,m.version,m.arch,m.hw_scope,m.os_scope,m.sha256,a.object_version,m.deps
 FROM media_items m JOIN media_artifacts a ON a.media_id=m.id AND a.sha256=m.sha256 AND a.size=m.size
 WHERE m.scope_key IN('global',$1) AND m.type=$2 AND m.state='approved' AND ($3='' OR m.arch IN($3,'noarch')) ORDER BY m.id`
	if lock {
		q += " FOR SHARE OF m,a"
	}
	rows, e := tx.QueryContext(ctx, q, scope, mediaType, arch)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []CatalogMedia{}
	for rows.Next() {
		var m CatalogMedia
		var typ, name string
		if e = rows.Scan(&m.ID, &typ, &name, &m.Version, &m.Arch, &m.HwScope, &m.OsScope, &m.SHA256, &m.ObjectVersion, &m.Deps); e != nil {
			return nil, e
		}
		// URI segments are escaped individually, preserving a reversible reference.
		m.URI = mediaURI(typ, name, m.Version)
		out = append(out, m)
	}
	return out, rows.Err()
}

// ValidateCatalogTx requires its caller to authorize the project. Locks are held
// through execution insertion, so retirement/upsert cannot race snapshot creation.
// Unknown parameters remain owned by the Flow; registered retired keys fail closed.
func ValidateCatalogTx(ctx context.Context, tx *sql.Tx, projectID string, values map[string]any) (CatalogValidation, error) {
	out := CatalogValidation{Valid: true, Issues: []CatalogIssue{}, Selections: []CatalogSelection{}}
	if len(values) > 256 {
		return out, apiError(400, "VALIDATION_ERROR", "At most 256 parameters are allowed")
	}
	if values == nil {
		values = map[string]any{}
	}
	for key, value := range values {
		if !paramKeyPattern.MatchString(key) {
			return out, apiError(400, "VALIDATION_ERROR", "Invalid parameter key")
		}
		switch v := value.(type) {
		case string:
			if len(v) > 4096 {
				return out, apiError(400, "VALIDATION_ERROR", "Parameter value exceeds 4096 bytes")
			}
		case bool, float64, float32, int, int64, int32, json.Number:
		default:
			return out, apiError(400, "VALIDATION_ERROR", "Parameter values must be public scalar values")
		}
	}
	raw, e := json.Marshal(values)
	if e != nil || len(raw) > mediaDepsLimit {
		return out, apiError(400, "VALIDATION_ERROR", "Invalid parameter selection")
	}
	scope := "project:" + projectID
	rows, e := tx.QueryContext(ctx, `SELECT id,scope_key,key,value_type,options,default_value,source_media_type,deps,state,created_at,updated_at FROM param_keys
 WHERE scope_key IN('global',$1) AND key IN(SELECT jsonb_object_keys($2::jsonb)) ORDER BY key,scope_key FOR SHARE`, scope, raw)
	if e != nil {
		return out, e
	}
	keys := map[string]ParamKeySummary{}
	for rows.Next() {
		var k ParamKeySummary
		if e = rows.Scan(&k.ID, &k.ScopeKey, &k.Key, &k.ValueType, &k.Options, &k.DefaultValue, &k.SourceMediaType, &k.Deps, &k.State, &k.CreatedAt, &k.UpdatedAt); e != nil {
			rows.Close()
			return out, e
		}
		previous, ok := keys[k.Key]
		if !ok || previous.ScopeKey == "global" {
			keys[k.Key] = k
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	for key := range values {
		if _, ok := keys[key]; !ok && strings.HasSuffix(key, ".version") {
			out.Issues = append(out.Issues, CatalogIssue{Key: key, Code: "PARAM_KEY_NOT_FOUND", Message: "Version parameters must be registered in the catalog"})
		}
	}
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	issue := func(key, code, message string) {
		out.Issues = append(out.Issues, CatalogIssue{Key: key, Code: code, Message: message})
	}
	matrices := func(key string, raw json.RawMessage) {
		c, err := decodeCompatibility(raw)
		if err != nil {
			issue(key, "PARAM_DEPENDENCY_INVALID", "Registered compatibility metadata is invalid")
			return
		}
		if c == nil {
			return
		}
		if len(c.Combinations) > 0 && !matchesCompatibilityCombination(c, values, keys) {
			out.Issues = append(out.Issues, CatalogIssue{Key: key, Code: "PARAM_COMBINATION_REJECTED", Message: fmt.Sprintf("%s requires one complete registered combination from matrix %s@%s", key, c.Source, c.Version), Matrix: c})
		}
		deps := make([]string, 0, len(c.Requires))
		for dep := range c.Requires {
			deps = append(deps, dep)
		}
		sort.Strings(deps)
		for _, dep := range deps {
			v, ok := values[dep].(string)
			match := false
			for _, candidate := range c.Requires[dep] {
				match = match || ok && v == candidate
			}
			if dep != "cpu.arch" {
				registered, exists := keys[dep]
				match = match && exists && registered.State == "active"
			}
			if !match {
				out.Issues = append(out.Issues, CatalogIssue{Key: key, Code: "PARAM_DEPENDENCY_REJECTED", Message: fmt.Sprintf("%s requires %s from matrix %s@%s", key, dep, c.Source, c.Version), Dependency: dep, Expected: c.Requires[dep], Matrix: c})
			}
		}
	}
	arch, _ := values["cpu.arch"].(string)
	for _, key := range names {
		k := keys[key]
		value, ok := values[key].(string)
		if k.State != "active" {
			issue(key, "PARAM_KEY_RETIRED", "Registered parameter is retired")
			continue
		}
		if !ok {
			issue(key, "PARAM_VALUE_REJECTED", "Catalog values must be strings")
			continue
		}
		selection := CatalogSelection{KeyID: k.ID, Key: key, Value: value, UpdatedAt: k.UpdatedAt, Deps: k.Deps}
		if k.ValueType == "version" {
			if arch != "amd64" && arch != "arm64" && arch != "noarch" {
				issue(key, "PARAM_ARCH_REQUIRED", "Select cpu.arch (amd64, arm64 or noarch) for a media version")
				continue
			}
			candidates, err := catalogMediaOptions(ctx, tx, scope, k.SourceMediaType, arch, true)
			if err != nil {
				return out, err
			}
			matches := []CatalogMedia{}
			for _, m := range candidates {
				if m.Version == value {
					matches = append(matches, m)
				}
			}
			if len(matches) == 0 {
				issue(key, "PARAM_VALUE_REJECTED", "No approved stored artifact matches the selected version and CPU architecture")
				continue
			}
			if len(matches) != 1 {
				issue(key, "PARAM_MEDIA_AMBIGUOUS", "Multiple artifacts share this version; give media distinct version labels before selection")
				continue
			}
			selection.Media = &matches[0]
			matrices(key, matches[0].Deps)
		} else {
			var options []string
			if e = json.Unmarshal(k.Options, &options); e != nil {
				return out, e
			}
			allowed := false
			for _, v := range options {
				allowed = allowed || v == value
			}
			if !allowed {
				issue(key, "PARAM_VALUE_REJECTED", "Value is not in the catalog option list")
				continue
			}
		}
		matrices(key, k.Deps)
		out.Selections = append(out.Selections, selection)
	}
	sort.SliceStable(out.Issues, func(i, j int) bool { return out.Issues[i].Key < out.Issues[j].Key })
	out.Valid = len(out.Issues) == 0
	return out, nil
}

// All columns must match one row. Never project columns into independent sets:
// that would incorrectly admit combinations that the report did not register.
func matchesCompatibilityCombination(c *Compatibility, values map[string]any, keys map[string]ParamKeySummary) bool {
	for _, row := range c.Combinations {
		match := true
		for dep, expected := range row {
			actual, ok := values[dep].(string)
			if !ok || actual != expected {
				match = false
				break
			}
			if dep != "cpu.arch" {
				key, exists := keys[dep]
				if !exists || key.State != "active" {
					match = false
					break
				}
			}
		}
		if match {
			return true
		}
	}
	return false
}

func (s *Store) ValidateCatalog(ctx context.Context, a *AuthSession, p *ProjectSummary, values map[string]any) (CatalogValidation, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return CatalogValidation{}, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, p.ID); e != nil {
		return CatalogValidation{}, e
	}
	return ValidateCatalogTx(ctx, tx, p.ID, values)
}

func mediaURI(typ, name, version string) string {
	return "media://" + typ + "/" + url.PathEscape(name) + "/" + url.PathEscape(version)
}

// VerifyCatalogArtifactsTx is the execution admission boundary. Selection-time
// metadata checks are cheap; commit re-reads and hashes each selected pinned
// artifact once. Existing approved metadata cannot bypass a missing/corrupt file.
func (s *Store) VerifyCatalogArtifactsTx(ctx context.Context, tx *sql.Tx, selections []CatalogSelection) error {
	seen := map[string]bool{}
	for _, selection := range selections {
		ref := selection.Media
		if ref == nil || seen[ref.ID] {
			continue
		}
		seen[ref.ID] = true
		var m MediaItemSummary
		if e := tx.QueryRowContext(ctx, `SELECT id,sha256,size FROM media_items WHERE id=$1 AND state='approved' FOR SHARE`, ref.ID).Scan(&m.ID, &m.Sha256, &m.Size); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return apiError(422, "PARAM_VALUE_REJECTED", "Selected media is no longer approved")
			}
			return e
		}
		var version string
		if e := tx.QueryRowContext(ctx, "SELECT object_version FROM media_artifacts WHERE media_id=$1", ref.ID).Scan(&version); e != nil {
			return e
		}
		if version != ref.ObjectVersion || m.Sha256 != ref.SHA256 {
			return apiError(422, "MEDIA_DIGEST_MISMATCH", "Selected media identity changed")
		}
		staged, e := s.checkedMedia(ctx, tx, &m)
		if e != nil {
			return e
		}
		staged.close()
	}
	return nil
}
