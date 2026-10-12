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
		result, e := a.store.ListParamKeys(c.Request.Context(), actor, project, p)
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
		result, e := a.store.RetireParamKey(c.Request.Context(), actor, project, c.Param("keyId"), c.GetString("trace_id"))
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
		result, e := a.store.CheckParamValue(c.Request.Context(), actor, project, c.Param("keyName"), c.Query("value"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
}
