// Package agentpki handles only local key custody and bound Agent certificates.
package agentpki

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

var ErrInvalid = errors.New("Agent identity material is invalid or unavailable")
var idPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

type Authority struct {
	Certificate *x509.Certificate
	PEM         []byte
	key         ed25519.PrivateKey
	ID          string
}

// ReadFile opens every component without following links and bounds the read.
func ReadFile(path string, limit int64, secret bool) ([]byte, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || limit <= 0 {
		return nil, ErrInvalid
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	fd, e := unix.Open("/", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, ErrInvalid
	}
	defer func() { unix.Close(fd) }()
	for _, part := range parts[:len(parts)-1] {
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return nil, ErrInvalid
		}
		unix.Close(fd)
		fd = next
	}
	file, e := unix.Openat(fd, parts[len(parts)-1], unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, ErrInvalid
	}
	f := os.NewFile(uintptr(file), "agent-material")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(file, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Geteuid()) || st.Nlink != 1 || st.Size <= 0 || st.Size > limit || st.Mode&0022 != 0 || (secret && st.Mode&0077 != 0) {
		return nil, ErrInvalid
	}
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(raw)) > limit {
		return nil, ErrInvalid
	}
	return raw, nil
}

func WriteNew(path string, raw []byte) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || len(raw) == 0 {
		return ErrInvalid
	}
	// A private existing parent is required; no directory creation or replacement.
	parent := filepath.Dir(path)
	info, e := os.Lstat(parent)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return ErrInvalid
	}
	// Verify the parent chain using the same no-link path walk as reads.
	parts := strings.Split(strings.TrimPrefix(parent, "/"), "/")
	fd, e := unix.Open("/", unix.O_DIRECTORY|unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if e != nil {
		return ErrInvalid
	}
	defer func() { unix.Close(fd) }()
	for _, part := range parts {
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if err != nil {
			return ErrInvalid
		}
		unix.Close(fd)
		fd = next
	}
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Geteuid()) || st.Mode&0077 != 0 {
		return ErrInvalid
	}
	out, e := unix.Openat(fd, filepath.Base(path), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if e != nil {
		return ErrInvalid
	}
	f := os.NewFile(uintptr(out), "agent-material")
	success := false
	defer func() {
		f.Close()
		if !success {
			unix.Unlinkat(fd, filepath.Base(path), 0)
		}
	}()
	if _, e = f.Write(raw); e != nil {
		return ErrInvalid
	}
	if f.Sync() != nil {
		return ErrInvalid
	}
	if f.Close() != nil {
		return ErrInvalid
	}
	if unix.Fsync(fd) != nil {
		return ErrInvalid
	}
	success = true
	return nil
}

func Hash(raw []byte) string { s := sha256.Sum256(raw); return hex.EncodeToString(s[:]) }
func URI(project, machine, credential string) *url.URL {
	return &url.URL{Scheme: "spiffe", Host: "keel", Path: "/agent/project/" + project + "/machine/" + machine + "/credential/" + credential}
}
func Certificate(raw []byte) (*x509.Certificate, error) {
	b, rest := pem.Decode(raw)
	if b == nil || b.Type != "CERTIFICATE" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, ErrInvalid
	}
	c, e := x509.ParseCertificate(b.Bytes)
	if e != nil {
		return nil, ErrInvalid
	}
	return c, nil
}
func Load(certificate, key string) (*Authority, error) {
	raw, e := ReadFile(certificate, 16384, false)
	if e != nil {
		return nil, e
	}
	keyRaw, e := ReadFile(key, 16384, true)
	if e != nil {
		return nil, e
	}
	pair, e := tls.X509KeyPair(raw, keyRaw)
	if e != nil {
		return nil, ErrInvalid
	}
	c, e := Certificate(raw)
	if e != nil {
		return nil, e
	}
	signer, ok := pair.PrivateKey.(ed25519.PrivateKey)
	now := time.Now()
	if !ok || !c.IsCA || !c.BasicConstraintsValid || c.KeyUsage&x509.KeyUsageCertSign == 0 || c.CheckSignatureFrom(c) != nil || now.Before(c.NotBefore) || !now.Before(c.NotAfter) {
		return nil, ErrInvalid
	}
	return &Authority{Certificate: c, PEM: raw, key: signer, ID: Hash(c.Raw)}, nil
}
func NewAuthority(now time.Time) ([]byte, []byte, error) {
	pub, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		return nil, nil, ErrInvalid
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return nil, nil, ErrInvalid
	}
	c := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Keel Agent enrollment CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.AddDate(1, 0, 0), IsCA: true, BasicConstraintsValid: true, MaxPathLen: 0, MaxPathLenZero: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	der, e := x509.CreateCertificate(rand.Reader, c, c, pub, key)
	if e != nil {
		return nil, nil, ErrInvalid
	}
	keyDER, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return nil, nil, ErrInvalid
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}
func CSR(raw []byte) (ed25519.PublicKey, error) {
	if len(raw) > 8192 {
		return nil, ErrInvalid
	}
	b, rest := pem.Decode(raw)
	if b == nil || b.Type != "CERTIFICATE REQUEST" || len(bytes.TrimSpace(rest)) != 0 {
		return nil, ErrInvalid
	}
	r, e := x509.ParseCertificateRequest(b.Bytes)
	if e != nil || r.CheckSignature() != nil {
		return nil, ErrInvalid
	}
	pub, ok := r.PublicKey.(ed25519.PublicKey)
	if !ok {
		return nil, ErrInvalid
	}
	return pub, nil
}
func (a *Authority) Issue(csr []byte, project, machine, id string, now time.Time) ([]byte, time.Time, error) {
	if a == nil || !idPattern.MatchString(project) || !idPattern.MatchString(machine) || !idPattern.MatchString(id) {
		return nil, time.Time{}, ErrInvalid
	}
	pub, e := CSR(csr)
	if e != nil {
		return nil, time.Time{}, e
	}
	now = now.UTC().Truncate(time.Second)
	expires := now.Add(time.Hour)
	if now.Before(a.Certificate.NotBefore) || expires.After(a.Certificate.NotAfter) {
		return nil, time.Time{}, ErrInvalid
	}
	serial, e := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if e != nil {
		return nil, time.Time{}, ErrInvalid
	}
	c := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: id}, NotBefore: now, NotAfter: expires, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, URIs: []*url.URL{URI(project, machine, id)}}
	der, e := x509.CreateCertificate(rand.Reader, c, a.Certificate, pub, a.key)
	if e != nil {
		return nil, time.Time{}, ErrInvalid
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), expires, nil
}
