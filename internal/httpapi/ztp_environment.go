package httpapi

import (
	"context"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"net/http"
	"time"
)

func (a *authAPI) registerZTPEnvironment(r *gin.Engine) {
	base := "/api/v1/projects/:id/ztp/environment-checks"
	r.POST(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var input platform.ZTPEnvironmentInput
		if !decodeBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.RunZTPEnvironmentCheck(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, out)
	})
	r.GET(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.LatestZTPEnvironmentCheck(c.Request.Context(), actor, project, platform.ZTPEnvironmentInput{BatchRef: c.Query("batch_ref"), BootChannel: c.Query("boot_channel")})
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, out)
	})
}

// Only installed dedicated TLS listeners expose this readiness contract. This
// checks the process/schema and channel; it never claims device readiness.
func serveBootReadiness(store *platform.Store, capability string, w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/readyz" {
		return false
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", 405)
		return true
	}
	if r.TLS == nil {
		http.Error(w, "TLS required", 403)
		return true
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Second)
	defer cancel()
	if store.CheckMigrations(ctx) != nil {
		http.Error(w, "not ready", 503)
		return true
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if capability == "ztp239" {
		_, _ = w.Write([]byte(`{"service":"keel-ztp","status":"ready","capabilities":["ztp239"]}`))
	} else {
		_, _ = w.Write([]byte(`{"service":"keel-ztp","status":"ready","capabilities":["sztp143"]}`))
	}
	return true
}
