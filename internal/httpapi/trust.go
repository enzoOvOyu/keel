package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerSshTrust wires the mock trust channel (WP-1.5, FR-1.2). Actions
// need project-admin (model:manage level); the log listing is readable by
// every project member.
func (a *authAPI) registerSshTrust(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/trust/actions", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.TrustActionInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		results, e := a.store.RunTrustAction(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"results": results})
	})
	r.GET("/api/v1/projects/:id/trust/logs", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		pagination, ok := readPagination(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		logs, e := a.store.ListTrustLogs(c.Request.Context(), actor, project, pagination)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, logs)
	})
}
