package taskrunner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

const artifactOwnerVersion = "keel-artifact-cache/v1"
const artifactCacheQuota int64 = 8 << 30

type artifactOwner struct {
	Version       string    `json:"version"`
	SHA256        string    `json:"sha256"`
	Size          int64     `json:"size"`
	MediaID       string    `json:"seed_media_id"`
	ObjectVersion string    `json:"seed_object_version"`
	Offset        int64     `json:"offset"`
	State         string    `json:"state"`
	CommandID     string    `json:"command_id"`
	FencingToken  int64     `json:"fencing_token"`
	ExpiresAt     int64     `json:"expires_at"`
	VerifiedAt    time.Time `json:"verified_at,omitempty"`
}

func readArtifactOwner(dir int, name string) (artifactOwner, error) {
	var o artifactOwner
	f, err := openCacheFile(dir, "owner.json", 4096)
	if err != nil {
		return o, ErrRejected
	}
	defer f.Close()
	d := json.NewDecoder(io.LimitReader(f, 4097))
	d.DisallowUnknownFields()
	if d.Decode(&o) != nil || d.Decode(new(any)) != io.EOF || o.Version != artifactOwnerVersion || o.SHA256 != name || !scriptDigestPattern.MatchString(o.SHA256) || o.Size < 1 || o.Size > MaxArtifactBytes || !idPattern.MatchString(o.MediaID) || !validScriptVersion(o.ObjectVersion) || !idPattern.MatchString(o.CommandID) || o.FencingToken < 1 || o.ExpiresAt < 1 || o.Offset < 0 || o.Offset > o.Size || o.Offset%ArtifactChunkBytes != 0 && o.Offset != o.Size {
		return o, ErrRejected
	}
	if o.State != "partial" && o.State != "paused" && o.State != "verified" || o.State == "verified" && (o.Offset != o.Size || o.VerifiedAt.IsZero()) || o.State != "verified" && !o.VerifiedAt.IsZero() {
		return o, ErrRejected
	}
	return o, nil
}
func writeArtifactOwner(dir int, o artifactOwner) error {
	raw, err := json.Marshal(o)
	if err != nil {
		return ErrUnknown
	}
	stage, err := writeCacheFile(dir, raw, 0600)
	if err != nil {
		return err
	}
	defer unix.Unlinkat(dir, stage, 0)
	if unix.Renameat(dir, stage, dir, "owner.json") != nil || unix.Fsync(dir) != nil {
		return ErrUnknown
	}
	return nil
}
func artifactDirectory(root int, name string) (int, error) {
	fd, err := unix.Openat(root, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, err
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		unix.Close(fd)
		return -1, ErrRejected
	}
	return fd, nil
}
func reserveArtifact(root int, c TaskCommand) error {
	dup, err := unix.Dup(root)
	if err != nil {
		return ErrRejected
	}
	directory := os.NewFile(uintptr(dup), "artifacts")
	names, err := directory.Readdirnames(130)
	directory.Close()
	if err != nil && !errors.Is(err, io.EOF) || len(names) >= 130 {
		return ErrRejected
	}
	var reserved int64
	count := 0
	for _, name := range names {
		if !scriptDigestPattern.MatchString(name) {
			continue
		}
		count++
		fd, err := artifactDirectory(root, name)
		if err != nil {
			return ErrRejected
		}
		owner, err := readArtifactOwner(fd, name)
		unix.Close(fd)
		if err != nil {
			return ErrRejected
		}
		reserved += owner.Size
	}
	if count >= 64 || reserved+c.ArtifactSize > artifactCacheQuota {
		return ErrRejected
	}
	return nil
}
func artifactOutput(c TaskCommand, offset int64, verified, stopped bool, reason string) map[string]any {
	return map[string]any{"artifact_sha256": c.ArtifactSHA256, "artifact_size": c.ArtifactSize, "checkpoint_offset": offset, "cache_directory": ArtifactCacheDirectory, "content_verified": verified, "writer_stopped": stopped, "reason": reason}
}
func artifactTerminal(commandDir string, o Observation, offset int64, verified bool, reason string) (Observation, error) {
	o.State = "failed"
	if verified {
		o.State = "success"
	}
	o.Output = artifactOutput(o.Command, offset, verified, true, reason)
	o.FinishedAt = time.Now().UTC()
	if writeObservation(commandDir, o) != nil {
		return Observation{}, ErrUnknown
	}
	return o, o.Validate(o.Command)
}
func verifyArtifactFile(ctx context.Context, f *os.File, c TaskCommand) error {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return ErrUnknown
	}
	h := sha256.New()
	buf := make([]byte, 128<<10)
	var size int64
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		n, err := f.Read(buf)
		if n > 0 {
			size += int64(n)
			if size > c.ArtifactSize {
				return ErrRejected
			}
			h.Write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return ErrUnknown
		}
	}
	if size != c.ArtifactSize || hex.EncodeToString(h.Sum(nil)) != c.ArtifactSHA256 {
		return ErrRejected
	}
	return nil
}

func handleArtifactTransfer(ctx context.Context, state, cache string, r Request, enabled bool) (Observation, error) {
	var zero Observation
	if os.Geteuid() == 0 || r.Validate() != nil || r.Command.Operation != "media.stage" || (r.Action != "run" && r.Action != "query" && r.Action != "cancel" && r.Action != "append" && r.Action != "commit") {
		return zero, ErrRejected
	}
	if ctx.Err() != nil {
		return zero, ErrUnknown
	}
	writes := r.Action == "run" || r.Action == "append" || r.Action == "commit"
	if writes && (!enabled || !time.UnixMicro(r.Command.AuthorizationExpiresAt).After(time.Now())) {
		return zero, ErrRejected
	}
	commandDir := filepath.Join(state, r.Command.CommandID)
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
	commandFD, err := agentDirectory(commandDir)
	if err != nil {
		return zero, ErrUnknown
	}
	defer unix.Close(commandFD)
	lock, err := agentFile(commandFD, "run.lock", true)
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
		o = Observation{Command: r.Command, State: "accepted", Output: map[string]any{}}
		if writeObservation(commandDir, o) != nil {
			return zero, ErrUnknown
		}
		o.State = "running"
		o.StartedAt = time.Now().UTC()
		if writeObservation(commandDir, o) != nil {
			return zero, ErrUnknown
		}
	}
	root, err := agentDirectory(cache)
	if err != nil {
		if r.Action == "run" {
			return artifactTerminal(commandDir, o, 0, false, "artifact_cache_unavailable")
		}
		return zero, ErrUnknown
	}
	defer unix.Close(root)
	global, err := agentFile(root, ".keel-artifacts.lock", true)
	if err != nil {
		return zero, ErrUnknown
	}
	defer unix.Close(global)
	if unix.Flock(global, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return zero, ErrUnknown
	}
	defer unix.Flock(global, unix.LOCK_UN)
	fd, err := artifactDirectory(root, r.Command.ArtifactSHA256)
	created := false
	if errors.Is(err, unix.ENOENT) && r.Action == "run" {
		if reserveArtifact(root, r.Command) != nil {
			return artifactTerminal(commandDir, o, 0, false, "artifact_cache_rejected")
		}
		if unix.Mkdirat(root, r.Command.ArtifactSHA256, 0700) != nil || unix.Fsync(root) != nil {
			return zero, ErrUnknown
		}
		fd, err = artifactDirectory(root, r.Command.ArtifactSHA256)
		created = true
	}
	if err != nil {
		return zero, ErrUnknown
	}
	defer unix.Close(fd)
	var owner artifactOwner
	if created {
		owner = artifactOwner{Version: artifactOwnerVersion, SHA256: r.Command.ArtifactSHA256, Size: r.Command.ArtifactSize, MediaID: r.Command.ArtifactMediaID, ObjectVersion: r.Command.ArtifactObjectVersion, State: "partial", CommandID: r.Command.CommandID, FencingToken: r.Command.FencingToken, ExpiresAt: r.Command.AuthorizationExpiresAt}
		if writeArtifactOwner(fd, owner) != nil {
			return zero, ErrUnknown
		}
		data, err := unix.Openat(fd, "content", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
		if err != nil {
			return zero, ErrUnknown
		}
		err = unix.Fsync(data)
		unix.Close(data)
		if err != nil || unix.Fsync(fd) != nil {
			return zero, ErrUnknown
		}
	} else {
		owner, err = readArtifactOwner(fd, r.Command.ArtifactSHA256)
		if err != nil {
			return zero, ErrUnknown
		}
	}
	if owner.Size != r.Command.ArtifactSize {
		return zero, ErrRejected
	}
	file, err := openCacheFile(fd, "content", owner.Size)
	if err != nil {
		return zero, ErrUnknown
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.Size() != owner.Offset {
		return zero, ErrUnknown
	}
	work := ctx
	var cancel context.CancelFunc
	if writes {
		work, cancel = context.WithDeadline(ctx, time.UnixMicro(r.Command.AuthorizationExpiresAt))
		defer cancel()
	}
	if r.Action == "run" && owner.State == "verified" {
		if err := verifyArtifactFile(work, file, r.Command); err != nil {
			if errors.Is(err, ErrRejected) {
				return artifactTerminal(commandDir, o, owner.Offset, false, "artifact_digest_mismatch")
			}
			return zero, ErrUnknown
		}
		o, err = artifactTerminal(commandDir, o, owner.Offset, true, "artifact_verified")
		return o, err
	}
	if owner.CommandID != r.Command.CommandID || owner.FencingToken != r.Command.FencingToken {
		if r.Action != "run" || r.Command.FencingToken <= owner.FencingToken || owner.State != "paused" && time.UnixMicro(owner.ExpiresAt).After(time.Now()) {
			return artifactTerminal(commandDir, o, 0, false, "artifact_transfer_busy")
		}
		owner.CommandID, owner.FencingToken, owner.ExpiresAt, owner.State = r.Command.CommandID, r.Command.FencingToken, r.Command.AuthorizationExpiresAt, "partial"
		if writeArtifactOwner(fd, owner) != nil {
			return zero, ErrUnknown
		}
	}
	if owner.State == "verified" {
		return artifactTerminal(commandDir, o, owner.Offset, true, "artifact_verified")
	}
	if r.Action == "cancel" || r.Action == "query" && !time.UnixMicro(r.Command.AuthorizationExpiresAt).After(time.Now()) {
		owner.State = "paused"
		if writeArtifactOwner(fd, owner) != nil {
			return zero, ErrUnknown
		}
		return artifactTerminal(commandDir, o, owner.Offset, false, "artifact_checkpointed")
	}
	if r.Action == "append" {
		if owner.State != "partial" || work.Err() != nil {
			return zero, ErrRejected
		}
		if r.ChunkOffset < owner.Offset {
			if r.ChunkOffset+int64(len(r.Chunk)) > owner.Offset {
				return zero, ErrRejected
			}
			previous := make([]byte, len(r.Chunk))
			if _, err = file.ReadAt(previous, r.ChunkOffset); err != nil || !bytes.Equal(previous, r.Chunk) {
				return zero, ErrRejected
			}
		} else {
			if r.ChunkOffset != owner.Offset {
				return zero, ErrRejected
			}
			raw, err := unix.Openat(fd, "content", unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
			if err != nil {
				return zero, ErrUnknown
			}
			var st unix.Stat_t
			if unix.Fstat(raw, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || st.Mode&0077 != 0 || st.Size != owner.Offset {
				unix.Close(raw)
				return zero, ErrRejected
			}
			f := os.NewFile(uintptr(raw), "content")
			n, err := f.WriteAt(r.Chunk, r.ChunkOffset)
			if err == nil && n != len(r.Chunk) {
				err = io.ErrShortWrite
			}
			if err == nil {
				err = f.Sync()
			}
			f.Close()
			if err != nil {
				return zero, ErrUnknown
			}
			owner.Offset += int64(len(r.Chunk))
			if writeArtifactOwner(fd, owner) != nil {
				return zero, ErrUnknown
			}
		}
	}
	if r.Action == "commit" {
		if owner.State != "partial" || owner.Offset != owner.Size {
			return zero, ErrRejected
		}
		err = verifyArtifactFile(work, file, r.Command)
		if err != nil {
			if errors.Is(err, ErrRejected) {
				owner.State = "paused"
				if writeArtifactOwner(fd, owner) != nil {
					return zero, ErrUnknown
				}
				return artifactTerminal(commandDir, o, owner.Offset, false, "artifact_digest_mismatch")
			}
			return zero, ErrUnknown
		}
		if work.Err() != nil {
			return zero, ErrUnknown
		}
		if unix.Fchmod(int(file.Fd()), 0400) != nil || file.Sync() != nil {
			return zero, ErrUnknown
		}
		owner.State = "verified"
		owner.VerifiedAt = time.Now().UTC()
		if writeArtifactOwner(fd, owner) != nil {
			return zero, ErrUnknown
		}
		return artifactTerminal(commandDir, o, owner.Offset, true, "artifact_verified")
	}
	o.Output = artifactOutput(r.Command, owner.Offset, false, false, "artifact_receiving")
	if writeObservation(commandDir, o) != nil {
		return zero, ErrUnknown
	}
	return o, nil
}
