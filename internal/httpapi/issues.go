package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerIssues wires the issue board (WP-2.7, FR-4.7). Creating from a
// difference, registering repairs and every transition need project-admin;
// the board is readable by every member with status/source/phase filters.
func (a *authAPI) registerIssues(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/issues", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.CreateIssueInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		issue, e := a.store.CreateIssueFromDifference(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusCreated, issue)
	})
	r.POST("/api/v1/projects/:id/issues/repair", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.RepairIssueInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		issue, e := a.store.RepairIssue(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, issue)
	})
	r.POST("/api/v1/projects/:id/issues/:issueId/transition", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.TransitionIssueInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		input.IssueID = c.Param("issueId")
		action := c.Query("action")
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		issue, e := a.store.TransitionIssue(c.Request.Context(), actor, project, input, action, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, issue)
	})
	r.GET("/api/v1/projects/:id/issues", func(c *gin.Context) {
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
		issues, e := a.store.ListIssues(c.Request.Context(), actor, project, c.Query("status"), c.Query("source"), c.Query("phase"), pagination)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, issues)
	})
}
