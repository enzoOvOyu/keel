package platform

import (
	"context"
	"database/sql"
	"io"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type TaskArtifact struct {
	MediaID       string    `json:"media_id"`
	Name          string    `json:"name"`
	Version       string    `json:"version"`
	SHA256        string    `json:"sha256"`
	Size          int64     `json:"size"`
	ObjectVersion string    `json:"object_version"`
	ApprovedBy    string    `json:"approved_by"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (p TaskArtifact) Command(c taskrunner.TaskCommand) taskrunner.TaskCommand {
	c.ArtifactMediaID, c.ArtifactObjectVersion, c.ArtifactSHA256, c.ArtifactSize = p.MediaID, p.ObjectVersion, p.SHA256, p.Size
	return c
}
func (s *Store) ResolveTaskArtifactTx(ctx context.Context, tx *sql.Tx, project, id string, frozen *TaskArtifact) (*TaskArtifact, error) {
	if s == nil || !s.ArtifactTransfersEnabled {
		return nil, apiError(409, "ARTIFACT_TRANSFERS_DISABLED", "Approved artifact distribution is disabled")
	}
	m, err := mediaForScopeLocked(ctx, tx, id, s.scopeKeyFor(&ProjectSummary{ID: project}), true)
	if err != nil {
		return nil, err
	}
	if m.State != "approved" || !m.ArtifactStored || m.ArtifactQuarantined || m.Size < 1 || m.Size > taskrunner.MaxArtifactBytes || m.ApprovedBy == "" || m.ApprovedBy == m.UploadedBy {
		return nil, apiError(409, "TASK_ARTIFACT_NOT_APPROVED", "An independently approved stored artifact of 1 byte to 1 GiB is required")
	}
	p := &TaskArtifact{MediaID: m.ID, Name: m.Name, Version: m.Version, SHA256: m.Sha256, Size: m.Size, ApprovedBy: m.ApprovedBy, UpdatedAt: m.UpdatedAt}
	var sha string
	var size int64
	if err = tx.QueryRowContext(ctx, `SELECT object_version,sha256,size FROM media_artifacts WHERE media_id=$1 FOR SHARE`, id).Scan(&p.ObjectVersion, &sha, &size); err != nil {
		return nil, err
	}
	if p.SHA256 != sha || p.Size != size {
		return nil, apiError(409, "TASK_ARTIFACT_CHANGED", "Artifact metadata differs from approved media")
	}
	check := p.Command(taskrunner.TaskCommand{APIVersion: taskrunner.APIVersion, ProjectID: project, MachineID: project, CommandID: project, AttemptID: project, FencingToken: 1, Operation: "media.stage", TimeoutMS: 5000, AuthorizationExpiresAt: 1})
	if check.Validate() != nil {
		return nil, apiError(409, "TASK_ARTIFACT_INVALID", "Approved artifact has invalid immutable metadata")
	}
	if frozen != nil {
		normalized := *frozen
		normalized.UpdatedAt = p.UpdatedAt
		if normalized != *p || !frozen.UpdatedAt.Equal(p.UpdatedAt) {
			return nil, apiError(409, "TASK_ARTIFACT_CHANGED", "Frozen approved artifact changed")
		}
	}
	if err = mediaIntegrityGate(ctx, tx, id, true); err != nil {
		return nil, err
	}
	return p, nil
}

type taskArtifactFile struct{ staged *stagedMedia }

func (f *taskArtifactFile) Read(p []byte) (int, error) { return f.staged.file.Read(p) }
func (f *taskArtifactFile) Seek(offset int64, whence int) (int64, error) {
	return f.staged.file.Seek(offset, whence)
}
func (f *taskArtifactFile) Close() error { f.staged.close(); return nil }

// Detached IO preserves the pinned version without holding a database/resource
// transaction while reading bytes. Approval and integrity are rechecked at end.
func (s *Store) OpenTaskArtifact(ctx context.Context, project string, frozen *TaskArtifact, actor, trace string) (io.ReadSeekCloser, error) {
	if frozen == nil {
		return nil, ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = s.ResolveTaskArtifactTx(ctx, tx, project, frozen.MediaID, frozen); err != nil {
		return nil, err
	}
	m, err := mediaForScopeLocked(ctx, tx, frozen.MediaID, s.scopeKeyFor(&ProjectSummary{ID: project}), true)
	if err != nil {
		return nil, err
	}
	var key string
	if err = tx.QueryRowContext(ctx, `SELECT object_key FROM media_artifacts WHERE media_id=$1 AND object_version=$2`, frozen.MediaID, frozen.ObjectVersion).Scan(&key); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	finish := func() error {
		check, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer check.Rollback()
		if _, err = s.ResolveTaskArtifactTx(ctx, check, project, frozen.MediaID, frozen); err != nil {
			return err
		}
		return check.Commit()
	}
	staged, err := s.stagePinnedMedia(ctx, m, key, frozen.ObjectVersion, frozen.SHA256, frozen.Size, nil, finish)
	if err != nil {
		return nil, s.RecordMediaIntegrityFailure(ctx, err, actor, trace)
	}
	return &taskArtifactFile{staged: staged}, nil
}
