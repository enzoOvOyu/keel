package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

// User control-plane routes use the existing session/project/CSRF boundary.
// Boot enrollment/reporting is intentionally a separate signed protocol; no
// bootstrap credential or public boot callback is exposed on these routes.
func (a *authAPI) registerProvisionInstallations(r *gin.Engine) {
	const base = "/api/v1/projects/:id/provision/installations"
	r.POST("/api/v1/projects/:id/provision/requests/:requestId/install", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.CreateProvisionInstallationInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := service.CreateProvisionInstallation(c.Request.Context(), actor, c.Param("id"), c.Param("requestId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(201, out)
	})
	r.GET(base, func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		out, err := a.store.ListProvisionInstallations(c.Request.Context(), actor, c.Param("id"), p)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
	r.GET(base+"/:installationId", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, err := a.store.ReadProvisionInstallation(c.Request.Context(), actor, c.Param("id"), c.Param("installationId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
	r.POST(base+"/:installationId/storage", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.SelectProvisionStorageInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := a.store.SelectProvisionStorage(c.Request.Context(), actor, c.Param("id"), c.Param("installationId"), in, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
	r.POST(base+"/:installationId/storage/approve", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.ProvisionRequestActionInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := a.store.ApproveProvisionStorage(c.Request.Context(), actor, c.Param("id"), c.Param("installationId"), in, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
}
