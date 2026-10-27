package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerBMCCredentialVerification(r *gin.Engine) {
	base := "/api/v1/projects/:id/bmc/credential-batches"
	r.GET(base, func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListBMCCredentialBatches(c.Request.Context(), actor, c.Param("id"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:batchRef/verification-scope", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.PreviewBMCCredentialVerification(c.Request.Context(), actor, c.Param("id"), c.Param("batchRef"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/:batchRef/verifications", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		service.BMCStore = a.store
		var in execution.BMCCredentialVerificationInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := service.CreateBMCCredentialVerification(c.Request.Context(), actor, c.Param("id"), c.Param("batchRef"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(202, out)
	})
	r.GET(base+"/:batchRef/verifications", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListBMCCredentialVerifications(c.Request.Context(), actor, c.Param("id"), c.Param("batchRef"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:batchRef/verifications/:verificationId", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.GetBMCCredentialVerification(c.Request.Context(), actor, c.Param("id"), c.Param("batchRef"), c.Param("verificationId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
