package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerModelImports wires scan-import endpoints. Import requires project
// admin (model:manage); draft listing rides on model:read; discard requires
// model:manage again.
func (a *authAPI) registerModelImports(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/models/import", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.ImportModelScanInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.ImportModelScan(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusCreated, result)
	})
	r.GET("/api/v1/projects/:id/models/:modelId/drafts", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		items, e := a.store.ListModelDrafts(c.Request.Context(), actor, project, c.Param("modelId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	})
	r.POST("/api/v1/projects/:id/models/:modelId/drafts/:draftId/discard", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		if e := a.store.DeleteModelDraft(c.Request.Context(), actor, project, c.Param("draftId"), c.GetString("trace_id")); e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"discarded": true})
	})
}
