package mediamanifest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fixture(t testing.TB) (*Signer, ed25519.PublicKey, Payload) {
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
	trusted, err := LoadPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	leaf := Artifact{MediaID: strings.Repeat("1", 32), ScopeKey: "global", Type: "tool", Name: "leaf", Version: "v1", Arch: "noarch", ObjectVersion: "object-v1", ApprovedBy: strings.Repeat("a", 32), ApprovedAt: time.Unix(1000000, 0).UTC(), Dependencies: []Dependency{}}
	root := leaf
	root.MediaID = strings.Repeat("2", 32)
	root.Name = "root"
	root.Arch = "amd64"
	for _, node := range []*Artifact{&leaf, &root} {
		bytes := []byte(node.Name + " bytes")
		hash := sha256.Sum256(bytes)
		node.Size = int64(len(bytes))
		node.SHA256 = hex.EncodeToString(hash[:])
		node.URI = "media://tool/" + node.Name + "/v1"
	}
	root.Dependencies = []Dependency{{MediaID: leaf.MediaID, SHA256: leaf.SHA256}}
	payload := Payload{RootID: root.MediaID, ExportedAt: time.Unix(1000010, 0).UTC(), Artifacts: []Artifact{leaf, root}}
	return signer, trusted, payload
}
func TestSignedManifestTrustAndTampering(t *testing.T) {
	signer, key, payload := fixture(t)
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.MarshalIndent(signed, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Verify(raw, key); err != nil {
		t.Fatal(err)
	}
	_, other, _ := fixture(t)
	if _, err = Verify(raw, other); err == nil {
		t.Fatal("manifest trusted its own key identity")
	}
	for _, mutate := range []func(*Envelope){func(m *Envelope) { m.Payload.Artifacts[0].Version = "v2" }, func(m *Envelope) { m.Payload.Artifacts[1].Dependencies = nil }, func(m *Envelope) { m.Payload.ExportedAt = m.Payload.ExportedAt.Add(time.Hour) }, func(m *Envelope) { m.Algorithm = "none" }, func(m *Envelope) { m.Signature = "" }, func(m *Envelope) { m.Payload.KeyID = strings.Repeat("0", 64) }} {
		var altered Envelope
		json.Unmarshal(raw, &altered)
		mutate(&altered)
		forged, _ := json.Marshal(altered)
		if _, err = Verify(forged, key); err == nil {
			t.Fatal("tampered envelope accepted")
		}
	}
	for _, invalid := range [][]byte{append(append([]byte{}, raw...), []byte("{}")...), []byte(strings.Replace(string(raw), `"algorithm":`, `"unsigned": true, "algorithm":`, 1)), []byte(strings.Repeat(" ", MaxBytes+1))} {
		if _, err = Verify(invalid, key); err == nil {
			t.Fatal("malformed document accepted")
		}
	}
}
func TestSignedManifestRejectsInvalidClosures(t *testing.T) {
	signer, _, original := fixture(t)
	cases := map[string]func(*Payload){
		"duplicate": func(p *Payload) { p.Artifacts = append(p.Artifacts, p.Artifacts[0]) },
		"cycle": func(p *Payload) {
			p.Artifacts[0].Arch = "amd64"
			p.Artifacts[0].Dependencies = []Dependency{{MediaID: p.Artifacts[1].MediaID, SHA256: p.Artifacts[1].SHA256}}
		},
		"missing":          func(p *Payload) { p.Artifacts[1].Dependencies[0].MediaID = strings.Repeat("f", 32) },
		"digest":           func(p *Payload) { p.Artifacts[1].Dependencies[0].SHA256 = strings.Repeat("0", 64) },
		"scope":            func(p *Payload) { p.Artifacts[0].ScopeKey = "project:" + strings.Repeat("b", 32) },
		"architecture":     func(p *Payload) { p.Artifacts[0].Arch = "arm64" },
		"unreachable":      func(p *Payload) { p.Artifacts[1].Dependencies = []Dependency{} },
		"unsorted":         func(p *Payload) { p.Artifacts[0], p.Artifacts[1] = p.Artifacts[1], p.Artifacts[0] },
		"unsafe file":      func(p *Payload) { p.Artifacts[0].MediaID = "../secret" },
		"too large":        func(p *Payload) { p.Artifacts[0].Size = MaxArtifactBytes + 1 },
		"missing approval": func(p *Payload) { p.Artifacts[0].ApprovedBy = "" },
	}
	raw, _ := json.Marshal(original)
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var payload Payload
			json.Unmarshal(raw, &payload)
			mutate(&payload)
			if _, err := signer.Sign(payload); err == nil {
				t.Fatal("invalid closure signed")
			}
		})
	}
}
func TestSignedManifestOfflineBytes(t *testing.T) {
	signer, key, payload := fixture(t)
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(signed)
	verified, err := Verify(raw, key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, node := range verified.Payload.Artifacts {
		if err = os.WriteFile(filepath.Join(dir, node.MediaID+".bin"), []byte(node.Name+" bytes"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = VerifyFiles(verified.Payload, dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, verified.Payload.Artifacts[0].MediaID+".bin")
	os.WriteFile(path, []byte("bad! bytes"), 0600)
	if err = VerifyFiles(verified.Payload, dir); err == nil {
		t.Fatal("corrupt bytes accepted")
	}
	os.Remove(path)
	if err = VerifyFiles(verified.Payload, dir); err == nil {
		t.Fatal("missing bytes accepted")
	}
	target := filepath.Join(dir, "other.bin")
	os.WriteFile(target, []byte("leaf bytes"), 0600)
	os.Symlink(target, path)
	if err = VerifyFiles(verified.Payload, dir); err == nil {
		t.Fatal("symlink artifact accepted")
	}
}
func TestSigningKeyFileProtection(t *testing.T) {
	dir := t.TempDir()
	private, public := filepath.Join(dir, "private.pem"), filepath.Join(dir, "public.pem")
	id, err := GenerateKeyFiles(private, public)
	if err != nil || len(id) != 64 {
		t.Fatalf("key generation %s %v", id, err)
	}
	original, _ := os.ReadFile(private)
	if _, err = GenerateKeyFiles(private, public); err == nil {
		t.Fatal("existing key overwritten")
	}
	after, _ := os.ReadFile(private)
	if string(after) != string(original) {
		t.Fatal("key changed")
	}
	for _, path := range []string{private, public} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe key mode %v %v", info, err)
		}
	}
	os.Chmod(private, 0644)
	if _, err = LoadSigner(private); err == nil {
		t.Fatal("readable private key accepted")
	}
	os.Chmod(private, 0600)
	link := filepath.Join(dir, "link.pem")
	os.Symlink(private, link)
	if _, err = LoadSigner(link); err == nil {
		t.Fatal("key symlink accepted")
	}
	if _, err = GenerateKeyFiles(filepath.Join(dir, "new.pem"), public); err == nil {
		t.Fatal("existing public key overwritten")
	}
	if _, err = os.Stat(filepath.Join(dir, "new.pem")); !os.IsNotExist(err) {
		t.Fatal("failed pair left a private key")
	}
	if signer, err := LoadSigner(""); err != nil || signer != nil {
		t.Fatal("unconfigured signing enabled")
	}
}

func TestSignedManifestRejectsDuplicateKeys(t *testing.T) {
	signer, key, payload := fixture(t)
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	for _, document := range []string{
		strings.Replace(string(raw), `"algorithm":`, `"algorithm":"none","algorithm":`, 1),
		strings.Replace(string(raw), `"version":`, `"version":"forged","version":`, 1),
	} {
		if _, err = Verify([]byte(document), key); err == nil {
			t.Fatal("accepted ambiguous duplicated JSON field")
		}
	}
}

func TestSignedManifestExactFieldNames(t *testing.T) {
	signer, key, payload := fixture(t)
	// Compatibility keys are data: do not fold or reject legitimate case pairs.
	payload.Artifacts[0].Compatibility = json.RawMessage(`{"vendor":{"Version":"a","version":"b"}}`)
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(signed)
	if _, err = Verify(raw, key); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"algorithm", "payload", "signature", "schema", "key_id", "root_id", "exported_at", "artifacts", "media_id", "scope_key", "uri", "type", "name", "version", "arch", "sha256", "size", "object_version", "approved_by", "approved_at", "dependencies", "compatibility"} {
		t.Run(field, func(t *testing.T) {
			altered := strings.Replace(string(raw), `"`+field+`":`, `"`+strings.ToUpper(field)+`":`, 1)
			if _, err := Verify([]byte(altered), key); err == nil {
				t.Fatal("accepted noncanonical schema field")
			}
		})
	}
	// An alternative alias may not coexist with the signed canonical field.
	altered := strings.Replace(string(raw), `"algorithm":`, `"Algorithm":"none","algorithm":`, 1)
	if _, err := Verify([]byte(altered), key); err == nil {
		t.Fatal("accepted aliased duplicate")
	}
	// Invalid UTF-8 must not silently become replacement characters.
	if err := uniqueJSONKeys([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}

func TestSigningRejectsAmbiguousCompatibilityMetadata(t *testing.T) {
	signer, _, payload := fixture(t)
	for _, raw := range []string{`{"source":"forged","source":"real"}`, `{"nested":` + strings.Repeat("[", 65) + `0` + strings.Repeat("]", 65) + `}`} {
		payload.Artifacts[0].Compatibility = json.RawMessage(raw)
		if _, err := signer.Sign(payload); err == nil {
			t.Fatal("signed metadata rejected by the paired strict verifier")
		}
	}
}

// Accepted untrusted JSON must remain valid under the documented typed encoding;
// malformed nesting, alternate field names and duplicate keys must never panic.
func FuzzSignedManifestParser(f *testing.F) {
	signer, key, payload := fixture(f)
	signed, err := signer.Sign(payload)
	if err != nil {
		f.Fatal(err)
	}
	raw, _ := json.Marshal(signed)
	f.Add(raw)
	f.Add([]byte(`{"algorithm":"none","payload":{},"signature":""}`))
	f.Add([]byte(`{"payload":{"artifacts":[{"compatibility":{"nested":[[[]]]}}]}}`))
	f.Fuzz(func(t *testing.T, document []byte) {
		verified, err := Verify(document, key)
		if err != nil {
			return
		}
		encoded, err := json.Marshal(verified)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = Verify(encoded, key); err != nil {
			t.Fatal("accepted document does not round-trip under the signed wire encoding", err)
		}
	})
}
