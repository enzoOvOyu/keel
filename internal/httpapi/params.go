package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

// Parameter catalog routes. Version-typed options sync from approved media.
func (a *authAPI) registerParams(r *gin.Engine) {
	r.GET("/api/v1/projects/:id/params", func(c *gin.Context) {
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
		includeRetired := c.Query("include_retired")
		if includeRetired != "" && includeRetired != "true" && includeRetired != "false" {
			a.respondError(c, &platform.Error{Status: 400, Code: "VALIDATION_ERROR", Message: "include_retired must be true or false"})
			return
		}
		result, e := a.store.ListParamKeysIncludingRetired(c.Request.Context(), actor, project, p, c.Query("arch"), includeRetired == "true")
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.POST("/api/v1/projects/:id/params/validate", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input struct {
			Values map[string]any `json:"values"`
		}
		if !decodeBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.ValidateCatalog(c.Request.Context(), actor, project, input.Values)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.PUT("/api/v1/projects/:id/params", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.UpsertParamKeyInput
		if !decodeBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.UpsertParamKey(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, result)
	})
	r.POST("/api/v1/projects/:id/params/:keyId/retire", func(c *gin.Context) {
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
		var input platform.RetireParamKeyInput
		if c.Request.ContentLength != 0 && !decodeBody(c, &input) {
			return
		}
		result, e := a.store.RetireParamKeyChecked(c.Request.Context(), actor, project, c.Param("keyId"), input.ExpectedUpdatedAt, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.GET("/api/v1/projects/:id/params/:keyName/check", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.CheckParamValue(c.Request.Context(), actor, project, c.Param("keyName"), c.Query("value"), c.Query("arch"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
}
