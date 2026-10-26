package taskrunner

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

type artifactReadPin struct {
	Command    TaskCommand `json:"command"`
	Device     uint64      `json:"device"`
	Inode      uint64      `json:"inode"`
	ModifiedNS int64       `json:"modified_ns"`
	ChangedNS  int64       `json:"changed_ns"`
}

func fetchPin(c TaskCommand, f *os.File) (artifactReadPin, error) {
	var st unix.Stat_t
	if unix.Fstat(int(f.Fd()), &st) != nil || st.Size != c.ArtifactSize {
		return artifactReadPin{}, ErrRejected
	}
	return artifactReadPin{Command: c, Device: uint64(st.Dev), Inode: st.Ino, ModifiedNS: st.Mtim.Sec*1_000_000_000 + st.Mtim.Nsec, ChangedNS: st.Ctim.Sec*1_000_000_000 + st.Ctim.Nsec}, nil
}
func fetchOutput(c TaskCommand, offset int64, verified, stopped bool, reason string) map[string]any {
	out := artifactOutput(c, offset, verified, false, reason)
	delete(out, "writer_stopped")
	out["reader_stopped"] = stopped
	return out
}
func fetchTerminal(dir string, o Observation, offset int64, verified bool, reason string) (Observation, error) {
	o.State = "failed"
	if verified {
		o.State = "success"
	}
	o.FinishedAt = time.Now().UTC()
	o.Output = fetchOutput(o.Command, offset, verified, true, reason)
	if writeObservation(dir, o) != nil {
		return Observation{}, ErrUnknown
	}
	return o, o.Validate(o.Command)
}
func openFetchContent(root int, c TaskCommand) (*os.File, error) {
	dir, err := artifactDirectory(root, c.ArtifactSHA256)
	if err != nil {
		return nil, ErrRejected
	}
	defer unix.Close(dir)
	owner, err := readArtifactOwner(dir, c.ArtifactSHA256)
	if err != nil || owner.State != "verified" || owner.Size != c.ArtifactSize {
		return nil, ErrRejected
	}
	f, err := openCacheFile(dir, "content", c.ArtifactSize)
	if err != nil {
		return nil, ErrRejected
	}
	info, err := f.Stat()
	if err != nil || info.Size() != c.ArtifactSize {
		f.Close()
		return nil, ErrRejected
	}
	return f, nil
}
func handleArtifactFetch(ctx context.Context, state, cache string, r Request, enabled bool) (Observation, error) {
	if r.Action == "settle" {
		return settleArtifactNonstart(ctx, state, r)
	}
	var zero Observation
	if os.Geteuid() == 0 || r.Validate() != nil || r.Command.Operation != "media.fetch" || r.Action != "run" && r.Action != "read" && r.Action != "commit" && r.Action != "query" && r.Action != "cancel" {
		return zero, ErrRejected
	}
	active := r.Action == "run" || r.Action == "read" || r.Action == "commit"
	if ctx.Err() != nil {
		return zero, ErrUnknown
	}
	if active && (!enabled || !time.UnixMicro(r.Command.AuthorizationExpiresAt).After(time.Now())) {
		return zero, ErrRejected
	}
	parent, err := agentDirectory(state)
	if err != nil {
		return zero, ErrRejected
	}
	defer unix.Close(parent)
	if r.Action == "run" {
		if err = unix.Mkdirat(parent, r.Command.CommandID, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
			return zero, ErrRejected
		}
		if unix.Fsync(parent) != nil {
			return zero, ErrUnknown
		}
	}
	commandDir := filepath.Join(state, r.Command.CommandID)
	dir, err := agentDirectory(commandDir)
	if err != nil {
		return zero, ErrUnknown
	}
	defer unix.Close(dir)
	lock, err := agentFile(dir, "run.lock", true)
	if err != nil {
		return zero, ErrRejected
	}
	defer unix.Close(lock)
	if unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return zero, ErrUnknown
	}
	defer unix.Flock(lock, unix.LOCK_UN)
	o, err := readObservation(filepath.Join(commandDir, "receipt.json"))
	if err == nil {
		if o.Validate(r.Command) != nil {
			return zero, ErrRejected
		}
		if o.Terminal() {
			return o, nil
		}
		if r.Action == "run" {
			return o, nil
		}
	} else {
		if !errors.Is(err, os.ErrNotExist) || r.Action != "run" {
			return zero, ErrUnknown
		}
		o = Observation{Command: r.Command, State: "running", StartedAt: time.Now().UTC(), Output: fetchOutput(r.Command, 0, false, false, "artifact_reading")}
		if writeObservation(commandDir, o) != nil {
			return zero, ErrUnknown
		}
	}
	offset, ok := scriptOutputInteger(o.Output["checkpoint_offset"])
	if !ok || offset < 0 || offset > r.Command.ArtifactSize {
		return zero, ErrUnknown
	}
	if r.Action == "cancel" || r.Action == "query" && !time.UnixMicro(r.Command.AuthorizationExpiresAt).After(time.Now()) {
		return fetchTerminal(commandDir, o, offset, false, "artifact_read_stopped")
	}
	if r.Action == "query" {
		return o, nil
	}
	work, done := context.WithDeadline(ctx, time.UnixMicro(r.Command.AuthorizationExpiresAt))
	defer done()
	root, err := agentDirectory(cache)
	if err != nil {
		return fetchTerminal(commandDir, o, offset, false, "artifact_source_unavailable")
	}
	defer unix.Close(root)
	global, err := agentFile(root, ".keel-artifacts.lock", true)
	if err != nil {
		return zero, ErrUnknown
	}
	defer unix.Close(global)
	if unix.Flock(global, unix.LOCK_SH|unix.LOCK_NB) != nil {
		return zero, ErrUnknown
	}
	defer unix.Flock(global, unix.LOCK_UN)
	file, err := openFetchContent(root, r.Command)
	if err != nil {
		return fetchTerminal(commandDir, o, offset, false, "artifact_source_unavailable")
	}
	defer file.Close()
	pin, err := fetchPin(r.Command, file)
	if err != nil {
		return fetchTerminal(commandDir, o, offset, false, "artifact_source_changed")
	}
	if r.Action == "run" {
		if err = verifyArtifactFile(work, file, r.Command); err != nil {
			if errors.Is(err, ErrRejected) {
				return fetchTerminal(commandDir, o, 0, false, "artifact_digest_mismatch")
			}
			return zero, ErrUnknown
		}
		after, err := fetchPin(r.Command, file)
		if err != nil || after != pin {
			return zero, ErrUnknown
		}
		raw, err := json.Marshal(pin)
		if err != nil {
			return zero, ErrUnknown
		}
		name, err := writeCacheFile(dir, raw, 0600)
		if err != nil {
			return zero, ErrUnknown
		}
		defer unix.Unlinkat(dir, name, 0)
		if unix.Renameat2(dir, name, dir, "artifact-read.json", unix.RENAME_NOREPLACE) != nil || unix.Fsync(dir) != nil {
			return zero, ErrUnknown
		}
		o.Output = fetchOutput(r.Command, 0, false, false, "artifact_reading")
		o.Output["source_verified"] = true
		if writeObservation(commandDir, o) != nil {
			return zero, ErrUnknown
		}
		return o, nil
	}
	saved, err := openCacheFile(dir, "artifact-read.json", 4096)
	if err != nil {
		return zero, ErrUnknown
	}
	defer saved.Close()
	d := json.NewDecoder(io.LimitReader(saved, 4097))
	d.DisallowUnknownFields()
	var original artifactReadPin
	if d.Decode(&original) != nil || d.Decode(new(any)) != io.EOF || original != pin {
		return fetchTerminal(commandDir, o, offset, false, "artifact_source_changed")
	}
	if work.Err() != nil {
		return zero, ErrUnknown
	}
	if r.Action == "commit" {
		if offset != r.Command.ArtifactSize {
			return zero, ErrRejected
		}
		return fetchTerminal(commandDir, o, offset, true, "artifact_retrieved")
	}
	length := min(int64(ArtifactChunkBytes), r.Command.ArtifactSize-r.ChunkOffset)
	if r.ChunkOffset > offset || r.ChunkOffset < offset && r.ChunkOffset+length > offset {
		return zero, ErrRejected
	}
	body := make([]byte, int(length))
	if _, err = file.ReadAt(body, r.ChunkOffset); err != nil {
		return zero, ErrUnknown
	}
	after, err := fetchPin(r.Command, file)
	if err != nil || after != pin {
		return fetchTerminal(commandDir, o, offset, false, "artifact_source_changed")
	}
	if work.Err() != nil {
		return zero, ErrUnknown
	}
	if r.ChunkOffset == offset {
		offset += length
	}
	o.Output = fetchOutput(r.Command, offset, false, false, "artifact_reading")
	o.Output["source_verified"] = true
	if writeObservation(commandDir, o) != nil {
		return zero, ErrUnknown
	}
	sum := sha256.Sum256(body)
	o.Output["chunk_offset"] = r.ChunkOffset
	o.Output["chunk_base64"] = base64.StdEncoding.EncodeToString(body)
	o.Output["chunk_sha256"] = hex.EncodeToString(sum[:])
	return o, nil
}
