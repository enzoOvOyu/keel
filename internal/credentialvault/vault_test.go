package credentialvault

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func testVault(t *testing.T) (*Vault, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "master.key")
	if err := GenerateMaster(path); err != nil {
		t.Fatal(err)
	}
	v, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return v, path
}

func TestCiphertextBindsProjectVersionPublicIdentityAndExpiry(t *testing.T) {
	v, _ := testVault(t)
	i := Identity{ProjectID: "11111111111111111111111111111111", Ref: "keel-ctrl-project", Version: "v1", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	i, sealed, err := v.Generate(i)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := v.Open(i, sealed)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("PRIVATE KEY")) || bytes.Contains(sealed, []byte(i.PublicKey)) {
		t.Fatal("ciphertext contains key representation")
	}
	signature, err := signer.Sign(rand.Reader, []byte("proof"))
	if err != nil {
		t.Fatal(err)
	}
	if err = signer.PublicKey().Verify([]byte("proof"), signature); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*Identity){
		func(x *Identity) { x.ProjectID = "22222222222222222222222222222222" },
		func(x *Identity) { x.Ref = "other" }, func(x *Identity) { x.Version = "v2" },
		func(x *Identity) { x.PublicKey += "tampered" }, func(x *Identity) { x.ExpiresAt = x.ExpiresAt.Add(time.Second) },
		func(x *Identity) { x.ExpiresAt = time.Now().Add(-time.Second) },
	}
	for index, change := range mutations {
		changed := i
		change(&changed)
		if _, err = v.Open(changed, sealed); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("identity mutation %d accepted", index)
		}
	}
	corrupt := bytes.Clone(sealed)
	corrupt[len(corrupt)-1] ^= 1
	if _, err = v.Open(i, corrupt); !errors.Is(err, ErrUnavailable) {
		t.Fatal("corrupt ciphertext accepted")
	}
	other, _ := testVault(t)
	if _, err = other.Open(i, sealed); !errors.Is(err, ErrUnavailable) {
		t.Fatal("wrong master key accepted")
	}
	for _, short := range [][]byte{nil, {1, 2, 3}, make([]byte, 2048)} {
		if _, err = v.Open(i, short); err == nil {
			t.Fatal("invalid size accepted")
		}
	}
}

func TestMasterKeyFileSecurityAndExclusiveCreation(t *testing.T) {
	_, path := testVault(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = GenerateMaster(path); !errors.Is(err, ErrUnavailable) {
		t.Fatal("existing master key replaced")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("existing bytes changed")
	}
	if err = os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path); err == nil {
		t.Fatal("readable master accepted")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := path + ".link"
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(link); err == nil {
		t.Fatal("symlink accepted")
	}
	fifo := path + ".fifo"
	if err = unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(fifo); err == nil {
		t.Fatal("fifo accepted")
	}
	for _, bad := range []string{filepath.Dir(path), "relative.key"} {
		if _, err = Load(bad); err == nil {
			t.Fatal("invalid path accepted")
		}
	}
	if err = os.WriteFile(path, make([]byte, 31), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = Load(path); err == nil {
		t.Fatal("short master accepted")
	}
	if v, err := Load(""); err != nil || v != nil {
		t.Fatal("disabled provider changed")
	}
}
