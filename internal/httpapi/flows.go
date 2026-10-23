package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerFlowWorkbench(r *gin.Engine) {
	base := "/api/v1/projects/:id/flows"
	r.POST(base+"/validate", func(c *gin.Context) {
		_, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.FlowValidationInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := execution.ValidateFlow(in)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:flowId/versions/:versionId", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := s.FlowRevision(c.Request.Context(), c.Param("id"), c.Param("flowId"), c.Param("versionId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:flowId/diff", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := s.FlowDiff(c.Request.Context(), c.Param("id"), c.Param("flowId"), c.Query("before"), c.Query("after"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/:flowId/rollback", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.RollbackFlowInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := s.RollbackFlow(c.Request.Context(), actor, c.Param("id"), c.Param("flowId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
}
