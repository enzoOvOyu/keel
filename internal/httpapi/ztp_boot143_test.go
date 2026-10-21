package httpapi

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestSZTPRequestParsingAndCertificateGate(t *testing.T) {
	cases := []struct {
		body, media string
		signed, ok  bool
	}{
		{"", "", false, true},
		{`{"ietf-sztp-bootstrap-server:input":{}}`, "application/yang-data+json", false, true},
		{`{"ietf-sztp-bootstrap-server:input":{"signed-data-preferred":[null]}}`, "application/yang-data+json", true, true},
		{`{"ietf-sztp-bootstrap-server:input":{"signed-data-preferred":true}}`, "application/yang-data+json", false, false},
		{`{"ietf-sztp-bootstrap-server:input":{"serial":"spoofed"}}`, "application/yang-data+json", false, false},
		{`{"ietf-sztp-bootstrap-server:input":{}}`, "application/json", false, false},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, sztpRPCPath, strings.NewReader(tc.body))
		if tc.media != "" {
			req.Header.Set("Content-Type", tc.media)
		}
		signed, err := parseSZTPRequest(req)
		if (err == nil) != tc.ok || (err == nil && signed != tc.signed) {
			t.Fatalf("parse %q: signed=%t err=%v", tc.body, signed, err)
		}
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test manufacturer"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	handler := NewSZTPHandler(&platform.Store{}, roots)
	req := httptest.NewRequest(http.MethodPost, "https://boot.example.invalid"+sztpRPCPath, bytes.NewReader(nil))
	req.RemoteAddr = "192.0.2.8:1234"
	req.Header.Set("X-Forwarded-Client-Cert", "spoof")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("proxy certificate header bypassed TLS gate: %d", recorder.Code)
	}
}
