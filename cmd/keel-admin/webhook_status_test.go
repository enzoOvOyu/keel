package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWebhookCLIBoundariesAndIndependentKeygen(t *testing.T) {
	t.Setenv("KEEL_DATABASE_URL", "")
	for _, args := range [][]string{
		{"webhook-delivery-status"},
		{"webhook-delivery-status", "--project", "invalid"},
		{"webhook-delivery-status", "--project", "11111111111111111111111111111111", "--limit", "101"},
		{"webhook-delivery-status", "--project", "11111111111111111111111111111111", "--offset", "-1"},
		{"webhook-delivery-status", "--output", "/tmp/not-created"},
		{"webhook-signing-keygen", "--project", "11111111111111111111111111111111"},
		{"webhook-signing-keygen", "unexpected"},
		{"webhook-signing-keygen", "--output", "relative-key"},
	} {
		if err := run(args); err == nil {
			t.Fatal("invalid notification CLI accepted", args)
		}
	}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "webhook-key")
	if err := run([]string{"webhook-signing-keygen", "--output", path}); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil || len(original) != 32 {
		t.Fatal("invalid independent key", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("unprotected independent key", err)
	}
	if err = run([]string{"webhook-signing-keygen", "--output", path}); err == nil {
		t.Fatal("existing signing key overwritten")
	}
	again, _ := os.ReadFile(path)
	if !bytes.Equal(original, again) {
		t.Fatal("existing key changed")
	}
}
