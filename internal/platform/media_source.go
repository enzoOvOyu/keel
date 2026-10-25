package platform

import (
	"context"
	"errors"
	"github.com/enzoOvOyu/keel/internal/mediasource"
	"io"
	"net/url"
	"time"
)

// FetchMediaSource materializes an explicitly allowed external source into the
// same immutable artifact binding as upload. It never verifies or approves it.
func (s *Store) FetchMediaSource(ctx context.Context, a *AuthSession, project *ProjectSummary, id, trace string) (*MediaItemSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireMediaManager(ctx, tx, a, project); err != nil {
		return nil, err
	}
	m, err := mediaForScope(ctx, tx, id, s.scopeKeyFor(project))
	if err != nil {
		return nil, err
	}
	if m.ScopeKey != s.scopeKeyFor(project) {
		return nil, ErrForbidden
	}
	if m.UploadedBy != a.User.ID {
		return nil, apiError(403, "MEDIA_UPLOADER_REQUIRED", "Only the original uploader can fetch source bytes")
	}
	if m.State != "uploaded" {
		return nil, apiError(409, "MEDIA_UPLOAD_STATE", "Only unverified media can receive a source file")
	}
	if m.ArtifactStored {
		return nil, apiError(409, "MEDIA_ARTIFACT_IMMUTABLE", "The registered artifact cannot be replaced; register a new version")
	}
	if m.Size > MediaMaxBytes {
		return nil, apiError(413, "MEDIA_TOO_LARGE", "Source fetch exceeds the inline 1 GiB limit")
	}
	if s.MediaSources == nil {
		return nil, apiError(503, "MEDIA_SOURCE_UNAVAILABLE", "Media source fetching is not configured")
	}
	if s.MediaObjects == nil {
		return nil, apiError(503, "MEDIA_STORAGE_UNAVAILABLE", "Media storage is not configured")
	}
	var last error
	for attempt := 1; attempt <= mediaReadAttempts; attempt++ {
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		body, e := s.MediaSources.Open(ctx, m.ManifestRef)
		if errors.Is(e, mediasource.ErrNotAllowed) {
			return nil, apiError(422, "MEDIA_SOURCE_REJECTED", "Source URL or redirect is outside the configured origin policy")
		}
		if e != nil {
			last = apiError(503, "MEDIA_SOURCE_UNAVAILABLE", "Allowed media source is unavailable")
		} else {
			staged, e := stageMedia(io.LimitReader(body, m.Size+1))
			body.Close()
			if e != nil {
				last = e
			} else if staged.sha256 != m.Sha256 || staged.size != m.Size {
				staged.close()
				last = apiError(422, "MEDIA_DIGEST_MISMATCH", "Source digest or size differs from the registered identity")
			} else {
				defer staged.close()
				if err = s.bindMediaArtifact(ctx, tx, a, m, staged, trace); err != nil {
					return nil, err
				}
				source, _ := url.Parse(m.ManifestRef)
				if err = appendAudit(ctx, tx, a.User.ID, projectIDOf(project), "media.source.fetch", m.ID, trace, map[string]any{"source_origin": source.Scheme + "://" + source.Host, "sha256": staged.sha256, "size": staged.size, "attempts": attempt}); err != nil {
					return nil, err
				}
				if err = tx.Commit(); err != nil {
					return nil, err
				}
				return m, nil
			}
		}
		if attempt < mediaReadAttempts {
			if err = mediaRetryWait(ctx, attempt); err != nil {
				return nil, err
			}
		}
	}
	return nil, last
}
