package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestOptionalZTP239ListenerUsesDedicatedTLS(t *testing.T) {
	t.Setenv("KEEL_ZTP239_LISTEN_ADDR", "127.0.0.1:0")
	if _, _, err := optionalZTP239Listener(&platform.Store{}); err == nil {
		t.Fatal("partial TLS configuration accepted")
	}
	fixture := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer fixture.Close()
	cert := fixture.TLS.Certificates[0]
	privateDER, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.pem")
	keyPath := filepath.Join(dir, "key.pem")
	if err = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEEL_ZTP239_TLS_CERT_FILE", certPath)
	t.Setenv("KEEL_ZTP239_TLS_KEY_FILE", keyPath)
	srv, listener, err := optionalZTP239Listener(&platform.Store{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, srv, listener) }()
	t.Cleanup(func() { cancel(); _ = <-done })
	roots := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(parsed)
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	response, err := client.Get("https://" + listener.Addr().String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("unrelated route available: %d", response.StatusCode)
	}
}
