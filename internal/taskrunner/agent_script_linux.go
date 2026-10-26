package taskrunner

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// This fixed operator-installed binary owns subreaper/process-tree semantics.
// Never enable those semantics in the long-lived, concurrent Agent itself.
const agentScriptHelper = "/usr/local/libexec/keel-ssh-runner"

func (a Agent) runScriptHelper(ctx context.Context, r Request) (Observation, error) {
	var zero Observation
	fd, err := unix.Open(agentScriptHelper, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return zero, ErrUnknown
	}
	f := os.NewFile(uintptr(fd), "keel-script-helper")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&0022 != 0 || st.Mode&0111 == 0 {
		return zero, ErrRejected
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw) > 14336 {
		return zero, ErrRejected
	}
	cmd := exec.CommandContext(ctx, "/proc/self/fd/3")
	cmd.ExtraFiles = []*os.File{f}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "KEEL_SSH_RECEIPT_DIRECTORY=" + a.StateDirectory, "KEEL_SSH_SCRIPTS_ENABLED=true"}
	cmd.Stdin = bytes.NewReader(raw)
	var out limitedOutput
	cmd.Stdout = &out
	// Give the dedicated helper its bounded descendant cleanup period before
	// forcing termination. A lost helper remains unknown, never proof of stop.
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 3 * time.Second
	if cmd.Run() != nil {
		return zero, ErrUnknown
	}
	d := json.NewDecoder(bytes.NewReader(out.Bytes()))
	d.DisallowUnknownFields()
	var observed Observation
	var extra any
	if d.Decode(&observed) != nil || d.Decode(&extra) != io.EOF || observed.Validate(r.Command) != nil {
		return zero, ErrUnknown
	}
	return observed, nil
}
