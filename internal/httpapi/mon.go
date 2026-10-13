package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerMon wires the monitoring data plane (WP-2.10, FR-13). Policies
// and ingest need project-admin; alerts are readable by every member.
func (a *authAPI) registerMon(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/mon/policies", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.SetPolicyInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		policy, e := a.store.SetMonPolicy(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, policy)
	})
	r.POST("/api/v1/projects/:id/mon/ingest", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.IngestInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.IngestMonSamples(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, result)
	})
	r.GET("/api/v1/projects/:id/mon/alerts", func(c *gin.Context) {
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
		alerts, e := a.store.ListMonAlerts(c.Request.Context(), actor, project, pagination)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, alerts)
	})
}
