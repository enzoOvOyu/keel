package oidcauth_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/oidcauth"
)

type authorization struct{ nonce, challenge, redirect, mode string }
type identityFixture struct {
	server *httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	codes  map[string]authorization
	mode   string
	issued int
}

func newIdentityFixture(t *testing.T) *identityFixture {
	t.Helper()
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	f := &identityFixture{key: key, codes: map[string]authorization{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.server.Close)
	return f
}
func (f *identityFixture) setMode(mode string) { f.mu.Lock(); defer f.mu.Unlock(); f.mode = mode }
func (f *identityFixture) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/.well-known/openid-configuration":
		json.NewEncoder(w).Encode(map[string]any{"issuer": f.server.URL, "authorization_endpoint": f.server.URL + "/authorize", "token_endpoint": f.server.URL + "/token", "jwks_uri": f.server.URL + "/jwks", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}, "token_endpoint_auth_methods_supported": []string{"client_secret_basic"}, "code_challenge_methods_supported": []string{"S256"}})
	case "/jwks":
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "fixture", "n": base64.RawURLEncoding.EncodeToString(f.key.N.Bytes()), "e": "AQAB"}}})
	case "/authorize":
		q := r.URL.Query()
		if q.Get("client_id") != "keel-fixture" || q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" || q.Get("state") == "" {
			http.Error(w, "invalid request", 400)
			return
		}
		f.mu.Lock()
		f.issued++
		code := strings.Repeat("c", 32) + string(rune('a'+f.issued%26)) + time.Now().Format("150405.000000000")
		f.codes[code] = authorization{q.Get("nonce"), q.Get("code_challenge"), q.Get("redirect_uri"), f.mode}
		f.mu.Unlock()
		u, e := url.Parse(q.Get("redirect_uri"))
		if e != nil {
			http.Error(w, "invalid redirect", 400)
			return
		}
		values := u.Query()
		values.Set("state", q.Get("state"))
		values.Set("code", code)
		u.RawQuery = values.Encode()
		http.Redirect(w, r, u.String(), 302)
	case "/token":
		if r.Method != "POST" || r.ParseForm() != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		id, secret, ok := r.BasicAuth()
		if !ok || id != "keel-fixture" || secret != "private-fixture-client-secret" {
			http.Error(w, "invalid client", 401)
			return
		}
		f.mu.Lock()
		a, found := f.codes[r.Form.Get("code")]
		delete(f.codes, r.Form.Get("code"))
		f.mu.Unlock()
		challenge := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !found || r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("redirect_uri") != a.redirect || base64.RawURLEncoding.EncodeToString(challenge[:]) != a.challenge {
			http.Error(w, "invalid grant", 400)
			return
		}
		claims := map[string]any{"iss": f.server.URL, "sub": "fixture-subject", "aud": "keel-fixture", "exp": time.Now().Add(5 * time.Minute).Unix(), "iat": time.Now().Unix(), "nonce": a.nonce, "email": "admin@example.invalid", "email_verified": true}
		switch a.mode {
		case "nonce":
			claims["nonce"] = "other"
		case "issuer":
			claims["iss"] = "https://wrong.invalid"
		case "audience":
			claims["aud"] = "other-client"
		case "expired":
			claims["exp"] = time.Now().Add(-5 * time.Minute).Unix()
		case "unmapped":
			claims["sub"] = "unmapped-subject"
		case "azp":
			claims["azp"] = "other-client"
		}
		header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "fixture"})
		body, _ := json.Marshal(claims)
		signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
		digest := sha256.Sum256([]byte(signed))
		signature, e := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, digest[:])
		if e != nil {
			http.Error(w, "signing failed", 500)
			return
		}
		if a.mode == "signature" {
			signature[0] ^= 1
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "fixture-access-token", "token_type": "Bearer", "expires_in": 300, "id_token": signed + "." + base64.RawURLEncoding.EncodeToString(signature)})
	default:
		http.NotFound(w, r)
	}
}
func (f *identityFixture) config(origin, name string) oidcauth.ProviderConfig {
	return oidcauth.ProviderConfig{ID: "organization", Name: "Fixture Organization", Issuer: f.server.URL, ClientID: "keel-fixture", ClientSecret: "private-fixture-client-secret", RedirectURI: origin + "/api/v1/auth/oidc/organization/callback", Bindings: map[string]string{"fixture-subject": name}}
}
func authorize(t *testing.T, destination string) url.Values {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, e := client.Get(destination)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	if r.StatusCode != 302 {
		t.Fatal("identity fixture rejected authorization")
	}
	u, e := url.Parse(r.Header.Get("Location"))
	if e != nil {
		t.Fatal(e)
	}
	return u.Query()
}
func TestOIDCProtocol(t *testing.T) {
	f := newIdentityFixture(t)
	ctx := context.Background()
	s, e := oidcauth.New(ctx, []oidcauth.ProviderConfig{f.config("http://127.0.0.1:3109", "oidc-user")}, []string{"http://127.0.0.1:3109"}, true)
	if e != nil {
		t.Fatal(e)
	}
	browser := strings.Repeat("b", 64)
	for _, mode := range []string{"", "nonce", "issuer", "audience", "expired", "unmapped", "signature", "azp"} {
		t.Run("signed_identity_"+mode, func(t *testing.T) {
			f.setMode(mode)
			destination, e := s.Start("organization", browser, "//attacker.invalid")
			if e != nil {
				t.Fatal(e)
			}
			q := authorize(t, destination)
			if _, _, e = s.Complete(ctx, "organization", strings.Repeat("x", 64), q.Get("state"), q.Get("code")); e == nil {
				t.Fatal("foreign browser accepted")
			}
			name, next, e := s.Complete(ctx, "organization", browser, q.Get("state"), q.Get("code"))
			if mode == "" {
				if e != nil || name != "oidc-user" || next != "/projects" {
					t.Fatalf("valid identity rejected: %v", e)
				}
			} else if e == nil {
				t.Fatal("invalid identity accepted")
			}
			if _, _, e = s.Complete(ctx, "organization", browser, q.Get("state"), q.Get("code")); e == nil {
				t.Fatal("state replay accepted")
			}
		})
	}
	for _, next := range []string{"https://attacker.invalid", "//attacker.invalid", "/\\attacker.invalid", "/%2f%2fattacker.invalid", "/projects?next=https://attacker.invalid", "/admin/users#fragment"} {
		if oidcauth.SafeNext(next) != "/projects" {
			t.Fatal("unsafe next accepted")
		}
	}
	public, _ := json.Marshal(s.Public())
	if strings.Contains(string(public), f.server.URL) || strings.Contains(string(public), "secret") || strings.Contains(string(public), "oidc-user") {
		t.Fatal("public provider leaks configuration")
	}
}
func TestOIDCPKCEAndConcurrentCallback(t *testing.T) {
	f := newIdentityFixture(t)
	ctx := context.Background()
	s, e := oidcauth.New(ctx, []oidcauth.ProviderConfig{f.config("http://127.0.0.1:3109", "oidc-user")}, []string{"http://127.0.0.1:3109"}, true)
	if e != nil {
		t.Fatal(e)
	}
	browser := strings.Repeat("b", 64)
	destination, e := s.Start("organization", browser, "/projects")
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(destination)
	query := u.Query()
	query.Set("code_challenge", "attacker-challenge")
	u.RawQuery = query.Encode()
	callback := authorize(t, u.String())
	if _, _, e = s.Complete(ctx, "organization", browser, callback.Get("state"), callback.Get("code")); e == nil {
		t.Fatal("wrong PKCE challenge accepted")
	}
	destination, e = s.Start("organization", browser, "/projects")
	if e != nil {
		t.Fatal(e)
	}
	callback = authorize(t, destination)
	var success atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if _, _, err := s.Complete(ctx, "organization", browser, callback.Get("state"), callback.Get("code")); err == nil {
				success.Add(1)
			}
		}()
	}
	group.Wait()
	if success.Load() != 1 {
		t.Fatal("state was not consumed exactly once")
	}
	if !s.AcceptIssuer("organization", f.server.URL) || s.AcceptIssuer("organization", "https://wrong.invalid") {
		t.Fatal("authorization response issuer mismatch")
	}
}

func TestOIDCConfigBoundaries(t *testing.T) {
	f := newIdentityFixture(t)
	ctx := context.Background()
	c := f.config("http://127.0.0.1:3109", "oidc-user")
	origins := []string{"http://127.0.0.1:3109"}
	if _, e := oidcauth.New(ctx, []oidcauth.ProviderConfig{c}, origins, false); e == nil {
		t.Fatal("production HTTP allowed")
	}
	bad := c
	bad.RedirectURI = "http://127.0.0.1:3109/other"
	if _, e := oidcauth.New(ctx, []oidcauth.ProviderConfig{bad}, origins, true); e == nil {
		t.Fatal("unregistered callback allowed")
	}
	if _, e := oidcauth.New(ctx, []oidcauth.ProviderConfig{c, c}, origins, true); e == nil {
		t.Fatal("duplicate provider allowed")
	}
	path := filepath.Join(t.TempDir(), "providers.json")
	raw, _ := json.Marshal([]oidcauth.ProviderConfig{c})
	if e := os.WriteFile(path, raw, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := oidcauth.Load(ctx, path, origins, true); e != nil {
		t.Fatal(e)
	}
	if e := os.Chmod(path, 0644); e != nil {
		t.Fatal(e)
	}
	if _, e := oidcauth.Load(ctx, path, origins, true); e == nil {
		t.Fatal("public config allowed")
	}
	os.Chmod(path, 0600)
	link := filepath.Join(t.TempDir(), "link")
	if e := os.Symlink(path, link); e != nil {
		t.Fatal(e)
	}
	if _, e := oidcauth.Load(ctx, link, origins, true); e == nil {
		t.Fatal("symlink allowed")
	}
	if s, e := oidcauth.Load(ctx, "", origins, true); e != nil || s != nil {
		t.Fatal("disabled provider not opt-in")
	}
}
