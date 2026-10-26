package taskrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"golang.org/x/sys/unix"
)

// Agent shares the SSH wrapper's durable command/receipt protocol, but admits
// bounded host operations under one enrolled machine identity. Scripts run in a
// separate operator-installed helper, never inside the persistent Agent. Its own
// global process lock is supplemental to the control plane's resource leases.
type Agent struct {
	ArtifactTransfersEnabled bool
	StateDirectory           string
	ProjectID                string
	MachineID                string
	ScriptsEnabled           bool
	MediaCacheEnabled        bool
	CacheInventoryEnabled    bool
}

// PrepareAgentState creates only the local tasks child of an already protected
// Agent state directory. It never follows an ancestor link or repairs modes.
func PrepareAgentState(state string) (string, error) {
	if os.Geteuid() == 0 {
		return "", ErrRejected
	}
	fd, e := agentDirectory(state)
	if e != nil {
		return "", e
	}
	defer unix.Close(fd)
	if e = unix.Mkdirat(fd, "tasks", 0700); e != nil && !errors.Is(e, unix.EEXIST) {
		return "", ErrRejected
	}
	dir := filepath.Join(state, "tasks")
	child, e := agentDirectory(dir)
	if e != nil {
		return "", e
	}
	unix.Close(child)
	if unix.Fsync(fd) != nil {
		return "", ErrUnknown
	}
	return dir, nil
}

type agentFence struct {
	Command TaskCommand `json:"command"`
}

// agentDirectory rejects links in every ancestor, then checks the opened leaf.
func agentDirectory(path string) (int, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return -1, ErrRejected
	}
	fd, e := unix.Open("/", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if e != nil {
		return -1, ErrRejected
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, err := unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return -1, ErrRejected
		}
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		unix.Close(fd)
		return -1, ErrRejected
	}
	return fd, nil
}
func agentFile(fd int, name string, create bool) (int, error) {
	flags := unix.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	if create {
		flags = unix.O_RDWR | unix.O_CREAT | unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	}
	f, e := unix.Openat(fd, name, flags, 0600)
	if e != nil {
		return -1, e
	}
	var st unix.Stat_t
	if unix.Fstat(f, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || st.Mode&0077 != 0 || st.Size > 16384 {
		unix.Close(f)
		return -1, ErrRejected
	}
	return f, nil
}
func agentCommandFiles(dir string) error {
	fd, e := agentDirectory(dir)
	if e != nil {
		return e
	}
	defer unix.Close(fd)
	for _, name := range []string{"receipt.json", "run.lock", "cancel"} {
		f, err := agentFile(fd, name, false)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return ErrRejected
		}
		unix.Close(f)
	}
	return nil
}
func readAgentFence(path string) (agentFence, error) {
	var f agentFence
	raw, e := agentpki.ReadFile(path, 4096, true)
	if e != nil {
		if _, stat := os.Lstat(path); errors.Is(stat, os.ErrNotExist) {
			return f, nil
		}
		return f, ErrUnknown
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || f.Command.Validate() != nil {
		return f, ErrUnknown
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return f, ErrUnknown
	}
	return f, nil
}
func writeAgentFence(dir string, fence agentFence) error {
	f, e := os.CreateTemp(dir, ".agent-fence-")
	if e != nil {
		return ErrUnknown
	}
	name := f.Name()
	defer os.Remove(name)
	e = json.NewEncoder(f).Encode(fence)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return ErrUnknown
	}
	if os.Rename(name, filepath.Join(dir, ".agent-fence.json")) != nil {
		return ErrUnknown
	}
	d, e := os.Open(dir)
	if e != nil {
		return ErrUnknown
	}
	defer d.Close()
	if d.Sync() != nil {
		return ErrUnknown
	}
	return nil
}
func (a Agent) Handle(ctx context.Context, r Request) (Observation, error) {
	var zero Observation
	if os.Geteuid() == 0 || !idPattern.MatchString(a.ProjectID) || !idPattern.MatchString(a.MachineID) || r.Validate() != nil || r.Command.ProjectID != a.ProjectID || r.Command.MachineID != a.MachineID || (r.Command.Operation != "host.identity" && r.Command.Operation != "host.wait" && r.Command.Operation != "script.run" && r.Command.Operation != "host.cache.inventory" && r.Command.Operation != "host.artifact.inventory" && r.Command.Operation != "media.stage") {
		return zero, ErrRejected
	}
	if r.Command.Operation == "media.stage" && (r.Action == "run" || r.Action == "append" || r.Action == "commit") && !a.ArtifactTransfersEnabled {
		return zero, ErrRejected
	}
	if r.Action == "run" && r.Command.Operation == "script.run" && !a.ScriptsEnabled {
		return zero, ErrRejected
	}
	if r.Action == "run" && r.Command.Operation == "host.cache.inventory" && !a.CacheInventoryEnabled {
		return zero, ErrRejected
	}
	if r.Action == "run" && r.Command.Operation == "host.artifact.inventory" && !a.ArtifactTransfersEnabled {
		return zero, ErrRejected
	}
	fd, e := agentDirectory(a.StateDirectory)
	if e != nil {
		return zero, e
	}
	defer unix.Close(fd)
	if r.Action == "cache" {
		return probeScriptCache(ctx, MediaCacheDirectory, a.ScriptsEnabled && a.MediaCacheEnabled, r.Command)
	}
	dir := filepath.Join(a.StateDirectory, r.Command.CommandID)
	if _, e = os.Lstat(dir); e == nil {
		if e = agentCommandFiles(dir); e != nil {
			return zero, e
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return zero, ErrRejected
	}
	wrapper := Wrapper{StateDirectory: a.StateDirectory, CacheInventoryEnabled: a.CacheInventoryEnabled, ArtifactTransfersEnabled: a.ArtifactTransfersEnabled}
	if r.Action == "append" || r.Action == "commit" {
		lock, err := agentFile(fd, ".agent-run.lock", true)
		if err != nil {
			return zero, ErrRejected
		}
		defer unix.Close(lock)
		if unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB) != nil {
			return zero, ErrUnknown
		}
		defer unix.Flock(lock, unix.LOCK_UN)
		fence, err := readAgentFence(filepath.Join(a.StateDirectory, ".agent-fence.json"))
		if err != nil || fence.Command != r.Command {
			return zero, ErrRejected
		}
		return wrapper.Handle(ctx, r)
	}
	if r.Action != "run" {
		return wrapper.Handle(ctx, r)
	}
	// A retry may return its original durable receipt, but never restart work.
	if _, e = os.Lstat(filepath.Join(dir, "receipt.json")); e == nil {
		return wrapper.Handle(ctx, Request{Action: "query", Command: r.Command})
	}
	lock, e := agentFile(fd, ".agent-run.lock", true)
	if e != nil {
		return zero, ErrRejected
	}
	defer unix.Close(lock)
	if unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return zero, ErrUnknown
	}
	defer unix.Flock(lock, unix.LOCK_UN)
	// Another invocation may have persisted its receipt after the first lookup.
	// Query such proof rather than treating an equal fence as a new admission.
	if _, e = os.Lstat(dir); e == nil {
		if e = agentCommandFiles(dir); e != nil {
			return zero, e
		}
		if _, e = os.Lstat(filepath.Join(dir, "receipt.json")); e == nil {
			return wrapper.Handle(ctx, Request{Action: "query", Command: r.Command})
		}
	}
	fence, e := readAgentFence(filepath.Join(a.StateDirectory, ".agent-fence.json"))
	if e != nil {
		return zero, e
	}
	// A matching persisted fence with missing proof is ambiguous: the previous
	// process may have run and lost its receipt. Never use absence to retry work.
	if fence.Command == r.Command {
		return zero, ErrUnknown
	}
	if fence.Command.FencingToken > r.Command.FencingToken || (fence.Command.FencingToken == r.Command.FencingToken && fence.Command != r.Command) {
		return zero, ErrRejected
	}
	if fence.Command.FencingToken < r.Command.FencingToken {
		if e = writeAgentFence(a.StateDirectory, agentFence{Command: r.Command}); e != nil {
			return zero, e
		}
	}
	// Recheck after taking the cross-process lock; accepted/running receipts from
	// a crashed process remain evidence of unknown work, never a rerun license.
	if _, e = os.Lstat(dir); e == nil {
		if e = agentCommandFiles(dir); e != nil {
			return zero, e
		}
	}
	if r.Command.Operation == "script.run" {
		return a.runScriptHelper(ctx, r)
	}
	return wrapper.Handle(ctx, r)
}
