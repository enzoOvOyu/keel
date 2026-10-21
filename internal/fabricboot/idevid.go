package fabricboot

import (
	"crypto/tls"
	"crypto/x509"
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
