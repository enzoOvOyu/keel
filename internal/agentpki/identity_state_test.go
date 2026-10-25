package agentpki

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestAgentIdentityAtomicState(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "identity.json")
	old, next := []byte(`{"generation":1}`), []byte(`{"generation":2}`)
	if err := WriteNew(path, old); err != nil {
		t.Fatal(err)
	}
	unlock, err := LockIdentity(dir)
	if err != nil {
		t.Fatal(err)
	}
	if other, e := LockIdentity(dir); e == nil {
		other()
		t.Fatal("concurrent identity writer admitted")
	}
	if err = ReplaceIdentity(path, []byte("stale"), next); err == nil {
		t.Fatal("stale identity snapshot replaced")
	}
	if err = ReplaceIdentity(path, old, next); err != nil {
		t.Fatal(err)
	}
	unlock()
	data, err := ReadFile(path, 16384, true)
	if err != nil || string(data) != string(next) {
		t.Fatal("identity replacement was not durable", err)
	}
	if err = ReplaceIdentity(path, old, []byte("stale writer")); err == nil {
		t.Fatal("old writer overwrote new identity")
	}
	if err = os.Link(path, filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if err = ReplaceIdentity(path, next, old); err == nil {
		t.Fatal("hard-linked identity replaced")
	}
	if err = os.Remove(filepath.Join(dir, "alias")); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path, filepath.Join(dir, "original")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(dir, "original"), path); err != nil {
		t.Fatal(err)
	}
	if err = ReplaceIdentity(path, next, old); err == nil {
		t.Fatal("symlink identity replaced")
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = unix.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err = ReplaceIdentity(path, next, old); err == nil {
		t.Fatal("FIFO identity opened")
	}
	if err = os.Remove(filepath.Join(dir, ".identity.lock")); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(dir, "original"), filepath.Join(dir, ".identity.lock")); err != nil {
		t.Fatal(err)
	}
	if release, e := LockIdentity(dir); e == nil {
		release()
		t.Fatal("symlink lock opened")
	}
}
