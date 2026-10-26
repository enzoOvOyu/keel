//go:build linux

package provisioninitrd

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
)

// LinuxHelpers is only for the root PID1 of an accepted installation image.
// Tests substitute the process boundary, never run host hardware collectors.
type LinuxHelpers struct{}

func (LinuxHelpers) Check(ctx context.Context, plan mediaboot.Plan, arch string) error {
	if RequireInitramfs() != nil || arch != runtime.GOARCH || plan.ValidateInstallable() != nil {
		return ErrUnavailable
	}
	count := 0
	for _, artifact := range plan.Artifacts {
		path := ""
		if artifact.Role == "preflight" {
			path = "/" + provisionimage.PreflightPath
		}
		if artifact.Role == "ssh-helper" {
			path = "/" + provisionimage.SSHHelperPath
		}
		if path == "" {
			continue
		}
		if _, err := readHelper(ctx, path, artifact, arch); err != nil {
			return ErrUnavailable
		}
		count++
	}
	if count != 2 {
		return ErrUnavailable
	}
	return nil
}
func readHelper(ctx context.Context, path string, artifact mediaboot.Artifact, arch string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	file := os.NewFile(uintptr(fd), "approved-helper")
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() != artifact.Size || info.Size() < 1 || info.Size() > provisionimage.MaxBinaryBytes || info.Mode().Perm()&0222 != 0 || info.Mode().Perm()&0111 == 0 {
		return nil, ErrUnavailable
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(file, provisionimage.MaxBinaryBytes+1))
	if err != nil || int64(len(raw)) != artifact.Size || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	if provisionimage.VerifyStaticBinary(ctx, provisionimage.Input{MediaID: artifact.MediaID, SHA256: artifact.SHA256, Size: artifact.Size, File: bytes.NewReader(raw)}, arch) != nil {
		return nil, ErrUnavailable
	}
	return raw, nil
}

// sealedFile copies the verified bytes into an immutable memfd. Both the binary
// and specification are inherited as fixed descriptors: no path race, shell,
// arbitrary argv or inherited controller environment reaches the child.
func sealedFile(name string, raw []byte, executable bool) (*os.File, error) {
	flags := unix.MFD_CLOEXEC | unix.MFD_ALLOW_SEALING
	if executable {
		flags |= unix.MFD_EXEC
	} else {
		flags |= unix.MFD_NOEXEC_SEAL
	}
	fd, err := unix.MemfdCreate(name, flags)
	if err != nil {
		return nil, ErrUnavailable
	}
	file := os.NewFile(uintptr(fd), name)
	failed := func() (*os.File, error) { file.Close(); return nil, ErrUnavailable }
	if _, err = file.Write(raw); err != nil {
		return failed()
	}
	mode := os.FileMode(0400)
	if executable {
		mode = 0500
	}
	if err = file.Chmod(mode); err != nil {
		return failed()
	}
	if _, err = unix.FcntlInt(file.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return failed()
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return failed()
	}
	return file, nil
}
func (LinuxHelpers) Run(ctx context.Context, rawSpec []byte, artifact mediaboot.Artifact, arch string) ([]byte, int, error) {
	if RequireInitramfs() != nil || arch != runtime.GOARCH || artifact.Role != "preflight" || len(rawSpec) < 1 || len(rawSpec) > 64<<10 {
		return nil, -1, ErrUnavailable
	}
	raw, err := readHelper(ctx, "/"+provisionimage.PreflightPath, artifact, arch)
	if err != nil {
		return nil, -1, err
	}
	binary, err := sealedFile("keel-preflight", raw, true)
	if err != nil {
		return nil, -1, err
	}
	defer binary.Close()
	spec, err := sealedFile("keel-preflight-spec", rawSpec, false)
	if err != nil {
		return nil, -1, err
	}
	defer spec.Close()
	command := exec.CommandContext(ctx, "/proc/self/fd/3", "--spec-file", "/proc/self/fd/4")
	command.Args[0] = "keel-preflight"
	command.ExtraFiles = []*os.File{binary, spec}
	command.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	command.Stderr = io.Discard
	command.WaitDelay = 2 * time.Second
	var output boundedReport
	command.Stdout = &output
	err = command.Run()
	if ctx.Err() != nil || output.overflow {
		return nil, -1, ErrUnavailable
	}
	exit := 0
	if err != nil {
		var status *exec.ExitError
		if !errors.As(err, &status) {
			return nil, -1, ErrUnavailable
		}
		exit = status.ExitCode()
	}
	if exit != 0 && exit != 1 {
		return nil, -1, ErrUnavailable
	}
	return output.Bytes(), exit, nil
}

type boundedReport struct {
	bytes.Buffer
	overflow bool
}

func (w *boundedReport) Write(raw []byte) (int, error) {
	if w.overflow || w.Len()+len(raw) > 2<<20 {
		w.overflow = true
		return 0, ErrUnavailable
	}
	return w.Buffer.Write(raw)
}
