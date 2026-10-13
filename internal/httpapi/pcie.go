package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerPcie wires the topology verification channel (WP-2.12). Submit
// needs project-admin; diffs are readable by every member.
func (a *authAPI) registerPcie(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/pcie/snapshots", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.SubmitPcieInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.SubmitPcieSnapshot(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, result)
	})
	r.GET("/api/v1/projects/:id/pcie/diffs", func(c *gin.Context) {
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
		diffs, e := a.store.ListPcieDiffs(c.Request.Context(), actor, project, c.Query("machine_id"), pagination)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, diffs)
	})
}
