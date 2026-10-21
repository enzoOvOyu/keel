package fabricboot

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"testing"
	"time"
)

func TestVerifyIDevID(t *testing.T) {
	now := time.Now().UTC()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootT := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test manufacturer"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootT, rootT, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	makeLeaf := func(serial string, eku []x509.ExtKeyUsage) *x509.Certificate {
		t.Helper()
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			t.Fatal(e)
		}
		tmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{SerialNumber: serial}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Minute), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: eku}
		der, e := x509.CreateCertificate(rand.Reader, tmpl, root, &key.PublicKey, rootKey)
		if e != nil {
			t.Fatal(e)
		}
		cert, e := x509.ParseCertificate(der)
		if e != nil {
			t.Fatal(e)
		}
		return cert
	}
	state := &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{makeLeaf("SW-001", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})}}
	if sn, e := VerifyIDevID(state, roots, now); e != nil || sn != "SW-001" {
		t.Fatalf("valid IDevID: %q %v", sn, e)
	}
	cases := []struct {
		name  string
		state *tls.ConnectionState
		roots *x509.CertPool
		at    time.Time
	}{
		{"no TLS", nil, roots, now},
		{"empty trust", state, x509.NewCertPool(), now},
		{"unknown root", state, x509.NewCertPool(), now},
		{"expired", state, roots, now.Add(2 * time.Hour)},
		{"wrong EKU", &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{makeLeaf("SW-002", []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth})}}, roots, now},
		{"missing serial", &tls.ConnectionState{HandshakeComplete: true, PeerCertificates: []*x509.Certificate{makeLeaf("", []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth})}}, roots, now},
		{"uncompleted handshake", &tls.ConnectionState{PeerCertificates: state.PeerCertificates}, roots, now},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if sn, e := VerifyIDevID(tc.state, tc.roots, tc.at); e == nil {
				t.Fatalf("accepted %q", sn)
			}
		})
	}
}
