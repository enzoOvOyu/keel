package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Media library (WP-1.10, FR-1.8). A media item is registry metadata with an
// auditable one-way lifecycle: uploaded -> verified -> approved -> retired.
// Artifact bytes live in object storage; distribution and caching stay with
// the execution channels (WP-1.4/WP-1.5).

type MediaItemSummary struct {
	ID          string          `json:"id"`
	ScopeKey    string          `json:"scope_key"`
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Version     string          `json:"version"`
	Sha256      string          `json:"sha256"`
	Size        int64           `json:"size"`
	Arch        string          `json:"arch"`
	OsScope     string          `json:"os_scope"`
	HwScope     string          `json:"hw_scope"`
	Deps        json.RawMessage `json:"deps"`
	ManifestRef string          `json:"manifest_ref"`
	State       string          `json:"state"`
	UploadedBy  string          `json:"uploaded_by"`
	ApprovedBy  string          `json:"approved_by"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type RegisterMediaInput struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Version     string         `json:"version"`
	Sha256      string         `json:"sha256"`
	Size        int64          `json:"size"`
	Arch        string         `json:"arch"`
	OsScope     string         `json:"os_scope"`
	HwScope     string         `json:"hw_scope"`
	Deps        map[string]any `json:"deps"`
	ManifestRef string         `json:"manifest_ref"`
}

const mediaDepsLimit = 64 * 1024

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

var mediaTransitions = map[string]map[string]bool{
	"uploaded": {"verified": true, "retired": true},
	"verified": {"approved": true, "retired": true},
	"approved": {"retired": true},
	"retired":  {},
}

func validateMediaInput(input RegisterMediaInput) error {
	if input.Type != "driver" && input.Type != "firmware" && input.Type != "os" && input.Type != "nccl" && input.Type != "tool" && input.Type != "config" && input.Type != "agent" {
		return apiError(400, "VALIDATION_ERROR", "Media type must be driver, firmware, os, nccl, tool, config or agent")
	}
	if utf8.RuneCountInString(strings.TrimSpace(input.Name)) < 1 || utf8.RuneCountInString(input.Name) > 120 {
		return apiError(400, "VALIDATION_ERROR", "Media name must be 1-120 characters")
	}
	if utf8.RuneCountInString(strings.TrimSpace(input.Version)) < 1 || utf8.RuneCountInString(input.Version) > 120 {
		return apiError(400, "VALIDATION_ERROR", "Media version must be 1-120 characters")
	}
	if !sha256Pattern.MatchString(input.Sha256) {
		return apiError(400, "VALIDATION_ERROR", "sha256 must be a 64-character lowercase hex digest")
	}
	if input.Size < 0 {
		return apiError(400, "VALIDATION_ERROR", "Media size cannot be negative")
	}
	if input.Arch != "amd64" && input.Arch != "arm64" && input.Arch != "noarch" {
		return apiError(400, "VALIDATION_ERROR", "Media arch must be amd64, arm64 or noarch")
	}
	if utf8.RuneCountInString(input.OsScope) > 120 || utf8.RuneCountInString(input.HwScope) > 120 || utf8.RuneCountInString(input.ManifestRef) > 200 {
		return apiError(400, "VALIDATION_ERROR", "Media scope or manifest reference exceeds its limit")
	}
	if input.Deps != nil {
		encoded, e := json.Marshal(input.Deps)
		if e != nil || len(encoded) > mediaDepsLimit {
			return apiError(400, "VALIDATION_ERROR", "Media dependency map must be valid JSON within 64 KiB")
		}
	}
	return nil
}

func (s *Store) ListMedia(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[MediaItemSummary], error) {
	out := NewPage[MediaItemSummary](p)
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
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM media_items WHERE "+filter, scope).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT id,scope_key,type,name,version,sha256,size,arch,os_scope,hw_scope,deps,manifest_ref,state,uploaded_by,coalesce(approved_by,''),created_at,updated_at FROM media_items WHERE "+filter+" ORDER BY type,name,version DESC LIMIT $2 OFFSET $3", scope, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var m MediaItemSummary
		if e = rows.Scan(&m.ID, &m.ScopeKey, &m.Type, &m.Name, &m.Version, &m.Sha256, &m.Size, &m.Arch, &m.OsScope, &m.HwScope, &m.Deps, &m.ManifestRef, &m.State, &m.UploadedBy, &m.ApprovedBy, &m.CreatedAt, &m.UpdatedAt); e != nil {
			return out, e
		}
		out.Items = append(out.Items, m)
	}
	return out, rows.Err()
}

func (s *Store) RegisterMedia(ctx context.Context, a *AuthSession, project *ProjectSummary, input RegisterMediaInput, trace string) (*MediaItemSummary, error) {
	if e := validateMediaInput(input); e != nil {
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
	now := time.Now().UTC().Truncate(time.Microsecond)
	m := &MediaItemSummary{ID: RandomID(), ScopeKey: scope, Type: input.Type, Name: strings.TrimSpace(input.Name), Version: strings.TrimSpace(input.Version), Sha256: input.Sha256, Size: input.Size, Arch: input.Arch, OsScope: input.OsScope, HwScope: input.HwScope, Deps: modelSpecJSON(input.Deps), ManifestRef: input.ManifestRef, State: "uploaded", UploadedBy: a.User.ID, CreatedAt: now, UpdatedAt: now}
	_, e = tx.ExecContext(ctx, "INSERT INTO media_items(id,scope_key,type,name,version,sha256,size,arch,os_scope,hw_scope,deps,manifest_ref,state,uploaded_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,'uploaded',$13,$14,$14)",
		m.ID, m.ScopeKey, m.Type, m.Name, m.Version, m.Sha256, m.Size, m.Arch, m.OsScope, m.HwScope, m.Deps, m.ManifestRef, a.User.ID, now)
	if e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			return nil, apiError(409, "MEDIA_EXISTS", "This media type, name and version already exist in the scope")
		}
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "media.register", m.ID, trace, map[string]any{"type": m.Type, "name": m.Name, "version": m.Version, "sha256": m.Sha256, "arch": m.Arch}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return m, nil
}

// TransitionMedia advances the one-way lifecycle. Verification checks the
// recorded digest against the stored artifact manifest; approval is an
// explicit reviewer action; retirement is irreversible.
func (s *Store) TransitionMedia(ctx context.Context, a *AuthSession, project *ProjectSummary, mediaID, target string, trace string) (*MediaItemSummary, error) {
	if target != "verified" && target != "approved" && target != "retired" {
		return nil, apiError(400, "VALIDATION_ERROR", "Target state must be verified, approved or retired")
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
	m, e := mediaForScope(ctx, tx, mediaID, scope)
	if e != nil {
		return nil, e
	}
	allowed := mediaTransitions[m.State][target]
	if !allowed {
		return nil, apiError(409, "MEDIA_TRANSITION_INVALID", "Media state "+m.State+" cannot transition to "+target)
	}
	// Verification requires a manifest reference so the digest stays checkable.
	if target == "verified" && m.ManifestRef == "" {
		return nil, apiError(409, "MEDIA_MANIFEST_MISSING", "Verification requires a stored manifest reference")
	}
	// The uploader cannot approve their own media; approval is a review gate.
	if target == "approved" && m.UploadedBy == a.User.ID {
		return nil, apiError(409, "MEDIA_SELF_APPROVAL", "The uploader cannot approve their own media")
	}
	approver := m.ApprovedBy
	if target == "approved" {
		approver = a.User.ID
	} else if m.State != "approved" {
		approver = ""
	}
	if _, e = tx.ExecContext(ctx, "UPDATE media_items SET state=$2, approved_by=CASE WHEN $3='' THEN NULL ELSE $3 END, updated_at=$4 WHERE id=$1", mediaID, target, approver, time.Now().UTC()); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "media."+target, mediaID, trace, map[string]any{"before_state": m.State, "after_state": target}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	m.State = target
	m.ApprovedBy = approver
	return m, nil
}

func mediaForScope(ctx context.Context, tx *sql.Tx, mediaID, scope string) (*MediaItemSummary, error) {
	if !modelIDPattern.MatchString(mediaID) {
		return nil, apiError(404, "MEDIA_NOT_FOUND", "Media not found")
	}
	var m MediaItemSummary
	e := tx.QueryRowContext(ctx, "SELECT id,scope_key,type,name,version,sha256,size,arch,os_scope,hw_scope,deps,manifest_ref,state,uploaded_by,coalesce(approved_by,''),created_at,updated_at FROM media_items WHERE id=$1", mediaID).
		Scan(&m.ID, &m.ScopeKey, &m.Type, &m.Name, &m.Version, &m.Sha256, &m.Size, &m.Arch, &m.OsScope, &m.HwScope, &m.Deps, &m.ManifestRef, &m.State, &m.UploadedBy, &m.ApprovedBy, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(404, "MEDIA_NOT_FOUND", "Media not found")
	}
	if e != nil {
		return nil, e
	}
	if m.ScopeKey != scope && m.ScopeKey != "global" {
		return nil, apiError(404, "MEDIA_NOT_FOUND", "Media not found")
	}
	return &m, nil
}
