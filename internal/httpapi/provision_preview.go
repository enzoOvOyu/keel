package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerProvisionPreview(r *gin.Engine) {
	const base = "/api/v1/projects/:id/assets/:assetId/provision"
	r.GET(base+"/target", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		target, err := a.store.GetProvisionTarget(c.Request.Context(), actor, project, c.Param("assetId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, target)
	})
	r.POST(base+"/preview", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var input platform.ProvisionPreviewInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		preview, err := a.store.PreviewProvisionSeed(c.Request.Context(), actor, project, c.Param("assetId"), input, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, preview)
	})
}
