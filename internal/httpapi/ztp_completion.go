package httpapi

import (
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerZTPCompletion(r *gin.Engine) {
	base := "/api/v1/projects/:id/ztp"
	r.GET(base+"/management-artifacts/preview", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.PreviewZTPManagementArtifacts(c.Request.Context(), actor, p, c.Query("batch_ref"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/manageability-checks", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.ListZTPManageabilityChecks(c.Request.Context(), actor, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, gin.H{"items": out})
	})
	r.GET(base+"/device-sessions", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.ListZTPDeviceSessions(c.Request.Context(), actor, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, gin.H{"items": out})
	})
	r.GET(base+"/device-sessions/:sessionId", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.GetZTPDeviceSession(c.Request.Context(), actor, p, c.Param("sessionId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})

}
