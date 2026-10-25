package platform

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// MediaQuarantineEvidence reports the immutable last complete failed observation.
// It is not a fresh storage read, a vendor diagnosis, or permission to clear it.
type MediaQuarantineEvidence struct {
	MediaID        string    `json:"media_id"`
	State          string    `json:"state"`
	ObjectVersion  string    `json:"object_version"`
	ExpectedSHA256 string    `json:"expected_sha256"`
	ExpectedSize   int64     `json:"expected_size"`
	ObservedSHA256 string    `json:"observed_sha256"`
	ObservedSize   int64     `json:"observed_size"`
	QuarantinedAt  time.Time `json:"quarantined_at"`
	Reason         string    `json:"reason"`
	MismatchCount  int       `json:"mismatch_count"`
}

func (s *Store) GetMediaQuarantineEvidence(ctx context.Context, actor *AuthSession, project *ProjectSummary, id string) (*MediaQuarantineEvidence, error) {
	tx, err := s.beginControlRead(ctx, actor)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if project == nil && !actor.User.IsAdmin {
		return nil, ErrForbidden
	}
	if project != nil {
		if err = requireProjectMember(ctx, tx, actor, project.ID); err != nil {
			return nil, err
		}
	}
	var out MediaQuarantineEvidence
	err = tx.QueryRowContext(ctx, `SELECT m.id,m.state,a.object_version,m.sha256,m.size,i.observed_sha256,i.observed_size,i.quarantined_at,i.reason,i.mismatch_count
 FROM media_items m JOIN media_artifacts a ON a.media_id=m.id JOIN media_artifact_integrity i ON i.media_id=m.id
 WHERE m.id=$1 AND (m.scope_key=$2 OR m.scope_key='global') AND i.quarantined_at IS NOT NULL`, id, s.scopeKeyFor(project)).Scan(&out.MediaID, &out.State, &out.ObjectVersion, &out.ExpectedSHA256, &out.ExpectedSize, &out.ObservedSHA256, &out.ObservedSize, &out.QuarantinedAt, &out.Reason, &out.MismatchCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
