package execution

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func (s *Service) streamArtifactFetch(ctx context.Context, runner taskrunner.Runner, claim *Claim, c taskrunner.TaskCommand) (taskrunner.Observation, error) {
	o, err := runner.Do(ctx, taskrunner.Request{Action: "run", Command: c})
	if err != nil || o.Terminal() {
		return o, err
	}
	offset, err := artifactCheckpoint(o, c)
	if err != nil || offset != 0 || o.Output["source_verified"] != true {
		return o, taskrunner.ErrUnknown
	}
	file, err := os.CreateTemp("", "keel-execution-artifact-*")
	if err != nil {
		return o, err
	}
	defer func() { name := file.Name(); file.Close(); os.Remove(name) }()
	digest := sha256.New()
	for offset < c.ArtifactSize {
		if ctx.Err() != nil {
			return o, ctx.Err()
		}
		o, err = runner.Do(ctx, taskrunner.Request{Action: "read", Command: c, ChunkOffset: offset})
		if err != nil || o.Terminal() {
			return o, err
		}
		body, err := taskrunner.DecodeArtifactRead(o, c, offset)
		if err != nil {
			return o, err
		}
		if _, err = file.Write(body); err != nil {
			return o, err
		}
		digest.Write(body)
		offset += int64(len(body))
	}
	if hex.EncodeToString(digest.Sum(nil)) != c.ArtifactSHA256 {
		return o, taskrunner.ErrRejected
	}
	if err = file.Sync(); err != nil {
		return o, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return o, err
	}
	key := fmt.Sprintf("execution-artifacts/%s/%s/%s", c.ProjectID, c.AttemptID, c.ArtifactSHA256)
	if s.MediaStore == nil || s.MediaStore.MediaObjects == nil {
		return o, taskrunner.ErrRejected
	}
	version, err := s.MediaStore.MediaObjects.Put(ctx, key, file, c.ArtifactSize)
	if err != nil {
		return o, err
	}
	check := c
	check.ArtifactObjectVersion = version
	if check.Validate() != nil {
		return o, taskrunner.ErrRejected
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return o, err
	}
	defer tx.Rollback()
	var current bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_task_attempts a JOIN exec_executions x ON x.id=a.execution_id WHERE a.id=$1 AND a.command_id=$2 AND a.fencing_token=$3 AND a.state='running' AND a.lease_until>clock_timestamp() AND x.desired_state='running')`, c.AttemptID, c.CommandID, c.FencingToken).Scan(&current); err != nil {
		return o, err
	}
	if !current || s.scriptHeartbeatTx(ctx, tx, claim) {
		return o, taskrunner.ErrRejected
	}
	original, err := originalDispatchCommandTx(ctx, tx, claim, "artifact.dispatch_committed")
	if err != nil || original != c {
		return o, taskrunner.ErrRejected
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO exec_artifact_outputs(attempt_id,execution_id,project_id,command_id,fencing_token,media_id,sha256,size,object_key,object_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, c.AttemptID, claim.Attempt.ExecutionID, c.ProjectID, c.CommandID, c.FencingToken, c.ArtifactMediaID, c.ArtifactSHA256, c.ArtifactSize, key, version)
	if err != nil {
		return o, err
	}
	if err = event(ctx, tx, claim.Attempt.ExecutionID, "artifact.capture_stored", map[string]any{"attempt_id": c.AttemptID, "sha256": c.ArtifactSHA256, "size": c.ArtifactSize, "target_stop_verified": false}); err != nil {
		return o, err
	}
	if err = tx.Commit(); err != nil {
		return o, err
	}
	return runner.Do(ctx, taskrunner.Request{Action: "commit", Command: c})
}

func (s *Service) artifactOutputForReceipt(ctx context.Context, c taskrunner.TaskCommand) (map[string]any, error) {
	var execution, sha string
	var size int64
	err := s.DB.QueryRowContext(ctx, `SELECT execution_id,sha256,size FROM exec_artifact_outputs WHERE attempt_id=$1 AND command_id=$2 AND fencing_token=$3 AND project_id=$4 AND media_id=$5`, c.AttemptID, c.CommandID, c.FencingToken, c.ProjectID, c.ArtifactMediaID).Scan(&execution, &sha, &size)
	if err != nil || sha != c.ArtifactSHA256 || size != c.ArtifactSize {
		return nil, taskrunner.ErrUnknown
	}
	return map[string]any{"sha256": sha, "size": size, "content_verified": true, "download_url": fmt.Sprintf("/api/v1/projects/%s/executions/%s/outputs/%s/content", c.ProjectID, execution, c.AttemptID)}, nil
}

type RetrievedArtifact struct {
	Body      io.ReadSeekCloser
	SHA256    string
	Size      int64
	CreatedAt time.Time
}

// Download reads captured output only; it never contacts or authorizes a target.
func (s *Service) DownloadArtifact(ctx context.Context, a *Actor, project, execution, attempt, trace string) (*RetrievedArtifact, error) {
	if a == nil {
		return nil, platform.ErrUnauthenticated
	}
	if !opaqueID.MatchString(execution) || !opaqueID.MatchString(attempt) {
		return nil, platform.ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = platform.RecheckSessionTx(ctx, tx, a); err != nil {
		return nil, err
	}
	if !a.User.IsAdmin {
		var member bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE project_id=$1 AND user_id=$2)`, project, a.User.ID).Scan(&member); err != nil {
			return nil, err
		}
		if !member {
			return nil, platform.ErrNotFound
		}
	}
	var key, version, sha string
	var size int64
	var created time.Time
	err = tx.QueryRowContext(ctx, `SELECT o.object_key,o.object_version,o.sha256,o.size,o.created_at FROM exec_artifact_outputs o JOIN exec_task_attempts a ON a.id=o.attempt_id AND a.execution_id=o.execution_id AND a.command_id=o.command_id AND a.fencing_token=o.fencing_token JOIN exec_runner_receipts r ON r.attempt_id=o.attempt_id AND r.command_id=o.command_id AND r.fencing_token=o.fencing_token JOIN exec_executions x ON x.id=o.execution_id AND x.project_id=o.project_id WHERE o.attempt_id=$1 AND o.execution_id=$2 AND o.project_id=$3 AND r.result='success' AND r.payload->'output'->>'operation'='media.fetch' AND r.payload->'output'->'artifact_output'->>'sha256'=o.sha256 AND (r.payload->'output'->'artifact_output'->>'size')::bigint=o.size`, attempt, execution, project).Scan(&key, &version, &sha, &size, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, platform.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	if s.MediaStore == nil {
		return nil, failure(503, "ARTIFACT_STORAGE_UNAVAILABLE", "Captured output storage unavailable")
	}
	body, err := s.MediaStore.OpenCapturedArtifact(ctx, key, version, sha, size)
	if err != nil {
		return nil, err
	}
	auditTx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		body.Close()
		return nil, err
	}
	defer auditTx.Rollback()
	if err = platform.RecheckSessionTx(ctx, auditTx, a); err != nil {
		body.Close()
		return nil, err
	}
	if !a.User.IsAdmin {
		var member bool
		if err = auditTx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memberships WHERE project_id=$1 AND user_id=$2)`, project, a.User.ID).Scan(&member); err != nil || !member {
			body.Close()
			return nil, platform.ErrNotFound
		}
	}
	if err = audit(ctx, auditTx, a.User.ID, project, "execution.artifact.download", attempt, trace, map[string]any{"execution_id": execution, "sha256": sha, "size": size}); err != nil {
		body.Close()
		return nil, err
	}
	if err = auditTx.Commit(); err != nil {
		body.Close()
		return nil, err
	}
	return &RetrievedArtifact{Body: body, SHA256: sha, Size: size, CreatedAt: created}, nil
}
