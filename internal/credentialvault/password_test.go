package credentialvault

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPasswordCiphertextPinsProjectScopeVersionAndExpiry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "device-master")
	if e := GenerateMaster(path); e != nil {
		t.Fatal(e)
	}
	vault, e := Load(path)
	if e != nil {
		t.Fatal(e)
	}
	identity := SecretIdentity{ProjectID: strings.Repeat("a", 32), Ref: "BMC-batch", Version: "v1", Scope: "bmc", ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
	secret := []byte(`{"username":"reader","password":"protected-only-fixture"}`)
	sealed, e := vault.SealSecret(identity, secret)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(sealed, []byte("protected-only-fixture")) {
		t.Fatal("plaintext stored in ciphertext")
	}
	plain, e := vault.OpenSecret(identity, sealed)
	if e != nil || !bytes.Equal(plain, secret) {
		t.Fatal("cannot recover exact scoped secret", e)
	}
	clear(plain)
	for _, kind := range []string{"project", "batch", "version", "scope", "expiry", "expired", "tamper"} {
		t.Run(kind, func(t *testing.T) {
			copy := identity
			cipher := append([]byte(nil), sealed...)
			switch kind {
			case "project":
				copy.ProjectID = strings.Repeat("b", 32)
			case "batch":
				copy.Ref = "other-batch"
			case "version":
				copy.Version = "v2"
			case "scope":
				copy.Scope = "switch"
			case "expiry":
				copy.ExpiresAt = copy.ExpiresAt.Add(time.Second)
			case "expired":
				copy.ExpiresAt = time.Now().Add(-time.Second)
			case "tamper":
				cipher[len(cipher)-1] ^= 1
			}
			if raw, e := vault.OpenSecret(copy, cipher); e == nil {
				clear(raw)
				t.Fatal("accepted changed identity/ciphertext")
			}
		})
	}
	sshIdentity := Identity{ProjectID: identity.ProjectID, Ref: identity.Ref, Version: identity.Version, ExpiresAt: identity.ExpiresAt}
	if _, e = vault.Open(sshIdentity, sealed); e == nil {
		t.Fatal("device password accepted as SSH signing key")
	}
}
