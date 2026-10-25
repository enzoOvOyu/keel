package agentpki_test

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"golang.org/x/sys/unix"
)

func write(t *testing.T, path string, raw []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, raw, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestProtectedRead(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	write(t, secret, []byte("fixture-only"), 0600)
	if raw, err := agentpki.ReadFile(secret, 12, true); err != nil || string(raw) != "fixture-only" {
		t.Fatal("private file could not be read")
	}
	if _, err := agentpki.ReadFile(secret, 11, true); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("oversize file accepted")
	}
	write(t, secret, []byte("public"), 0644)
	if _, err := agentpki.ReadFile(secret, 12, true); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("public-readable secret accepted")
	}
	if _, err := agentpki.ReadFile(secret, 12, false); err != nil {
		t.Fatal("public certificate mode rejected")
	}
	write(t, secret, []byte("public"), 0664)
	if _, err := agentpki.ReadFile(secret, 12, false); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("group-writable public material accepted")
	}
	write(t, secret, nil, 0600)
	if _, err := agentpki.ReadFile(secret, 12, true); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("empty material accepted")
	}
	for _, path := range []string{"relative", dir + "/../" + filepath.Base(dir) + "/secret", dir, "/dev/null"} {
		if _, err := agentpki.ReadFile(path, 12, true); !errors.Is(err, agentpki.ErrInvalid) {
			t.Errorf("nonregular or noncanonical path accepted: %q", path)
		}
	}
}

func TestProtectedFilesRejectLinksAndFIFO(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "original")
	write(t, original, []byte("fixture-only"), 0600)
	link := filepath.Join(dir, "symlink")
	if err := os.Symlink(original, link); err != nil {
		t.Fatal(err)
	}
	if _, err := agentpki.ReadFile(link, 128, true); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("final symlink accepted")
	}
	parentLink := filepath.Join(dir, "parent-link")
	if err := os.Symlink(dir, parentLink); err != nil {
		t.Fatal(err)
	}
	if _, err := agentpki.ReadFile(filepath.Join(parentLink, "original"), 128, true); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("ancestor symlink accepted")
	}
	if err := agentpki.WriteNew(filepath.Join(parentLink, "new"), []byte("fixture")); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("write through ancestor symlink accepted")
	}
	hardlink := filepath.Join(dir, "hardlink")
	if err := os.Link(original, hardlink); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{original, hardlink} {
		if _, err := agentpki.ReadFile(path, 128, true); !errors.Is(err, agentpki.ErrInvalid) {
			t.Fatal("hardlinked material accepted")
		}
	}
	fifo := filepath.Join(dir, "fifo")
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	// No writer exists: an ordinary blocking open would hang this test.
	if _, err := agentpki.ReadFile(fifo, 128, true); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("FIFO accepted")
	}
}

func TestWriteNewPreservesExistingFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "new")
	if err := agentpki.WriteNew(path, []byte("original fixture")); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("new secret is not mode 0600")
	}
	if err := agentpki.WriteNew(path, []byte("replacement")); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("existing material overwritten")
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "original fixture" {
		t.Fatal("failed exclusive create changed existing material")
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if err := agentpki.WriteNew(link, []byte("replacement")); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("existing symlink overwritten")
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatal("failed write removed preexisting symlink")
	}
	if err := os.Chmod(dir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := agentpki.WriteNew(filepath.Join(dir, "other"), []byte("fixture")); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("nonprivate parent accepted")
	}
}

func authority(t *testing.T, now time.Time) (*agentpki.Authority, string, string) {
	t.Helper()
	cert, key, err := agentpki.NewAuthority(now)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem")
	write(t, certPath, cert, 0644)
	write(t, keyPath, key, 0600)
	a, err := agentpki.Load(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	return a, certPath, keyPath
}

func request(t *testing.T, template *x509.CertificateRequest) ([]byte, ed25519.PublicKey) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, template, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), pub
}

func TestAuthorityAndCSRValidation(t *testing.T) {
	a, certPath, keyPath := authority(t, time.Now())
	if a.ID != agentpki.Hash(a.Certificate.Raw) || len(a.ID) != 64 || !a.Certificate.MaxPathLenZero || a.Certificate.CheckSignatureFrom(a.Certificate) != nil {
		t.Fatal("authority identity or path constraints missing")
	}
	_, _, otherKey := authority(t, time.Now())
	if _, err := agentpki.Load(certPath, otherKey); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("mismatched CA private key accepted")
	}
	if err := os.Chmod(keyPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := agentpki.Load(certPath, keyPath); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("public-readable CA key accepted")
	}
	csr, pub := request(t, &x509.CertificateRequest{})
	if got, err := agentpki.CSR(csr); err != nil || !bytes.Equal(got, pub) {
		t.Fatal("valid signed CSR rejected")
	}
	block, _ := pem.Decode(csr)
	block.Bytes[len(block.Bytes)-1] ^= 1
	if _, err := agentpki.CSR(pem.EncodeToMemory(block)); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("CSR with invalid proof of key possession accepted")
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := agentpki.CSR(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})); !errors.Is(err, agentpki.ErrInvalid) {
		t.Fatal("non-Ed25519 CSR accepted")
	}
	for _, raw := range [][]byte{nil, []byte(strings.Repeat("x", 8193)), append(append([]byte{}, csr...), csr...), append(append([]byte{}, csr...), []byte("unexpected trailing data")...)} {
		if _, err := agentpki.CSR(raw); !errors.Is(err, agentpki.ErrInvalid) {
			t.Fatal("malformed, oversized or multiple CSR accepted")
		}
	}
	for _, raw := range [][]byte{nil, csr, append(append([]byte{}, a.PEM...), a.PEM...), append(append([]byte{}, a.PEM...), []byte("unexpected trailing data")...)} {
		if _, err := agentpki.Certificate(raw); !errors.Is(err, agentpki.ErrInvalid) {
			t.Fatal("malformed or multiple certificate accepted")
		}
	}
}

func TestIssuedCertificateUsesOnlyBoundIdentity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	a, _, _ := authority(t, now)
	attackerURI, _ := url.Parse("spiffe://keel/administrator")
	csr, pub := request(t, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: "administrator", Organization: []string{"untrusted"}},
		DNSNames: []string{"administrator.internal"}, EmailAddresses: []string{"administrator@example.invalid"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, URIs: []*url.URL{attackerURI},
	})
	project, machine, credential := strings.Repeat("1", 32), strings.Repeat("2", 32), strings.Repeat("3", 32)
	raw, expires, err := a.Issue(csr, project, machine, credential, now)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := agentpki.Certificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cert.Subject.CommonName != credential || len(cert.Subject.Organization) != 0 || len(cert.DNSNames) != 0 || len(cert.EmailAddresses) != 0 || len(cert.IPAddresses) != 0 || len(cert.URIs) != 1 || cert.URIs[0].String() != agentpki.URI(project, machine, credential).String() {
		t.Fatal("CSR-supplied identity escaped server binding")
	}
	if !bytes.Equal(cert.PublicKey.(ed25519.PublicKey), pub) || cert.IsCA || cert.KeyUsage != x509.KeyUsageDigitalSignature || len(cert.ExtKeyUsage) != 1 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatal("certificate grants unexpected identity key or authority")
	}
	if !cert.NotBefore.Equal(now) || !cert.NotAfter.Equal(now.Add(time.Hour)) || !expires.Equal(cert.NotAfter) {
		t.Fatal("short-lived certificate validity incorrect")
	}
	roots := x509.NewCertPool()
	roots.AddCert(a.Certificate)
	if _, err := cert.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: now.Add(time.Second), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatal("issued certificate does not verify for client authentication")
	}
	for _, options := range []x509.VerifyOptions{
		{Roots: roots, CurrentTime: now.Add(time.Second), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}},
		{Roots: roots, CurrentTime: expires.Add(time.Second), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}},
	} {
		if _, err := cert.Verify(options); err == nil {
			t.Fatal("certificate accepted for server authentication or past expiry")
		}
	}
	for _, binding := range [][3]string{{"invalid", machine, credential}, {project, "", credential}, {project, machine, strings.Repeat("A", 32)}} {
		if _, _, err := a.Issue(csr, binding[0], binding[1], binding[2], now); !errors.Is(err, agentpki.ErrInvalid) {
			t.Fatal("invalid identity binding accepted")
		}
	}
	for _, outside := range []time.Time{a.Certificate.NotBefore.Add(-time.Second), a.Certificate.NotAfter.Add(-time.Minute), a.Certificate.NotAfter.Add(time.Second)} {
		if _, _, err := a.Issue(csr, project, machine, credential, outside); !errors.Is(err, agentpki.ErrInvalid) {
			t.Fatal("leaf validity outside CA validity accepted")
		}
	}
}

func TestLoadRejectsUnusableAuthorities(t *testing.T) {
	now := time.Now()
	for _, instant := range []time.Time{now.AddDate(-2, 0, 0), now.Add(24 * time.Hour)} {
		cert, key, err := agentpki.NewAuthority(instant)
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		certPath, keyPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem")
		write(t, certPath, cert, 0600)
		write(t, keyPath, key, 0600)
		if _, err := agentpki.Load(certPath, keyPath); !errors.Is(err, agentpki.ErrInvalid) {
			t.Fatal("expired or future CA accepted")
		}
	}
	for _, invalid := range []string{"leaf", "missing-cert-sign", "not-self-signed"} {
		t.Run(invalid, func(t *testing.T) {
			pub, key, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			parent := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture-ca"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), BasicConstraintsValid: true, IsCA: true, KeyUsage: x509.KeyUsageCertSign}
			child := *parent
			signer := key
			switch invalid {
			case "leaf":
				child.IsCA = false
				child.KeyUsage = x509.KeyUsageDigitalSignature
			case "missing-cert-sign":
				child.KeyUsage = x509.KeyUsageDigitalSignature
			case "not-self-signed":
				_, signer, err = ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				child.Subject.CommonName = "different-ca"
			}
			der, err := x509.CreateCertificate(rand.Reader, &child, parent, pub, signer)
			if err != nil {
				t.Fatal(err)
			}
			keyDER, err := x509.MarshalPKCS8PrivateKey(key)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			certPath, keyPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem")
			write(t, certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)
			write(t, keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0600)
			if _, err := agentpki.Load(certPath, keyPath); !errors.Is(err, agentpki.ErrInvalid) {
				t.Fatal("non-CA, signing-disabled or externally signed authority accepted")
			}
		})
	}
}
