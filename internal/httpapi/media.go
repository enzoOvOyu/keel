package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

// Media registry and private version-pinned artifact routes.
func (a *authAPI) registerMedia(r *gin.Engine) {
	a.registerMediaScope(r, "/api/v1/projects/:id/media", false)
	a.registerMediaScope(r, "/api/v1/admin/media", true)
}

func (a *authAPI) registerMediaScope(r *gin.Engine, base string, global bool) {
	a.registerMediaArtifacts(r, base, global)
	r.GET(base, func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, c.Request.Method != "GET", global)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		result, e := a.store.ListMedia(c.Request.Context(), actor, project, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.POST(base, func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, c.Request.Method != "GET", global)
		if !ok {
			return
		}
		var input platform.RegisterMediaInput
		if !decodeBody(c, &input) {
			return
		}
		result, e := a.store.RegisterMedia(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, result)
	})
	r.POST(base+"/:mediaId/transition", func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, c.Request.Method != "GET", global)
		if !ok {
			return
		}
		var input struct {
			State string `json:"state"`
		}
		if !decodeBody(c, &input) {
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		result, e := a.store.TransitionMedia(c.Request.Context(), actor, project, c.Param("mediaId"), input.State, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
}
