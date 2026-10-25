package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerBomImport wires the two-phase BOM import (FR-2.1) and the project
// asset listing. Preview is a read-only validation pass; Commit is the only
// writing path and requires the same project-admin authority as publishing.
func (a *authAPI) registerBomImport(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/bom/preview", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		var input platform.BomImportInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		preview, e := a.store.PreviewBomImport(c.Request.Context(), actor, project, input)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, preview)
	})
	r.POST("/api/v1/projects/:id/bom/commit", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.BomImportInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.CommitBomImport(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusCreated, result)
	})
	r.GET("/api/v1/projects/:id/assets/:assetId/model", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		binding, err := a.store.GetAssetModelBinding(c.Request.Context(), actor, project, c.Param("assetId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, binding)
	})
	r.GET("/api/v1/projects/:id/assets", func(c *gin.Context) {
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
		assets, e := a.store.ListProjectAssets(c.Request.Context(), actor, project, pagination)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, assets)
	})
}
