package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerZTPIdentityHistory(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/ztp/replacements/:replacementId/reject", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.ZTPReplacementApproval
		if !decodeBody(c, &in) {
			return
		}
		p, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.RejectZTPReplacement(c.Request.Context(), actor, p, c.Param("replacementId"), in, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, out)
	})
	r.GET("/api/v1/projects/:id/ztp/replacements", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ListZTPReplacements(c.Request.Context(), actor, p)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, out)
	})
	r.POST("/api/v1/projects/:id/ztp/replacements", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.ZTPReplacementInput
		if !decodeBody(c, &in) {
			return
		}
		p, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.CreateZTPReplacement(c.Request.Context(), actor, p, in, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(201, out)
	})
	r.POST("/api/v1/projects/:id/ztp/replacements/:replacementId/approve", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.ZTPReplacementApproval
		if !decodeBody(c, &in) {
			return
		}
		p, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ApproveZTPReplacement(c.Request.Context(), actor, p, c.Param("replacementId"), in, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, out)
	})
	path := "/api/v1/projects/:id/ztp/identity-history"
	r.GET(path, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ListZTPIdentityHistory(c.Request.Context(), actor, project)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, out)
	})
	r.POST(path, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.ZTPIdentityHistoryInput
		if !decodeBody(c, &in) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.RecordZTPIdentityHistory(c.Request.Context(), actor, project, in, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(201, out)
	})
}
