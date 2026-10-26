package taskrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"time"

	"golang.org/x/sys/unix"
)

const artifactInventoryVersion = "keel-artifact-inventory/v1"
const artifactInventoryPageSize = 8

// This is a current ownership/size observation, not a fresh whole-file SHA proof.
// Neither private seed references nor active command identities leave the host.
type ArtifactInventoryEntry struct {
	SHA256           string     `json:"sha256"`
	Size             int64      `json:"size"`
	CheckpointOffset int64      `json:"checkpoint_offset"`
	State            string     `json:"state"`
	ModifiedAt       time.Time  `json:"modified_at"`
	LastVerifiedAt   *time.Time `json:"last_verified_at,omitempty"`
}

func artifactInventoryEntries(ctx context.Context, root int) ([]ArtifactInventoryEntry, error) {
	dup, err := unix.Dup(root)
	if err != nil {
		return nil, ErrRejected
	}
	dir := os.NewFile(uintptr(dup), "artifacts")
	names, err := dir.Readdirnames(130)
	dir.Close()
	if err != nil && !errors.Is(err, io.EOF) || len(names) >= 130 {
		return nil, ErrRejected
	}
	entries := make([]ArtifactInventoryEntry, 0)
	for _, name := range names {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !scriptDigestPattern.MatchString(name) {
			continue
		}
		if len(entries) >= 64 {
			return nil, ErrRejected
		}
		fd, err := artifactDirectory(root, name)
		if err != nil {
			return nil, ErrRejected
		}
		owner, err := readArtifactOwner(fd, name)
		if err != nil {
			unix.Close(fd)
			return nil, ErrRejected
		}
		file, err := openCacheFile(fd, "content", owner.Size)
		unix.Close(fd)
		if err != nil {
			return nil, ErrRejected
		}
		info, err := file.Stat()
		file.Close()
		if err != nil || info.Size() != owner.Offset {
			return nil, ErrRejected
		}
		item := ArtifactInventoryEntry{SHA256: name, Size: owner.Size, CheckpointOffset: owner.Offset, State: owner.State, ModifiedAt: info.ModTime().UTC()}
		if !owner.VerifiedAt.IsZero() {
			verified := owner.VerifiedAt
			item.LastVerifiedAt = &verified
		}
		entries = append(entries, item)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].SHA256 < entries[j].SHA256 })
	return entries, nil
}

func readArtifactInventory(ctx context.Context, directory, cursor, expected string) (map[string]any, string) {
	root, err := agentDirectory(directory)
	if err != nil {
		return nil, "cache_directory_unavailable"
	}
	defer unix.Close(root)
	lock, err := agentFile(root, ".keel-artifacts.lock", true)
	if err != nil {
		return nil, "cache_index_unavailable"
	}
	defer unix.Close(lock)
	if unix.Flock(lock, unix.LOCK_SH|unix.LOCK_NB) != nil {
		return nil, "cache_index_busy"
	}
	defer unix.Flock(lock, unix.LOCK_UN)
	entries, err := artifactInventoryEntries(ctx, root)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "cache_query_interrupted"
		}
		return nil, "cache_content_mismatch"
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return nil, "cache_index_unavailable"
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	if expected != "" && expected != digest {
		return nil, "cache_snapshot_changed"
	}
	start := 0
	if cursor != "" {
		found := false
		for i, item := range entries {
			if item.SHA256 == cursor {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, "cache_cursor_invalid"
		}
	}
	end := min(start+artifactInventoryPageSize, len(entries))
	next := ""
	if end < len(entries) {
		next = entries[end-1].SHA256
	}
	return map[string]any{"cache_version": artifactInventoryVersion, "cache_directory": ArtifactCacheDirectory, "manifest_sha256": digest, "registered_count": len(entries), "offset": start, "page_size": artifactInventoryPageSize, "items": entries[start:end], "next_cursor": next, "page_content_verified": false, "collected_at": time.Now().UTC(), "ttl_seconds": 30}, "artifact_index_observed"
}
