package credentialvault

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func maintenanceVault(t *testing.T) *Vault {
	t.Helper()
	path := filepath.Join(t.TempDir(), "master")
	if err := GenerateMaster(path); err != nil {
		t.Fatal(err)
	}
	v, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestMasterRewrapPreservesIdentityAndAuthorization(t *testing.T) {
	previous, next := maintenanceVault(t), maintenanceVault(t)
	i, sealed, err := previous.Generate(Identity{ProjectID: strings.Repeat("1", 32), Ref: "project-key", Version: "v1", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	before, err := previous.Open(i, sealed)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := previous.Rewrap(next, i, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(sealed, wrapped) || previous.KeyID() == next.KeyID() || len(next.KeyID()) != 64 {
		t.Fatal("custody key or ciphertext did not change")
	}
	after, err := next.Open(i, wrapped)
	if err != nil || !bytes.Equal(before.PublicKey().Marshal(), after.PublicKey().Marshal()) {
		t.Fatal("rewrap changed project SSH identity")
	}
	if _, err = previous.Open(i, wrapped); err == nil {
		t.Fatal("old master opened newly wrapped key")
	}
	if _, err = next.Open(i, sealed); err == nil {
		t.Fatal("new master opened old ciphertext")
	}
	if _, err = previous.Rewrap(previous, i, sealed); err == nil {
		t.Fatal("same master accepted as rotation")
	}
	for name, mutate := range map[string]func(*Identity){"project": func(i *Identity) { i.ProjectID = strings.Repeat("2", 32) }, "version": func(i *Identity) { i.Version = "v2" }, "expiry": func(i *Identity) { i.ExpiresAt = i.ExpiresAt.Add(time.Second) }, "public": func(i *Identity) { i.PublicKey = strings.Replace(i.PublicKey, "keel-ctrl-", "other-", 1) }} {
		t.Run(name, func(t *testing.T) {
			copy := i
			mutate(&copy)
			if _, e := previous.Rewrap(next, copy, sealed); e == nil {
				t.Fatal("tampered authenticated identity rewrapped")
			}
		})
	}
	tampered := append([]byte{}, sealed...)
	tampered[len(tampered)-1] ^= 1
	if _, err = previous.Rewrap(next, i, tampered); err == nil {
		t.Fatal("corrupt ciphertext rewrapped")
	}
	if _, err = next.Rewrap(previous, i, sealed); err == nil {
		t.Fatal("wrong source master rewrapped key")
	}
	// Historical custody remains verifiable without granting a signer for an
	// expired identity. This synthetic archive is made only inside this unit test.
	der, err := previous.decryptForMaintenance(i, sealed)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(der)
	i.ExpiresAt = time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	nonce := bytes.Repeat([]byte{1}, previous.aead.NonceSize())
	archived := previous.aead.Seal(nonce, nonce, der, associatedData(i))
	if err = previous.CheckSealed(i, archived); err != nil {
		t.Fatal(err)
	}
	archivedNext, err := previous.Rewrap(next, i, archived)
	if err != nil {
		t.Fatal(err)
	}
	if err = next.CheckSealed(i, archivedNext); err != nil {
		t.Fatal(err)
	}
	if _, err = next.Open(i, archivedNext); err == nil {
		t.Fatal("maintenance enabled expired signing")
	}
}
