package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerTopology(r *gin.Engine) {
	base := "/api/v1/projects/:id/assets/:assetId/topology"
	r.GET("/api/v1/projects/:id/validation/topology/fleet", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.TopologyFleet(c.Request.Context(), actor, c.Param("id"), c.Query("verdict"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:snapshotId/relations", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.TopologyRelations(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("snapshotId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET("/api/v1/issues/:issueId/topology-retests/scope", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.PreviewTopologyRetest(c.Request.Context(), actor, c.Param("issueId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET("/api/v1/issues/:issueId/topology-retests", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListTopologyRetests(c.Request.Context(), actor, c.Param("issueId"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST("/api/v1/issues/:issueId/topology-retests", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.TopologyRetestInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := a.store.CreateTopologyRetest(c.Request.Context(), actor, c.Param("issueId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
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
