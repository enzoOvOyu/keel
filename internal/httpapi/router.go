package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"io"
	"log/slog"
	"net/http"
	"path"
	"strings"
)

// New preserves the fail-closed preview mode when no database is configured.
func New(version, staticDir string) http.Handler {
	return newRouter(version, staticDir, nil, platform.Config{})
}

func NewWithStore(version, staticDir string, store *platform.Store, config platform.Config) http.Handler {
	if store == nil {
		panic("NewWithStore requires a store")
	}
	if err := config.Validate(); err != nil {
		panic(err)
	}
	return newRouter(version, staticDir, store, config)
}
func newRouter(version, staticDir string, store *platform.Store, config platform.Config) http.Handler {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.SetTrustedProxies(nil)
	r.Use(gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
		slog.Error("request panic", "trace_id", c.GetString("trace_id"))
		failure(c, 500, "INTERNAL_ERROR", "Internal server error")
		c.Abort()
	}))
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
		c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'nonce-"+hex.EncodeToString(id[:])+"'; img-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
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
		authentication := "not_configured"
		if store != nil {
			ready, err := store.HasAdministrator(c.Request.Context())
			if err != nil {
				failure(c, 503, "SERVICE_UNAVAILABLE", "Service temporarily unavailable")
				return
			}
			authentication = "setup_required"
			if ready {
				authentication = "ready"
			}
		}
		c.JSON(200, gin.H{"name": "keel", "version": version, "phase": "p1-foundation", "authentication": authentication, "business_ready": false})
	})
	if store == nil {
		r.GET("/api/v1/auth/providers", func(c *gin.Context) {
			c.JSON(200, gin.H{"status": "not_configured", "methods": []string{}, "environment": "development"})
		})
		r.POST("/api/v1/auth/login", func(c *gin.Context) { failure(c, 503, "AUTH_NOT_CONFIGURED", "Identity service is not enabled") })
	} else {
		auth := &authAPI{store: store, config: config, preRequests: make(map[string]preRate)}
		auth.register(r)
		auth.registerControl(r)
		auth.registerExecution(r)
		auth.registerExecutionViews(r)
		auth.registerExecutionRerun(r)
		auth.registerFlowWorkbench(r)
		auth.registerModels(r)
		auth.registerModelImports(r)
		auth.registerBomImport(r)
		auth.registerProvisionPreview(r)
		auth.registerProvisionRequests(r)
		auth.registerProvisionScopes(r)
		auth.registerProvisionInstallations(r)
		auth.registerProvisionPrewriteRecovery(r)
		auth.registerProvisionInstallationProgress(r)
		auth.registerProvisionSSHRetry(r)
		auth.registerProvisionSSHVerification(r)
		auth.registerProvisionRackRequests(r)
		auth.registerBOMMappingProfiles(r)
		auth.registerBOMPackages(r)
		auth.registerNetworkBaselines(r)
		auth.registerZTPIdentityPreview(r)
		auth.registerZTPSimSessions(r)
		auth.registerZTPManagement(r)
		auth.registerZTPEnvironment(r)
		auth.registerKeaDeployments(r)
		auth.registerFabricCollection(r)
		auth.registerFabricDiagnostics(r)
		auth.registerNetworkTemplates(r)
		auth.registerNetworkPush(r)
		auth.registerNetworkHealth(r)
		auth.registerNetworkDelivery(r)
		auth.registerZTPCompletion(r)
		auth.registerNetworkRules(r)
		auth.registerNetworkEvidence(r)
		auth.registerZTPMockKea(r)
		auth.registerZTP239Manifest(r)
		auth.registerSshTrust(r)
		auth.registerSSHTrustRequests(r)
		auth.registerMedia(r)
		auth.registerParams(r)
		auth.registerCatalogAdmin(r)
	}
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
		if clean != "/" && clean != "/index.html" {
			if f, err := fs.Open(clean); err == nil {
				info, statErr := f.Stat()
				f.Close()
				if statErr == nil && !info.IsDir() {
					server.ServeHTTP(c.Writer, c.Request)
					return
				}
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
		defer f.Close()
		index, err := io.ReadAll(io.LimitReader(f, 2*1024*1024+1))
		if err != nil || len(index) > 2*1024*1024 {
			failure(c, 503, "UI_NOT_BUILT", "UI entry is unavailable")
			return
		}
		nonce := c.GetString("trace_id")
		html := strings.Replace(string(index), "<head>", "<head><meta name=\"keel-style-nonce\" content=\""+nonce+"\">", 1)
		c.Header("Cache-Control", "no-store")
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(html))
	})
	return r
}
func failure(c *gin.Context, status int, code, message string, details ...string) {
	if details == nil {
		details = []string{}
	}
	c.JSON(status, gin.H{"code": code, "message": message, "details": details, "trace_id": c.GetString("trace_id")})
}
