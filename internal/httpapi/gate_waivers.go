package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerGateWaivers(r *gin.Engine) {
	base := "/api/v1/projects/:id/assets/:assetId/gates/G1/waivers"
	r.GET(base+"/scope", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.GateWaiverScope(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListGateWaivers(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.GateWaiverInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := a.store.ApproveGateWaiver(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/:waiverId/revoke", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.GateWaiverRevocationInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		if e := a.store.RevokeGateWaiver(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("waiverId"), in.Reason, c.GetString("trace_id")); e != nil {
			a.respondError(c, e)
			return
		}
		c.Status(204)
	})
}
