package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/auditevidence"
	"golang.org/x/sys/unix"
)

func TestAuditEvidenceOfflineCLIAndFileBoundaries(t *testing.T) {
	t.Setenv("KEEL_DATABASE_URL", "")
	dir := t.TempDir()
	private, public := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	if _, err := auditevidence.GenerateKeyFiles(private, public); err != nil {
		t.Fatal(err)
	}
	signer, err := auditevidence.LoadSigner(private)
	if err != nil {
		t.Fatal(err)
	}
	defer signer.Close()
	var index bytes.Buffer
	writer := auditevidence.NewIndexWriter(&index)
	if err = writer.Add(auditevidence.Entry{ID: strings.Repeat("1", 32), SHA256: strings.Repeat("2", 64)}); err != nil {
		t.Fatal(err)
	}
	envelope, err := writer.Seal(signer, strings.Repeat("3", 32), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	seal, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	indexPath, sealPath := filepath.Join(dir, "index.jsonl"), filepath.Join(dir, "seal.json")
	if err = os.WriteFile(indexPath, index.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(sealPath, seal, 0600); err != nil {
		t.Fatal(err)
	}
	if err = runAuditEvidence("audit-evidence-verify", []string{"--public-key-file", public, "--index-file", indexPath, "--seal-file", sealPath}); err != nil {
		t.Fatal("offline verify required a database:", err)
	}
	if _, err = exportAuditFiles(context.Background(), nil, nil, dir); err == nil {
		t.Fatal("existing evidence directory was reused")
	}
	if raw, e := os.ReadFile(indexPath); e != nil || !bytes.Equal(raw, index.Bytes()) {
		t.Fatal("existing evidence was modified")
	}
	link := filepath.Join(dir, "index-link")
	if err = os.Symlink(indexPath, link); err != nil {
		t.Fatal(err)
	}
	if file, e := openAuditIndex(link); e == nil {
		file.Close()
		t.Fatal("index symlink accepted")
	}
	fifo := filepath.Join(dir, "index-fifo")
	if err = unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if file, e := openAuditIndex(fifo); e == nil {
		file.Close()
		t.Fatal("index FIFO accepted")
	}
	for _, test := range []struct {
		command string
		args    []string
	}{
		{"audit-evidence-export", []string{"--public-key-file", public}},
		{"audit-evidence-verify", []string{"--private-key-file", private}},
		{"audit-evidence-compare", []string{"--output-directory", dir}},
		{"audit-signing-keygen", []string{"--index-file", indexPath}},
	} {
		if e := runAuditEvidence(test.command, test.args); e == nil {
			t.Fatal("unrelated audit flag accepted")
		}
	}
}
