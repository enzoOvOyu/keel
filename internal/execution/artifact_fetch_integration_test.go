package execution

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// Software transport fixture: actual private-file pin/read tests live in
// taskrunner. This fixture exercises PostgreSQL capture and publication rules.
type artifactCaptureRunner struct {
	base        *agentKernelRunner
	store       *platform.Store
	credential  *platform.AgentCredential
	body        []byte
	offset      int64
	started     time.Time
	runs        int
	loseCommit  bool
	lost        bool
	observation taskrunner.Observation
}

func (r *artifactCaptureRunner) Binding(project, machine string) (taskrunner.Binding, error) {
	return r.base.Binding(project, machine)
}
func (r *artifactCaptureRunner) Do(ctx context.Context, in taskrunner.Request) (taskrunner.Observation, error) {
	var zero taskrunner.Observation
	if err := r.store.AgentRequestAllowed(ctx, in, r.credential); err != nil {
		return zero, err
	}
	if r.lost {
		return zero, taskrunner.ErrUnknown
	}
	c := in.Command
	if in.Action == "run" {
		r.runs++
		r.started = time.Now().UTC()
	}
	output := map[string]any{"artifact_sha256": c.ArtifactSHA256, "artifact_size": c.ArtifactSize, "checkpoint_offset": r.offset, "cache_directory": taskrunner.ArtifactCacheDirectory, "content_verified": false, "reader_stopped": false, "source_verified": true, "reason": "artifact_reading"}
	o := taskrunner.Observation{Command: c, State: "running", StartedAt: r.started, Output: output}
	switch in.Action {
	case "read":
		if in.ChunkOffset != r.offset {
			return zero, taskrunner.ErrRejected
		}
		end := min(r.offset+taskrunner.ArtifactChunkBytes, c.ArtifactSize)
		part := r.body[r.offset:end]
		sum := sha256.Sum256(part)
		output["chunk_offset"] = r.offset
		output["chunk_base64"] = base64.StdEncoding.EncodeToString(part)
		output["chunk_sha256"] = hex.EncodeToString(sum[:])
		r.offset = end
		output["checkpoint_offset"] = end
	case "commit":
		if r.offset != c.ArtifactSize {
			return zero, taskrunner.ErrRejected
		}
		o.State = "success"
		o.FinishedAt = time.Now().UTC()
		output["content_verified"] = true
		output["reader_stopped"] = true
		output["reason"] = "artifact_retrieved"
	case "cancel":
		o.State = "failed"
		o.FinishedAt = time.Now().UTC()
		output["reader_stopped"] = true
		output["reason"] = "artifact_read_stopped"
	case "query":
		return r.observation, nil
	}
	if err := o.Validate(c); err != nil {
		return zero, err
	}
	r.observation = o
	if in.Action == "commit" && r.loseCommit {
		r.lost = true
		return zero, taskrunner.ErrUnknown
	}
	return o, nil
}

func approvedRetrieval(t *testing.T, f *agentKernelFixture, body []byte) (*platform.MediaItemSummary, *agentScriptObjects) {
	t.Helper()
	ctx := context.Background()
	objects := &agentScriptObjects{}
	f.store.MediaObjects = objects
	f.store.ArtifactTransfersEnabled = true
	f.service.MediaStore = f.store
	project := &platform.ProjectSummary{ID: f.project}
	media, err := f.store.IngestMedia(ctx, f.actor, project, platform.RegisterMediaInput{Type: "tool", Name: "retrieve-artifact", Version: "v1", Arch: "noarch", OsScope: "linux"}, bytes.NewReader(body), "retrieval-test")
	mustAgent(t, err)
	username, password := "retrieval-reviewer-"+platform.RandomID()[:10], "isolated-retrieval-reviewer-only-password"
	_, err = f.store.CreateUser(ctx, username, username, password, true, false)
	mustAgent(t, err)
	pre, _, err := f.store.NewPreSession(ctx)
	mustAgent(t, err)
	_, approver, err := f.store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: username, Password: password, Method: "local"}, "198.51.100.185", "retrieval-test")
	mustAgent(t, err)
	for _, state := range []string{"verified", "approved"} {
		media, err = f.store.TransitionMedia(ctx, approver, project, media.ID, state, "retrieval-test")
		mustAgent(t, err)
	}
	return media, objects
}
func TestAgentArtifactCaptureAndVerifiedDownload(t *testing.T) {
	for _, lose := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "lost-commit"}[lose], func(t *testing.T) {
			f := agentKernel(t)
			ctx := context.Background()
			body := bytes.Repeat([]byte("software-captured-content"), 800)
			media, objects := approvedRetrieval(t, f, body)
			in := ArtifactCreateInput{MachineIDs: []string{f.machine}, MediaID: media.ID, Transport: "agent", TimeoutMS: 30000}
			x, err := f.service.CreateArtifactRetrieval(ctx, f.actor, f.project, in, platform.RandomID(), "retrieval-test")
			mustAgent(t, err)
			if x.Snapshot.Flow.Definition.Tasks[0].Type != "media.fetch" {
				t.Fatal("wrong frozen operation")
			}
			claim := f.claim(t, x.ID)
			if claim == nil {
				t.Fatal("retrieval not claimed")
			}
			runner := &artifactCaptureRunner{base: f.runner, store: f.store, credential: f.credential, body: body, loseCommit: lose}
			f.service.AgentRunner = runner
			runErr := f.service.RunClaim(ctx, claim)
			if runner.runs != 1 {
				t.Fatal("retrieval dispatched more than once")
			}
			var count int
			mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_artifact_outputs WHERE attempt_id=$1`, claim.Attempt.ID).Scan(&count))
			if count != 1 {
				t.Fatal("verified bytes not captured")
			}
			if lose {
				if runErr == nil {
					t.Fatal("lost terminal proof became complete")
				}
				if _, err = f.service.DownloadArtifact(ctx, f.actor, f.project, x.ID, claim.Attempt.ID, "test"); err == nil {
					t.Fatal("capture without original target receipt downloadable")
				}
				return
			}
			mustAgent(t, runErr)
			out, err := f.service.DownloadArtifact(ctx, f.actor, f.project, x.ID, claim.Attempt.ID, "test")
			mustAgent(t, err)
			captured, err := io.ReadAll(out.Body)
			out.Body.Close()
			mustAgent(t, err)
			if !bytes.Equal(captured, body) || out.SHA256 != media.Sha256 {
				t.Fatal("captured output differs")
			}
			f.store.ArtifactTransfersEnabled = false
			out, err = f.service.DownloadArtifact(ctx, f.actor, f.project, x.ID, claim.Attempt.ID, "test")
			mustAgent(t, err)
			out.Body.Close()
			if _, err = f.service.DownloadArtifact(ctx, f.actor, platform.RandomID(), x.ID, claim.Attempt.ID, "test"); err == nil {
				t.Fatal("captured output crossed project scope")
			}
			objects.body = bytes.Repeat([]byte("x"), len(body))
			if _, err = f.service.DownloadArtifact(ctx, f.actor, f.project, x.ID, claim.Attempt.ID, "test"); err == nil {
				t.Fatal("tampered captured version downloaded")
			}
			var quarantined bool
			mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT quarantined_at IS NOT NULL FROM media_artifact_integrity WHERE media_id=$1`, media.ID).Scan(&quarantined))
			if quarantined {
				t.Fatal("captured-output corruption quarantined independent source")
			}
		})
	}
}
