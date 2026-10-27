package taskrunner

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/precollect"
)

func precollectTestCommand() TaskCommand {
	return TaskCommand{APIVersion: APIVersion, ProjectID: strings.Repeat("1", 32), MachineID: strings.Repeat("2", 32), CommandID: strings.Repeat("3", 32), AttemptID: strings.Repeat("4", 32), FencingToken: 1, Operation: "host.precollect", AuthorizationExpiresAt: time.Now().Add(time.Minute).UnixMicro(), TimeoutMS: 30000}
}
func TestPrecollectProtocolDoesNotAdmitArbitraryPayload(t *testing.T) {
	c := precollectTestCommand()
	if c.Validate() != nil {
		t.Fatal("fixed operation rejected")
	}
	for _, request := range []Request{
		{Action: "run", Command: c, Script: []byte("arbitrary shell")},
		{Action: "read", Command: c, ChunkSHA256: strings.Repeat("a", 64), ChunkOffset: 1},
		{Action: "read", Command: c, ChunkSHA256: strings.Repeat("a", 64), ChunkOffset: precollect.MaxBundleBytes},
		{Action: "read", Command: c, ChunkSHA256: strings.Repeat("a", 64), Chunk: []byte("upload")},
		{Action: "read", Command: c, ChunkSHA256: strings.Repeat("A", 64)},
	} {
		if request.Validate() == nil {
			t.Fatalf("unsafe request admitted: %+v", request)
		}
	}
	c.AuthorizationExpiresAt = 0
	if c.Validate() == nil {
		t.Fatal("unbounded authorization admitted")
	}
}
func TestPrecollectOriginalStoppedArtifactChunks(t *testing.T) {
	ctx := context.Background()
	c := precollectTestCommand()
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	owned := filepath.Join(dir, c.CommandID)
	if e := os.Mkdir(owned, 0700); e != nil {
		t.Fatal(e)
	}
	raw := []byte(strings.Repeat("bounded-retained-facts", 1000))
	sha := precollect.Hash(raw)
	if e := os.WriteFile(filepath.Join(owned, "precollect.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	o := Observation{Command: c, State: "success", StartedAt: now.Add(-time.Second), FinishedAt: now, Output: map[string]any{"reason": "precollect_observed", "stopped": true, "precollect_version": precollect.Version, "precollect_sha256": sha, "precollect_size": len(raw)}}
	if e := writeObservation(owned, o); e != nil {
		t.Fatal(e)
	}
	w := Wrapper{StateDirectory: dir}
	var reconstructed []byte
	// Read/query work after the run flag is disabled; they never collect again.
	for offset := int64(0); offset < int64(len(raw)); offset += precollect.ChunkBytes {
		part, e := w.Handle(ctx, Request{Action: "read", Command: c, ChunkOffset: offset, ChunkSHA256: sha})
		if e != nil {
			t.Fatal(e)
		}
		body, e := base64.StdEncoding.Strict().DecodeString(part.Output["chunk"].(string))
		if e != nil {
			t.Fatal(e)
		}
		if part.Command != c || part.Output["chunk_sha256"] != precollect.Hash(body) {
			t.Fatal("chunk not bound to original command")
		}
		reconstructed = append(reconstructed, body...)
	}
	if precollect.Hash(reconstructed) != sha {
		t.Fatal("full digest differs")
	}
	changed := c
	changed.FencingToken++
	if _, e := w.Handle(ctx, Request{Action: "read", Command: changed, ChunkSHA256: sha}); e == nil {
		t.Fatal("foreign fence read original artifact")
	}
	if _, e := w.Handle(ctx, Request{Action: "read", Command: c, ChunkSHA256: strings.Repeat("a", 64)}); e == nil {
		t.Fatal("foreign digest read original artifact")
	}
	if _, e := w.Handle(ctx, Request{Action: "run", Command: c}); e == nil {
		t.Fatal("disabled collector ran")
	}
	// Even after enabling, an existing durable command only returns its original
	// result; it does not invoke host probes during this unit test.
	w.PrecollectEnabled = true
	again, e := w.Handle(ctx, Request{Action: "run", Command: c})
	if e != nil || again.Output["precollect_sha256"] != sha {
		t.Fatalf("original receipt not reused: %v", e)
	}
	if e = os.Remove(filepath.Join(owned, "precollect.json")); e != nil {
		t.Fatal(e)
	}
	foreign := filepath.Join(dir, "foreign.json")
	if e = os.WriteFile(foreign, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.Symlink(foreign, filepath.Join(owned, "precollect.json")); e != nil {
		t.Fatal(e)
	}
	if _, e = w.Handle(ctx, Request{Action: "read", Command: c, ChunkSHA256: sha}); e == nil {
		t.Fatal("symlink artifact accepted")
	}
}
