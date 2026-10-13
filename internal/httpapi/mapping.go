package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerMapping wires the mapping library (WP-2.5, G3). Publishing needs
// project-admin; the revision history is readable by every member.
func (a *authAPI) registerMapping(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/mappings/publish", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.PublishMappingInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		revision, e := a.store.PublishMappingRevision(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusCreated, revision)
	})
	r.GET("/api/v1/projects/:id/mappings", func(c *gin.Context) {
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
		revisions, e := a.store.ListMappingRevisions(c.Request.Context(), actor, project, c.Query("machine_id"), pagination)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, revisions)
	})
}

// registerMachinePorts exposes the BOM ports of a machine (read, members).
func (a *authAPI) registerMachinePorts(r *gin.Engine) {
	r.GET("/api/v1/projects/:id/machines/:machineId/ports", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		ports, e := a.store.ListMachinePorts(c.Request.Context(), actor, project, c.Param("machineId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": ports})
	})
}
