package ldapauth_test

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
	"github.com/enzoOvOyu/keel/internal/ldapauth"
	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestLDAPPostgresHTTPAndBrowser(t *testing.T) {
	dsn := os.Getenv("KEEL_TEST_LDAP_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated LDAP PostgreSQL database not configured")
	}
	parsed, e := url.Parse(dsn)
	if e != nil || !strings.HasPrefix(parsed.Path, "/keel_test_ldap_") || parsed.Hostname() != "127.0.0.1" || parsed.Port() != "15433" {
		t.Fatal("refusing non-isolated LDAP database")
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
	if _, e = s.CreateUser(ctx, "directory-admin", "Directory administrator", "private-local-admin-password", true, true); e != nil {
		t.Fatal(e)
	}
	const localPassword = "private-local-user-password"
	u, e := s.CreateUser(ctx, "directory-user", "Directory user", localPassword, false, false)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateUser(ctx, "unmapped-user", "Unmapped local user", localPassword, false, false); e != nil {
		t.Fatal(e)
	}
	f := newDirectoryFixture(t)
	s.LDAP, e = ldapauth.New(f.config())
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	server.Config.Handler = httpapi.NewWithStore("ldap-isolated-integration", os.Getenv("KEEL_TEST_LDAP_STATIC_DIR"), s, platform.Config{Environment: "development", InsecureDevCookie: true, PublicOrigins: []string{origin}})
	server.Start()
	defer server.Close()
	var source atomic.Int32
	source.Store(1)
	newBrowser := func() *http.Client {
		jar, _ := cookiejar.New(nil)
		dialer := &net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0." + strconv.Itoa(int(source.Add(1))))}}
		return &http.Client{Jar: jar, Transport: &http.Transport{DialContext: dialer.DialContext}}
	}
	call := func(client *http.Client, method, path, csrf string, body any) (int, http.Header, []byte) {
		t.Helper()
		var reader io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			reader = strings.NewReader(string(raw))
		}
		request, _ := http.NewRequest(method, origin+path, reader)
		if method == "POST" {
			request.Header.Set("Origin", origin)
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("X-CSRF-Token", csrf)
		}
		response, e := client.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		raw, e := io.ReadAll(response.Body)
		if e != nil {
			t.Fatal(e)
		}
		return response.StatusCode, response.Header, raw
	}
	pre := func(client *http.Client) string {
		t.Helper()
		status, _, raw := call(client, "GET", "/api/v1/auth/csrf", "", nil)
		var body struct {
			Token string `json:"csrf_token"`
		}
		if status != 200 || json.Unmarshal(raw, &body) != nil || body.Token == "" {
			t.Fatal("pre-session failed")
		}
		return body.Token
	}
	login := func(client *http.Client, method, name, password string) (int, []byte) {
		t.Helper()
		csrf := pre(client)
		status, _, body := call(client, "POST", "/api/v1/auth/login", csrf, platform.LoginInput{Method: method, Username: name, Password: password})
		return status, body
	}
	t.Run("configured_provider_only_exposes_public_label", func(t *testing.T) {
		status, _, raw := call(newBrowser(), "GET", "/api/v1/auth/providers", "", nil)
		if status != 200 || !strings.Contains(string(raw), "Fixture Directory") || strings.Contains(string(raw), "ldaps:") || strings.Contains(string(raw), fixtureDN) || strings.Contains(string(raw), f.ca) {
			t.Fatal("directory provider configuration exposed")
		}
	})
	t.Run("real_bind_cookie_rotation_local_roles_and_logout", func(t *testing.T) {
		client := newBrowser()
		csrf := pre(client)
		base, _ := url.Parse(origin)
		old := client.Jar.Cookies(base)[0].Value
		status, _, raw := call(client, "POST", "/api/v1/auth/login", csrf, platform.LoginInput{Method: "ldap", Username: "DIRECTORY-USER", Password: fixturePassword})
		var session platform.SessionResponse
		if status != 200 || json.Unmarshal(raw, &session) != nil || session.User.ID != u.ID || session.User.IsAdmin {
			t.Fatalf("LDAP login failed: %d", status)
		}
		if client.Jar.Cookies(base)[0].Value == old {
			t.Fatal("session fixation")
		}
		if _, e = s.CSRFSession(ctx, old); e == nil {
			t.Fatal("old pre-session survived")
		}
		status, _, _ = call(client, "POST", "/api/v1/auth/logout", session.CSRFToken, nil)
		if status != 204 {
			t.Fatal("LDAP session logout failed")
		}
		if status, _, _ = call(client, "GET", "/api/v1/auth/session", "", nil); status != 401 {
			t.Fatal("logout did not revoke session")
		}
	})
	t.Run("no_local_fallback_and_uniform_bad_credentials", func(t *testing.T) {
		for _, input := range []struct{ method, name, password string }{{"ldap", "directory-user", localPassword}, {"ldap", "unmapped-user", fixturePassword}, {"ldap", "missing-user", fixturePassword}, {"local", "directory-user", fixturePassword}} {
			status, raw := login(newBrowser(), input.method, input.name, input.password)
			var failure struct {
				Code string `json:"code"`
			}
			if status != 401 || json.Unmarshal(raw, &failure) != nil || failure.Code != "INVALID_CREDENTIALS" {
				t.Fatalf("credential method bypass or enumeration: %d", status)
			}
		}
		if status, _ := login(newBrowser(), "local", "directory-user", localPassword); status != 200 {
			t.Fatal("explicit local method regressed")
		}
	})
	t.Run("csrf_precedes_bind_and_no_anonymous_directory_login", func(t *testing.T) {
		client := newBrowser()
		pre(client)
		before := f.binds.Load()
		status, _, _ := call(client, "POST", "/api/v1/auth/login", "", platform.LoginInput{Method: "ldap", Username: "directory-user", Password: fixturePassword})
		if status != 403 || f.binds.Load() != before {
			t.Fatal("missing CSRF reached LDAP")
		}
		if status, _ := login(newBrowser(), "ldap", "directory-user", ""); status != 400 {
			t.Fatal("empty directory password accepted")
		}
	})
	t.Run("backend_failure_is_503_without_fallback_or_account_lock", func(t *testing.T) {
		f.setMode("drop")
		status, _ := login(newBrowser(), "ldap", "directory-user", localPassword)
		f.setMode("")
		if status != 503 {
			t.Fatal("LDAP outage became password authentication")
		}
		if status, _ := login(newBrowser(), "ldap", "directory-user", fixturePassword); status != 200 {
			t.Fatal("outage locked account or failed recovery")
		}
	})
	t.Run("pre_session_expiry_during_bind_denies_session", func(t *testing.T) {
		client := newBrowser()
		csrf := pre(client)
		base, _ := url.Parse(origin)
		token := client.Jar.Cookies(base)[0].Value
		if _, e = s.DB.ExecContext(ctx, "UPDATE sessions SET expires_at=now()+interval '50 milliseconds' WHERE id=$1", platform.TokenHash(token)); e != nil {
			t.Fatal(e)
		}
		f.setMode("delay")
		status, _, _ := call(client, "POST", "/api/v1/auth/login", csrf, platform.LoginInput{Method: "ldap", Username: "directory-user", Password: fixturePassword})
		f.setMode("")
		if status != 403 {
			t.Fatal("expired pre-session accepted after LDAP bind")
		}
	})
	if script := os.Getenv("KEEL_TEST_LDAP_BROWSER_SCRIPT"); script != "" {
		t.Run("actual_browser_directory_and_local_login", func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "fixture.json")
			raw, _ := json.Marshal(map[string]string{"base_url": origin, "username": "directory-user", "password": fixturePassword, "local_password": localPassword})
			if e := os.WriteFile(file, raw, 0600); e != nil {
				t.Fatal(e)
			}
			command := exec.Command("node", script)
			command.Env = append(os.Environ(), "KEEL_LDAP_BROWSER_FIXTURE_FILE="+file)
			output, e := command.CombinedOutput()
			if e != nil {
				t.Fatalf("LDAP browser failed: %v\n%s", e, output)
			}
			t.Log(string(output))
		})
	}
	t.Run("shared_account_lock_cannot_be_bypassed_by_local_method", func(t *testing.T) {
		client := newBrowser()
		for i := 0; i < 5; i++ {
			status, _ := login(client, "ldap", "directory-user", "wrong-directory-password")
			if status != 401 {
				t.Fatalf("unexpected pre-lock status: %d", status)
			}
		}
		if status, _ := login(newBrowser(), "local", "directory-user", localPassword); status != 429 {
			t.Fatal("local method bypassed directory account lock")
		}
	})
	t.Run("disabled_local_account_never_binds", func(t *testing.T) {
		if e = s.UnlockUser(ctx, "directory-user"); e != nil {
			t.Fatal(e)
		}
		client := newBrowser()
		if status, _ := login(client, "ldap", "directory-user", fixturePassword); status != 200 {
			t.Fatal("pre-disable LDAP session failed")
		}
		base, _ := url.Parse(origin)
		liveToken := client.Jar.Cookies(base)[0].Value
		if e = s.DisableUser(ctx, "directory-user"); e != nil {
			t.Fatal(e)
		}
		if _, err := s.Authenticate(ctx, liveToken); err == nil {
			t.Fatal("disabled account kept authenticated LDAP session")
		}
		before := f.binds.Load()
		status, _ := login(newBrowser(), "ldap", "directory-user", fixturePassword)
		if status != 401 {
			t.Fatal("disabled account accepted")
		}
		if before != f.binds.Load() {
			t.Fatal("disabled account reached directory")
		}
	})
	var count int
	if e = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE metadata::text LIKE '%directory-only-secret%' OR metadata::text LIKE '%private-local-%' OR metadata::text LIKE '%uid=directory-user%'").Scan(&count); e != nil || count != 0 {
		t.Fatal("directory secrets leaked to audit")
	}
}
