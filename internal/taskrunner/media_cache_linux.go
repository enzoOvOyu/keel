package taskrunner

import (
	"bytes"
	"context"
	"crypto/rand"
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

// Operator-created private directory, never a request-supplied destination.
const MediaCacheDirectory = "/opt/keel-cache/media"
const mediaCacheManifest = ".keel-owned-v1.json"
const mediaCacheVersion = "keel-media-cache/v1"
const mediaCacheEntries = 64
const mediaCachePageSize = 16

type CacheEntry struct {
	SHA256            string    `json:"sha256"`
	Size              int64     `json:"size"`
	SeedMediaID       string    `json:"seed_media_id"`
	SeedObjectVersion string    `json:"seed_object_version"`
	StoredAt          time.Time `json:"stored_at"`
}

// Public inventory omits seed media/object references: a machine may have moved
// between projects since cache creation, while these are current host facts.
type CacheInventoryEntry struct {
	SHA256   string    `json:"sha256"`
	Size     int64     `json:"size"`
	StoredAt time.Time `json:"stored_at"`
}

type cacheManifest struct {
	Version string       `json:"version"`
	Entries []CacheEntry `json:"entries"`
}

func openCacheFile(dir int, name string, limit int64) (*os.File, error) {
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), name)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || st.Mode&0077 != 0 || st.Size < 0 || st.Size > limit {
		f.Close()
		return nil, ErrRejected
	}
	return f, nil
}

func loadCacheManifest(dir int) (cacheManifest, []byte, error) {
	m := cacheManifest{Version: mediaCacheVersion, Entries: []CacheEntry{}}
	f, err := openCacheFile(dir, mediaCacheManifest, 128<<10)
	if errors.Is(err, unix.ENOENT) {
		raw, _ := json.Marshal(m)
		return m, raw, nil
	}
	if err != nil {
		return m, nil, ErrRejected
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (128<<10)+1))
	if err != nil || len(raw) > 128<<10 {
		return m, nil, ErrRejected
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || d.Decode(new(any)) != io.EOF || m.Version != mediaCacheVersion || len(m.Entries) > mediaCacheEntries {
		return m, nil, ErrRejected
	}
	previous := ""
	for _, entry := range m.Entries {
		if !scriptDigestPattern.MatchString(entry.SHA256) || entry.SHA256 <= previous || entry.Size < 1 || entry.Size > MaxScriptBytes || !idPattern.MatchString(entry.SeedMediaID) || !validScriptVersion(entry.SeedObjectVersion) || entry.StoredAt.IsZero() {
			return m, nil, ErrRejected
		}
		previous = entry.SHA256
	}
	return m, raw, nil
}

func verifyCachedEntry(ctx context.Context, dir int, entry CacheEntry) error {
	if ctx.Err() != nil {
		return ErrRejected
	}
	f, err := openCacheFile(dir, entry.SHA256, MaxScriptBytes)
	if err != nil {
		return ErrRejected
	}
	defer f.Close()
	body, err := io.ReadAll(io.LimitReader(f, MaxScriptBytes+1))
	if err != nil || int64(len(body)) != entry.Size {
		return ErrRejected
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != entry.SHA256 {
		return ErrRejected
	}
	return nil
}

func writeCacheFile(dir int, body []byte, mode uint32) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", ErrUnknown
	}
	name := ".keel-stage-" + hex.EncodeToString(nonce[:])
	fd, err := unix.Openat(dir, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return "", ErrUnknown
	}
	f := os.NewFile(uintptr(fd), name)
	_, err = f.Write(body)
	if err == nil {
		err = unix.Fchmod(fd, mode)
	}
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil || closed != nil {
		unix.Unlinkat(dir, name, 0)
		return "", ErrUnknown
	}
	return name, nil
}

// cacheApprovedScript never adopts, replaces or deletes an unregistered object.
// A changed or missing registered file blocks use; recovery is an operator act.
func cacheApprovedScript(ctx context.Context, directory string, c TaskCommand, body []byte) (bool, error) {
	if validateScriptBody(c, body) != nil || ctx.Err() != nil {
		return false, ErrRejected
	}
	dir, err := agentDirectory(directory)
	if err != nil {
		return false, ErrRejected
	}
	defer unix.Close(dir)
	lock, err := agentFile(dir, ".keel-cache.lock", true)
	if err != nil {
		return false, ErrRejected
	}
	defer unix.Close(lock)
	if unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return false, ErrRejected
	}
	defer unix.Flock(lock, unix.LOCK_UN)
	m, _, err := loadCacheManifest(dir)
	if err != nil {
		return false, err
	}
	for _, entry := range m.Entries {
		if entry.SHA256 == c.ScriptSHA256 {
			if entry.Size != c.ScriptSize {
				return false, ErrRejected
			}
			return true, verifyCachedEntry(ctx, dir, entry)
		}
	}
	if len(m.Entries) >= mediaCacheEntries {
		return false, ErrRejected
	}
	// RENAME_NOREPLACE refuses a file absent from our ownership manifest,
	// including an orphan from an interrupted cache/index commit.
	stage, err := writeCacheFile(dir, body, 0400)
	if err != nil {
		return false, err
	}
	defer unix.Unlinkat(dir, stage, 0)
	if ctx.Err() != nil {
		return false, ErrRejected
	}
	if err = unix.Renameat2(dir, stage, dir, c.ScriptSHA256, unix.RENAME_NOREPLACE); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return false, ErrRejected
		}
		return false, ErrUnknown
	}
	m.Entries = append(m.Entries, CacheEntry{SHA256: c.ScriptSHA256, Size: c.ScriptSize, SeedMediaID: c.ScriptMediaID, SeedObjectVersion: c.ScriptObjectVersion, StoredAt: time.Now().UTC().Truncate(time.Microsecond)})
	sort.Slice(m.Entries, func(i, j int) bool { return m.Entries[i].SHA256 < m.Entries[j].SHA256 })
	raw, err := json.Marshal(m)
	if err != nil {
		return false, ErrUnknown
	}
	index, err := writeCacheFile(dir, raw, 0600)
	if err != nil {
		return false, ErrUnknown
	}
	defer unix.Unlinkat(dir, index, 0)
	if unix.Renameat(dir, index, dir, mediaCacheManifest) != nil || unix.Fsync(dir) != nil {
		return false, ErrUnknown
	}
	return false, nil
}

func readMediaCache(ctx context.Context, directory, cursor, expected string) (map[string]any, string) {
	dir, err := agentDirectory(directory)
	if err != nil {
		return nil, "cache_directory_unavailable"
	}
	defer unix.Close(dir)
	lock, err := agentFile(dir, ".keel-cache.lock", true)
	if err != nil {
		return nil, "cache_index_unavailable"
	}
	defer unix.Close(lock)
	if unix.Flock(lock, unix.LOCK_SH|unix.LOCK_NB) != nil {
		return nil, "cache_index_busy"
	}
	defer unix.Flock(lock, unix.LOCK_UN)
	m, raw, err := loadCacheManifest(dir)
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
		for i, entry := range m.Entries {
			if entry.SHA256 == cursor {
				start = i + 1
				found = true
				break
			}
		}
		if !found {
			return nil, "cache_cursor_invalid"
		}
	}
	end := start + mediaCachePageSize
	if end > len(m.Entries) {
		end = len(m.Entries)
	}
	items := make([]CacheInventoryEntry, 0, end-start)
	for _, entry := range m.Entries[start:end] {
		if verifyCachedEntry(ctx, dir, entry) != nil {
			if ctx.Err() != nil {
				return nil, "cache_query_interrupted"
			}
			return nil, "cache_content_mismatch"
		}
		items = append(items, CacheInventoryEntry{SHA256: entry.SHA256, Size: entry.Size, StoredAt: entry.StoredAt})
	}
	next := ""
	if end < len(m.Entries) {
		next = m.Entries[end-1].SHA256
	}
	return map[string]any{"cache_version": mediaCacheVersion, "cache_directory": MediaCacheDirectory, "manifest_sha256": digest, "registered_count": len(m.Entries), "offset": start, "page_size": mediaCachePageSize, "items": items, "next_cursor": next, "page_content_verified": true}, "cache_page_verified"
}

// readCachedScriptBody copies verified bytes while holding the ownership index
// lock. Execution uses this copy, never a pathname reopened after verification.
func readCachedScriptBody(ctx context.Context, directory string, c TaskCommand) ([]byte, error) {
	if c.Validate() != nil || c.Operation != "script.run" || !c.ScriptCacheAllowed || ctx.Err() != nil {
		return nil, ErrRejected
	}
	dir, err := agentDirectory(directory)
	if err != nil {
		return nil, ErrRejected
	}
	defer unix.Close(dir)
	lock, err := agentFile(dir, ".keel-cache.lock", true)
	if err != nil {
		return nil, ErrRejected
	}
	defer unix.Close(lock)
	if unix.Flock(lock, unix.LOCK_SH|unix.LOCK_NB) != nil {
		return nil, ErrRejected
	}
	defer unix.Flock(lock, unix.LOCK_UN)
	m, _, err := loadCacheManifest(dir)
	if err != nil {
		return nil, ErrRejected
	}
	for _, entry := range m.Entries {
		if entry.SHA256 != c.ScriptSHA256 {
			continue
		}
		if entry.Size != c.ScriptSize {
			return nil, ErrRejected
		}
		f, err := openCacheFile(dir, entry.SHA256, MaxScriptBytes)
		if err != nil {
			return nil, ErrRejected
		}
		defer f.Close()
		body, err := io.ReadAll(io.LimitReader(f, MaxScriptBytes+1))
		if err != nil || ctx.Err() != nil || validateScriptBody(c, body) != nil {
			return nil, ErrRejected
		}
		return body, nil
	}
	return nil, ErrRejected
}

// Probe never creates a command receipt or consumes its fence. A miss authorizes
// no execution; the controller may supply the approved body before its single run.
func probeScriptCache(ctx context.Context, directory string, enabled bool, c TaskCommand) (Observation, error) {
	if (Request{Action: "cache", Command: c}).Validate() != nil || ctx.Err() != nil || !time.UnixMicro(c.AuthorizationExpiresAt).After(time.Now()) {
		return Observation{}, ErrRejected
	}
	hit := false
	if enabled {
		_, err := readCachedScriptBody(ctx, directory, c)
		hit = err == nil
	}
	if ctx.Err() != nil {
		return Observation{}, ErrRejected
	}
	return Observation{Command: c, State: "accepted", Output: map[string]any{
		"cache_probe": "keel-script-cache/v1", "cache_hit": hit,
		"sha256": c.ScriptSHA256, "size": c.ScriptSize,
	}}, nil
}
