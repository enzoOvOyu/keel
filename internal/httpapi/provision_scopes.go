package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerProvisionScopes(r *gin.Engine) {
	const base = "/api/v1/projects/:id/provision/boot-scopes"
	r.GET(base, func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		out, err := a.store.ListProvisionBootScopes(c.Request.Context(), actor, c.Param("id"), p)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
	r.GET(base+"/:scopeId", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, err := a.store.ReadProvisionBootScope(c.Request.Context(), actor, c.Param("id"), c.Param("scopeId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
	r.POST(base, func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.CreateProvisionBootScopeInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := a.store.CreateProvisionBootScope(c.Request.Context(), actor, c.Param("id"), in, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(201, out)
	})
	for _, action := range []string{"approve", "revoke"} {
		r.POST(base+"/:scopeId/"+action, func(c *gin.Context) {
			actor, _, ok := a.executionProject(c, true)
			if !ok {
				return
			}
			var in platform.ProvisionBootScopeActionInput
			if !decodeExecutionBody(c, &in) {
				return
			}
			out, err := a.store.ChangeProvisionBootScope(c.Request.Context(), actor, c.Param("id"), c.Param("scopeId"), action, in, c.GetString("trace_id"))
			if err != nil {
				a.respondError(c, err)
				return
			}
			c.Header("Cache-Control", "private, no-store")
			c.JSON(200, out)
		})
	}
}
