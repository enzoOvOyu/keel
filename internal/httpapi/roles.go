package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerRoles wires the role scheme and the four batch-configuration
// renderers (WP-3.2, FR-5.2–5.5, FR-5.9). Reading the plan and the resolved
// bindings is member-readable; saving a plan (which re-binds every port of
// the project) and applying a render need project-admin.
func (a *authAPI) registerRoles(r *gin.Engine) {
	r.GET("/api/v1/projects/:id/roles", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		plan, e := a.store.GetRolePlan(c.Request.Context(), actor, project)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, plan)
	})
	r.PUT("/api/v1/projects/:id/roles", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.RolePlanInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		plan, e := a.store.SaveRolePlan(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, plan)
	})
	r.GET("/api/v1/projects/:id/roles/bindings", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		bindings, e := a.store.ListRoleBindings(c.Request.Context(), actor, project, c.Query("machine_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": bindings})
	})
	r.POST("/api/v1/projects/:id/config/render", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.ConfigRenderInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		apply := c.Query("apply") == "1"
		result, e := a.store.RenderConfig(c.Request.Context(), actor, project, input, apply, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, result)
	})
}
