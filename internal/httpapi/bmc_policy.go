package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerBMCPolicies(r *gin.Engine) {
	base := "/api/v1/projects/:id/assets/:assetId/bmc/policies"
	r.GET(base, func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := service.ListBMCPolicies(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	save := func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.BMCPolicyInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := service.SaveBMCPolicy(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("policyId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	}
	r.POST(base, save)
	r.PUT(base+"/:policyId", save)
	r.GET(base+"/:policyId/runs", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := service.ListBMCPolicyRuns(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("policyId"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
