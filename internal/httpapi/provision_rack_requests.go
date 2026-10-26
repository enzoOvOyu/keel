package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerProvisionRackRequests(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/provision/rack-requests", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.CreateProvisionRackRequestsInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		out, err := a.store.CreateProvisionRackRequests(c.Request.Context(), actor, project, in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(201, out)
	})
}
