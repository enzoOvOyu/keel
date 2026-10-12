package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

// Device model library routes. The management scope is platform-level; the
// project scope nests under /projects/:id. Both resolve the project record
// before touching model data, so scope checks stay in the platform layer.
func (a *authAPI) registerModels(r *gin.Engine) {
	// Platform-level: list every model across scopes; admins only.
	r.GET("/api/v1/models", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		result, e := a.store.ListDeviceModels(c.Request.Context(), actor, nil, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.GET("/api/v1/projects/:id/models", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.ListDeviceModels(c.Request.Context(), actor, project, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.POST("/api/v1/projects/:id/models", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.CreateDeviceModelInput
		if !decodeBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.CreateDeviceModel(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, result)
	})
	r.POST("/api/v1/projects/:id/models/:modelId/retire", func(c *gin.Context) {
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
		result, e := a.store.RetireDeviceModel(c.Request.Context(), actor, project, c.Param("modelId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.GET("/api/v1/projects/:id/models/:modelId/versions", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.ListModelVersions(c.Request.Context(), actor, project, c.Param("modelId"), p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.POST("/api/v1/projects/:id/models/:modelId/versions", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.PublishModelVersionInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.PublishModelVersion(c.Request.Context(), actor, project, c.Param("modelId"), input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, result)
	})
}
