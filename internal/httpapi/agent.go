package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerAgent wires the agent installer module (WP-3.5, FR-5.7): batch
// installation plans over media://agent artifacts, the enrollment /
// credential lifecycle endpoints and the distribution accounting reads.
// Planning and lifecycle transitions need project-admin; the machine-side
// enrollment call authenticates as a project member presenting the
// machine-bound one-time token (no self-registration on a bare SN).
func (a *authAPI) registerAgent(r *gin.Engine) {
	r.POST("/api/v1/projects/:id/agent/plans", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.AgentPlanInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		plan, e := a.store.CreateAgentPlan(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusCreated, plan)
	})
	r.GET("/api/v1/projects/:id/agent/plans", func(c *gin.Context) {
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
		plans, e := a.store.ListAgentPlans(c.Request.Context(), actor, project, pagination)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, plans)
	})
	r.GET("/api/v1/projects/:id/agent/plans/:planId", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		detail, e := a.store.GetAgentPlan(c.Request.Context(), actor, project, c.Param("planId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
	r.POST("/api/v1/projects/:id/agent/plans/:planId/advance", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.AgentAdvanceInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		detail, e := a.store.AdvanceAgentPlan(c.Request.Context(), actor, project, c.Param("planId"), input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
	r.POST("/api/v1/projects/:id/agent/plans/:planId/abort", func(c *gin.Context) {
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
		detail, e := a.store.AbortAgentPlan(c.Request.Context(), actor, project, c.Param("planId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
	r.POST("/api/v1/projects/:id/agent/enroll", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.AgentEnrollInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		target, e := a.store.AgentEnroll(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, target)
	})
	r.POST("/api/v1/projects/:id/agent/credentials/rotate", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input struct {
			MachineID   string `json:"machine_id"`
			Fingerprint string `json:"fingerprint"`
		}
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		target, e := a.store.AgentRotateCredential(c.Request.Context(), actor, project, input.MachineID, input.Fingerprint, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, target)
	})
	r.POST("/api/v1/projects/:id/agent/credentials/revoke", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input struct {
			MachineID string `json:"machine_id"`
		}
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		target, e := a.store.AgentRevokeCredential(c.Request.Context(), actor, project, input.MachineID, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, target)
	})
}
