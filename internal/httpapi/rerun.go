package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/execution"
)

// registerRerun wires the FR-1.4 rerun endpoint (WP-1.4): single targets or
// the failed-only set of a terminal execution re-open with fresh attempt
// chains under the same circuit breaker.
func (a *authAPI) registerRerun(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/executions/:executionId/rerun", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var input execution.RerunInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		updated, e := service.Rerun(c.Request.Context(), actor, c.Param("id"), c.Param("executionId"), input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"execution": updated})
	})
}
