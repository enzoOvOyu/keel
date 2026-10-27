package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerIssues(r *gin.Engine) {
	r.GET("/api/v1/issues/:issueId/port-diagnostics", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.FabricPortIssueEvidence(c.Request.Context(), actor, c.Param("issueId"), p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET("/api/v1/issues/:issueId/fabric-observations", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.FabricIssueObservations(c.Request.Context(), actor, c.Param("issueId"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET("/api/v1/projects/:id/issues/targets", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.IssueTargets(c.Request.Context(), actor, c.Param("id"), c.Query("q"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET("/api/v1/issues", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListIssues(c.Request.Context(), actor, p, platform.IssueFilter{ProjectID: c.Query("project_id"), State: c.Query("state"), Category: c.Query("category"), Source: c.Query("source"), Phase: c.Query("phase"), ResponsibleDomain: c.Query("responsible_domain")})
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET("/api/v1/issues/:issueId", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.GetIssue(c.Request.Context(), actor, c.Param("issueId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET("/api/v1/projects/:id/issues/assignees", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.IssueAssignees(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST("/api/v1/projects/:id/issues", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var input platform.CreateIssueInput
		if !decodeBody(c, &input) {
			return
		}
		out, e := a.store.CreateIssue(c.Request.Context(), actor, c.Param("id"), input, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
	for _, action := range []string{"assign", "start", "repair", "verify", "close"} {
		r.POST("/api/v1/issues/:issueId/"+action, func(c *gin.Context) {
			actor, ok := a.authenticated(c)
			if !ok || !a.writeAllowed(c) {
				return
			}
			var input platform.IssueActionInput
			if !decodeBody(c, &input) {
				return
			}
			out, e := a.store.ActOnIssue(c.Request.Context(), actor, c.Param("issueId"), action, input, c.GetString("trace_id"))
			if e != nil {
				a.respondError(c, e)
				return
			}
			c.JSON(200, out)
		})
	}
}
