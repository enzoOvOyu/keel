package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerTopologyFirmwareMock(r *gin.Engine) {
	base := "/api/v1/issues/:issueId/firmware-mock"
	r.GET(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.TopologyFirmwareWorkspace(c.Request.Context(), actor, c.Param("issueId"))
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
		var in platform.TopologyFirmwareInput
		if !decodeBody(c, &in) {
			return
		}
		out, e := a.store.CreateTopologyFirmwarePlan(c.Request.Context(), actor, c.Param("issueId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
	r.POST(base+"/:planId/approve", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.TopologyFirmwareApproval
		if !decodeBody(c, &in) {
			return
		}
		out, e := a.store.ApproveTopologyFirmwarePlan(c.Request.Context(), actor, c.Param("issueId"), c.Param("planId"), in, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/:planId/executions", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.TopologyFirmwareApproval
		if !decodeBody(c, &in) {
			return
		}
		detail, e := a.store.GetIssue(c.Request.Context(), actor, c.Param("issueId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		service := execution.New(a.store.DB)
		service.IssueStore = a.store
		out, e := service.CreateTopologyFirmwareExecution(c.Request.Context(), actor, detail.Issue.ProjectID, c.Param("issueId"), c.Param("planId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(202, out)
	})
}
