package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (a *authAPI) registerZTPSimSessions(r *gin.Engine) {
	base := "/api/v1/projects/:id/ztp/sim-sessions"
	r.POST("/api/v1/projects/:id/ztp/identity-issues/:issueId/resolve", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.ZTPIdentityIssueResolveInput
		if !decodeBody(c, &input) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ResolveZTPIdentityIssue(c.Request.Context(), actor, project, c.Param("issueId"), input.Reason, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
	r.GET("/api/v1/projects/:id/ztp/identity-issues", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ListZTPIdentityIssues(c.Request.Context(), actor, project)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
	r.GET("/api/v1/projects/:id/ztp/device-observations", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ListZTPDeviceObservations(c.Request.Context(), actor, project)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
	r.GET(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ListZTPSimSessions(c.Request.Context(), actor, project)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
	r.POST(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.ZTPSimSessionInput
		if !decodeBody(c, &input) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.CreateZTPSimSession(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusCreated, out)
	})
	r.GET(base+"/:sessionId", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.GetZTPSession(c.Request.Context(), actor, project, c.Param("sessionId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
	r.POST(base+"/:sessionId/actions", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.ZTPSessionAction
		if !decodeBody(c, &input) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.AdvanceZTPSimSession(c.Request.Context(), actor, project, c.Param("sessionId"), input, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
}
