package oidcauth_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/enzoOvOyu/keel/internal/httpapi"
	"github.com/enzoOvOyu/keel/internal/oidcauth"
	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestOIDCPostgresHTTPAndBrowser(t *testing.T) {
	dsn := os.Getenv("KEEL_TEST_OIDC_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated OIDC PostgreSQL database not configured")
	}
	u, e := url.Parse(dsn)
	if e != nil || !strings.HasPrefix(u.Path, "/keel_test_oidc_") || u.Hostname() != "127.0.0.1" || u.Port() != "15433" {
		t.Fatal("refusing non-isolated OIDC database")
	}
	ctx := context.Background()
	s, e := platform.Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateUser(ctx, "oidc-admin", "OIDC administrator", "private-fixture-admin-password", true, true); e != nil {
		t.Fatal(e)
	}
	user, e := s.CreateUser(ctx, "oidc-user", "Organization user", "private-fixture-user-password", false, false)
	if e != nil {
		t.Fatal(e)
	}
	f := newIdentityFixture(t)
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	config := platform.Config{Environment: "development", InsecureDevCookie: true, PublicOrigins: []string{origin}}
	s.OIDC, e = oidcauth.New(ctx, []oidcauth.ProviderConfig{f.config(origin, "oidc-user")}, config.PublicOrigins, true)
	if e != nil {
		t.Fatal(e)
	}
	server.Config.Handler = httpapi.NewWithStore("oidc-isolated-integration", os.Getenv("KEEL_TEST_OIDC_STATIC_DIR"), s, config)
	server.Start()
	defer server.Close()
	var source atomic.Int32
	source.Store(1)
	newBrowser := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0." + strconv.Itoa(int(source.Add(1))))}}
		return &http.Client{Jar: jar, Transport: &http.Transport{DialContext: dialer.DialContext}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	call := func(client *http.Client, path string) (int, http.Header, []byte) {
		t.Helper()
		r, e := client.Get(origin + path)
		if e != nil {
			t.Fatal(e)
		}
		defer r.Body.Close()
		body, e := io.ReadAll(r.Body)
		if e != nil {
			t.Fatal(e)
		}
		return r.StatusCode, r.Header, body
	}
	callback := func(client *http.Client, mode string) string {
		t.Helper()
		f.setMode(mode)
		if status, _, _ := call(client, "/api/v1/auth/csrf"); status != 200 {
			t.Fatal("pre-session failed")
		}
		status, header, _ := call(client, "/api/v1/auth/oidc/organization/start?next=%2Fprojects")
		if status != 302 {
			t.Fatalf("start failed: %d", status)
		}
		q := authorize(t, header.Get("Location"))
		return "/api/v1/auth/oidc/organization/callback?" + q.Encode()
	}
	t.Run("public_configuration_is_redacted", func(t *testing.T) {
		client := newBrowser()
		status, _, body := call(client, "/api/v1/auth/providers")
		if status != 200 || !strings.Contains(string(body), "Fixture Organization") || strings.Contains(string(body), f.server.URL) || strings.Contains(string(body), "oidc-user") || strings.Contains(string(body), "client_secret") {
			t.Fatal("provider contract or redaction failed")
		}
	})
	t.Run("fresh_cookie_current_local_roles_and_logout", func(t *testing.T) {
		client := newBrowser()
		path := callback(client, "")
		parsed, _ := url.Parse(origin)
		old := client.Jar.Cookies(parsed)[0].Value
		status, header, _ := call(client, path)
		if status != 303 || !strings.HasPrefix(header.Get("Location"), "/auth/callback?status=ok") {
			t.Fatalf("valid callback rejected: %d %s", status, header.Get("Location"))
		}
		if client.Jar.Cookies(parsed)[0].Value == old {
			t.Fatal("session fixation")
		}
		status, _, body := call(client, "/api/v1/auth/session")
		var session platform.SessionResponse
		if json.Unmarshal(body, &session) != nil || status != 200 || session.User.ID != user.ID || session.User.IsAdmin {
			t.Fatal("OIDC did not preserve local authorization")
		}
		request, _ := http.NewRequest("GET", origin+"/api/v1/auth/session", nil)
		request.AddCookie(&http.Cookie{Name: "keel_session", Value: old})
		response, e := newBrowser().Do(request)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != 401 {
			t.Fatal("old pre-session survived")
		}
		if status, _, _ := call(client, "/api/v1/auth/oidc/organization/start"); status != 409 {
			t.Fatal("authenticated account switch allowed")
		}
		request, _ = http.NewRequest("POST", origin+"/api/v1/auth/logout", nil)
		request.Header.Set("Origin", origin)
		request.Header.Set("X-CSRF-Token", session.CSRFToken)
		response, e = client.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != 204 {
			t.Fatal("OIDC session logout failed")
		}
		if status, _, _ := call(client, "/api/v1/auth/session"); status != 401 {
			t.Fatal("logout did not revoke OIDC session")
		}
	})
	t.Run("wrong_browser_then_one_use_callback", func(t *testing.T) {
		client := newBrowser()
		path := callback(client, "")
		if _, h, _ := call(newBrowser(), path); h.Get("Location") != "/auth/callback?status=failed" {
			t.Fatal("foreign browser accepted")
		}
		if _, h, _ := call(client, path); !strings.Contains(h.Get("Location"), "status=ok") {
			t.Fatal("foreign browser consumed legitimate state")
		}
		if _, h, _ := call(client, path); h.Get("Location") != "/auth/callback?status=failed" {
			t.Fatal("callback replay accepted")
		}
	})
	for _, mode := range []string{"nonce", "issuer", "audience", "expired", "signature", "unmapped", "azp"} {
		t.Run("generic_error_"+mode, func(t *testing.T) {
			client := newBrowser()
			path := callback(client, mode)
			status, h, body := call(client, path)
			if status != 303 || h.Get("Location") != "/auth/callback?status=failed" || strings.Contains(string(body), "fixture-subject") {
				t.Fatal("unsafe failure response")
			}
			if status, _, _ := call(client, "/api/v1/auth/session"); status != 401 {
				t.Fatal("invalid identity created a session")
			}
		})
	}
	t.Run("cross_site_start_and_duplicate_callback_parameters", func(t *testing.T) {
		client := newBrowser()
		request, _ := http.NewRequest("GET", origin+"/api/v1/auth/oidc/organization/start", nil)
		request.Header.Set("Sec-Fetch-Site", "cross-site")
		response, e := client.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatal("cross-site start accepted")
		}
		path := callback(client, "")
		if _, h, _ := call(client, path+"&state=duplicate"); h.Get("Location") != "/auth/callback?status=failed" {
			t.Fatal("duplicate state accepted")
		}
	})
	// Each protocol client binds a distinct real loopback source IP; browser
	// evidence uses the untouched 127.0.0.1 budget. No forwarded-IP spoofing.
	if script := os.Getenv("KEEL_TEST_OIDC_BROWSER_SCRIPT"); script != "" {
		t.Run("actual_browser_provider_roundtrip", func(t *testing.T) {
			f.setMode("")
			file := filepath.Join(t.TempDir(), "fixture.json")
			raw, _ := json.Marshal(map[string]string{"base_url": origin, "username": "oidc-user", "password": "private-fixture-user-password"})
			if e := os.WriteFile(file, raw, 0600); e != nil {
				t.Fatal(e)
			}
			command := exec.Command("node", script)
			command.Env = append(os.Environ(), "KEEL_OIDC_BROWSER_FIXTURE_FILE="+file)
			output, e := command.CombinedOutput()
			if e != nil {
				t.Fatalf("OIDC browser failed: %v\n%s", e, output)
			}
			t.Log(string(output))
		})
	}
	t.Run("callback_error_and_issuer_mismatch_consume_state", func(t *testing.T) {
		for _, suffix := range []string{"&error=access_denied", "&iss=https%3A%2F%2Fwrong.invalid"} {
			client := newBrowser()
			path := callback(client, "")
			if _, h, _ := call(client, path+suffix); h.Get("Location") != "/auth/callback?status=failed" {
				t.Fatal("invalid response accepted")
			}
			if _, h, _ := call(client, path); h.Get("Location") != "/auth/callback?status=failed" {
				t.Fatal("failed response state remained reusable")
			}
		}
	})
	t.Run("real_source_rate_limit_also_bounds_audit_writes", func(t *testing.T) {
		client := newBrowser()
		for i := 0; i < 30; i++ {
			status, _, _ := call(client, "/api/v1/auth/oidc/organization/start")
			if status != 302 {
				t.Fatalf("unexpected pre-limit failure: %d", status)
			}
		}
		status, header, _ := call(client, "/api/v1/auth/oidc/organization/start")
		if status != 429 || header.Get("Retry-After") != "60" {
			t.Fatal("rate limit not enforced")
		}
		var before, after int
		if e := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits").Scan(&before); e != nil {
			t.Fatal(e)
		}
		for i := 0; i < 4; i++ {
			call(client, "/api/v1/auth/oidc/organization/callback?state=invalid&code=invalid")
		}
		if e := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits").Scan(&after); e != nil || before != after {
			t.Fatal("over-limit callback still writes audit rows")
		}
	})
	t.Run("disabled_account_cannot_complete_pending_login", func(t *testing.T) {
		client := newBrowser()
		path := callback(client, "")
		if e = s.DisableUser(ctx, "oidc-user"); e != nil {
			t.Fatal(e)
		}
		if _, h, _ := call(client, path); h.Get("Location") != "/auth/callback?status=failed" {
			t.Fatal("disabled local account accepted")
		}
	})
	var secretLeaks int
	if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE metadata::text LIKE '%private-fixture%' OR metadata::text LIKE '%fixture-access-token%' OR metadata::text LIKE '%code_verifier%'").Scan(&secretLeaks); e != nil || secretLeaks != 0 {
		t.Fatal("audit contains authentication secrets")
	}
}
