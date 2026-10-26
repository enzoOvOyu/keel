package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerProvisionSSHRetry(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/provision/installations/:installationId/ssh-repair/query", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.ProvisionSSHRetryInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := a.store.RequestProvisionSSHRetry(c.Request.Context(), actor, c.Param("id"), c.Param("installationId"), in, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(202, out)
	})
}
