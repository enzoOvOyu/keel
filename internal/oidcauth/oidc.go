// Package oidcauth keeps authorization-code secrets on the server. Provider
// membership is an explicit operator binding; email never grants an account.
package oidcauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/sys/unix"
)

var ErrDenied = errors.New("organization sign-in failed")
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
var usernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
var nextPattern = regexp.MustCompile(`^/p/[0-9a-f]{32}(/(members|audits))?$`)

type limitedBody struct {
	io.Reader
	io.Closer
}
type transport struct{ dev bool }

func (t transport) RoundTrip(r *http.Request) (*http.Response, error) {
	if !validURL(r.URL.Scheme+"://"+r.URL.Host+r.URL.Path, t.dev) {
		return nil, ErrDenied
	}
	response, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return nil, ErrDenied
	}
	if response.ContentLength > 1024*1024 {
		response.Body.Close()
		return nil, ErrDenied
	}
	response.Body = limitedBody{io.LimitReader(response.Body, 1024*1024+1), response.Body}
	return response, nil
}

type ProviderConfig struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RedirectURI  string `json:"redirect_uri"`
	// Exact subject -> existing local username. No registration or email match.
	Bindings map[string]string `json:"bindings"`
}
type PublicProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type provider struct {
	config   ProviderConfig
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
}
type attempt struct {
	provider, browser, nonce, verifier, next string
	expires                                  time.Time
}
type Service struct {
	mu        sync.Mutex
	providers map[string]*provider
	attempts  map[[32]byte]attempt
	client    *http.Client
}

// Load is deliberately opt-in. The file contains client secrets and identity
// bindings, so it is owned by the runtime UID and inaccessible to other users.
func Load(ctx context.Context, path string, origins []string, dev bool) (*Service, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, ErrDenied
	}
	fd, e := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if e != nil {
		return nil, ErrDenied
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm()&0077 != 0 || st.Size() > 1024*1024 {
		return nil, ErrDenied
	}
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Uid != uint32(os.Geteuid()) || stat.Nlink != 1 {
		return nil, ErrDenied
	}
	var configs []ProviderConfig
	d := json.NewDecoder(io.LimitReader(f, 1024*1024+1))
	d.DisallowUnknownFields()
	if d.Decode(&configs) != nil {
		return nil, ErrDenied
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, ErrDenied
	}
	return New(ctx, configs, origins, dev)
}

func validURL(raw string, dev bool) bool {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Opaque != "" {
		return false
	}
	return u.Scheme == "https" || (dev && u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"))
}

func New(ctx context.Context, configs []ProviderConfig, origins []string, dev bool) (*Service, error) {
	if len(configs) == 0 || len(configs) > 8 {
		return nil, ErrDenied
	}
	s := &Service{providers: map[string]*provider{}, attempts: map[[32]byte]attempt{}, client: &http.Client{Transport: transport{dev: dev}, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrDenied }}}
	for _, c := range configs {
		if !idPattern.MatchString(c.ID) || s.providers[c.ID] != nil || len(c.Name) == 0 || len(c.Name) > 80 || strings.ContainsAny(c.Name, "\r\n\x00") || !validURL(c.Issuer, dev) || !validURL(c.RedirectURI, dev) || len(c.ClientID) == 0 || len(c.ClientID) > 512 || len(c.ClientSecret) > 8192 || len(c.Bindings) > 10000 {
			return nil, ErrDenied
		}
		u, _ := url.Parse(c.RedirectURI)
		allowed := false
		for _, o := range origins {
			allowed = allowed || o == u.Scheme+"://"+u.Host
		}
		if !allowed || u.Path != "/api/v1/auth/oidc/"+c.ID+"/callback" {
			return nil, ErrDenied
		}
		bindings := make(map[string]string, len(c.Bindings))
		for subject, name := range c.Bindings {
			if len(subject) == 0 || len(subject) > 255 || !usernamePattern.MatchString(name) {
				return nil, ErrDenied
			}
			bindings[subject] = name
		}
		c.Bindings = bindings
		work, cancel := context.WithTimeout(oidc.ClientContext(ctx, s.client), 10*time.Second)
		remote, e := oidc.NewProvider(work, c.Issuer)
		cancel()
		if e != nil {
			return nil, ErrDenied
		}
		endpoint := remote.Endpoint()
		var discovery struct {
			JWKS string `json:"jwks_uri"`
		}
		if remote.Claims(&discovery) != nil || !validURL(discovery.JWKS, dev) || !validURL(endpoint.AuthURL, dev) || !validURL(endpoint.TokenURL, dev) {
			return nil, ErrDenied
		}
		// Keep the verifier's key-set context alive beyond startup discovery.
		verifier := remote.VerifierContext(oidc.ClientContext(context.Background(), s.client), &oidc.Config{ClientID: c.ClientID, SupportedSigningAlgs: []string{"RS256", "ES256", "EdDSA"}})
		s.providers[c.ID] = &provider{config: c, oauth: oauth2.Config{ClientID: c.ClientID, ClientSecret: c.ClientSecret, RedirectURL: c.RedirectURI, Endpoint: endpoint, Scopes: []string{oidc.ScopeOpenID}}, verifier: verifier}
	}
	return s, nil
}
func (s *Service) Public() []PublicProvider {
	out := []PublicProvider{}
	if s == nil {
		return out
	}
	for _, p := range s.providers {
		out = append(out, PublicProvider{p.config.ID, p.config.Name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Service) Has(id string) bool { return s != nil && s.providers[id] != nil }
func (s *Service) AcceptIssuer(id, issuer string) bool {
	return s.Has(id) && (issuer == "" || issuer == s.providers[id].config.Issuer)
}
func (s *Service) Abort(id, browser, state string) {
	if s == nil || len(state) != 64 {
		return
	}
	key := sha256.Sum256([]byte(state))
	s.mu.Lock()
	defer s.mu.Unlock()
	if a, ok := s.attempts[key]; ok && a.provider == id && subtle.ConstantTimeCompare([]byte(a.browser), []byte(browser)) == 1 {
		delete(s.attempts, key)
	}
}
func random() (string, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", ErrDenied
	}
	return hex.EncodeToString(b[:]), nil
}

// SafeNext only offers currently implemented routes. Authorization is still
// checked at the destination, using the newly established current session.
func SafeNext(next string) string {
	if next == "/projects" || next == "/account/security" || next == "/admin/users" || next == "/admin/audits" {
		return next
	}
	if nextPattern.MatchString(next) {
		return next
	}
	return "/projects"
}

func (s *Service) Start(id, browser, next string) (string, error) {
	if s == nil || len(browser) != 64 {
		return "", ErrDenied
	}
	p := s.providers[id]
	if p == nil {
		return "", ErrDenied
	}
	state, e := random()
	if e != nil {
		return "", e
	}
	nonce, e := random()
	if e != nil {
		return "", e
	}
	verifier := oauth2.GenerateVerifier()
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, a := range s.attempts {
		if !a.expires.After(now) {
			delete(s.attempts, key)
		}
	}
	if len(s.attempts) >= 4096 {
		return "", ErrDenied
	}
	s.attempts[sha256.Sum256([]byte(state))] = attempt{id, browser, nonce, verifier, SafeNext(next), now.Add(5 * time.Minute)}
	return p.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

// Complete consumes matching state before any token exchange. Wrong-browser
// callbacks cannot consume somebody else's attempt. Secrets are never returned.
func (s *Service) Complete(ctx context.Context, id, browser, state, code string) (username, next string, err error) {
	if s == nil || len(state) != 64 || len(code) == 0 || len(code) > 4096 {
		return "", "", ErrDenied
	}
	key := sha256.Sum256([]byte(state))
	s.mu.Lock()
	a, ok := s.attempts[key]
	if !ok || a.provider != id || subtle.ConstantTimeCompare([]byte(a.browser), []byte(browser)) != 1 {
		s.mu.Unlock()
		return "", "", ErrDenied
	}
	delete(s.attempts, key)
	s.mu.Unlock()
	if !a.expires.After(time.Now()) {
		return "", "", ErrDenied
	}
	p := s.providers[id]
	if p == nil {
		return "", "", ErrDenied
	}
	ctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, s.client), 10*time.Second)
	defer cancel()
	token, e := p.oauth.Exchange(ctx, code, oauth2.VerifierOption(a.verifier))
	if e != nil {
		return "", "", ErrDenied
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok || len(raw) > 64*1024 {
		return "", "", ErrDenied
	}
	identity, e := p.verifier.Verify(ctx, raw)
	if e != nil || subtle.ConstantTimeCompare([]byte(identity.Nonce), []byte(a.nonce)) != 1 || identity.Subject == "" || len(identity.Subject) > 255 {
		return "", "", ErrDenied
	}
	var claims struct {
		AuthorizedParty string `json:"azp"`
	}
	if identity.Claims(&claims) != nil || (len(identity.Audience) > 1 && claims.AuthorizedParty != p.config.ClientID) || (claims.AuthorizedParty != "" && claims.AuthorizedParty != p.config.ClientID) {
		return "", "", ErrDenied
	}
	if e = identity.VerifyAccessToken(token.AccessToken); identity.AccessTokenHash != "" && e != nil {
		return "", "", ErrDenied
	}
	name := p.config.Bindings[identity.Subject]
	if name == "" {
		return "", "", ErrDenied
	}
	return name, a.next, nil
}
