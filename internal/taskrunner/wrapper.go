package taskrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

// Wrapper is a per-invocation helper, not a persistent agent. StateDirectory is
// provisioned by the operator for keel-ctrl, mode0700. Never use a shared /tmp.
type Wrapper struct {
	ScriptMediaCacheEnabled bool
	CacheInventoryEnabled   bool
	StateDirectory          string
	AuthorizedKeysFile      string
	// Set only by a dedicated helper process after PrepareScriptProcess.
	ScriptProcessIsolation bool
}

func privateDirectory(path string) error {
	s, e := os.Lstat(path)
	if e != nil || !s.IsDir() || s.Mode().Perm()&0077 != 0 {
		return ErrRejected
	}
	// os.FileInfo.Sys is syscall.Stat_t, use stat through the opened directory
	// instead to enforce the actual effective UID without following its final link.
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if e != nil {
		return ErrRejected
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) {
		return ErrRejected
	}
	return nil
}

func readObservation(path string) (Observation, error) {
	var o Observation
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return o, e
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	s, e := f.Stat()
	if e != nil || !s.Mode().IsRegular() || s.Mode().Perm()&0077 != 0 || s.Size() > 16384 {
		return o, ErrRejected
	}
	d := json.NewDecoder(io.LimitReader(f, 16385))
	d.DisallowUnknownFields()
	if e = d.Decode(&o); e != nil {
		return o, ErrRejected
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return o, ErrRejected
	}
	return o, nil
}

func writeObservation(dir string, o Observation) error {
	f, e := os.CreateTemp(dir, ".receipt-")
	if e != nil {
		return e
	}
	name := f.Name()
	defer os.Remove(name)
	e = json.NewEncoder(f).Encode(o)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return e
	}
	if e = os.Rename(name, filepath.Join(dir, "receipt.json")); e != nil {
		return e
	}
	d, e := os.Open(dir)
	if e != nil {
		return e
	}
	defer d.Close()
	return d.Sync()
}

func (w Wrapper) Handle(ctx context.Context, r Request) (Observation, error) {
	var zero Observation
	if os.Geteuid() == 0 {
		return zero, ErrRejected
	}
	if r.Command.Validate() != nil || (r.Action != "run" && r.Action != "query" && r.Action != "cancel") || privateDirectory(w.StateDirectory) != nil {
		return zero, ErrRejected
	}
	if r.Command.Operation == "script.run" {
		if r.Action == "run" && (!w.ScriptProcessIsolation || validateScriptBody(r.Command, r.Script) != nil) {
			return zero, ErrRejected
		}
	} else if len(r.Script) > 0 {
		return zero, ErrRejected
	}
	if r.Action != "run" && len(r.Script) > 0 {
		return zero, ErrRejected
	}
	if r.Action == "run" && r.Command.Operation == "host.cache.inventory" && !w.CacheInventoryEnabled {
		return zero, ErrRejected
	}
	dir := filepath.Join(w.StateDirectory, r.Command.CommandID)
	if r.Action == "run" {
		if e := os.Mkdir(dir, 0700); e != nil && !errors.Is(e, os.ErrExist) {
			return zero, ErrRejected
		}
		parent, e := os.Open(w.StateDirectory)
		if e != nil {
			return zero, ErrUnknown
		}
		e = parent.Sync()
		parent.Close()
		if e != nil {
			return zero, ErrUnknown
		}
	}
	if privateDirectory(dir) != nil {
		return zero, ErrUnknown
	}
	path := filepath.Join(dir, "receipt.json")
	if r.Action != "run" {
		o, e := readObservation(path)
		if e != nil {
			return zero, ErrUnknown
		}
		if o.Validate(r.Command) != nil {
			return zero, ErrRejected
		}
		if r.Action == "cancel" && !o.Terminal() {
			f, e := os.OpenFile(filepath.Join(dir, "cancel"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil && !errors.Is(e, os.ErrExist) {
				return zero, ErrUnknown
			}
			if e == nil {
				e = f.Sync()
				f.Close()
				if e != nil {
					return zero, ErrUnknown
				}
			}
		}
		return o, nil
	}
	fd, e := unix.Open(filepath.Join(dir, "run.lock"), unix.O_WRONLY|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if e != nil {
		return zero, ErrRejected
	}
	defer unix.Close(fd)
	var lockStat unix.Stat_t
	if unix.Fstat(fd, &lockStat) != nil || lockStat.Mode&unix.S_IFMT != unix.S_IFREG || lockStat.Uid != uint32(os.Geteuid()) || lockStat.Mode&0077 != 0 {
		return zero, ErrRejected
	}
	if e = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); e != nil {
		o, e := readObservation(path)
		if e != nil {
			return zero, ErrUnknown
		}
		return o, o.Validate(r.Command)
	}
	defer unix.Flock(fd, unix.LOCK_UN)
	if o, e := readObservation(path); e == nil {
		return o, o.Validate(r.Command)
	} else if !errors.Is(e, os.ErrNotExist) {
		return zero, ErrUnknown
	}
	o := Observation{Command: r.Command, State: "accepted", Output: map[string]any{}}
	if e = writeObservation(dir, o); e != nil {
		return zero, ErrUnknown
	}
	// accepted is durable before work; a crash at any later boundary is never a
	// license to execute the command again, even if the old process is gone.
	o.State = "running"
	o.StartedAt = time.Now().UTC().Truncate(time.Microsecond)
	if e = writeObservation(dir, o); e != nil {
		return zero, ErrUnknown
	}
	work, cancel := context.WithTimeout(ctx, time.Duration(r.Command.TimeoutMS)*time.Millisecond)
	defer cancel()
	if r.Command.Operation == "trust.install" || r.Command.Operation == "script.run" {
		var stop context.CancelFunc
		work, stop = context.WithDeadline(work, time.UnixMicro(r.Command.AuthorizationExpiresAt))
		defer stop()
	}
	result, reason := "success", "completed"
	if r.Command.Operation == "host.wait" {
		done := time.NewTimer(time.Duration(r.Command.WaitMS) * time.Millisecond)
		defer done.Stop()
		poll := time.NewTicker(25 * time.Millisecond)
		defer poll.Stop()
	wait:
		for {
			select {
			case <-done.C:
				break wait
			case <-work.Done():
				result, reason = "failed", "deadline_or_process_stop_confirmed"
				break wait
			case <-poll.C:
				if _, e := os.Lstat(filepath.Join(dir, "cancel")); e == nil {
					result, reason = "cancelled", "cooperative_stop_confirmed"
					break wait
				}
			}
		}
	} else if r.Command.Operation == "script.run" {
		var scriptErr error
		o.Output, result, reason, scriptErr = executeScriptProcess(work, dir, r.Command, r.Script, w.ScriptMediaCacheEnabled)
		if scriptErr != nil {
			return zero, ErrUnknown
		}
	} else if r.Command.Operation == "host.cache.inventory" {
		o.Output, reason = readMediaCache(work, MediaCacheDirectory, r.Command.CacheCursor, r.Command.CacheManifestSHA256)
		if o.Output == nil {
			o.Output = map[string]any{}
			result = "failed"
		}
	} else if r.Command.Operation == "trust.install" {
		fingerprint, err := installDomainKey(work, w.AuthorizedKeysFile, r.Command)
		if errors.Is(err, ErrUnknown) {
			return zero, ErrUnknown
		}
		if err != nil {
			result, reason = "failed", "key_installation_rejected"
		} else {
			o.Output = map[string]any{"key_installed": true, "public_key_fingerprint": fingerprint}
		}
	} else {
		host, e := os.Hostname()
		if e != nil {
			result, reason = "failed", "identity_unavailable"
		} else {
			o.Output = map[string]any{"hostname": host, "os": runtime.GOOS, "arch": runtime.GOARCH}
		}
	}
	o.State = result
	o.FinishedAt = time.Now().UTC().Truncate(time.Microsecond)
	o.Output["reason"] = reason
	if e = writeObservation(dir, o); e != nil {
		return zero, ErrUnknown
	}
	return o, nil
}

func (w Wrapper) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	body, err := io.ReadAll(io.LimitReader(in, 16385))
	if err != nil || len(body) > 16384 {
		return ErrRejected
	}
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	var r Request
	if d.Decode(&r) != nil {
		return ErrRejected
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return ErrRejected
	}
	o, e := w.Handle(ctx, r)
	if e != nil {
		return e
	}
	return json.NewEncoder(out).Encode(o)
}
