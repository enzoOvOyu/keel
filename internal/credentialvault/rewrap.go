package credentialvault

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"

	"golang.org/x/crypto/ssh"
)

// KeyID is a domain-separated public fingerprint of a high-entropy master key.
// It identifies wrapping generations without exposing any key bytes.
func (v *Vault) KeyID() string {
	if v == nil {
		return ""
	}
	return v.keyID
}

// decryptForMaintenance authenticates immutable identity even for expired or
// revoked historical records. It must never be used to authorize SSH signing;
// Open keeps its independent expiry check and Provider enforces revocation.
func (v *Vault) decryptForMaintenance(i Identity, sealed []byte) ([]byte, error) {
	if v == nil || !i.Valid() || len(sealed) < v.aead.NonceSize()+v.aead.Overhead() || len(sealed) > 1024 {
		return nil, ErrUnavailable
	}
	n := v.aead.NonceSize()
	der, err := v.aead.Open(nil, sealed[:n], sealed[n:], associatedData(i))
	if err != nil {
		return nil, ErrUnavailable
	}
	valid := false
	defer func() {
		if !valid {
			clear(der)
		}
	}()
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, ErrUnavailable
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, ErrUnavailable
	}
	defer clear(key)
	public, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(i.PublicKey))
	if err != nil || len(options) != 0 || len(rest) != 0 || comment != "keel-ctrl-"+i.ProjectID+"-"+i.Ref+"-"+i.Version {
		return nil, ErrUnavailable
	}
	generated, err := ssh.NewPublicKey(key.Public())
	if err != nil || !bytes.Equal(public.Marshal(), generated.Marshal()) {
		return nil, ErrUnavailable
	}
	valid = true
	return der, nil
}

// CheckSealed verifies storage custody; success does not make a revoked or
// expired key eligible for use. It never returns a signer or plaintext.
func (v *Vault) CheckSealed(i Identity, sealed []byte) error {
	der, err := v.decryptForMaintenance(i, sealed)
	clear(der)
	return err
}

// Rewrap preserves the exact project key and authenticated identity, using a
// fresh nonce under an independently supplied new master key.
func (v *Vault) Rewrap(next *Vault, i Identity, sealed []byte) ([]byte, error) {
	if v == nil || next == nil || v.KeyID() == "" || next.KeyID() == "" || v.KeyID() == next.KeyID() {
		return nil, ErrUnavailable
	}
	der, err := v.decryptForMaintenance(i, sealed)
	if err != nil {
		return nil, err
	}
	defer clear(der)
	nonce := make([]byte, next.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, ErrUnavailable
	}
	out := next.aead.Seal(nonce, nonce, der, associatedData(i))
	if err = next.CheckSealed(i, out); err != nil {
		return nil, err
	}
	return out, nil
}
