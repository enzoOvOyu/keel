package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const sessionCookie = "keel_session"

type authAPI struct {
	store       *platform.Store
	config      platform.Config
	mu          sync.Mutex
	preRequests map[string]preRate
}
type preRate struct {
	count int
	start time.Time
}

func (a *authAPI) respondError(c *gin.Context, err error) {
	var e *platform.Error
	if errors.As(err, &e) {
		if e.RetryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(e.RetryAfter))
		}
		failure(c, e.Status, e.Code, e.Message)
		return
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		slog.Error("request storage operation failed", "trace_id", c.GetString("trace_id"), "sqlstate", pg.Code, "detail", err.Error())
	} else {
		slog.Error("request storage operation failed", "trace_id", c.GetString("trace_id"), "detail", err.Error())
	}
	failure(c, 503, "SERVICE_UNAVAILABLE", "Service temporarily unavailable")
}
func (a *authAPI) cookie(c *gin.Context, token string, expiry time.Time) {
	maxAge := int(time.Until(expiry).Seconds())
	if token == "" {
		maxAge = -1
		expiry = time.Unix(1, 0)
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, Secure: !a.config.InsecureDevCookie, SameSite: http.SameSiteLaxMode, Expires: expiry, MaxAge: maxAge})
}
func cookieToken(c *gin.Context) string { v, _ := c.Cookie(sessionCookie); return v }
func (a *authAPI) origin(c *gin.Context) bool {
	if !a.config.AllowedHost(c.Request.Host) || !a.config.AllowedOrigin(c.GetHeader("Origin")) {
		failure(c, 403, "ORIGIN_FORBIDDEN", "Request origin is not allowed")
		return false
	}
	return true
}
func (a *authAPI) host(c *gin.Context) bool {
	if !a.config.AllowedHost(c.Request.Host) {
		failure(c, 403, "ORIGIN_FORBIDDEN", "Request host is not allowed")
		return false
	}
	return true
}
func (a *authAPI) authenticated(c *gin.Context) (*platform.AuthSession, bool) {
	if !a.host(c) {
		return nil, false
	}
	s, e := a.store.Authenticate(c.Request.Context(), cookieToken(c))
	if e != nil {
		a.respondError(c, e)
		return nil, false
	}
	return s, true
}
func (a *authAPI) writeAllowed(c *gin.Context) bool {
	if !a.origin(c) {
		return false
	}
	if e := a.store.CheckCSRF(c.Request.Context(), cookieToken(c), c.GetHeader("X-CSRF-Token")); e != nil {
		a.respondError(c, e)
		return false
	}
	return true
}
func decodeBody(c *gin.Context, target any) bool {
	kind, _, e := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if e != nil || kind != "application/json" {
		failure(c, 415, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	e = decoder.Decode(target)
	if e == nil {
		var extra any
		if decoder.Decode(&extra) != io.EOF {
			e = errors.New("multiple JSON values")
		}
	}
	if e != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(e, &tooLarge) {
			failure(c, 413, "REQUEST_TOO_LARGE", "Request body exceeds 64 KiB")
		} else {
			failure(c, 400, "VALIDATION_ERROR", "Invalid JSON body or unknown fields")
		}
		return false
	}
	return true
}
func sourceIP(c *gin.Context) string {
	ip, _, e := net.SplitHostPort(c.Request.RemoteAddr)
	if e != nil {
		return "unknown"
	}
	if parsed := net.ParseIP(ip); parsed != nil {
		return parsed.String()
	}
	return "unknown"
}
func (a *authAPI) allowPre(c *gin.Context) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	ip := sourceIP(c)
	for k, v := range a.preRequests {
		if now.Sub(v.start) >= time.Minute {
			delete(a.preRequests, k)
		}
	}
	v, ok := a.preRequests[ip]
	if !ok {
		if len(a.preRequests) >= 10000 {
			return false
		}
		v = preRate{start: now}
	}
	v.count++
	a.preRequests[ip] = v
	return v.count <= 30
}
func (a *authAPI) register(r *gin.Engine) {
	r.GET("/api/v1/auth/providers", func(c *gin.Context) {
		yes, e := a.store.HasAdministrator(c.Request.Context())
		if e != nil {
			a.respondError(c, e)
			return
		}
		status := "setup_required"
		methods := []string{}
		if yes {
			status = "ready"
			methods = append(methods, "local")
		}
		c.JSON(200, gin.H{"status": status, "methods": methods, "environment": a.config.Environment})
	})
	r.GET("/api/v1/auth/csrf", func(c *gin.Context) {
		if !a.host(c) {
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" && !a.config.AllowedOrigin(origin) {
			failure(c, 403, "ORIGIN_FORBIDDEN", "Request origin is not allowed")
			return
		}
		if strings.EqualFold(c.GetHeader("Sec-Fetch-Site"), "cross-site") {
			failure(c, 403, "ORIGIN_FORBIDDEN", "Cross-site authentication request denied")
			return
		}
		token := cookieToken(c)
		expiry, e := a.store.CSRFSession(c.Request.Context(), token)
		if e != nil {
			var pe *platform.Error
			if !errors.As(e, &pe) || pe.Status != 401 {
				a.respondError(c, e)
				return
			}
			if !a.allowPre(c) {
				c.Header("Retry-After", "60")
				failure(c, 429, "RATE_LIMITED", "Too many authentication sessions")
				return
			}
			token, expiry, e = a.store.NewPreSession(c.Request.Context())
			if e != nil {
				a.respondError(c, e)
				return
			}
			a.cookie(c, token, expiry)
		}
		c.JSON(200, gin.H{"csrf_token": platform.CSRFToken(token), "expires_at": expiry})
	})
	r.POST("/api/v1/auth/login", func(c *gin.Context) {
		if !a.origin(c) {
			return
		}
		var input platform.LoginInput
		if !decodeBody(c, &input) {
			return
		}
		ready, e := a.store.HasAdministrator(c.Request.Context())
		if e != nil {
			a.respondError(c, e)
			return
		}
		if !ready {
			failure(c, 503, "SETUP_REQUIRED", "Create the first administrator on the server")
			return
		}
		token, session, e := a.store.Login(c.Request.Context(), cookieToken(c), c.GetHeader("X-CSRF-Token"), input, sourceIP(c), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		body, e := a.store.SessionResponse(c.Request.Context(), session)
		if e != nil {
			a.respondError(c, e)
			return
		}
		// Cookie lifetime follows the absolute bound; server enforces the shorter idle bound.
		a.cookie(c, token, time.Now().UTC().Add(platform.AbsoluteTimeout))
		c.JSON(200, body)
	})
	r.GET("/api/v1/auth/session", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		body, e := a.store.SessionResponse(c.Request.Context(), s)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, body)
	})
	r.POST("/api/v1/auth/logout", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		if e := a.store.Logout(c.Request.Context(), s, c.GetString("trace_id")); e != nil {
			a.respondError(c, e)
			return
		}
		a.cookie(c, "", time.Time{})
		c.Status(204)
	})
	r.GET("/api/v1/projects", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		result, e := a.store.ListProjectsPage(c.Request.Context(), s, page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.GET("/api/v1/projects/:id", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), s, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, p)
	})
	r.POST("/api/v1/projects", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.CreateProjectInput
		if !decodeBody(c, &input) {
			return
		}
		p, e := a.store.CreateProject(c.Request.Context(), s, input, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.Header("Location", "/api/v1/projects/"+p.ID)
		c.JSON(201, p)
	})
}
