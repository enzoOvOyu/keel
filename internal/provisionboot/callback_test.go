package provisionboot

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

// This fixture checks transport/proof composition only. It makes no claim about
// database leases, physical inventory, boot-image acceptance or installation.
type callbackFixture struct {
	key          ed25519.PublicKey
	boot         string
	installation string
	sequence     int64
	token        string
	calls        int
	raw          []byte
	expires      time.Time
}

func (f *callbackFixture) EnrollBoot(ctx context.Context, s Scope, id string, peer netip.Addr, token string, in EnrollmentSubmission) (*BootAcknowledgment, error) {
	f.calls++
	key, err := in.Enrollment.Key()
	raw, _ := json.Marshal(in.Enrollment)
	if ctx.Err() != nil || !s.CheckPeer(peer) || !opaqueID.MatchString(id) || token != f.token || f.sequence != 0 || err != nil || VerifyBootProof(key, in.Enrollment.InstallationID, in.Enrollment.BootID, 1, "enroll", raw, in.Proof) != nil {
		return nil, ErrDenied
	}
	f.key = key
	f.installation = in.Enrollment.InstallationID
	f.boot = in.Enrollment.BootID
	f.sequence = 1
	return &BootAcknowledgment{InstallationID: f.installation, BootID: f.boot, Sequence: 1, State: "booting", ExpiresAt: f.expires}, nil
}
func (f *callbackFixture) ReportPreflight(ctx context.Context, s Scope, id string, peer netip.Addr, in PreflightSubmission) (*BootAcknowledgment, error) {
	f.calls++
	if ctx.Err() != nil || !s.CheckPeer(peer) || !opaqueID.MatchString(id) || f.sequence != 1 || VerifyBootProof(f.key, f.installation, f.boot, 2, "preflight", in.Report, in.Proof) != nil {
		return nil, ErrDenied
	}
	f.sequence = 2
	f.raw = append([]byte{}, in.Report...)
	return &BootAcknowledgment{InstallationID: f.installation, BootID: f.boot, Sequence: 2, State: "preflight_ready", ExpiresAt: f.expires}, nil
}
func callbackRequest(g *CallbackGateway, action, token string, raw []byte, mutate func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, g.Scope.Origin+"/boot/"+strings.Repeat("3", 32)+"/"+action, bytes.NewReader(raw))
	r.RemoteAddr = "192.0.2.2:41000"
	r.TLS = &tls.ConnectionState{}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if mutate != nil {
		mutate(r)
	}
	w := httptest.NewRecorder()
	g.ServeHTTP(w, r)
	return w
}
func enrollmentFixture(t *testing.T) (EnrollmentSubmission, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	e := BootEnrollment{InstallationID: strings.Repeat("4", 32), BootID: strings.Repeat("5", 32), PublicKey: base64.StdEncoding.EncodeToString(public)}
	raw, _ := json.Marshal(e)
	proof, err := SignBootProof(private, e.InstallationID, e.BootID, 1, "enroll", raw)
	if err != nil {
		t.Fatal(err)
	}
	return EnrollmentSubmission{Enrollment: e, Proof: proof}, private
}
func TestBootCallbackSignedEnrollmentReportAndReplay(t *testing.T) {
	s, _ := fixture()
	f := &callbackFixture{token: strings.Repeat("a", 64), expires: time.Now().Add(time.Minute)}
	g := &CallbackGateway{Scope: s, Authority: f}
	in, private := enrollmentFixture(t)
	raw, _ := json.Marshal(in)
	if w := callbackRequest(g, "enroll", f.token, raw, nil); w.Code != 200 {
		t.Fatal("enrollment rejected", w.Code)
	}
	if w := callbackRequest(g, "enroll", f.token, raw, nil); w.Code != 404 {
		t.Fatal("single-use token replayed", w.Code)
	}
	report := []byte("{\"fixture_exact_stdout\":true}\n")
	proof, err := SignBootProof(private, in.Enrollment.InstallationID, in.Enrollment.BootID, 2, "preflight", report)
	if err != nil {
		t.Fatal(err)
	}
	packet, _ := json.Marshal(PreflightSubmission{Report: report, Proof: proof})
	if w := callbackRequest(g, "preflight", "", packet, nil); w.Code != 200 {
		t.Fatal("report rejected", w.Code)
	}
	if !bytes.Equal(f.raw, report) {
		t.Fatal("signed report bytes changed in transit")
	}
	if w := callbackRequest(g, "preflight", "", packet, nil); w.Code != 404 {
		t.Fatal("report replayed", w.Code)
	}
}
func TestBootCallbackRejectsUntrustedTransportAndEnvelope(t *testing.T) {
	s, _ := fixture()
	in, _ := enrollmentFixture(t)
	raw, _ := json.Marshal(in)
	for name, mutate := range map[string]func(*http.Request){
		"plaintext":  func(r *http.Request) { r.TLS = nil },
		"wrong_host": func(r *http.Request) { r.Host = "other.example.test" },
		"outside_peer": func(r *http.Request) {
			r.RemoteAddr = "198.51.100.2:41000"
			r.Header.Set("X-Forwarded-For", "192.0.2.2")
		},
		"origin":                  func(r *http.Request) { r.Header.Set("Origin", s.Origin) },
		"cookie":                  func(r *http.Request) { r.Header.Set("Cookie", "keel_session=untrusted") },
		"query_token":             func(r *http.Request) { r.URL.RawQuery = "token=public-secret" },
		"escaped_path":            func(r *http.Request) { r.URL.RawPath = r.URL.Path },
		"compressed":              func(r *http.Request) { r.Header.Set("Content-Encoding", "gzip") },
		"duplicate_authorization": func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+strings.Repeat("a", 64)) },
	} {
		t.Run(name, func(t *testing.T) {
			f := &callbackFixture{token: strings.Repeat("a", 64), expires: time.Now().Add(time.Minute)}
			g := &CallbackGateway{Scope: s, Authority: f}
			w := callbackRequest(g, "enroll", f.token, raw, mutate)
			if w.Code != 404 || f.calls != 0 {
				t.Fatal("untrusted request reached authority", w.Code, f.calls)
			}
		})
	}
	for name, payload := range map[string][]byte{
		"duplicate":  append([]byte(`{"enrollment":{},`), raw[1:]...),
		"case_alias": bytes.Replace(raw, []byte(`"enrollment"`), []byte(`"Enrollment"`), 1),
		"unknown":    append([]byte(`{"extra":true,`), raw[1:]...),
		"trailing":   append(append([]byte{}, raw...), []byte(`{}`)...),
	} {
		t.Run(name, func(t *testing.T) {
			f := &callbackFixture{token: strings.Repeat("a", 64)}
			g := &CallbackGateway{Scope: s, Authority: f}
			w := callbackRequest(g, "enroll", f.token, payload, nil)
			if w.Code != 404 || f.calls != 0 {
				t.Fatal("ambiguous envelope accepted", w.Code, f.calls)
			}
		})
	}
}
func TestBootCallbackCapacityAndFutureOperationsDenied(t *testing.T) {
	s, _ := fixture()
	f := &callbackFixture{token: strings.Repeat("a", 64)}
	g := &CallbackGateway{Scope: s, Authority: f}
	g.once.Do(func() { g.slots = make(chan struct{}, 1) })
	g.slots <- struct{}{}
	in, _ := enrollmentFixture(t)
	raw, _ := json.Marshal(in)
	if w := callbackRequest(g, "enroll", f.token, raw, nil); w.Code != 503 || f.calls != 0 {
		t.Fatal("bounded admission ignored", w.Code)
	}
	if w := callbackRequest(g, "write-permit", f.token, raw, nil); w.Code != 404 {
		t.Fatal("unimplemented writer operation exposed", w.Code)
	}
}
