package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Authenticated planning preview only. The public device boot endpoint will
// have its own channel-specific transport authentication and isolation rules.
func (a *authAPI) registerZTPIdentityPreview(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/ztp/identity/preview", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.ZTPIdentityInput
		if !decodeBody(c, &input) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ResolveZTPIdentity(c.Request.Context(), actor, project, input)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
}
