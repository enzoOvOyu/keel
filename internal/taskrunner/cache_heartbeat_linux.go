package taskrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentinventory"
	"golang.org/x/sys/unix"
)

func CollectMediaCacheIndex(ctx context.Context, now time.Time) agentinventory.CacheFrame {
	return collectMediaCacheIndex(ctx, MediaCacheDirectory, now)
}

// Heartbeat observes only the bounded ownership manifest. Actual file bytes are
// checked by inventory tasks and script execution, never inferred from this list.
func collectMediaCacheIndex(ctx context.Context, directory string, now time.Time) agentinventory.CacheFrame {
	f := agentinventory.CacheFrame{Version: agentinventory.CacheVersion, State: "unavailable", CapturedAt: now.UTC(), Entries: []agentinventory.CacheItem{}, FailureCode: "cache_directory_unavailable"}
	if ctx.Err() != nil {
		f.FailureCode = "cache_observation_interrupted"
		return f
	}
	dir, err := agentDirectory(directory)
	if err != nil {
		return f
	}
	defer unix.Close(dir)
	f.FailureCode = "cache_index_unavailable"
	lock, err := agentFile(dir, ".keel-cache.lock", true)
	if err != nil {
		return f
	}
	defer unix.Close(lock)
	if unix.Flock(lock, unix.LOCK_SH|unix.LOCK_NB) != nil {
		f.FailureCode = "cache_index_busy"
		return f
	}
	defer unix.Flock(lock, unix.LOCK_UN)
	m, raw, err := loadCacheManifest(dir)
	if err != nil {
		return f
	}
	if ctx.Err() != nil {
		f.FailureCode = "cache_observation_interrupted"
		return f
	}
	sum := sha256.Sum256(raw)
	f.ManifestSHA256 = hex.EncodeToString(sum[:])
	for _, entry := range m.Entries {
		f.Entries = append(f.Entries, agentinventory.CacheItem{SHA256: entry.SHA256, Size: entry.Size})
	}
	f.State, f.FailureCode = "ready", ""
	return f
}
