package platform

import "testing"

func TestCookieConfigurationFailsClosed(t *testing.T) {
	cases := []struct {
		c  Config
		ok bool
	}{
		{Config{Environment: "development", PublicOrigins: []string{"http://localhost:3000"}, InsecureDevCookie: true}, true},
		{Config{Environment: "development", PublicOrigins: []string{"http://127.0.0.1:3001", "http://localhost:5173"}, InsecureDevCookie: true}, true},
		{Config{Environment: "production", PublicOrigins: []string{"https://keel.example.com"}}, true},
		{Config{Environment: "production", PublicOrigins: []string{"http://localhost:3000"}, InsecureDevCookie: true}, false},
		{Config{Environment: "development", PublicOrigins: []string{"http://keel.example.com"}, InsecureDevCookie: true}, false},
		{Config{Environment: "development", PublicOrigins: []string{"http://localhost:3000/evil"}, InsecureDevCookie: true}, false},
		{Config{Environment: "production", PublicOrigins: []string{"http://keel.example.com"}}, false},
		{Config{Environment: "production", PublicOrigins: []string{"https://keel.example.com/"}}, false},
		{Config{Environment: "production", PublicOrigins: []string{"https://user:pass@keel.example.com"}}, false},
	}
	for _, tc := range cases {
		if e := tc.c.Validate(); (e == nil) != tc.ok {
			t.Errorf("config %#v: %v", tc.c, e)
		}
	}
}
func TestPasswordAndTokenBoundaries(t *testing.T) {
	hash, e := HashPassword("test-only-long-password")
	if e != nil {
		t.Fatal(e)
	}
	if !VerifyPassword(hash, "test-only-long-password") || VerifyPassword(hash, "incorrect") {
		t.Fatal("password verification failed")
	}
	if VerifyPassword("$argon2id$v=19$m=999999999,t=3,p=4$bad$bad", "anything") {
		t.Fatal("unbounded hash parameters accepted")
	}
	if _, e := HashPassword("short"); e == nil {
		t.Fatal("short password accepted")
	}
	token, e := randomToken()
	if e != nil {
		t.Fatal(e)
	}
	if TokenHash(token) == CSRFToken(token) || TokenHash(token) == token || !validToken(token) {
		t.Fatal("token domain separation failed")
	}
}
