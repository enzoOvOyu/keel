package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerProvisionPrewriteRecovery(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/provision/installations/:installationId/prewrite-reconcile", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.ProvisionRequestActionInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := service.ReconcileProvisionBeforeWrite(c.Request.Context(), actor, c.Param("id"), c.Param("installationId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
}
