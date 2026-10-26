//go:build linux

package provisionwriter

import (
	"fmt"
	"os"

	"github.com/enzoOvOyu/keel/internal/provisionstorage"
	"golang.org/x/sys/unix"
)

type loopDevice struct {
	file      *os.File
	backing   unix.Stat_t
	partition provisionstorage.Partition
	sector    int
	number    int
}

func newLoop(disk *os.File, partition provisionstorage.Partition, sector int) (*loopDevice, error) {
	if requireInitramfs() != nil || disk == nil || partition.StartBytes < provisionstorage.MiB || partition.SizeBytes < provisionstorage.MiB || (sector != 512 && sector != 4096) {
		return nil, ErrDenied
	}
	var backing unix.Stat_t
	if unix.Fstat(int(disk.Fd()), &backing) != nil || backing.Mode&unix.S_IFMT != unix.S_IFBLK {
		return nil, ErrDenied
	}
	control, err := unix.Open("/dev/loop-control", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrDenied
	}
	defer unix.Close(control)
	var cs unix.Stat_t
	if unix.Fstat(control, &cs) != nil || cs.Mode&unix.S_IFMT != unix.S_IFCHR || unix.Major(uint64(cs.Rdev)) != 10 || unix.Minor(uint64(cs.Rdev)) != 237 {
		return nil, ErrDenied
	}
	number, err := unix.IoctlRetInt(control, unix.LOOP_CTL_GET_FREE)
	if err != nil || number < 0 || number > 1<<20 {
		return nil, ErrDenied
	}
	fd, err := unix.Open(fmt.Sprintf("/dev/loop%d", number), unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrDenied
	}
	file := os.NewFile(uintptr(fd), "bounded-install-partition")
	failed := func() (*loopDevice, error) { _ = file.Close(); return nil, ErrDenied }
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFBLK || unix.Major(uint64(stat.Rdev)) != 7 || unix.Minor(uint64(stat.Rdev)) != uint32(number) {
		return failed()
	}
	config := unix.LoopConfig{Fd: uint32(disk.Fd()), Size: uint32(sector), Info: unix.LoopInfo64{Offset: uint64(partition.StartBytes), Sizelimit: uint64(partition.SizeBytes), Flags: unix.LO_FLAGS_AUTOCLEAR}}
	if unix.IoctlLoopConfigure(fd, &config) != nil {
		return failed()
	}
	l := &loopDevice{file: file, backing: backing, partition: partition, sector: sector, number: number}
	if l.check() != nil {
		_ = l.Close()
		return nil, ErrDenied
	}
	return l, nil
}

func (l *loopDevice) check() error {
	if l == nil || l.file == nil {
		return ErrDenied
	}
	fd := int(l.file.Fd())
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFBLK || unix.Major(uint64(st.Rdev)) != 7 || unix.Minor(uint64(st.Rdev)) != uint32(l.number) {
		return ErrDenied
	}
	info, err := unix.IoctlLoopGetStatus64(fd)
	if err != nil || info.Rdevice != uint64(l.backing.Rdev) || info.Device != uint64(l.backing.Dev) || info.Inode != l.backing.Ino || info.Offset != uint64(l.partition.StartBytes) || info.Sizelimit != uint64(l.partition.SizeBytes) || info.Flags != unix.LO_FLAGS_AUTOCLEAR || info.Encrypt_type != 0 || info.Encrypt_key_size != 0 {
		return ErrDenied
	}
	size, err := ioctlUint64(fd, unix.BLKGETSIZE64)
	if err != nil || size != uint64(l.partition.SizeBytes) {
		return ErrDenied
	}
	sector, err := unix.IoctlGetInt(fd, unix.BLKSSZGET)
	if err != nil || sector != l.sector {
		return ErrDenied
	}
	return nil
}

func (l *loopDevice) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	// Only a mapping whose complete ownership still matches may be detached.
	// A replaced mapping is retained as unknown instead of touching it.
	if l.check() != nil {
		return ErrDenied
	}
	if unix.IoctlSetInt(int(l.file.Fd()), unix.LOOP_CLR_FD, 0) != nil {
		return ErrDenied
	}
	err := l.file.Close()
	l.file = nil
	return err
}
