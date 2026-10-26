package taskrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func stagedArtifactFetchFixture(t *testing.T, body []byte) (string, string, TaskCommand) {
	t.Helper()
	state, cache := cacheDirectoryFixture(t), cacheDirectoryFixture(t)
	ctx := context.Background()
	c := artifactTestCommand(body, "d", 1)
	requests := []Request{{Action: "run", Command: c}}
	for offset := int64(0); offset < int64(len(body)); offset += ArtifactChunkBytes {
		end := min(offset+ArtifactChunkBytes, int64(len(body)))
		requests = append(requests, artifactTestChunk(c, offset, body[offset:end]))
	}
	requests = append(requests, Request{Action: "commit", Command: c})
	for _, r := range requests {
		if _, err := handleArtifactTransfer(ctx, state, cache, r, true); err != nil {
			t.Fatal(err)
		}
	}
	fetch := artifactTestCommand(body, "e", 2)
	fetch.Operation = "media.fetch"
	return state, cache, fetch
}

func TestArtifactFetchVerifiedBytesAndHistoricalStop(t *testing.T) {
	body := bytes.Repeat([]byte("read-fixture"), 1000)
	state, cache, c := stagedArtifactFetchFixture(t, body)
	ctx := context.Background()
	o, err := handleArtifactFetch(ctx, state, cache, Request{Action: "run", Command: c}, true)
	if err != nil || o.Terminal() || o.Output["source_verified"] != true {
		t.Fatal("retrieval not admitted", o, err)
	}
	captured := make([]byte, 0, len(body))
	for offset := int64(0); offset < c.ArtifactSize; {
		o, err = handleArtifactFetch(ctx, state, cache, Request{Action: "read", Command: c, ChunkOffset: offset}, true)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(o)
		if len(raw) > 14336 {
			t.Fatal("read response exceeds task frame", len(raw))
		}
		part, err := DecodeArtifactRead(o, c, offset)
		if err != nil {
			t.Fatal(err)
		}
		captured = append(captured, part...)
		offset += int64(len(part))
		historical, err := handleArtifactFetch(ctx, state, cache, Request{Action: "query", Command: c}, false)
		if err != nil || historical.Output["chunk_base64"] != nil {
			t.Fatal("chunk bytes persisted into historical receipt", err)
		}
	}
	if !bytes.Equal(captured, body) {
		t.Fatal("retrieved bytes differ")
	}
	o, err = handleArtifactFetch(ctx, state, cache, Request{Action: "commit", Command: c}, true)
	if err != nil || o.Validate(c) != nil || o.State != "success" || o.Output["reader_stopped"] != true || o.Output["writer_stopped"] != nil {
		t.Fatal("reader proof missing or confused with writer", o, err)
	}
	original := o
	o, err = handleArtifactFetch(ctx, state, cache, Request{Action: "query", Command: c}, false)
	if err != nil || o.State != original.State || o.Output["chunk_base64"] != nil {
		t.Fatal("historical retrieval proof lost", err)
	}
}
func TestArtifactFetchRejectsChangedSourceAndDirectionConfusion(t *testing.T) {
	body := bytes.Repeat([]byte("p"), ArtifactChunkBytes+1)
	state, cache, c := stagedArtifactFetchFixture(t, body)
	ctx := context.Background()
	if _, err := handleArtifactFetch(ctx, state, cache, Request{Action: "run", Command: c}, true); err != nil {
		t.Fatal(err)
	}
	if r := artifactTestChunk(c, 0, body[:ArtifactChunkBytes]); r.Validate() == nil {
		t.Fatal("retrieval command accepted upload bytes")
	}
	upload := c
	upload.Operation = "media.stage"
	if (Request{Action: "read", Command: upload}).Validate() == nil {
		t.Fatal("upload command accepted read action")
	}
	path := filepath.Join(cache, c.ArtifactSHA256, "content")
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("q"), len(body)), 0600); err != nil {
		t.Fatal(err)
	}
	o, err := handleArtifactFetch(ctx, state, cache, Request{Action: "read", Command: c}, true)
	if err != nil || o.State != "failed" || o.Output["reason"] != "artifact_source_changed" || o.Output["reader_stopped"] != true || o.Output["chunk_base64"] != nil {
		t.Fatal("changed source read as original", o, err)
	}
}
func TestArtifactFetchCannotCommitUnobservedBytes(t *testing.T) {
	state, cache, c := stagedArtifactFetchFixture(t, []byte("bounded-read"))
	ctx := context.Background()
	if _, err := handleArtifactFetch(ctx, state, cache, Request{Action: "run", Command: c}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := handleArtifactFetch(ctx, state, cache, Request{Action: "commit", Command: c}, true); err == nil {
		t.Fatal("unread bytes became retrieved")
	}
	o, err := handleArtifactFetch(ctx, state, cache, Request{Action: "cancel", Command: c}, false)
	if err != nil || o.State != "failed" || o.Output["reason"] != "artifact_read_stopped" || o.Output["content_verified"] != false {
		t.Fatal("cancel published partial content", o, err)
	}
}
