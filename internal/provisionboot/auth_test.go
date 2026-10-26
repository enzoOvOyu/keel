package provisionboot

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestBootProofRejectsChangedBindingAndReplay(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	installation, boot := strings.Repeat("1", 32), strings.Repeat("2", 32)
	payload := []byte(`{"report":"read-only hardware evidence"}`)
	proof, err := SignBootProof(private, installation, boot, 2, "preflight", payload)
	if err != nil {
		t.Fatal(err)
	}
	if err = VerifyBootProof(public, installation, boot, 2, "preflight", payload, proof); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*BootProof){
		"installation": func(p *BootProof) { p.InstallationID = strings.Repeat("3", 32) },
		"boot":         func(p *BootProof) { p.BootID = strings.Repeat("3", 32) },
		"sequence":     func(p *BootProof) { p.Sequence++ },
		"operation":    func(p *BootProof) { p.Operation = "write-permit" },
		"digest":       func(p *BootProof) { p.PayloadSHA256 = strings.Repeat("0", 64) },
		"signature": func(p *BootProof) {
			p.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
		},
		"signature_newline": func(p *BootProof) { p.Signature += "\n" },
		"schema":            func(p *BootProof) { p.Schema = "keel.agent-boot-proof/v1" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := proof
			change(&changed)
			if VerifyBootProof(public, installation, boot, 2, "preflight", payload, changed) == nil {
				t.Fatal("changed proof accepted")
			}
		})
	}
	if VerifyBootProof(public, installation, boot, 3, "preflight", payload, proof) == nil {
		t.Fatal("old sequence replayed")
	}
	if VerifyBootProof(public, installation, boot, 2, "preflight", append(payload, ' '), proof) == nil {
		t.Fatal("changed exact payload accepted")
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if VerifyBootProof(other, installation, boot, 2, "preflight", payload, proof) == nil {
		t.Fatal("other boot key accepted")
	}
}

func TestBootProofEnrollmentCanonicalKeyAndBounds(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	e := BootEnrollment{InstallationID: strings.Repeat("1", 32), BootID: strings.Repeat("2", 32), PublicKey: base64.StdEncoding.EncodeToString(public)}
	key, err := e.Key()
	if err != nil || !key.Equal(public) {
		t.Fatal("canonical key rejected", err)
	}
	raw, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	p, err := SignBootProof(private, e.InstallationID, e.BootID, 1, "enroll", raw)
	if err != nil || VerifyBootProof(key, e.InstallationID, e.BootID, 1, "enroll", raw, p) != nil {
		t.Fatal("enrollment proof rejected", err)
	}
	for _, value := range []string{e.PublicKey + "\n", base64.StdEncoding.EncodeToString(make([]byte, 31)), "not-a-key"} {
		e.PublicKey = value
		if _, err = e.Key(); err == nil {
			t.Fatal("invalid enrollment key accepted")
		}
	}
	for _, sequence := range []int64{0, 4097} {
		if _, err = SignBootProof(private, e.InstallationID, e.BootID, sequence, "preflight", raw); err == nil {
			t.Fatal("unbounded sequence accepted")
		}
	}
	if _, err = SignBootProof(private, e.InstallationID, e.BootID, 2, "arbitrary-command", raw); err == nil {
		t.Fatal("unknown operation accepted")
	}
	if _, err = SignBootProof(private, e.InstallationID, e.BootID, 2, "preflight", nil); err == nil {
		t.Fatal("empty payload accepted")
	}
}
