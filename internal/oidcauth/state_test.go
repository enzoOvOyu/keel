package oidcauth

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestExpiredAndBoundedAuthorizationState(t *testing.T) {
	s := &Service{providers: map[string]*provider{"organization": {oauth: oauth2.Config{Endpoint: oauth2.Endpoint{AuthURL: "https://identity.example.invalid/authorize"}}}}, attempts: map[[32]byte]attempt{}}
	state, browser := strings.Repeat("a", 64), strings.Repeat("b", 64)
	s.attempts[sha256.Sum256([]byte(state))] = attempt{provider: "organization", browser: browser, expires: time.Now().Add(-time.Second)}
	if _, _, err := s.Complete(context.Background(), "organization", browser, state, "code"); err == nil {
		t.Fatal("expired state accepted")
	}
	if len(s.attempts) != 0 {
		t.Fatal("expired callback did not consume state")
	}
	for i := 0; i < 4096; i++ {
		s.attempts[sha256.Sum256([]byte(fmt.Sprint(i)))] = attempt{expires: time.Now().Add(time.Minute)}
	}
	if _, err := s.Start("organization", browser, "/projects"); err == nil {
		t.Fatal("unbounded pre-auth memory admitted")
	}
	for key, a := range s.attempts {
		a.expires = time.Now().Add(-time.Second)
		s.attempts[key] = a
	}
	if _, err := s.Start("organization", browser, "/projects"); err != nil || len(s.attempts) != 1 {
		t.Fatal("expired state was not reclaimed")
	}
}
