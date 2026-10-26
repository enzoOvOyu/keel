package taskrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
)

// Explicit recovery uses the exact run lock, without opening the content cache,
// renewing authority, moving a fence or claiming any previously accepted work.
func settleArtifactNonstart(ctx context.Context, state string, r Request) (Observation, error) {
	var zero Observation
	if os.Geteuid() == 0 || r.Validate() != nil || r.Action != "settle" || r.Command.Operation != "media.stage" && r.Command.Operation != "media.fetch" || ctx.Err() != nil || time.UnixMicro(r.Command.AuthorizationExpiresAt).After(time.Now()) {
		return zero, ErrRejected
	}
	root, err := agentDirectory(state)
	if err != nil {
		return zero, ErrRejected
	}
	defer unix.Close(root)
	if err = unix.Mkdirat(root, r.Command.CommandID, 0700); err != nil && !errors.Is(err, unix.EEXIST) {
		return zero, ErrRejected
	}
	if unix.Fsync(root) != nil {
		return zero, ErrUnknown
	}
	path := filepath.Join(state, r.Command.CommandID)
	dir, err := agentDirectory(path)
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
	o, err := readObservation(filepath.Join(path, "receipt.json"))
	if err == nil {
		return o, o.Validate(r.Command)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return zero, ErrUnknown
	}
	if ctx.Err() != nil || time.UnixMicro(r.Command.AuthorizationExpiresAt).After(time.Now()) {
		return zero, ErrRejected
	}
	now := time.Now().UTC()
	o = Observation{Command: r.Command, State: "failed", StartedAt: now, FinishedAt: now}
	if r.Command.Operation == "media.fetch" {
		o.Output = fetchOutput(r.Command, 0, false, true, "artifact_authorization_expired_before_start")
	} else {
		o.Output = artifactOutput(r.Command, 0, false, true, "artifact_authorization_expired_before_start")
	}
	o.Output["nonstart_proof"] = true
	if writeObservation(path, o) != nil {
		return zero, ErrUnknown
	}
	return o, o.Validate(r.Command)
}
