package taskrunner

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// PrepareScriptProcess applies only to a dedicated, non-root helper process.
// The subreaper adopts daemonized descendants so kill confirmation can wait for
// ECHILD rather than equating a disconnected SSH channel with process death.
func PrepareScriptProcess() error {
	if os.Geteuid() == 0 {
		return ErrRejected
	}
	if unix.Prctl(unix.PR_SET_CHILD_SUBREAPER, 1, 0, 0, 0) != nil || unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil || unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0) != nil {
		return ErrRejected
	}
	_, err := os.ReadDir("/proc/self/task")
	return err
}

type scriptCapture struct {
	prefix   []byte
	digest   string
	size     int64
	complete bool
}

func captureScriptOutput(reader *os.File, overflow chan<- struct{}) <-chan scriptCapture {
	done := make(chan scriptCapture, 1)
	go func() {
		defer reader.Close()
		h := sha256.New()
		c := scriptCapture{}
		buf := make([]byte, 4096)
		for {
			n, err := reader.Read(buf)
			if n > 0 {
				c.size += int64(n)
				h.Write(buf[:n])
				if len(c.prefix) < 512 {
					take := n
					if take > 512-len(c.prefix) {
						take = 512 - len(c.prefix)
					}
					c.prefix = append(c.prefix, buf[:take]...)
				}
				if c.size > 1<<20 {
					select {
					case overflow <- struct{}{}:
					default:
					}
					break
				}
			}
			if err != nil {
				c.complete = err == io.EOF
				break
			}
		}
		c.digest = hex.EncodeToString(h.Sum(nil))
		done <- c
	}()
	return done
}

func scriptChildren() ([]int, error) {
	threads, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	pids := []int{}
	for _, thread := range threads {
		if !thread.IsDir() {
			continue
		}
		f, err := os.Open(filepath.Join("/proc/self/task", thread.Name(), "children"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(f, 65537))
		f.Close()
		if err != nil || len(data) > 65536 {
			return nil, ErrUnknown
		}
		for _, raw := range strings.Fields(string(data)) {
			pid, err := strconv.Atoi(raw)
			if err != nil || pid <= 1 || pid == os.Getpid() {
				return nil, ErrUnknown
			}
			if !seen[pid] {
				seen[pid] = true
				pids = append(pids, pid)
			}
		}
	}
	return pids, nil
}

// executeScriptProcess runs no request-supplied command line, path, arguments or
// environment. Approved script bytes are stdin to a fixed interpreter. Effects
// outside the process tree are not undone by cancellation.
func executeScriptProcess(ctx context.Context, dir string, c TaskCommand, body []byte) (map[string]any, string, string, error) {
	if validateScriptBody(c, body) != nil {
		return nil, "", "", ErrRejected
	}
	if ctx.Err() != nil {
		return map[string]any{"process_tree_stopped": true, "process_started": false}, "failed", "script_authorization_expired_before_start", nil
	}
	workspace := filepath.Join(dir, "workspace")
	if os.Mkdir(workspace, 0700) != nil {
		return map[string]any{"process_tree_stopped": true, "process_started": false}, "failed", "script_workspace_unavailable", nil
	}
	interpreter := "/bin/sh"
	args := []string{"sh", "-s"}
	if c.ScriptInterpreter == "python" {
		interpreter = "/usr/bin/python3"
		args = []string{"python3", "-I", "-"}
	}
	// Pin a regular interpreter descriptor; execute via /proc, never a writable
	// search path or a script shebang. Standard distribution interpreter symlinks
	// are resolved once and the exact opened executable stays pinned through exec.
	actual, err := filepath.EvalSymlinks(interpreter)
	if err != nil {
		return map[string]any{"process_tree_stopped": true, "process_started": false}, "failed", "script_interpreter_unavailable", nil
	}
	fd, err := unix.Open(actual, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return map[string]any{"process_tree_stopped": true, "process_started": false}, "failed", "script_interpreter_unavailable", nil
	}
	executable := os.NewFile(uintptr(fd), actual)
	defer executable.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&0022 != 0 || st.Mode&0111 == 0 {
		return map[string]any{"process_tree_stopped": true, "process_started": false}, "failed", "script_interpreter_unavailable", nil
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, "", "", err
	}
	defer inR.Close()
	defer inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		return nil, "", "", err
	}
	defer outR.Close()
	defer outW.Close()
	errR, errW, err := os.Pipe()
	if err != nil {
		return nil, "", "", err
	}
	defer errR.Close()
	defer errW.Close()
	// No child exists yet; the entire bounded body fits in a separate writer and
	// cannot delay cancellation handling in the supervising goroutine.
	inputDone := make(chan error, 1)
	go func() { _, e := inW.Write(body); inW.Close(); inputDone <- e }()
	if ctx.Err() != nil {
		return map[string]any{"process_tree_stopped": true, "process_started": false}, "failed", "script_authorization_expired_before_start", nil
	}
	process, err := os.StartProcess("/proc/self/fd/3", args, &os.ProcAttr{Dir: workspace, Env: []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "HOME=" + workspace}, Files: []*os.File{inR, outW, errW, executable}, Sys: &syscall.SysProcAttr{Setpgid: true}})
	if err != nil {
		return map[string]any{"process_tree_stopped": true, "process_started": false}, "failed", "script_process_not_started", nil
	}
	pid := process.Pid
	process.Release() // wait4 below is the only reaper in this dedicated helper.
	inR.Close()
	outW.Close()
	errW.Close()
	overflow := make(chan struct{}, 2)
	stdout := captureScriptOutput(outR, overflow)
	stderr := captureScriptOutput(errR, overflow)
	result, reason := "success", "script_completed"
	mainDone, stopping := false, false
	exitCode := -1
	cleanupDeadline := time.Time{}
	stopAll := func(why, state string) {
		if !stopping {
			stopping = true
			reason = why
			result = state
			cleanupDeadline = time.Now().Add(2 * time.Second)
		}
		_ = unix.Kill(-pid, unix.SIGKILL)
	}
	for {
		if ctx.Err() != nil {
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				stopAll("script_deadline", "failed")
			} else {
				stopAll("script_cancelled", "cancelled")
			}
		}
		if _, err := os.Lstat(filepath.Join(dir, "cancel")); err == nil {
			stopAll("script_cancelled", "cancelled")
		} else if !errors.Is(err, os.ErrNotExist) {
			stopAll("script_cancel_state_unavailable", "failed")
		}
		select {
		case <-overflow:
			stopAll("script_output_limit", "failed")
		default:
		}
		if stopping || mainDone {
			children, e := scriptChildren()
			if e != nil {
				return nil, "", "", ErrUnknown
			}
			for _, child := range children {
				_ = unix.Kill(child, unix.SIGKILL)
			}
		}
		empty := false
		for {
			var status unix.WaitStatus
			child, e := unix.Wait4(-1, &status, unix.WNOHANG, nil)
			if errors.Is(e, unix.EINTR) {
				continue
			}
			if errors.Is(e, unix.ECHILD) {
				empty = true
				break
			}
			if e != nil {
				return nil, "", "", ErrUnknown
			}
			if child == 0 {
				break
			}
			if child == pid {
				mainDone = true
				exitCode = status.ExitStatus()
				if status.Signaled() {
					exitCode = 128 + int(status.Signal())
				}
				if exitCode != 0 && !stopping {
					result = "failed"
					reason = "script_nonzero_exit"
				}
				if cleanupDeadline.IsZero() {
					cleanupDeadline = time.Now().Add(2 * time.Second)
				}
			}
		}
		if empty {
			if !mainDone {
				return nil, "", "", ErrUnknown
			}
			break
		}
		if !cleanupDeadline.IsZero() && time.Now().After(cleanupDeadline) {
			return nil, "", "", ErrUnknown
		}
		time.Sleep(10 * time.Millisecond)
	}
	getCapture := func(ch <-chan scriptCapture, reader *os.File) scriptCapture {
		select {
		case capture := <-ch:
			return capture
		case <-time.After(250 * time.Millisecond):
			reader.Close()
			return <-ch
		}
	}
	out, errOut := getCapture(stdout, outR), getCapture(stderr, errR)
	if out.size > 1<<20 || errOut.size > 1<<20 {
		result = "failed"
		reason = "script_output_limit"
	}
	if (!out.complete || !errOut.complete) && result == "success" {
		result = "failed"
		reason = "script_output_incomplete"
	}
	select {
	case inputErr := <-inputDone:
		if inputErr != nil && result == "success" {
			result = "failed"
			reason = "script_input_incomplete"
		}
	case <-time.After(250 * time.Millisecond):
		return nil, "", "", ErrUnknown
	}
	output := map[string]any{"script_media_id": c.ScriptMediaID, "script_object_version": c.ScriptObjectVersion, "script_sha256": c.ScriptSHA256, "script_interpreter": c.ScriptInterpreter, "exit_code": exitCode, "process_tree_stopped": true, "process_started": true, "stdout_prefix_base64": base64.StdEncoding.EncodeToString(out.prefix), "stdout_sha256": out.digest, "stdout_bytes": out.size, "stdout_complete": out.complete, "stderr_prefix_base64": base64.StdEncoding.EncodeToString(errOut.prefix), "stderr_sha256": errOut.digest, "stderr_bytes": errOut.size, "stderr_complete": errOut.complete}
	return output, result, reason, nil
}
