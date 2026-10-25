package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/mediasource"
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
	ArtifactQuarantined bool            `json:"artifact_quarantined"`
	ArtifactStored      bool            `json:"artifact_stored"`
	ID                  string          `json:"id"`
	ScopeKey            string          `json:"scope_key"`
	Type                string          `json:"type"`
	Name                string          `json:"name"`
	Version             string          `json:"version"`
	Sha256              string          `json:"sha256"`
	Size                int64           `json:"size"`
	Arch                string          `json:"arch"`
	OsScope             string          `json:"os_scope"`
	HwScope             string          `json:"hw_scope"`
	Deps                json.RawMessage `json:"deps"`
	ManifestRef         string          `json:"manifest_ref"`
	State               string          `json:"state"`
	UploadedBy          string          `json:"uploaded_by"`
	ApprovedBy          string          `json:"approved_by"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
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
	if input.Type != "driver" && input.Type != "firmware" && input.Type != "os" && input.Type != "nccl" && input.Type != "tool" && input.Type != "config" && input.Type != "ztp_script" {
		return apiError(400, "VALIDATION_ERROR", "Media type must be driver, firmware, os, nccl, tool, config or ztp_script")
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
	if (strings.HasPrefix(strings.ToLower(input.ManifestRef), "http://") || strings.HasPrefix(strings.ToLower(input.ManifestRef), "https://")) && mediasource.ValidateReference(input.ManifestRef) != nil {
		return apiError(400, "VALIDATION_ERROR", "Source URLs must not contain credentials, query strings or fragments")
	}
	if e := validateCompatibility(input.Deps); e != nil {
		return e
	}
	if input.Deps != nil {
		encoded, e := json.Marshal(input.Deps)
		if e != nil || len(encoded) > mediaDepsLimit {
			return apiError(400, "VALIDATION_ERROR", "Media dependency map must be valid JSON within 64 KiB")
		}
	}
	return nil
}

type MediaListFilter struct {
	Search         string
	Type           string
	State          string
	CompatibleArch string
	BootOnly       bool
	StoredOnly     bool
}

func (f MediaListFilter) validate() error {
	if !utf8.ValidString(f.Search) || utf8.RuneCountInString(f.Search) > 120 || strings.ContainsRune(f.Search, 0) {
		return apiError(400, "VALIDATION_ERROR", "Invalid media search")
	}
	if f.Type != "" && f.Type != "driver" && f.Type != "firmware" && f.Type != "os" && f.Type != "nccl" && f.Type != "tool" && f.Type != "config" && f.Type != "ztp_script" {
		return apiError(400, "VALIDATION_ERROR", "Invalid media type filter")
	}
	if f.State != "" && f.State != "uploaded" && f.State != "verified" && f.State != "approved" && f.State != "retired" {
		return apiError(400, "VALIDATION_ERROR", "Invalid media state filter")
	}
	if f.CompatibleArch != "" && f.CompatibleArch != "amd64" && f.CompatibleArch != "arm64" && f.CompatibleArch != "noarch" {
		return apiError(400, "VALIDATION_ERROR", "Invalid compatible architecture filter")
	}
	return nil
}

func (s *Store) ListMedia(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[MediaItemSummary], error) {
	return s.ListMediaFiltered(ctx, a, project, p, MediaListFilter{})
}

// ListMediaFiltered applies every filter inside the authorized scope before
// counting or paginating. StoredOnly describes the root, not full DAG admission.
func (s *Store) ListMediaFiltered(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination, criteria MediaListFilter) (Page[MediaItemSummary], error) {
	out := NewPage[MediaItemSummary](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	if err := criteria.validate(); err != nil {
		return out, err
	}
	criteria.Search = strings.TrimSpace(criteria.Search)
	scope := s.scopeKeyFor(project)
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if project == nil && !a.User.IsAdmin {
		return out, ErrForbidden
	}
	if project != nil {
		if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
			return out, e
		}
	}
	filter := "(scope_key = $1 OR scope_key = 'global')"
	args := []any{scope}
	add := func(clause string, value any) {
		args = append(args, value)
		filter += " AND " + fmt.Sprintf(clause, len(args))
	}
	if criteria.Search != "" {
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(criteria.Search)
		args = append(args, "%"+escaped+"%")
		filter += fmt.Sprintf(" AND (name ILIKE $%d ESCAPE E'\\\\' OR version ILIKE $%d ESCAPE E'\\\\')", len(args), len(args))
	}
	if criteria.Type != "" {
		add("type = $%d", criteria.Type)
	}
	if criteria.State != "" {
		add("state = $%d", criteria.State)
	}
	if criteria.CompatibleArch != "" {
		add("(arch = $%d OR arch = 'noarch')", criteria.CompatibleArch)
	}
	if criteria.BootOnly {
		filter += " AND type='os' AND arch IN('amd64','arm64') AND jsonb_typeof(deps->'os_boot')='object'"
	}
	if criteria.StoredOnly {
		filter += " AND EXISTS(SELECT 1 FROM media_artifacts ma WHERE ma.media_id=media_items.id) AND NOT EXISTS(SELECT 1 FROM media_artifact_integrity mi WHERE mi.media_id=media_items.id AND mi.quarantined_at IS NOT NULL)"
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM media_items WHERE "+filter, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT id,scope_key,type,name,version,sha256,size,arch,os_scope,hw_scope,deps,manifest_ref,state,uploaded_by,coalesce(approved_by,''),created_at,updated_at,EXISTS(SELECT 1 FROM media_artifacts ma WHERE ma.media_id=media_items.id),EXISTS(SELECT 1 FROM media_artifact_integrity mi WHERE mi.media_id=media_items.id AND mi.quarantined_at IS NOT NULL) FROM media_items WHERE "+filter+fmt.Sprintf(" ORDER BY type,name,version DESC,id LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2), append(args, p.PageSize, p.Offset())...)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var m MediaItemSummary
		if e = rows.Scan(&m.ID, &m.ScopeKey, &m.Type, &m.Name, &m.Version, &m.Sha256, &m.Size, &m.Arch, &m.OsScope, &m.HwScope, &m.Deps, &m.ManifestRef, &m.State, &m.UploadedBy, &m.ApprovedBy, &m.CreatedAt, &m.UpdatedAt, &m.ArtifactStored, &m.ArtifactQuarantined); e != nil {
			return out, e
		}
		out.Items = append(out.Items, m)
	}
	return out, rows.Err()
}

func (s *Store) RegisterMedia(ctx context.Context, a *AuthSession, project *ProjectSummary, input RegisterMediaInput, trace string) (*MediaItemSummary, error) {
	return s.registerMedia(ctx, a, project, input, trace, nil)
}
func (s *Store) registerMedia(ctx context.Context, a *AuthSession, project *ProjectSummary, input RegisterMediaInput, trace string, artifact *stagedMedia) (*MediaItemSummary, error) {
	if e := validateMediaInput(input); e != nil {
		return nil, e
	}
	scope := s.scopeKeyFor(project)
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireMediaManager(ctx, tx, a, project); e != nil {
		return nil, e
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	m := &MediaItemSummary{ID: RandomID(), ScopeKey: scope, Type: input.Type, Name: strings.TrimSpace(input.Name), Version: strings.TrimSpace(input.Version), Sha256: input.Sha256, Size: input.Size, Arch: input.Arch, OsScope: input.OsScope, HwScope: input.HwScope, Deps: modelSpecJSON(input.Deps), ManifestRef: input.ManifestRef, State: "uploaded", UploadedBy: a.User.ID, CreatedAt: now, UpdatedAt: now}
	if e = canonicalMediaDependencies(ctx, tx, m); e != nil {
		return nil, e
	}
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
	if artifact != nil {
		if e = s.bindMediaArtifact(ctx, tx, a, m, artifact, trace); e != nil {
			return nil, e
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return m, nil
}

// MediaTransitionInput binds an optional confirmation to the observed registry revision.
// Verification and approval still check pinned bytes and the one-way lifecycle.
type MediaTransitionInput struct {
	State             string     `json:"state"`
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at,omitempty"`
}

// Legacy callers retain the original one-way transition contract. Interactive
// clients send the version they actually reviewed to avoid stale confirmations.
func (s *Store) TransitionMedia(ctx context.Context, a *AuthSession, project *ProjectSummary, mediaID, target string, trace string) (*MediaItemSummary, error) {
	return s.TransitionMediaChecked(ctx, a, project, mediaID, MediaTransitionInput{State: target}, trace)
}

func (s *Store) TransitionMediaChecked(ctx context.Context, a *AuthSession, project *ProjectSummary, mediaID string, input MediaTransitionInput, trace string) (item *MediaItemSummary, resultErr error) {
	target := input.State
	if input.ExpectedUpdatedAt != nil && input.ExpectedUpdatedAt.IsZero() {
		return nil, apiError(400, "VALIDATION_ERROR", "expected_updated_at must be a non-zero timestamp")
	}

	if target != "verified" && target != "approved" && target != "retired" {
		return nil, apiError(400, "VALIDATION_ERROR", "Target state must be verified, approved or retired")
	}
	scope := s.scopeKeyFor(project)
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer func() { tx.Rollback(); resultErr = s.RecordMediaIntegrityFailure(ctx, resultErr, a.User.ID, trace) }()
	if e = requireMediaManager(ctx, tx, a, project); e != nil {
		return nil, e
	}
	m, e := mediaForScope(ctx, tx, mediaID, scope)
	if e != nil {
		return nil, e
	}
	if m.ScopeKey != scope {
		return nil, ErrForbidden
	}
	if input.ExpectedUpdatedAt != nil && !input.ExpectedUpdatedAt.Equal(m.UpdatedAt) {
		return nil, apiError(409, "MEDIA_CONFLICT", "Media changed since review; refresh and confirm its current state")
	}
	allowed := mediaTransitions[m.State][target]
	if !allowed {
		return nil, apiError(409, "MEDIA_TRANSITION_INVALID", "Media state "+m.State+" cannot transition to "+target)
	}
	if target == "verified" || target == "approved" {
		actual, err := s.checkedMedia(ctx, tx, m)
		if err != nil {
			return nil, err
		}
		actual.close()
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
	updatedAt := nextMediaUpdate(m.UpdatedAt)
	if _, e = tx.ExecContext(ctx, "UPDATE media_items SET state=$2, approved_by=CASE WHEN $3='' THEN NULL ELSE $3 END, updated_at=$4 WHERE id=$1", mediaID, target, approver, updatedAt); e != nil {
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
	m.UpdatedAt = updatedAt
	return m, nil
}

func mediaForScope(ctx context.Context, tx *sql.Tx, mediaID, scope string) (*MediaItemSummary, error) {
	return mediaForScopeLocked(ctx, tx, mediaID, scope, false)
}

func mediaForScopeLocked(ctx context.Context, tx *sql.Tx, mediaID, scope string, shared bool) (*MediaItemSummary, error) {
	if !modelIDPattern.MatchString(mediaID) {
		return nil, apiError(404, "MEDIA_NOT_FOUND", "Media not found")
	}
	var m MediaItemSummary
	lockClause := " FOR UPDATE"
	if shared {
		lockClause = " FOR SHARE"
	}
	e := tx.QueryRowContext(ctx, "SELECT id,scope_key,type,name,version,sha256,size,arch,os_scope,hw_scope,deps,manifest_ref,state,uploaded_by,coalesce(approved_by,''),created_at,updated_at,EXISTS(SELECT 1 FROM media_artifacts ma WHERE ma.media_id=media_items.id),EXISTS(SELECT 1 FROM media_artifact_integrity mi WHERE mi.media_id=media_items.id AND mi.quarantined_at IS NOT NULL) FROM media_items WHERE id=$1"+lockClause, mediaID).
		Scan(&m.ID, &m.ScopeKey, &m.Type, &m.Name, &m.Version, &m.Sha256, &m.Size, &m.Arch, &m.OsScope, &m.HwScope, &m.Deps, &m.ManifestRef, &m.State, &m.UploadedBy, &m.ApprovedBy, &m.CreatedAt, &m.UpdatedAt, &m.ArtifactStored, &m.ArtifactQuarantined)
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

// PostgreSQL timestamptz persists microseconds; return precisely the stored value.
func nextMediaUpdate(previous time.Time) time.Time {
	now := time.Now().UTC().Truncate(time.Microsecond)
	if !now.After(previous) {
		return previous.Add(time.Microsecond)
	}
	return now
}
