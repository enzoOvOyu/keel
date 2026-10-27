package bmc

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type testCredentials struct {
	value Credentials
	err   error
}

func (p *testCredentials) Resolve(context.Context, Target) (Credentials, error) {
	return p.value, p.err
}
func TestCanonicalPathRejectsEscapesAndArbitraryQueries(t *testing.T) {
	for _, path := range []string{"../Accounts", "/redfish/v1/../Accounts", "/redfish/v1/%2fAccounts", "/redfish/v1/%2e%2e/Accounts", "/redfish/v1/Systems#x", "//attacker.invalid/redfish/v1/", "https://attacker.invalid/redfish/v1/", "/redfish/v1/Systems?$filter=x", "/redfish/v1/Systems?$skip=1&$skip=2", "/other", "/redfish/v1/Systems\\x"} {
		if _, e := CanonicalPath("/redfish/v1/", path); e == nil {
			t.Errorf("accepted %q", path)
		}
	}
	for _, path := range []string{"/redfish/v1/", "/redfish/v1/Systems", "/redfish/v1/Systems?$skip=1", "/redfish/v1/Systems?$skiptoken=opaque%2Bvalue"} {
		if _, e := CanonicalPath("/redfish/v1/", path); e != nil {
			t.Errorf("rejected %q: %v", path, e)
		}
	}
}
func TestHTTPSClientPinsCredentialsOriginAndFailureClassification(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		user, password, ok := r.BasicAuth()
		if !ok || user != "reader" || password != "private-value" {
			t.Error("missing pinned authentication")
		}
		switch r.URL.Path {
		case "/redfish/v1/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"RedfishVersion":"1.0.0"}`))
		case "/redfish/v1/Systems":
			http.Redirect(w, r, "/redfish/v1/", 302)
		default:
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"reflected":"private-value"}`))
		}
	}))
	defer server.Close()
	target := testTarget()
	provider := &testCredentials{value: Credentials{Username: "reader", Password: "private-value", ExpiresAt: time.Now().Add(time.Hour), BatchRef: target.CredentialBatchRef, Version: target.CredentialVersion}}
	client, e := NewClient(server.URL, server.Client().Transport.(*http.Transport), time.Second, provider, target, "sim")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = client.Read(context.Background(), "/redfish/v1/"); e != nil {
		t.Fatal(e)
	}
	if _, e = client.Resolve("/redfish/v1/", server.URL+"/redfish/v1/"); e != nil {
		t.Fatal(e)
	}
	for _, reference := range []string{server.URL + "/redfish/v1/#hidden", "https://attacker.invalid/redfish/v1/", "https://user:password@attacker.invalid/redfish/v1/"} {
		if _, e = client.Resolve("/redfish/v1/", reference); e == nil {
			t.Fatal("accepted unsafe absolute reference")
		}
	}
	if _, e = client.Read(context.Background(), "/redfish/v1/Systems"); e == nil {
		t.Fatal("followed redirect")
	}
	if hits.Load() != 2 {
		t.Fatal("redirect made an extra request")
	}
	_, e = client.Read(context.Background(), "/redfish/v1/Managers")
	var f *Failure
	if !errors.As(e, &f) || f.Class != "authentication_failed" || strings.Contains(e.Error(), "private-value") {
		t.Fatalf("bad sanitized failure: %v", e)
	}
	provider.value.Version = "rotated"
	before := hits.Load()
	_, e = client.Read(context.Background(), "/redfish/v1/")
	if !errors.As(e, &f) || f.Code != "credential_identity_mismatch" || hits.Load() != before {
		t.Fatal("used rotated credentials for old request")
	}
	provider.err = ErrCredentialExpired
	_, e = client.Read(context.Background(), "/redfish/v1/")
	if !errors.As(e, &f) || f.Code != "credential_expired" || !f.Retryable {
		t.Fatal("lost credential expiry retry classification")
	}
	raw, _ := json.Marshal(provider.value)
	if string(raw) != "{}" {
		t.Fatal("credentials have JSON representation")
	}
	unsafe := http.DefaultTransport.(*http.Transport).Clone()
	unsafe.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	if _, e = NewClient(server.URL, unsafe, time.Second, provider, target, "real"); e == nil {
		t.Fatal("accepted untrusted TLS")
	}
	if _, e = NewClient("http://127.0.0.1", server.Client().Transport.(*http.Transport), time.Second, provider, target, "sim"); e == nil {
		t.Fatal("accepted plaintext credential transport")
	}
}
