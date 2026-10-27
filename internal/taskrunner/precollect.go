package taskrunner

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"

	"github.com/enzoOvOyu/keel/internal/precollect"
	"golang.org/x/sys/unix"
)

func validatePrecollectObservation(o Observation) error {
	if o.Output["stopped"] != true {
		return ErrRejected
	}
	if o.State != "success" {
		if o.Output["reason"] != "precollect_capture_failed" {
			return ErrRejected
		}
		return nil
	}
	digest, ok := o.Output["precollect_sha256"].(string)
	size, valid := scriptOutputInteger(o.Output["precollect_size"])
	if !ok || !scriptDigestPattern.MatchString(digest) || !valid || size < 1 || size > precollect.MaxBundleBytes || o.Output["precollect_version"] != precollect.Version || o.Output["reason"] != "precollect_observed" {
		return ErrRejected
	}
	return nil
}
func capturePrecollect(ctx context.Context, dir string) (map[string]any, string, error) {
	b, e := precollect.Collect(ctx)
	if e != nil {
		return map[string]any{"stopped": true}, "precollect_capture_failed", nil
	}
	raw, e := json.Marshal(b)
	if e != nil || len(raw) > precollect.MaxBundleBytes {
		return map[string]any{"stopped": true}, "precollect_capture_failed", nil
	}
	file, e := os.OpenFile(filepath.Join(dir, "precollect.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return nil, "", ErrUnknown
	}
	_, e = file.Write(raw)
	if e == nil {
		e = file.Sync()
	}
	closeErr := file.Close()
	if e != nil || closeErr != nil {
		return nil, "", ErrUnknown
	}
	parent, e := os.Open(dir)
	if e != nil {
		return nil, "", ErrUnknown
	}
	e = parent.Sync()
	parent.Close()
	if e != nil {
		return nil, "", ErrUnknown
	}
	return map[string]any{"stopped": true, "precollect_version": precollect.Version, "precollect_sha256": precollect.Hash(raw), "precollect_size": len(raw)}, "precollect_observed", nil
}
func readPrecollectChunk(ctx context.Context, dir string, r Request) (Observation, error) {
	var zero Observation
	if ctx.Err() != nil || r.Command.Operation != "host.precollect" || r.Action != "read" {
		return zero, ErrRejected
	}
	receipt, e := readObservation(filepath.Join(dir, "receipt.json"))
	if e != nil || receipt.Validate(r.Command) != nil || receipt.State != "success" || receipt.Output["precollect_sha256"] != r.ChunkSHA256 {
		return zero, ErrRejected
	}
	size, ok := scriptOutputInteger(receipt.Output["precollect_size"])
	if !ok || r.ChunkOffset >= size {
		return zero, ErrRejected
	}
	fd, e := unix.Open(filepath.Join(dir, "precollect.json"), unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return zero, ErrUnknown
	}
	file := os.NewFile(uintptr(fd), "precollect.json")
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() != size {
		return zero, ErrRejected
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Geteuid()) {
		return zero, ErrRejected
	}
	// Per-chunk digest is checked in addition to the controller's whole-object
	// digest. A changing or truncated owned file cannot become accepted evidence.
	chunk := make([]byte, min(int64(precollect.ChunkBytes), size-r.ChunkOffset))
	if _, e = io.ReadFull(io.NewSectionReader(file, r.ChunkOffset, int64(len(chunk))), chunk); e != nil {
		return zero, ErrUnknown
	}
	return Observation{Command: r.Command, State: "accepted", Output: map[string]any{"precollect_chunk_offset": r.ChunkOffset, "precollect_size": size, "precollect_sha256": r.ChunkSHA256, "chunk_sha256": precollect.Hash(chunk), "chunk": base64.StdEncoding.EncodeToString(chunk)}}, nil
}
