package taskrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArtifactInventoryPagesPartialStateAndSnapshotChanges(t *testing.T) {
	state, cache := cacheDirectoryFixture(t), cacheDirectoryFixture(t)
	ctx := context.Background()
	commands := make([]TaskCommand, 0, 9)
	for i := 0; i < 9; i++ {
		body := bytes.Repeat([]byte{byte('a' + i)}, ArtifactChunkBytes+1)
		c := artifactTestCommand(body, fmt.Sprintf("%x", i+1), int64(i+1))
		if _, err := handleArtifactTransfer(ctx, state, cache, Request{Action: "run", Command: c}, true); err != nil {
			t.Fatal(err)
		}
		if _, err := handleArtifactTransfer(ctx, state, cache, artifactTestChunk(c, 0, body[:ArtifactChunkBytes]), true); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, c)
	}
	out, reason := readArtifactInventory(ctx, cache, "", "")
	if out == nil || reason != "artifact_index_observed" || out["page_content_verified"] != false {
		t.Fatal("partial inventory missing", out, reason)
	}
	c := agentCommand("e", 10)
	c.Operation = "host.artifact.inventory"
	observation := Observation{Command: c, State: "success", StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now(), Output: out}
	out["reason"] = reason
	if observation.Validate(c) != nil {
		t.Fatal("valid metadata receipt rejected")
	}
	raw, err := json.Marshal(out)
	if err != nil || len(raw) > 14336 || strings.Contains(string(raw), "seed_media_id") || strings.Contains(string(raw), "seed_object_version") || strings.Contains(string(raw), "command_id") {
		t.Fatal("inventory leaks private source or exceeds frame", err)
	}
	c.CacheCursor = out["next_cursor"].(string)
	c.CacheManifestSHA256 = out["manifest_sha256"].(string)
	next, reason := readArtifactInventory(ctx, cache, c.CacheCursor, c.CacheManifestSHA256)
	if next == nil || len(next["items"].([]ArtifactInventoryEntry)) != 1 || next["next_cursor"] != "" {
		t.Fatal("snapshot continuation failed", next, reason)
	}
	next["reason"] = reason
	observation.Command = c
	observation.Output = next
	observation.FinishedAt = time.Now()
	if observation.Validate(c) != nil {
		t.Fatal("valid continuation rejected")
	}
	if _, err := handleArtifactTransfer(ctx, state, cache, Request{Action: "cancel", Command: commands[0]}, true); err != nil {
		t.Fatal(err)
	}
	if page, reason := readArtifactInventory(ctx, cache, c.CacheCursor, c.CacheManifestSHA256); page != nil || reason != "cache_snapshot_changed" {
		t.Fatal("changed snapshot silently continued", reason)
	}
}

func TestArtifactInventoryDoesNotClaimFreshSHA(t *testing.T) {
	state, cache := cacheDirectoryFixture(t), cacheDirectoryFixture(t)
	ctx := context.Background()
	body := []byte("verified cache")
	c := artifactTestCommand(body, "d", 1)
	for _, r := range []Request{{Action: "run", Command: c}, artifactTestChunk(c, 0, body), {Action: "commit", Command: c}} {
		if _, err := handleArtifactTransfer(ctx, state, cache, r, true); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(cache, c.ArtifactSHA256, "content")
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), len(body)), 0600); err != nil {
		t.Fatal(err)
	}
	out, reason := readArtifactInventory(ctx, cache, "", "")
	if out == nil || reason != "artifact_index_observed" || out["page_content_verified"] != false || out["items"].([]ArtifactInventoryEntry)[0].LastVerifiedAt == nil {
		t.Fatal("metadata confused with current whole-file verification", out, reason)
	}
	out["page_content_verified"] = true
	out["reason"] = reason
	q := agentCommand("e", 2)
	q.Operation = "host.artifact.inventory"
	o := Observation{Command: q, State: "success", StartedAt: time.Now().Add(-time.Second), FinishedAt: time.Now(), Output: out}
	if o.Validate(q) == nil {
		t.Fatal("metadata-only receipt became fresh SHA proof")
	}
	hit := artifactTestCommand(body, "f", 3)
	o, err := handleArtifactTransfer(ctx, state, cache, Request{Action: "run", Command: hit}, true)
	if err != nil || o.State != "failed" || o.Output["reason"] != "artifact_digest_mismatch" {
		t.Fatal("actual use accepted changed bytes", o, err)
	}
}
