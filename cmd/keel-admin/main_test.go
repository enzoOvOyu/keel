package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSSHCredentialCLIRejectsUnrelatedFlagsBeforeDatabaseAccess(t *testing.T) {
	t.Setenv("KEEL_DATABASE_URL", "")
	for _, args := range [][]string{
		{"ssh-key-list", "--project", "11111111111111111111111111111111", "--output", "/tmp/not-created"},
		{"credential-master-keygen", "--project", "11111111111111111111111111111111"},
		{"ssh-key-revoke", "unexpected"},
		{"credential-master-inspect", "--project", "11111111111111111111111111111111"},
		{"credential-master-rewrap", "--output", "/tmp/not-created"},
	} {
		if err := run(args); err == nil {
			t.Fatal("unrelated credential CLI arguments accepted")
		}
	}
}

func TestPasswordFileBoundary(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "password")
	password := "not-a-real-credential-for-test"
	if e := os.WriteFile(secret, []byte(password+"\n"), 0600); e != nil {
		t.Fatal(e)
	}
	got, e := readPassword(secret)
	if e != nil || got != password {
		t.Fatal("owner-only password file could not be read")
	}
	if e = os.Chmod(secret, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e = readPassword(secret); e == nil {
		t.Fatal("world-readable password file accepted")
	}
	if e = os.Chmod(secret, 0600); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(dir, "link")
	if e = os.Symlink(secret, link); e != nil {
		t.Fatal(e)
	}
	if _, e = readPassword(link); e == nil {
		t.Fatal("symlink password file accepted")
	}
	if _, e = readPassword(dir); e == nil {
		t.Fatal("directory accepted as password file")
	}
}
