package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFoundationBoundary(t *testing.T) {
	h := New("test-version", t.TempDir())
	cases := []struct {
		method, path string
		status       int
		contains     string
	}{
		{"GET", "/healthz", 200, "test-version"},
		{"POST", "/healthz", 405, "METHOD_NOT_ALLOWED"},
		{"GET", "/api/v1/auth/providers", 200, "not_configured"},
		{"POST", "/api/v1/auth/login", 503, "AUTH_NOT_CONFIGURED"},
		{"GET", "/api/v1/projects", 401, "UNAUTHENTICATED"},
		{"POST", "/api/v1/cleanup/runs", 401, "UNAUTHENTICATED"},
		{"GET", "/.env", 404, ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.contains) {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if w.Header().Get("X-Request-ID") == "" {
				t.Fatal("missing request correlation")
			}
			if w.Header().Get("Set-Cookie") != "" {
				t.Fatal("unconfigured auth must not issue session")
			}
		})
	}
}
