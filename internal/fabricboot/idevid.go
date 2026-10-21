package fabricboot

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"regexp"
	"strings"
	"time"
)

var idevidSerialPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// VerifyIDevID binds a direct TLS client certificate to its manufacturer
// trust anchor and extracts the X.509 subject serialNumber. HTTP headers are
// never accepted as device identity on the SZTP channel. A nil/empty roots
// pool fails closed; production must provision manufacturer roots explicitly.
func VerifyIDevID(state *tls.ConnectionState, roots *x509.CertPool, now time.Time) (string, error) {
	if state == nil || !state.HandshakeComplete || len(state.PeerCertificates) == 0 || roots == nil || len(roots.Subjects()) == 0 {
		return "", errors.New("verified device TLS client certificate required")
	}
	leaf := state.PeerCertificates[0]
	if leaf.IsCA {
		return "", errors.New("device certificate cannot be a CA")
	}
	serial := strings.TrimSpace(leaf.Subject.SerialNumber)
	if !idevidSerialPattern.MatchString(serial) || serial != leaf.Subject.SerialNumber {
		return "", errors.New("device certificate subject serialNumber is invalid")
	}
	intermediates := x509.NewCertPool()
	for _, cert := range state.PeerCertificates[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return "", err
	}
	return serial, nil
}

// ParseIDevIDRoots accepts only CA certificates from an explicitly supplied
// PEM bundle. The operating system trust store is never used for device auth.
func ParseIDevIDRoots(raw []byte) (*x509.CertPool, error) {
	roots := x509.NewCertPool()
	count := 0
	for len(raw) > 0 {
		block, rest := pem.Decode(raw)
		if block == nil {
			return nil, errors.New("invalid manufacturer CA PEM")
		}
		raw = rest
		if block.Type != "CERTIFICATE" {
			return nil, errors.New("manufacturer CA bundle contains non-certificate PEM")
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !cert.IsCA || cert.KeyUsage&x509.KeyUsageCertSign == 0 {
			return nil, errors.New("manufacturer trust certificate must be a signing CA")
		}
		roots.AddCert(cert)
		count++
	}
	if count == 0 {
		return nil, errors.New("manufacturer CA bundle is empty")
	}
	return roots, nil
}
