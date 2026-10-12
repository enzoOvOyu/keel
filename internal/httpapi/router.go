package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"net/http"
	"path"
	"strings"
)

// New serves only foundation metadata and static UI. Domain APIs remain denied
// until the session/project authorization implementation is delivered.
func New(version, staticDir string) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.HandleMethodNotAllowed = true
	r.Use(func(c *gin.Context) {
		var id [16]byte
		if _, err := rand.Read(id[:]); err != nil {
			c.AbortWithStatus(503)
			return
		}
		c.Set("trace_id", hex.EncodeToString(id[:]))
		c.Header("X-Request-ID", hex.EncodeToString(id[:]))
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Header("Cache-Control", "no-store")
		}
		c.Next()
	})
	health := func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "service": "keel", "version": version, "scope": "process"})
	}
	r.GET("/healthz", health)
	r.HEAD("/healthz", func(c *gin.Context) { c.Status(200) })
	r.GET("/api/v1", func(c *gin.Context) {
		c.JSON(200, gin.H{"name": "keel", "version": version, "phase": "foundation", "authentication": "not_configured", "business_ready": false})
	})
	r.GET("/api/v1/auth/providers", func(c *gin.Context) { c.JSON(200, gin.H{"status": "not_configured", "methods": []string{}}) })
	r.POST("/api/v1/auth/login", func(c *gin.Context) { failure(c, 503, "AUTH_NOT_CONFIGURED", "Identity service is not enabled") })
	r.NoMethod(func(c *gin.Context) { failure(c, 405, "METHOD_NOT_ALLOWED", "Method not allowed") })
	fs := http.Dir(staticDir)
	server := http.FileServer(fs)
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			failure(c, 401, "UNAUTHENTICATED", "Authentication required")
			return
		}
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			failure(c, 405, "METHOD_NOT_ALLOWED", "Method not allowed")
			return
		}
		clean := path.Clean("/" + c.Request.URL.Path)
		for _, segment := range strings.Split(clean, "/") {
			if strings.HasPrefix(segment, ".") {
				c.Status(404)
				return
			}
		}
		if f, err := fs.Open(clean); err == nil {
			info, statErr := f.Stat()
			f.Close()
			if statErr == nil && !info.IsDir() {
				server.ServeHTTP(c.Writer, c.Request)
				return
			}
		}
		if strings.HasPrefix(clean, "/assets/") {
			c.Status(404)
			return
		}
		f, err := fs.Open("/index.html")
		if err != nil {
			failure(c, 503, "UI_NOT_BUILT", "Build the web workspace first")
			return
		}
		f.Close()
		c.Header("Cache-Control", "no-cache")
		c.Request.URL.Path = "/"
		server.ServeHTTP(c.Writer, c.Request)
	})
	return r
}
func failure(c *gin.Context, status int, code, message string) {
	c.JSON(status, gin.H{"code": code, "message": message, "details": []string{}, "trace_id": c.GetString("trace_id")})
}
