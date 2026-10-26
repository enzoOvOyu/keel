package execution

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type ArtifactCreateInput struct {
	MachineIDs []string `json:"machine_ids"`
	MediaID    string   `json:"media_id"`
	TimeoutMS  int      `json:"timeout_ms"`
	Transport  string   `json:"transport"`
}

func (s *Service) CreateArtifactDistribution(ctx context.Context, a *Actor, project string, in ArtifactCreateInput, key, trace string) (*Execution, error) {
	if s.MediaStore == nil || !s.MediaStore.ArtifactTransfersEnabled {
		return nil, failure(409, "ARTIFACT_TRANSFERS_DISABLED", "Approved artifact distribution is disabled")
	}
	if !opaqueID.MatchString(in.MediaID) || in.TimeoutMS < 100 || in.TimeoutMS > 30000 {
		return nil, invalid("Approved media ID and a 100..30000 ms window are required")
	}
	input := SSHCreateInput{MachineIDs: in.MachineIDs, ArtifactMediaID: in.MediaID, Operation: "media.stage", TimeoutMS: in.TimeoutMS}
	if in.Transport == "ssh" {
		return s.createSSH(ctx, a, project, input, "", 0, key, trace)
	}
	if in.Transport == "agent" {
		return s.createAgent(ctx, a, project, input, key, trace)
	}
	return nil, invalid("transport must be ssh or agent")
}
func (s *Service) CreateArtifactRetrieval(ctx context.Context, a *Actor, project string, in ArtifactCreateInput, key, trace string) (*Execution, error) {
	if s.MediaStore == nil || !s.MediaStore.ArtifactTransfersEnabled {
		return nil, failure(409, "ARTIFACT_TRANSFERS_DISABLED", "Artifact retrieval is disabled")
	}
	if len(in.MachineIDs) != 1 || !opaqueID.MatchString(in.MediaID) || in.TimeoutMS < 100 || in.TimeoutMS > 30000 {
		return nil, invalid("One target, approved media and a bounded window are required")
	}
	input := SSHCreateInput{MachineIDs: in.MachineIDs, ArtifactMediaID: in.MediaID, Operation: "media.fetch", TimeoutMS: in.TimeoutMS}
	if in.Transport == "ssh" {
		return s.createSSH(ctx, a, project, input, "", 0, key, trace)
	}
	if in.Transport == "agent" {
		return s.createAgent(ctx, a, project, input, key, trace)
	}
	return nil, invalid("transport must be ssh or agent")
}
func validateArtifactCreate(in SSHCreateInput, request string) error {
	if in.Operation != "media.stage" && in.Operation != "media.fetch" {
		if in.ArtifactMediaID != "" {
			return invalid("Artifact media is allowed only on distribution tasks")
		}
		return nil
	}
	if !opaqueID.MatchString(in.ArtifactMediaID) || request != "" || in.ScriptMediaID != "" || in.ScriptResultFormat != "" || in.PreferScriptCache || in.CacheCursor != "" || in.CacheManifestSHA256 != "" || in.WaitMS != 0 || in.TimeoutMS < 100 || in.TimeoutMS > 30000 {
		return invalid("A bounded approved artifact distribution is required")
	}
	return nil
}
func (s *Service) artifactFrozenTx(ctx context.Context, tx *sql.Tx, project string, t Task) error {
	if t.Artifact == nil {
		if t.Type == "media.stage" || t.Type == "media.fetch" {
			return failure(409, "TASK_ARTIFACT_REQUIRED", "Frozen artifact snapshot required")
		}
		return nil
	}
	if t.Type != "media.stage" && t.Type != "media.fetch" || t.Script != nil || t.Retry != 0 || t.Idempotent || s.MediaStore == nil {
		return failure(409, "TASK_ARTIFACT_POLICY_INVALID", "Frozen distribution policy is unavailable")
	}
	_, err := s.MediaStore.ResolveTaskArtifactTx(ctx, tx, project, t.Artifact.MediaID, t.Artifact)
	return err
}
func artifactCheckpoint(o taskrunner.Observation, c taskrunner.TaskCommand) (int64, error) {
	if o.Validate(c) != nil {
		return 0, taskrunner.ErrRejected
	}
	value := o.Output["checkpoint_offset"]
	var offset int64
	switch n := value.(type) {
	case int64:
		offset = n
	case int:
		offset = int64(n)
	case float64:
		offset = int64(n)
		if float64(offset) != n {
			return 0, taskrunner.ErrRejected
		}
	default:
		return 0, taskrunner.ErrRejected
	}
	if offset < 0 || offset > c.ArtifactSize || offset%taskrunner.ArtifactChunkBytes != 0 && offset != c.ArtifactSize || o.Output["artifact_sha256"] != c.ArtifactSHA256 {
		return 0, taskrunner.ErrRejected
	}
	return offset, nil
}
func (s *Service) streamArtifact(ctx context.Context, runner taskrunner.Runner, claim *Claim, c taskrunner.TaskCommand, creator string) (taskrunner.Observation, error) {
	o, err := runner.Do(ctx, taskrunner.Request{Action: "run", Command: c})
	if err != nil || o.Terminal() {
		return o, err
	}
	offset, err := artifactCheckpoint(o, c)
	if err != nil {
		return o, err
	}
	if offset < c.ArtifactSize {
		source, err := s.MediaStore.OpenTaskArtifact(ctx, c.ProjectID, claim.Task.Artifact, creator, "")
		if err != nil {
			return o, err
		}
		defer source.Close()
		if _, err = source.Seek(offset, io.SeekStart); err != nil {
			return o, err
		}
		buffer := make([]byte, taskrunner.ArtifactChunkBytes)
		for offset < c.ArtifactSize {
			if ctx.Err() != nil {
				return o, ctx.Err()
			}
			count := min(int64(len(buffer)), c.ArtifactSize-offset)
			body := buffer[:int(count)]
			if _, err = io.ReadFull(source, body); err != nil {
				return o, err
			}
			digest := sha256.Sum256(body)
			o, err = runner.Do(ctx, taskrunner.Request{Action: "append", Command: c, ChunkOffset: offset, ChunkSHA256: hex.EncodeToString(digest[:]), Chunk: body})
			if err != nil || o.Terminal() {
				return o, err
			}
			next, err := artifactCheckpoint(o, c)
			if err != nil || next != offset+count {
				return o, taskrunner.ErrUnknown
			}
			offset = next
		}
	}
	return runner.Do(ctx, taskrunner.Request{Action: "commit", Command: c})
}
