package taskrunner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	osuser "os/user"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

// Only a public Ed25519 key generated for this project is accepted. Options,
// multiple lines and client-chosen commands are never installation inputs.
func installPublicIdentity(raw, project string) (ssh.PublicKey, error) {
	if len(raw) > 256 || strings.Count(raw, "\n") > 1 || strings.Contains(raw, "\r") {
		return nil, ErrRejected
	}
	key, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(raw))
	if err != nil || key.Type() != ssh.KeyAlgoED25519 || len(options) != 0 || len(rest) != 0 || !strings.HasPrefix(comment, "keel-ctrl-"+project+"-") {
		return nil, ErrRejected
	}
	return key, nil
}

type keyOwnership struct {
	Project string `json:"project_id"`
	Machine string `json:"machine_id"`
	Command string `json:"command_id"`
	Line    string `json:"line"`
}

func readKeyFile(dir int, name string, limit int64) ([]byte, error) {
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrRejected
	}
	f := os.NewFile(uintptr(fd), name)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Mode&0022 != 0 || st.Nlink != 1 || st.Size > limit {
		return nil, ErrRejected
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, ErrRejected
	}
	return b, nil
}

func writeKeyFile(dir int, name string, data []byte) error {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return ErrUnknown
	}
	tmp := ".keel-" + hex.EncodeToString(random[:])
	fd, err := unix.Openat(dir, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return ErrUnknown
	}
	defer unix.Unlinkat(dir, tmp, 0)
	f := os.NewFile(uintptr(fd), tmp)
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrUnknown
	}
	if unix.Renameat(dir, tmp, dir, name) != nil || unix.Fsync(dir) != nil {
		return ErrUnknown
	}
	return nil
}

// Append through the existing inode instead of replacing a customer file with
// an earlier snapshot. Concurrent customer appends cannot be overwritten; an
// ambiguous short write or path replacement is retained as unknown.
func appendAuthorizedKey(dir int, expected []byte, line string) error {
	fd, err := unix.Openat(dir, "authorized_keys", unix.O_RDWR|unix.O_APPEND|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return ErrRejected
	}
	f := os.NewFile(uintptr(fd), "authorized_keys")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Mode&0022 != 0 || st.Nlink != 1 || st.Size > 256*1024 {
		return ErrRejected
	}
	current, err := io.ReadAll(io.LimitReader(f, 256*1024+1))
	if err != nil || !bytes.Equal(current, expected) {
		return ErrRejected
	}
	payload := []byte(line)
	if len(current) > 0 && current[len(current)-1] != '\n' {
		payload = append([]byte{'\n'}, payload...)
	}
	if len(current)+len(payload) > 256*1024 {
		return ErrRejected
	}
	n, err := f.Write(payload)
	if err != nil || n != len(payload) || f.Sync() != nil || unix.Fsync(dir) != nil {
		return ErrUnknown
	}
	var linked unix.Stat_t
	if unix.Fstatat(dir, "authorized_keys", &linked, unix.AT_SYMLINK_NOFOLLOW) != nil || linked.Dev != st.Dev || linked.Ino != st.Ino || linked.Nlink != 1 {
		return ErrUnknown
	}
	return nil
}

// Installation appends one forced-command public key under a directory fd. It
// preserves customer bytes and records only our exact added line. It does not
// create accounts, change passwords, install executables or remove any key.
func installDomainKey(ctx context.Context, path string, c TaskCommand) (string, error) {
	if ctx.Err() != nil {
		return "", ErrRejected
	}
	key, err := installPublicIdentity(c.PublicKey, c.ProjectID)
	if err != nil {
		return "", err
	}
	if path == "" {
		user, err := osuser.Current()
		if err != nil || user.HomeDir == "" {
			return "", ErrRejected
		}
		dir := filepath.Join(user.HomeDir, ".ssh")
		if err = os.Mkdir(dir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return "", ErrRejected
		}
		path = filepath.Join(dir, "authorized_keys")
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Base(path) != "authorized_keys" {
		return "", ErrRejected
	}
	dir, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", ErrRejected
	}
	defer unix.Close(dir)
	var st unix.Stat_t
	if unix.Fstat(dir, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		return "", ErrRejected
	}
	lock, err := unix.Openat(dir, ".keel-key-install.lock", unix.O_WRONLY|unix.O_CREAT|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0600)
	if err != nil {
		return "", ErrRejected
	}
	defer unix.Close(lock)
	if unix.Fstat(lock, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 || st.Nlink != 1 {
		return "", ErrRejected
	}
	if unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB) != nil {
		return "", ErrUnknown
	}
	defer unix.Flock(lock, unix.LOCK_UN)
	existing, err := readKeyFile(dir, "authorized_keys", 256*1024)
	if err != nil {
		return "", err
	}
	raw, err := readKeyFile(dir, ".keel-key-ownership.json", 1024*1024)
	if err != nil {
		return "", err
	}
	owned := map[string]keyOwnership{}
	if len(raw) > 0 && json.Unmarshal(raw, &owned) != nil {
		return "", ErrRejected
	}
	if owned == nil || len(owned) >= 512 {
		return "", ErrRejected
	}
	fingerprint := ssh.FingerprintSHA256(key)
	line := `restrict,command="/usr/local/libexec/keel-ssh-runner" ` + strings.TrimSpace(c.PublicKey) + "\n"
	found := false
	for _, entry := range bytes.Split(existing, []byte{'\n'}) {
		other, _, _, _, parseErr := ssh.ParseAuthorizedKey(entry)
		if parseErr == nil && bytes.Equal(other.Marshal(), key.Marshal()) {
			record, ok := owned[fingerprint]
			if found || !ok || record.Project != c.ProjectID || record.Machine != c.MachineID || record.Line != line || !bytes.Equal(entry, []byte(strings.TrimSuffix(line, "\n"))) {
				return "", ErrRejected
			}
			found = true
		}
	}
	if found {
		return fingerprint, nil
	}
	if prior, ok := owned[fingerprint]; ok && (prior.Project != c.ProjectID || prior.Machine != c.MachineID || prior.Line != line) {
		return "", ErrRejected
	}
	// Persist ownership intent first. A crash can leave an intent or an installed
	// key without terminal proof; neither is promoted to trust automatically.
	owned[fingerprint] = keyOwnership{Project: c.ProjectID, Machine: c.MachineID, Command: c.CommandID, Line: line}
	raw, err = json.Marshal(owned)
	if err != nil {
		return "", ErrRejected
	}
	if ctx.Err() != nil {
		return "", ErrRejected
	}
	if err = writeKeyFile(dir, ".keel-key-ownership.json", raw); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ErrRejected
	}
	if err = appendAuthorizedKey(dir, existing, line); err != nil {
		return "", err
	}
	if ctx.Err() != nil {
		return "", ErrUnknown
	}
	return fingerprint, nil
}
