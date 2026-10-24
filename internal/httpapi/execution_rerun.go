package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerExecutionRerun(r *gin.Engine) {
	base := "/api/v1/projects/:id/executions/:executionId/rerun"
	r.POST(base+"/preview", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in struct {
			Mode      string   `json:"mode"`
			TargetIDs []string `json:"target_ids,omitempty"`
		}
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := s.RerunCandidates(c.Request.Context(), actor, c.Param("id"), c.Param("executionId"), in.Mode, in.TargetIDs)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base, func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.RerunInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		out, e := s.Rerun(c.Request.Context(), actor, c.Param("id"), c.Param("executionId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
}
