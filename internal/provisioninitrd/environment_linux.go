//go:build linux

package provisioninitrd

import (
	"os"

	"golang.org/x/sys/unix"
)

// RequireInitramfs is checked before privileged runtime adapters. Ordinary host
// processes and container overlay roots cannot activate boot-time networking or
// hardware helpers. Image approval remains a separate control-plane decision.
func RequireInitramfs() error {
	if os.Getpid() != 1 || os.Geteuid() != 0 {
		return ErrUnavailable
	}
	var root unix.Statfs_t
	if unix.Statfs("/", &root) != nil || (root.Type != unix.RAMFS_MAGIC && root.Type != unix.TMPFS_MAGIC) {
		return ErrUnavailable
	}
	return nil
}
