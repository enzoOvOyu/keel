package provisionimage

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"time"
)

// ParseBootRoots accepts only bounded, currently valid public CA certificates.
// The caller binds these exact bytes to independent image-input approval.
func ParseBootRoots(raw []byte, now time.Time) (*x509.CertPool, error) {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return nil, ErrInvalid
	}
	pool := x509.NewCertPool()
	count := 0
	seen := map[[32]byte]bool{}
	for len(raw) > 0 {
		// pem.Decode tolerates text before a block; trust files do not.
		for len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\r' || raw[0] == '\n' || raw[0] == '\t') {
			raw = raw[1:]
		}
		if len(raw) == 0 {
			break
		}
		if len(raw) < 27 || string(raw[:27]) != "-----BEGIN CERTIFICATE-----" {
			return nil, ErrInvalid
		}
		block, rest := pem.Decode(raw)
		if block == nil || block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, ErrInvalid
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.IsCA || !cert.BasicConstraintsValid || cert.KeyUsage&x509.KeyUsageCertSign == 0 || now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
			return nil, ErrInvalid
		}
		hash := sha256.Sum256(cert.Raw)
		if seen[hash] {
			return nil, ErrInvalid
		}
		seen[hash] = true
		count++
		if count > 32 {
			return nil, ErrInvalid
		}
		pool.AddCert(cert)
		raw = rest
	}
	if count == 0 {
		return nil, ErrInvalid
	}
	return pool, nil
}
