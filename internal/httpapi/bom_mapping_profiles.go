package httpapi

import (
	"net/http"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerBOMMappingProfiles(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/bom/mapping-profiles", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.CreateBOMMappingProfileInput
		if !decodeBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.CreateBOMMappingProfile(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusCreated, result)
	})
	r.POST("/api/v1/projects/:id/bom/mapping-profiles/:versionRef/approve", func(c *gin.Context) {
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
		result, e := a.store.ApproveBOMMappingProfile(c.Request.Context(), actor, project, c.Param("versionRef"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, result)
	})
	r.GET("/api/v1/projects/:id/bom/mapping-profiles/:versionRef", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		result, e := a.store.GetApprovedBOMMappingProfile(c.Request.Context(), actor, project, c.Param("versionRef"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, result)
	})
}
