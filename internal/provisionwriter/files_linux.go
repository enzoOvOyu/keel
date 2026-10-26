//go:build linux

package provisionwriter

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

func validOwnedPath(path string) bool {
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\x00") {
		return false
	}
	for _, part := range strings.Split(path, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func openMountRoot(path string) (*os.File, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrDenied
	}
	f := os.NewFile(uintptr(fd), "installed-filesystem-root")
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 {
		f.Close()
		return nil, ErrDenied
	}
	return f, nil
}

func ownedOpen(root *os.File, path string, flags int, mode uint32) (int, error) {
	if root == nil || !validOwnedPath(path) {
		return -1, ErrDenied
	}
	return unix.Openat2(int(root.Fd()), path, &unix.OpenHow{Flags: uint64(flags | unix.O_CLOEXEC | unix.O_NOFOLLOW), Mode: uint64(mode), Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
}

func openOwnedRead(root *os.File, path string, limit int64) (*os.File, error) {
	fd, err := ownedOpen(root, path, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, ErrDenied
	}
	f := os.NewFile(uintptr(fd), "installed-owned-file")
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Nlink != 1 || stat.Mode&0022 != 0 || stat.Size < 0 || stat.Size > limit {
		f.Close()
		return nil, ErrDenied
	}
	return f, nil
}

func mkdirOwned(root *os.File, path string) (int, error) {
	if root == nil || !validOwnedPath(path) {
		return -1, ErrDenied
	}
	parent, err := unix.FcntlInt(root.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return -1, ErrDenied
	}
	for _, part := range strings.Split(path, "/") {
		if e := unix.Mkdirat(parent, part, 0755); e != nil && e != unix.EEXIST {
			unix.Close(parent)
			return -1, ErrDenied
		}
		fd, e := unix.Openat2(parent, part, &unix.OpenHow{Flags: unix.O_RDONLY | unix.O_DIRECTORY | unix.O_NOFOLLOW | unix.O_CLOEXEC, Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV})
		unix.Close(parent)
		if e != nil {
			return -1, ErrDenied
		}
		var stat unix.Stat_t
		if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != 0 || stat.Mode&0022 != 0 {
			unix.Close(fd)
			return -1, ErrDenied
		}
		parent = fd
	}
	return parent, nil
}

func writeOwned(ctx context.Context, root *os.File, path string, reader io.Reader, size int64, mode uint32, replace bool) error {
	if ctx.Err() != nil || !validOwnedPath(path) || size < 0 || mode&^0777 != 0 {
		return ErrDenied
	}
	parts := strings.Split(path, "/")
	name := parts[len(parts)-1]
	var parent int
	var err error
	if len(parts) > 1 {
		parent, err = mkdirOwned(root, strings.Join(parts[:len(parts)-1], "/"))
	} else {
		parent, err = unix.FcntlInt(root.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	}
	if err != nil {
		return ErrDenied
	}
	defer unix.Close(parent)
	var prior unix.Stat_t
	err = unix.Fstatat(parent, name, &prior, unix.AT_SYMLINK_NOFOLLOW)
	if err == nil {
		if !replace || prior.Mode&unix.S_IFMT != unix.S_IFREG || prior.Uid != 0 || prior.Nlink != 1 {
			return ErrDenied
		}
	} else if err != unix.ENOENT {
		return ErrDenied
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return ErrDenied
	}
	temporary := ".keel-owned-" + hex.EncodeToString(nonce[:])
	fd, err := unix.Openat(parent, temporary, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, mode)
	if err != nil {
		return ErrDenied
	}
	f := os.NewFile(uintptr(fd), "installed-owned-output")
	defer f.Close()
	defer unix.Unlinkat(parent, temporary, 0)
	buffer := make([]byte, 64<<10)
	var count int64
	for count < size {
		if ctx.Err() != nil {
			return ErrDenied
		}
		nBytes := int64(len(buffer))
		if nBytes > size-count {
			nBytes = size - count
		}
		n, e := io.ReadFull(reader, buffer[:nBytes])
		if e != nil || n != int(nBytes) {
			return ErrDenied
		}
		if written, e := f.Write(buffer[:n]); e != nil || written != n {
			return ErrDenied
		}
		count += int64(n)
	}
	var extra [1]byte
	if n, e := reader.Read(extra[:]); n != 0 || e != io.EOF {
		return ErrDenied
	}
	if ctx.Err() != nil || f.Chmod(os.FileMode(mode)) != nil || f.Sync() != nil {
		return ErrDenied
	}
	flags := uint(unix.RENAME_NOREPLACE)
	if replace {
		flags = 0
	}
	if unix.Renameat2(parent, temporary, parent, name, flags) != nil || unix.Fsync(parent) != nil || ctx.Err() != nil {
		return ErrDenied
	}
	return nil
}

func emptyDirectoryOrAbsent(root *os.File, path string) bool {
	fd, err := ownedOpen(root, path, unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err == unix.ENOENT {
		return true
	}
	if err != nil {
		return false
	}
	f := os.NewFile(uintptr(fd), "empty-cloud-state")
	defer f.Close()
	names, err := f.Readdirnames(1)
	return len(names) == 0 && err == io.EOF
}

func absentOwned(root *os.File, path string) bool {
	fd, err := ownedOpen(root, path, unix.O_PATH, 0)
	if err == nil {
		unix.Close(fd)
		return false
	}
	return err == unix.ENOENT
}

func symlinkOwned(root *os.File, path, target string) error {
	if !validOwnedPath(path) || target != "../keel-firstboot.service" {
		return ErrDenied
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ErrDenied
	}
	fd, err := mkdirOwned(root, strings.Join(parts[:len(parts)-1], "/"))
	if err != nil {
		return ErrDenied
	}
	defer unix.Close(fd)
	if unix.Symlinkat(target, fd, parts[len(parts)-1]) != nil || unix.Fsync(fd) != nil {
		return ErrDenied
	}
	return nil
}

func checkOwnedSymlink(root *os.File, path, target string) error {
	if !validOwnedPath(path) {
		return ErrDenied
	}
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return ErrDenied
	}
	fd, err := ownedOpen(root, strings.Join(parts[:len(parts)-1], "/"), unix.O_RDONLY|unix.O_DIRECTORY, 0)
	if err != nil {
		return ErrDenied
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	name := parts[len(parts)-1]
	if unix.Fstatat(fd, name, &st, unix.AT_SYMLINK_NOFOLLOW) != nil || st.Mode&unix.S_IFMT != unix.S_IFLNK || st.Uid != 0 {
		return ErrDenied
	}
	buf := make([]byte, 256)
	n, err := unix.Readlinkat(fd, name, buf)
	if err != nil || string(buf[:n]) != target {
		return ErrDenied
	}
	return nil
}
