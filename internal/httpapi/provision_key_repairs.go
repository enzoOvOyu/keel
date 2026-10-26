package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerProvisionKeyRepairs(r *gin.Engine) {
	base := "/api/v1/projects/:id/provision/installations/:installationId/key-repairs"
	r.POST(base, func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.ProvisionKeyRepairInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := a.store.RequestProvisionKeyRepair(c.Request.Context(), actor, c.Param("id"), c.Param("installationId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(201, out)
	})
	for _, action := range []string{"approve", "revoke", "query"} {
		r.POST(base+"/:repairId/"+action, func(c *gin.Context) {
			actor, _, ok := a.executionProject(c, true)
			if !ok {
				return
			}
			var in platform.ProvisionKeyRepairActionInput
			if !decodeExecutionBody(c, &in) {
				return
			}
			out, err := a.store.ActProvisionKeyRepair(c.Request.Context(), actor, c.Param("id"), c.Param("installationId"), c.Param("repairId"), action, in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
			if err != nil {
				a.respondError(c, err)
				return
			}
			c.Header("Cache-Control", "private, no-store")
			c.JSON(200, out)
		})
	}
}
