package agentpki

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

func privateDirectory(path string) (int, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return -1, ErrInvalid
	}
	fd, e := unix.Open("/", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if e != nil {
		return -1, ErrInvalid
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, err := unix.Openat(fd, part, unix.O_DIRECTORY|unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if err != nil {
			return -1, ErrInvalid
		}
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		unix.Close(fd)
		return -1, ErrInvalid
	}
	return fd, nil
}

// LockIdentity serializes local certificate updates without locking task
// receipts or interrupting the existing transport. Never follows a symlink.
func LockIdentity(directory string) (func(), error) {
	dir, e := privateDirectory(directory)
	if e != nil {
		return nil, e
	}
	defer unix.Close(dir)
	fd, e := unix.Openat(dir, ".identity.lock", unix.O_RDWR|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if e != nil {
		return nil, ErrInvalid
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || st.Mode&0077 != 0 || unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		unix.Close(fd)
		return nil, ErrInvalid
	}
	return func() { unix.Flock(fd, unix.LOCK_UN); unix.Close(fd) }, nil
}

// ReplaceIdentity atomically replaces a verified private record, only if its
// bytes and inode still match the caller's snapshot. Callers hold LockIdentity.
// The old identity survives a failed write; a lost response can retry using it.
func ReplaceIdentity(path string, previous, raw []byte) error {
	if len(previous) == 0 || len(previous) > 16384 || len(raw) == 0 || len(raw) > 16384 || filepath.Clean(path) != path {
		return ErrInvalid
	}
	dir, e := privateDirectory(filepath.Dir(path))
	if e != nil {
		return e
	}
	defer unix.Close(dir)
	base := filepath.Base(path)
	fd, e := unix.Openat(dir, base, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return ErrInvalid
	}
	file := os.NewFile(uintptr(fd), "agent-identity")
	var old unix.Stat_t
	if unix.Fstat(fd, &old) != nil || old.Mode&unix.S_IFMT != unix.S_IFREG || old.Uid != uint32(os.Geteuid()) || old.Nlink != 1 || old.Mode&0077 != 0 || old.Size <= 0 || old.Size > 16384 {
		file.Close()
		return ErrInvalid
	}
	current, e := io.ReadAll(io.LimitReader(file, 16385))
	file.Close()
	if e != nil || !bytes.Equal(current, previous) {
		return ErrInvalid
	}
	var suffix [16]byte
	if _, e = rand.Read(suffix[:]); e != nil {
		return ErrInvalid
	}
	tmp := ".identity-" + hex.EncodeToString(suffix[:])
	fd, e = unix.Openat(dir, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return ErrInvalid
	}
	out := os.NewFile(uintptr(fd), "agent-identity")
	defer func() { out.Close(); unix.Unlinkat(dir, tmp, 0) }()
	if _, e = out.Write(raw); e != nil || out.Sync() != nil || out.Close() != nil {
		return ErrInvalid
	}
	var latest unix.Stat_t
	if unix.Fstatat(dir, base, &latest, unix.AT_SYMLINK_NOFOLLOW) != nil || latest.Ino != old.Ino || latest.Dev != old.Dev || latest.Mode&unix.S_IFMT != unix.S_IFREG || latest.Nlink != 1 {
		return ErrInvalid
	}
	if unix.Renameat(dir, tmp, dir, base) != nil || unix.Fsync(dir) != nil {
		return ErrInvalid
	}
	return nil
}
