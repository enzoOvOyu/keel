//go:build linux

package provisioninitrd

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
	"math/big"
	"os"
	"testing"
	"time"
)

func TestBootRootsRejectUnapprovedTrustMaterial(t *testing.T) {
	now := time.Now().UTC()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "offline boot fixture CA"}, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	valid := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if _, err = provisionimage.ParseBootRoots(valid, now); err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"empty": nil, "text prefix": append([]byte("ignored text\n"), valid...), "duplicate": append(append([]byte(nil), valid...), valid...), "private key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte{1, 2, 3}}), "bad DER": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte{1, 2, 3}}), "text suffix": append(append([]byte(nil), valid...), []byte("ignored text")...)} {
		t.Run(name, func(t *testing.T) {
			if _, err := provisionimage.ParseBootRoots(raw, now); err == nil {
				t.Fatal("accepted invalid public trust file")
			}
		})
	}
	if _, err = provisionimage.ParseBootRoots(valid, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired CA")
	}
	if _, err = provisionimage.ParseBootRoots(valid, now.Add(-2*time.Hour)); err == nil {
		t.Fatal("future CA")
	}
	ca.IsCA = false
	der, err = x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = provisionimage.ParseBootRoots(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), now); err == nil {
		t.Fatal("leaf as bootstrap root")
	}
}

func TestOrdinaryProcessCannotActivatePrivilegedInitrdAdapters(t *testing.T) {
	if os.Getpid() == 1 {
		t.Skip("guard fixture belongs to an ordinary software-test process")
	}
	if RequireInitramfs() == nil {
		t.Fatal("ordinary host process admitted")
	}
	// Only the first guard is reached: no mount, hardware read or socket occurs.
	if MountRuntime() == nil {
		t.Fatal("ordinary process mounted runtime")
	}
	if _, err := LoadBootRoots(); err == nil {
		t.Fatal("ordinary process read runtime trust file")
	}
	if _, err := LoadBootstrap(); err == nil {
		t.Fatal("ordinary process read protected boot credentials")
	}
}
