package fabricboot

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestEnvironmentProtocolProbes(t *testing.T) {
	var missing atomic.Bool
	artifact := []byte("approved-isolation-artifact-v1")
	sum := sha256.Sum256(artifact)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/kea":
			var body map[string]any
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["command"] != "config-hash-get" || r.Method != "POST" {
				t.Error("probe did not use read-only Kea protocol")
				w.WriteHeader(400)
				return
			}
			_, _ = w.Write([]byte(`[{"result":0,"arguments":{"hash":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}]`))
		case "/boot":
			_, _ = w.Write([]byte(`{"service":"keel-ztp","status":"ready","capabilities":["ztp239"]}`))
		case "/collector":
			if missing.Load() {
				_, _ = w.Write([]byte(`{"service":"keel-fabric-collector","status":"ready","capabilities":["lldp"]}`))
			} else {
				_, _ = w.Write([]byte(`{"service":"keel-fabric-collector","status":"ready","capabilities":["lldp","interfaces","ddm"]}`))
			}
		case "/artifact":
			_, _ = w.Write(artifact)
		case "/redirect":
			http.Redirect(w, r, "/artifact", 302)
		default:
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "certificate.pem"), filepath.Join(dir, "key.pem")
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	raw, e := x509.MarshalPKCS8PrivateKey(server.TLS.Certificates[0].PrivateKey)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(certPath, cert, 0600); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), 0600); e != nil {
		t.Fatal(e)
	}
	cfg := ProbeConfig{Version: "fabric-probes/v1", Targets: map[string]ProbeTarget{}}
	for name, path := range map[string]string{"kea": "/kea", "boot": "/boot", "collector": "/collector", "repository": "/artifact"} {
		cfg.Targets[name] = ProbeTarget{URL: server.URL + path, CAFile: certPath, CertificateFile: certPath, KeyFile: keyPath, SHA256: hex.EncodeToString(sum[:])}
	}
	suite, e := NewProbeSuite(cfg)
	if e != nil {
		t.Fatal(e)
	}
	for _, result := range suite.Check(context.Background(), "239") {
		if result.State != "ready" {
			t.Fatalf("protocol probe failed: %+v", result)
		}
	}
	missing.Store(true)
	for _, result := range suite.Check(context.Background(), "143") {
		if result.Service == "collector" || result.Service == "boot" {
			if result.State != "unknown" || result.Code != "capability_missing" {
				t.Fatalf("missing capability accepted: %+v", result)
			}
		}
	}
	target := cfg.Targets["repository"]
	target.URL = server.URL + "/redirect"
	cfg.Targets["repository"] = target
	suite, e = NewProbeSuite(cfg)
	if e != nil {
		t.Fatal(e)
	}
	for _, result := range suite.Check(context.Background(), "239") {
		if result.Service == "repository" && result.State != "failed" {
			t.Fatal("redirect followed")
		}
	}
	var absent *ProbeSuite
	for _, result := range absent.Check(context.Background(), "239") {
		if result.State != "unknown" || result.Code != "not_configured" {
			t.Fatal("missing configuration became ready")
		}
	}
}
