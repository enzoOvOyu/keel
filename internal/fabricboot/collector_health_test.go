package fabricboot

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestWiringCollectorProbeDoesNotDependOnBootServices(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != "GET" || r.URL.Path != "/collector" {
			t.Errorf("unexpected readiness request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write([]byte(`{"service":"keel-fabric-collector","status":"ready","capabilities":["lldp","interfaces","ddm"]}`))
	}))
	defer server.Close()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: server.TLS.Certificates}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	// Construct fixed configured targets directly: the G2 health operation must
	// never call any boot/Kea/repository target even when those are unavailable.
	suite := &ProbeSuite{targets: map[string]environmentProbe{"collector": {target: ProbeTarget{URL: server.URL + "/collector"}, client: client}, "kea": {target: ProbeTarget{URL: server.URL + "/kea"}, client: client}, "boot": {target: ProbeTarget{URL: server.URL + "/boot"}, client: client}, "repository": {target: ProbeTarget{URL: server.URL + "/artifact"}, client: client}}}
	result := suite.CheckCollector(context.Background())
	if result.State != "ready" || result.Code != "protocol_verified" || requests.Load() != 1 {
		t.Fatalf("G2 collector readiness depended on unrelated boot services: %+v requests=%d", result, requests.Load())
	}
	suite.targets["collector"] = environmentProbe{target: ProbeTarget{URL: server.URL + "/collector"}, client: server.Client()}
	result = suite.CheckCollector(context.Background())
	if result.State != "unknown" || requests.Load() != 1 {
		t.Fatal("collector probe permitted an unauthenticated configuration")
	}
	client.CloseIdleConnections()
}
