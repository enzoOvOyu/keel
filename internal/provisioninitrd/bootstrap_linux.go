//go:build linux

package provisioninitrd

import (
	"bytes"
	"io"
	"os"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"golang.org/x/sys/unix"
)

// BootstrapPath is a reserved trusted out-of-band handoff in runtime tmpfs.
// The immutable/public initrd never contains a token. No public endpoint can
// populate this file; absence keeps the existing unregistered read-only phase.
const BootstrapPath = "/run/keel-bootstrap.json"

func LoadBootstrap() (*provisionboot.Bootstrap, error) {
	if RequireInitramfs() != nil {
		return nil, ErrUnavailable
	}
	fd, err := unix.Open(BootstrapPath, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err == unix.ENOENT {
		return nil, nil
	}
	if err != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), BootstrapPath)
	defer f.Close()
	var stat unix.Stat_t
	var fs unix.Statfs_t
	if unix.Fstat(fd, &stat) != nil || unix.Fstatfs(fd, &fs) != nil || fs.Type != unix.TMPFS_MAGIC || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Mode&0077 != 0 || stat.Nlink != 1 || stat.Size < 1 || stat.Size > 64<<10 {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || int64(len(raw)) != stat.Size {
		return nil, ErrUnavailable
	}
	defer clear(raw)
	var bootstrap provisionboot.Bootstrap
	if provisionboot.DecodeCallback(bytes.TrimSuffix(raw, []byte{'\n'}), &bootstrap) != nil {
		return nil, ErrUnavailable
	}
	return &bootstrap, nil
}
