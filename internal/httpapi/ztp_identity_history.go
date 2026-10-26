package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerZTPIdentityHistory(r *gin.Engine) {
	path := "/api/v1/projects/:id/ztp/identity-history"
	r.GET(path, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ListZTPIdentityHistory(c.Request.Context(), actor, project)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, out)
	})
	r.POST(path, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.ZTPIdentityHistoryInput
		if !decodeBody(c, &in) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.RecordZTPIdentityHistory(c.Request.Context(), actor, project, in, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(201, out)
	})
}
