package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerTopology(r *gin.Engine) {
	base := "/api/v1/projects/:id/assets/:assetId/topology"
	r.GET(base+"/scope", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.PreviewTopology(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/validate", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.TopologyValidateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := a.store.ValidateTopology(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base, func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListTopology(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:snapshotId", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.GetTopology(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("snapshotId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
