package credentialvault

import (
	"crypto/rand"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

// SecretIdentity separates BMC/switch passwords from SSH signing keys even if
// an operator chooses the same wrapping key. Production BMC keys use their own
// configured master file and lifecycle; no secret bytes have JSON output here.
type SecretIdentity struct {
	ProjectID string    `json:"project_id"`
	Ref       string    `json:"credential_batch_ref"`
	Version   string    `json:"credential_version"`
	Scope     string    `json:"scope"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (i SecretIdentity) valid() bool {
	return utf8.ValidString(i.Ref) && utf8.ValidString(i.Version) && strings.TrimSpace(i.Ref) == i.Ref && strings.TrimSpace(i.Version) == i.Version && !strings.ContainsAny(i.Ref+i.Version, "\x00\r\n") && i.ExpiresAt.Year() >= 1970 && i.ExpiresAt.Year() <= 9999 && projectID.MatchString(i.ProjectID) && len(i.Ref) > 0 && len(i.Ref) <= 120 && len(i.Version) > 0 && len(i.Version) <= 120 && (i.Scope == "bmc" || i.Scope == "switch") && !i.ExpiresAt.IsZero()
}
func secretAssociatedData(i SecretIdentity) []byte {
	i.ExpiresAt = i.ExpiresAt.UTC().Truncate(time.Microsecond)
	raw, _ := json.Marshal(struct {
		Purpose  string
		Identity SecretIdentity
	}{"keel.device-password/v1", i})
	return raw
}
func (v *Vault) SealSecret(i SecretIdentity, plain []byte) ([]byte, error) {
	if v == nil || !i.valid() || !i.ExpiresAt.After(time.Now()) || len(plain) < 1 || len(plain) > 8192 {
		return nil, ErrUnavailable
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return nil, ErrUnavailable
	}
	return v.aead.Seal(nonce, nonce, plain, secretAssociatedData(i)), nil
}
func (v *Vault) OpenSecret(i SecretIdentity, sealed []byte) ([]byte, error) {
	if v == nil || !i.valid() || !time.Now().Before(i.ExpiresAt) || len(sealed) < v.aead.NonceSize()+v.aead.Overhead() || len(sealed) > 8256 {
		return nil, ErrUnavailable
	}
	n := v.aead.NonceSize()
	plain, e := v.aead.Open(nil, sealed[:n], sealed[n:], secretAssociatedData(i))
	if e != nil {
		return nil, ErrUnavailable
	}
	return plain, nil
}
