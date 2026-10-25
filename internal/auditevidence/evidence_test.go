package auditevidence

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func evidenceFixture(t *testing.T) (*Signer, []byte, []byte, string) {
	t.Helper()
	dir := t.TempDir()
	private, public := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	if _, err := GenerateKeyFiles(private, public); err != nil {
		t.Fatal(err)
	}
	signer, err := LoadSigner(private)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(signer.Close)
	var index bytes.Buffer
	writer := NewIndexWriter(&index)
	for _, id := range []string{strings.Repeat("1", 32), strings.Repeat("2", 32)} {
		if err = writer.Add(Entry{ID: id, SHA256: strings.Repeat("a", 64)}); err != nil {
			t.Fatal(err)
		}
	}
	seal, err := writer.Seal(signer, strings.Repeat("3", 32), time.Unix(1000000, 0))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(seal)
	if err != nil {
		t.Fatal(err)
	}
	return signer, index.Bytes(), raw, public
}

func TestAuditEvidenceIndependentTrustAndTampering(t *testing.T) {
	signer, index, seal, public := evidenceFixture(t)
	trusted, err := LoadPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	header, err := Verify(bytes.NewReader(index), seal, trusted, nil)
	if err != nil || header.Rows != 2 || header.IndexBytes != int64(len(index)) {
		t.Fatalf("evidence verification: %+v %v", header, err)
	}
	_, _, _, otherPublic := evidenceFixture(t)
	other, err := LoadPublicKey(otherPublic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(bytes.NewReader(index), seal, other, nil); err == nil {
		t.Fatal("evidence trusted an unrelated public key")
	}
	for name, mutate := range map[string]func([]byte) []byte{
		"changed row": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(strings.Repeat("a", 64)), []byte(strings.Repeat("b", 64)), 1)
		},
		"missing row":   func(raw []byte) []byte { return raw[:bytes.IndexByte(raw, '\n')+1] },
		"duplicate row": func(raw []byte) []byte { return append(raw, raw[:bytes.IndexByte(raw, '\n')+1]...) },
		"alias key":     func(raw []byte) []byte { return bytes.Replace(raw, []byte(`"id":`), []byte(`"ID":`), 1) },
		"duplicate key": func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(`"id":`), []byte(`"id":"`+strings.Repeat("0", 32)+`","id":`), 1)
		},
		"line endings": func(raw []byte) []byte { return bytes.ReplaceAll(raw, []byte{'\n'}, []byte{'\r', '\n'}) },
		"truncated":    func(raw []byte) []byte { return raw[:len(raw)-1] },
	} {
		t.Run(name, func(t *testing.T) {
			copy := append([]byte{}, index...)
			if _, e := Verify(bytes.NewReader(mutate(copy)), seal, trusted, nil); e == nil {
				t.Fatal("changed index verified")
			}
		})
	}
	for name, changed := range map[string][]byte{
		"unsigned count":  bytes.Replace(seal, []byte(`"rows":2`), []byte(`"rows":1`), 1),
		"alias field":     bytes.Replace(seal, []byte(`"snapshot_id":`), []byte(`"Snapshot_id":`), 1),
		"duplicate field": bytes.Replace(seal, []byte(`"algorithm":`), []byte(`"algorithm":"none","algorithm":`), 1),
		"second document": append(append([]byte{}, seal...), []byte(`{}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := Verify(bytes.NewReader(index), changed, trusted, nil); e == nil {
				t.Fatal("changed seal verified")
			}
		})
	}
	signer.Close()
	if _, err = NewIndexWriter(&bytes.Buffer{}).Seal(signer, strings.Repeat("4", 32), time.Now()); err == nil {
		t.Fatal("closed signer reused")
	}
}
