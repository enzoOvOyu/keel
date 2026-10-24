package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

func (a *authAPI) registerModelImports(r *gin.Engine) {
	a.registerModelImportsScope(r, "/api/v1/projects/:id/models", false)
	a.registerModelImportsScope(r, "/api/v1/admin/models", true)
}
func (a *authAPI) registerModelImportsScope(r *gin.Engine, base string, global bool) {
	r.POST(base+"/import", func(c *gin.Context) {
		actor, project, ok := a.modelScope(c, true, global)
		if !ok {
			return
		}
		var input platform.ImportModelScanInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		result, err := a.store.ImportModelScan(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusCreated, result)
	})
	r.GET(base+"/:modelId/drafts", func(c *gin.Context) {
		actor, project, ok := a.modelScope(c, false, global)
		if !ok {
			return
		}
		items, err := a.store.ListModelDrafts(c.Request.Context(), actor, project, c.Param("modelId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": items})
	})
	r.POST(base+"/:modelId/drafts/:draftId/discard", func(c *gin.Context) {
		actor, project, ok := a.modelScope(c, true, global)
		if !ok {
			return
		}
		if err := a.store.DeleteModelDraft(c.Request.Context(), actor, project, c.Param("modelId"), c.Param("draftId"), c.GetString("trace_id")); err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"discarded": true})
	})
}
