// Package credentialvault keeps SSH domain private keys encrypted at rest.
// Its master key is supplied independently from the database and never exported.
package credentialvault

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

var ErrUnavailable = errors.New("credential unavailable")
var identifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,63}$`)
var projectID = regexp.MustCompile(`^[a-f0-9]{32}$`)

type Identity struct {
	ProjectID string    `json:"project_id"`
	Ref       string    `json:"credential_ref"`
	Version   string    `json:"credential_version"`
	PublicKey string    `json:"public_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (i Identity) Valid() bool {
	return projectID.MatchString(i.ProjectID) && identifier.MatchString(i.Ref) && identifier.MatchString(i.Version) && !i.ExpiresAt.IsZero()
}

type Vault struct {
	aead  cipher.AEAD
	keyID string
}

// Load accepts exactly 32 random bytes in an owner-only regular file. Empty
// configuration disables the provider; malformed configuration fails closed.
func Load(path string) (*Vault, error) {
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		return nil, ErrUnavailable
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0077 != 0 || st.Uid != uint32(os.Geteuid()) || st.Size != 32 {
		return nil, ErrUnavailable
	}
	key, err := io.ReadAll(io.LimitReader(f, 33))
	defer clear(key)
	if err != nil || len(key) != 32 {
		return nil, ErrUnavailable
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrUnavailable
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrUnavailable
	}
	h := sha256.New()
	h.Write([]byte("keel.credential-master/v1\x00"))
	h.Write(key)
	return &Vault{aead: aead, keyID: hex.EncodeToString(h.Sum(nil))}, nil
}

// GenerateMaster creates a new file exclusively, never replacing an existing key.
func GenerateMaster(path string) error {
	if !filepath.IsAbs(path) {
		return ErrUnavailable
	}
	key := make([]byte, 32)
	defer clear(key)
	if _, err := rand.Read(key); err != nil {
		return ErrUnavailable
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrUnavailable
	}
	defer f.Close()
	if _, err = f.Write(key); err != nil {
		return ErrUnavailable
	}
	if err = f.Sync(); err != nil {
		return ErrUnavailable
	}
	return nil
}

func associatedData(i Identity) []byte {
	i.ExpiresAt = i.ExpiresAt.UTC().Truncate(time.Microsecond)
	b, _ := json.Marshal(struct {
		Purpose  string
		Identity Identity
	}{"keel.ssh-domain-key/v1", i})
	return b
}

// Generate returns ciphertext and the public identity only. The domain key never
// leaves the vault as plaintext and cannot be imported from browser input.
func (v *Vault) Generate(i Identity) (Identity, []byte, error) {
	if v == nil || !i.Valid() || !i.ExpiresAt.After(time.Now()) {
		return Identity{}, nil, ErrUnavailable
	}
	i.ExpiresAt = i.ExpiresAt.UTC().Truncate(time.Microsecond)
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return Identity{}, nil, ErrUnavailable
	}
	defer clear(key)
	public, err := ssh.NewPublicKey(pub)
	if err != nil {
		return Identity{}, nil, ErrUnavailable
	}
	i.PublicKey = strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public))) + " keel-ctrl-" + i.ProjectID + "-" + i.Ref + "-" + i.Version + "\n"
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return Identity{}, nil, ErrUnavailable
	}
	defer clear(der)
	nonce := make([]byte, v.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return Identity{}, nil, ErrUnavailable
	}
	sealed := v.aead.Seal(nonce, nonce, der, associatedData(i))
	return i, sealed, nil
}

func (v *Vault) Open(i Identity, sealed []byte) (ssh.Signer, error) {
	if v == nil || !i.Valid() || !time.Now().Before(i.ExpiresAt) || len(sealed) < v.aead.NonceSize()+v.aead.Overhead() || len(sealed) > 1024 {
		return nil, ErrUnavailable
	}
	n := v.aead.NonceSize()
	der, err := v.aead.Open(nil, sealed[:n], sealed[n:], associatedData(i))
	if err != nil {
		return nil, ErrUnavailable
	}
	defer clear(der)
	parsed, err := x509.ParsePKCS8PrivateKey(der)
	if err != nil {
		return nil, ErrUnavailable
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, ErrUnavailable
	}
	signer, err := ssh.NewSignerFromKey(key)
	public, comment, options, rest, parseErr := ssh.ParseAuthorizedKey([]byte(i.PublicKey))
	if err != nil || parseErr != nil || len(options) != 0 || len(rest) != 0 || comment != "keel-ctrl-"+i.ProjectID+"-"+i.Ref+"-"+i.Version || !bytes.Equal(public.Marshal(), signer.PublicKey().Marshal()) {
		clear(key)
		return nil, ErrUnavailable
	}
	return signer, nil
}

// Provider resolves an exact project/ref/version on every operation, including
// pooled connections. Revocation and database failures never fall back to files.
type Provider struct {
	DB    *sql.DB
	Vault *Vault
}

func (p *Provider) Resolve(ctx context.Context, project, ref, version string) (ssh.Signer, time.Time, error) {
	if p == nil || p.DB == nil || p.Vault == nil || !projectID.MatchString(project) || !identifier.MatchString(ref) || !identifier.MatchString(version) {
		return nil, time.Time{}, ErrUnavailable
	}
	i := Identity{ProjectID: project, Ref: ref, Version: version}
	var sealed []byte
	err := p.DB.QueryRowContext(ctx, `SELECT public_key,expires_at,ciphertext FROM ssh_domain_keys WHERE project_id=$1 AND credential_ref=$2 AND credential_version=$3 AND revoked_at IS NULL AND expires_at>clock_timestamp()`, project, ref, version).Scan(&i.PublicKey, &i.ExpiresAt, &sealed)
	if err != nil {
		return nil, time.Time{}, ErrUnavailable
	}
	signer, err := p.Vault.Open(i, sealed)
	if err != nil {
		return nil, time.Time{}, ErrUnavailable
	}
	return signer, i.ExpiresAt, nil
}
