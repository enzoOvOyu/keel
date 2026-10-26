package taskrunner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func artifactTestCommand(body []byte, id string, fence int64) TaskCommand {
	c := agentCommand(id, fence)
	digest := sha256.Sum256(body)
	c.Operation = "media.stage"
	c.ArtifactMediaID = strings.Repeat("5", 32)
	c.ArtifactObjectVersion = "fixture-object-v1"
	c.ArtifactSHA256 = hex.EncodeToString(digest[:])
	c.ArtifactSize = int64(len(body))
	c.AuthorizationExpiresAt = time.Now().Add(time.Minute).UnixMicro()
	return c
}
func artifactTestChunk(c TaskCommand, offset int64, body []byte) Request {
	digest := sha256.Sum256(body)
	return Request{Action: "append", Command: c, ChunkOffset: offset, ChunkSHA256: hex.EncodeToString(digest[:]), Chunk: body}
}
func TestArtifactProtocolBoundsAndStopProof(t *testing.T) {
	body := bytes.Repeat([]byte("a"), ArtifactChunkBytes+3)
	c := artifactTestCommand(body, "d", 1)
	r := artifactTestChunk(c, 0, body[:ArtifactChunkBytes])
	if r.Validate() != nil {
		t.Fatal("valid chunk rejected")
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > 14336 {
		t.Fatal("chunk exceeds task frame", len(raw), err)
	}
	for _, mutate := range []func(*Request){
		func(r *Request) { r.ChunkOffset = 1 },
		func(r *Request) { r.ChunkOffset = (1 << 63) - ArtifactChunkBytes },
		func(r *Request) { r.ChunkSHA256 = strings.Repeat("0", 64) },
		func(r *Request) { r.Chunk = r.Chunk[:1] },
		func(r *Request) { r.Script = []byte("execute") },
		func(r *Request) { r.Command.Operation = "host.identity" },
		func(r *Request) { r.Command.ArtifactSize = MaxArtifactBytes + 1 },
		func(r *Request) { r.Action = "run" },
	} {
		changed := r
		mutate(&changed)
		if changed.Validate() == nil {
			t.Fatal("invalid chunk admitted", changed.Action)
		}
	}
	o := Observation{Command: c, State: "success", StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now(), Output: artifactOutput(c, c.ArtifactSize, true, true, "artifact_verified")}
	if o.Validate(c) != nil {
		t.Fatal("full verified receipt rejected")
	}
	o.Output["writer_stopped"] = false
	if o.Validate(c) == nil {
		t.Fatal("success without stop proof")
	}
	o.Output["writer_stopped"] = true
	o.Output["checkpoint_offset"] = int64(ArtifactChunkBytes)
	if o.Validate(c) == nil {
		t.Fatal("partial file became success")
	}
}
func TestArtifactCheckpointResumeAndVerifiedCacheHit(t *testing.T) {
	state, cache := cacheDirectoryFixture(t), cacheDirectoryFixture(t)
	ctx := context.Background()
	body := bytes.Repeat([]byte("resume-fixture"), 1000)
	c := artifactTestCommand(body, "d", 1)
	call := func(r Request) Observation {
		t.Helper()
		o, err := handleArtifactTransfer(ctx, state, cache, r, true)
		if err != nil || o.Validate(r.Command) != nil {
			t.Fatal("artifact request", r.Action, err)
		}
		return o
	}
	if o := call(Request{Action: "run", Command: c}); o.Terminal() {
		t.Fatal("run unexpectedly terminal", o)
	}
	first := artifactTestChunk(c, 0, body[:ArtifactChunkBytes])
	call(first)
	path := filepath.Join(cache, c.ArtifactSHA256, "content")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := call(first)
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || duplicate.Output["checkpoint_offset"] != int64(ArtifactChunkBytes) {
		t.Fatal("duplicate chunk rewrote data", err)
	}
	stopped := call(Request{Action: "cancel", Command: c})
	if stopped.State != "failed" || stopped.Output["reason"] != "artifact_checkpointed" || stopped.Output["content_verified"] != false {
		t.Fatal("partial treated as complete", stopped)
	}
	if o := call(Request{Action: "run", Command: c}); o.State != "failed" {
		t.Fatal("consumed run restarted")
	}
	next := artifactTestCommand(body, "e", 2)
	o := call(Request{Action: "run", Command: next})
	if o.Output["checkpoint_offset"] != int64(ArtifactChunkBytes) {
		t.Fatal("resume offset lost", o)
	}
	if _, err = handleArtifactTransfer(ctx, state, cache, artifactTestChunk(c, ArtifactChunkBytes, body[ArtifactChunkBytes:]), true); err == nil { // Original terminal receipt is safe, but must not write.
		actual, _ := os.ReadFile(path)
		if len(actual) != ArtifactChunkBytes {
			t.Fatal("old command wrote after handover")
		}
	}
	call(artifactTestChunk(next, ArtifactChunkBytes, body[ArtifactChunkBytes:]))
	o = call(Request{Action: "commit", Command: next})
	if o.State != "success" || o.Output["content_verified"] != true {
		t.Fatal("verified completion missing", o)
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, body) {
		t.Fatal("completed bytes differ", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0400 {
		t.Fatal("verified file not read-only", err)
	}
	hit := artifactTestCommand(body, "f", 3)
	if o = call(Request{Action: "run", Command: hit}); o.State != "success" {
		t.Fatal("verified cache hit failed", o)
	}
	if o, err = handleArtifactTransfer(ctx, state, cache, Request{Action: "query", Command: next}, false); err != nil || o.State != "success" {
		t.Fatal("historical query blocked by opt-out", err)
	}
}
func TestArtifactRejectsCorruptionUnownedAndExpiredWrites(t *testing.T) {
	t.Run("corrupt-prefix", func(t *testing.T) {
		state, cache := cacheDirectoryFixture(t), cacheDirectoryFixture(t)
		ctx := context.Background()
		body := bytes.Repeat([]byte("b"), ArtifactChunkBytes+2)
		c := artifactTestCommand(body, "d", 1)
		for _, r := range []Request{{Action: "run", Command: c}, artifactTestChunk(c, 0, body[:ArtifactChunkBytes])} {
			if _, err := handleArtifactTransfer(ctx, state, cache, r, true); err != nil {
				t.Fatal(err)
			}
		}
		path := filepath.Join(cache, c.ArtifactSHA256, "content")
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.WriteAt([]byte("x"), 0)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if _, err = handleArtifactTransfer(ctx, state, cache, artifactTestChunk(c, ArtifactChunkBytes, body[ArtifactChunkBytes:]), true); err != nil {
			t.Fatal(err)
		}
		o, err := handleArtifactTransfer(ctx, state, cache, Request{Action: "commit", Command: c}, true)
		if err != nil || o.State != "failed" || o.Output["reason"] != "artifact_digest_mismatch" {
			t.Fatal("corrupt prefix passed", o, err)
		}
	})
	t.Run("unowned", func(t *testing.T) {
		state, cache := cacheDirectoryFixture(t), cacheDirectoryFixture(t)
		c := artifactTestCommand([]byte("unowned"), "d", 1)
		dir := filepath.Join(cache, c.ArtifactSHA256)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "content")
		if err := os.WriteFile(path, []byte("customer-file"), 0600); err != nil {
			t.Fatal(err)
		}
		if o, err := handleArtifactTransfer(context.Background(), state, cache, Request{Action: "run", Command: c}, true); err == nil && o.State == "success" {
			t.Fatal("unowned adopted")
		}
		actual, err := os.ReadFile(path)
		if err != nil || string(actual) != "customer-file" {
			t.Fatal("unowned file changed", err)
		}
	})
	t.Run("expired", func(t *testing.T) {
		state, cache := cacheDirectoryFixture(t), cacheDirectoryFixture(t)
		c := artifactTestCommand([]byte("expired"), "d", 1)
		c.AuthorizationExpiresAt = time.Now().Add(-time.Second).UnixMicro()
		if _, err := handleArtifactTransfer(context.Background(), state, cache, Request{Action: "run", Command: c}, true); err == nil {
			t.Fatal("expired run admitted")
		}
		entries, err := os.ReadDir(state)
		if err != nil || len(entries) != 0 {
			t.Fatal("expired run created state", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		state, cache := cacheDirectoryFixture(t), cacheDirectoryFixture(t)
		c := artifactTestCommand([]byte("symlink"), "d", 1)
		if err := os.Symlink(cacheDirectoryFixture(t), filepath.Join(cache, c.ArtifactSHA256)); err != nil {
			t.Fatal(err)
		}
		if o, err := handleArtifactTransfer(context.Background(), state, cache, Request{Action: "run", Command: c}, true); err == nil && o.State == "success" {
			t.Fatal("symlink adopted")
		}
	})
}
