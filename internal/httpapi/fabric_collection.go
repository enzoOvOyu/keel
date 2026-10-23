package httpapi

import (
	"context"
	"github.com/gin-gonic/gin"
	"time"
)

func (a *authAPI) registerFabricCollection(r *gin.Engine) {
	base := "/api/v1/projects/:id/fabric"
	r.GET(base+"/collections/latest", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.LatestFabricCollection(c.Request.Context(), actor, project, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		inventory, e := a.store.FabricInventory(c.Request.Context(), actor, project)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, gin.H{"configured": a.store.FabricProbes.CollectorConfigured(), "report": out, "inventory": inventory})
	})
	r.POST(base+"/collections", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		out, e := a.store.CollectFabric(ctx, actor, project, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
	r.GET(base+"/devices/:deviceId/evidence", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.FabricDeviceEvidence(c.Request.Context(), actor, project, c.Param("deviceId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
