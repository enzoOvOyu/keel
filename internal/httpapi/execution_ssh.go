package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerSSHExecution(r *gin.Engine, base string) {
	r.POST(base+"/ssh-executions", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.SSHCreateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		x, e := s.CreateSSH(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, x)
	})
	r.POST(base+"/executions/:executionId/attempts/:attemptId/ssh-receipt", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in struct {
			ExpectedVersion int64 `json:"expected_version"`
		}
		if !decodeExecutionBody(c, &in) {
			return
		}
		x, e := s.FetchSSHReceipt(c.Request.Context(), actor, c.Param("id"), c.Param("executionId"), c.Param("attemptId"), in.ExpectedVersion, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, x)
	})
}
