package taskrunner_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"golang.org/x/crypto/ssh"
)

func installCommand(t *testing.T) taskrunner.TaskCommand {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	c := command()
	c.Operation = "trust.install"
	c.PublicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(key))) + " keel-ctrl-" + projectID + "-test-v1\n"
	c.AuthorizationExpiresAt = time.Now().Add(time.Minute).UnixMicro()
	return c
}

func TestApprovedKeyAppendPreservesCustomerBytesAndDurableOwnership(t *testing.T) {
	c := installCommand(t)
	state, dir := t.TempDir(), t.TempDir()
	if err := os.Chmod(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "authorized_keys")
	customer := []byte("# customer configuration without final newline")
	if err := os.WriteFile(file, customer, 0600); err != nil {
		t.Fatal(err)
	}
	customerWriter, err := os.OpenFile(file, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer customerWriter.Close()
	w := taskrunner.Wrapper{StateDirectory: state, AuthorizedKeysFile: file}
	first, err := w.Handle(context.Background(), taskrunner.Request{Action: "run", Command: c})
	if err != nil || first.State != "success" || first.Output["key_installed"] != true {
		t.Fatal("append failed", first, err)
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, customer) || strings.Count(string(raw), strings.TrimSpace(c.PublicKey)) != 1 || !strings.Contains(string(raw), `restrict,command="/usr/local/libexec/keel-ssh-runner" `) {
		t.Fatal("customer bytes or restricted key line changed")
	}
	for _, action := range []string{"query", "run"} {
		again, err := w.Handle(context.Background(), taskrunner.Request{Action: action, Command: c})
		a, _ := json.Marshal(first)
		b, _ := json.Marshal(again)
		if err != nil || !bytes.Equal(a, b) {
			t.Fatal("persisted install receipt changed", err)
		}
	}
	after, _ := os.ReadFile(file)
	if !bytes.Equal(after, raw) {
		t.Fatal("duplicate dispatch appended again")
	}
	if _, err = customerWriter.WriteString("# customer append through existing descriptor\n"); err != nil {
		t.Fatal(err)
	}
	after, err = os.ReadFile(file)
	if err != nil || !bytes.Contains(after, []byte("# customer append through existing descriptor")) {
		t.Fatal("installer replaced customer inode and lost their concurrent writer", err)
	}
	ownership, err := os.ReadFile(filepath.Join(dir, ".keel-key-ownership.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ownership), c.ProjectID) || !strings.Contains(string(ownership), c.CommandID) {
		t.Fatal("installation ownership missing")
	}
	other := c
	other.CommandID = strings.Repeat("a", 32)
	other.AttemptID = strings.Repeat("b", 32)
	other.MachineID = strings.Repeat("c", 32)
	o, err := w.Handle(context.Background(), taskrunner.Request{Action: "run", Command: other})
	if err != nil || o.State != "failed" {
		t.Fatal("another target adopted the ownership record", err)
	}
}

func TestKeyInstallRejectsCustomerAdoptionSymlinksAndUnsafePaths(t *testing.T) {
	for _, kind := range []string{"existing_customer_key", "symlink", "hardlink", "directory_mode", "wrong_basename", "expired"} {
		t.Run(kind, func(t *testing.T) {
			c := installCommand(t)
			state, dir := t.TempDir(), t.TempDir()
			if err := os.Chmod(state, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(dir, "authorized_keys")
			outside := filepath.Join(t.TempDir(), "customer")
			if err := os.WriteFile(outside, []byte("customer sentinel"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "existing_customer_key":
				if err := os.WriteFile(file, []byte(c.PublicKey), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(outside, file); err != nil {
					t.Fatal(err)
				}
			case "hardlink":
				if err := os.Link(outside, file); err != nil {
					t.Fatal(err)
				}
			case "directory_mode":
				if err := os.Chmod(dir, 0755); err != nil {
					t.Fatal(err)
				}
			case "wrong_basename":
				file = filepath.Join(dir, "customer.txt")
			case "expired":
				c.AuthorizationExpiresAt = time.Now().Add(-time.Second).UnixMicro()
			}
			o, err := (taskrunner.Wrapper{StateDirectory: state, AuthorizedKeysFile: file}).Handle(context.Background(), taskrunner.Request{Action: "run", Command: c})
			if err == nil && o.State != "failed" {
				t.Fatal("unsafe installation succeeded")
			}
			b, err := os.ReadFile(outside)
			if err != nil || string(b) != "customer sentinel" {
				t.Fatal("customer target changed", err)
			}
		})
	}
	c := installCommand(t)
	for _, bad := range []string{`command="sh" ` + c.PublicKey, c.PublicKey + c.PublicKey, strings.Replace(c.PublicKey, projectID, machineID, 1)} {
		c.PublicKey = bad
		if c.Validate() == nil {
			t.Fatal("unsafe key payload accepted")
		}
	}
}
