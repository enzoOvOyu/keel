package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"time"
)

// This is the inline HTTP ingest limit, not the distribution/performance tier.
const MediaMaxBytes int64 = 1 << 30

type stagedMedia struct {
	file   *os.File
	sha256 string
	size   int64
}

func (v *stagedMedia) close() { name := v.file.Name(); v.file.Close(); os.Remove(name) }
func stageMedia(body io.Reader) (*stagedMedia, error) {
	file, e := os.CreateTemp("", "keel-media-*")
	if e != nil {
		return nil, apiError(503, "MEDIA_STAGING_UNAVAILABLE", "Media staging unavailable")
	}
	v := &stagedMedia{file: file}
	h := sha256.New()
	size, e := io.Copy(io.MultiWriter(file, h), io.LimitReader(body, MediaMaxBytes+1))
	if e != nil {
		v.close()
		return nil, apiError(422, "MEDIA_READ_FAILED", "Media bytes could not be read completely")
	}
	if size > MediaMaxBytes {
		v.close()
		return nil, apiError(413, "MEDIA_TOO_LARGE", "Inline media upload exceeds 1 GiB")
	}
	v.size = size
	v.sha256 = hex.EncodeToString(h.Sum(nil))
	if _, e = file.Seek(0, io.SeekStart); e != nil {
		v.close()
		return nil, e
	}
	return v, nil
}
func requireMediaManager(ctx context.Context, tx *sql.Tx, a *AuthSession, project *ProjectSummary) error {
	if project == nil {
		if a.User.IsAdmin {
			return nil
		}
		return ErrForbidden
	}
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return e
	}
	if a.User.IsAdmin {
		return nil
	}
	var role string
	if e := tx.QueryRowContext(ctx, "SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE", project.ID, a.User.ID).Scan(&role); e != nil {
		return e
	}
	if role != "Admin" && role != "SRE" {
		return ErrForbidden
	}
	return nil
}
func (s *Store) CheckMediaManage(ctx context.Context, a *AuthSession, project *ProjectSummary) error {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = requireMediaManager(ctx, tx, a, project); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) IngestMedia(ctx context.Context, a *AuthSession, project *ProjectSummary, input RegisterMediaInput, body io.Reader, trace string) (*MediaItemSummary, error) {
	if e := s.CheckMediaManage(ctx, a, project); e != nil {
		return nil, e
	}
	if s.MediaObjects == nil {
		return nil, apiError(503, "MEDIA_STORAGE_UNAVAILABLE", "Media storage is not configured")
	}
	staged, e := stageMedia(body)
	if e != nil {
		return nil, e
	}
	defer staged.close()
	// Digest and length always come from actual bytes, never browser metadata.
	input.Sha256 = staged.sha256
	input.Size = staged.size
	input.ManifestRef = "artifact:sha256:" + staged.sha256
	return s.registerMedia(ctx, a, project, input, trace, staged)
}
func (s *Store) bindMediaArtifact(ctx context.Context, tx *sql.Tx, a *AuthSession, m *MediaItemSummary, staged *stagedMedia, trace string) error {
	if s.MediaObjects == nil {
		return apiError(503, "MEDIA_STORAGE_UNAVAILABLE", "Media storage is not configured")
	}
	if m.State != "uploaded" {
		return apiError(409, "MEDIA_UPLOAD_STATE", "Only unverified media can receive a file")
	}
	if m.UploadedBy != a.User.ID {
		return apiError(403, "MEDIA_UPLOADER_REQUIRED", "Only the registered uploader can attach bytes")
	}
	if staged.sha256 != m.Sha256 || staged.size != m.Size {
		return apiError(422, "MEDIA_DIGEST_MISMATCH", "Actual digest or size differs from registered media")
	}
	var bound bool
	if e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM media_artifacts WHERE media_id=$1)", m.ID).Scan(&bound); e != nil {
		return e
	}
	if bound {
		return apiError(409, "MEDIA_ARTIFACT_IMMUTABLE", "The registered artifact cannot be replaced; register a new version")
	}
	key := m.ID + "/" + RandomID()
	version, e := s.MediaObjects.Put(ctx, key, staged.file, staged.size)
	if e != nil {
		return apiError(503, "MEDIA_STORAGE_UNAVAILABLE", "Versioned media upload failed")
	}
	if version == "" || version == "null" {
		return apiError(503, "MEDIA_STORAGE_UNAVAILABLE", "Media storage did not return an immutable version")
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO media_artifacts(media_id,object_key,object_version,sha256,size,uploaded_by,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)", m.ID, key, version, staged.sha256, staged.size, a.User.ID, time.Now().UTC()); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "INSERT INTO media_artifact_integrity(media_id) VALUES($1)", m.ID); e != nil {
		return e
	}
	updatedAt := nextMediaUpdate(m.UpdatedAt)
	if _, e = tx.ExecContext(ctx, "UPDATE media_items SET updated_at=$2 WHERE id=$1", m.ID, updatedAt); e != nil {
		return e
	}
	m.UpdatedAt = updatedAt
	m.ArtifactStored = true
	return appendAudit(ctx, tx, a.User.ID, projectIDFromScope(m.ScopeKey), "media.upload", m.ID, trace, map[string]any{"sha256": staged.sha256, "size": staged.size, "object_version": version})
}
func projectIDFromScope(scope string) string {
	if len(scope) > 8 && scope[:8] == "project:" {
		return scope[8:]
	}
	return ""
}
func (s *Store) AttachMedia(ctx context.Context, a *AuthSession, project *ProjectSummary, id string, body io.Reader, trace string) (*MediaItemSummary, error) {
	if e := s.CheckMediaManage(ctx, a, project); e != nil {
		return nil, e
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireMediaManager(ctx, tx, a, project); e != nil {
		return nil, e
	}
	m, e := mediaForScope(ctx, tx, id, s.scopeKeyFor(project))
	if e != nil {
		return nil, e
	}
	if m.ScopeKey != s.scopeKeyFor(project) {
		return nil, ErrForbidden
	}
	if m.UploadedBy != a.User.ID {
		return nil, ErrForbidden
	}
	staged, e := stageMedia(body)
	if e != nil {
		return nil, e
	}
	defer staged.close()
	if e = s.bindMediaArtifact(ctx, tx, a, m, staged, trace); e != nil {
		return nil, e
	}
	return m, tx.Commit()
}

// checkedMedia reads the entire pinned version before any response bytes or
// approval are emitted. Range responses therefore never expose unchecked data.
func (s *Store) checkedMedia(ctx context.Context, tx *sql.Tx, m *MediaItemSummary) (*stagedMedia, error) {
	staged, err := s.checkedMediaFile(ctx, tx, m)
	if err != nil {
		return nil, err
	}
	if err = checkPinnedMediaDependencies(ctx, tx, m.ID, s); err != nil {
		staged.close()
		return nil, err
	}
	return staged, nil
}

func (s *Store) checkedMediaFile(ctx context.Context, tx *sql.Tx, m *MediaItemSummary) (*stagedMedia, error) {
	var key, version, sha string
	var size int64
	e := tx.QueryRowContext(ctx, "SELECT object_key,object_version,sha256,size FROM media_artifacts WHERE media_id=$1", m.ID).Scan(&key, &version, &sha, &size)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "MEDIA_ARTIFACT_MISSING", "Upload the actual file before verification or use")
	}
	if e != nil {
		return nil, e
	}
	if s.MediaObjects == nil {
		return nil, apiError(503, "MEDIA_STORAGE_UNAVAILABLE", "Media storage is not configured")
	}
	if e = mediaIntegrityGate(ctx, tx, m.ID, false); e != nil {
		return nil, e
	}
	var last error
	var mismatch *mediaIntegrityFailure
	consecutive := 0
	for attempt := 1; attempt <= mediaReadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		body, err := s.MediaObjects.Open(ctx, key, version)
		if err != nil {
			last = apiError(503, "MEDIA_OBJECT_UNAVAILABLE", "Pinned media object is unavailable")
		} else {
			staged, err := stageMedia(body)
			body.Close()
			if err != nil {
				last = err
			} else if staged.sha256 != m.Sha256 || staged.size != m.Size || staged.sha256 != sha || staged.size != size {
				consecutive++
				mismatch = &mediaIntegrityFailure{cause: apiError(422, "MEDIA_DIGEST_MISMATCH", "Stored media digest or size differs from its approved identity"), mediaID: m.ID, objectVersion: version, expectedSHA: m.Sha256, observedSHA: staged.sha256, expectedSize: m.Size, observedSize: staged.size}
				staged.close()
				last = mismatch.cause
			} else {
				// Hold the final integrity gate through the caller's admission commit.
				if err = mediaIntegrityGate(ctx, tx, m.ID, true); err != nil {
					staged.close()
					return nil, err
				}
				return staged, nil
			}
		}
		if attempt < mediaReadAttempts {
			if err := mediaRetryWait(ctx, attempt); err != nil {
				return nil, err
			}
		}
	}
	if consecutive == mediaReadAttempts {
		return nil, mismatch
	}
	return nil, last
}

// DownloadMedia returns a private verified temporary file. The caller owns its
// close/removal. Raw object credentials/URLs are never returned to browsers.
func (s *Store) DownloadMedia(ctx context.Context, a *AuthSession, project *ProjectSummary, id, trace string) (file *os.File, item *MediaItemSummary, resultErr error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, nil, e
	}
	defer func() { tx.Rollback(); resultErr = s.RecordMediaIntegrityFailure(ctx, resultErr, a.User.ID, trace) }()
	if project == nil && !a.User.IsAdmin {
		return nil, nil, ErrForbidden
	}
	if project != nil {
		if e = requireProjectMember(ctx, tx, a, project.ID); e != nil {
			return nil, nil, e
		}
	}
	m, e := mediaForScope(ctx, tx, id, s.scopeKeyFor(project))
	if e != nil {
		return nil, nil, e
	}
	if m.State != "approved" {
		return nil, nil, apiError(409, "MEDIA_NOT_APPROVED", "Only approved, non-retired media can be downloaded")
	}
	staged, e := s.checkedMedia(ctx, tx, m)
	if e != nil {
		return nil, nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "media.download", m.ID, trace, map[string]any{"sha256": m.Sha256, "size": m.Size}); e != nil {
		staged.close()
		return nil, nil, e
	}
	if e = tx.Commit(); e != nil {
		staged.close()
		return nil, nil, e
	}
	return staged.file, m, nil
}
