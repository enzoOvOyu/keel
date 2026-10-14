package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerDriver wires the driver upgrade module (WP-3.4, FR-5.6): the
// component capability matrix, the plan lifecycle (create -> precheck ->
// start -> advance -> rollback/manual/abort) and the distribution
// accounting reads. Planning and every state transition need
// project-admin; reads are member-visible.
func (a *authAPI) registerDriver(r *gin.Engine) {
	r.GET("/api/v1/driver/components", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		components, e := a.store.ListDriverComponents(c.Request.Context(), actor)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"components": components})
	})
	r.POST("/api/v1/projects/:id/driver/plans", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.DriverPlanInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		plan, e := a.store.CreateDriverUpgradePlan(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusCreated, plan)
	})
	r.GET("/api/v1/projects/:id/driver/plans", func(c *gin.Context) {
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
		plans, e := a.store.ListDriverUpgradePlans(c.Request.Context(), actor, project, pagination)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, plans)
	})
	r.GET("/api/v1/projects/:id/driver/plans/:planId", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		detail, e := a.store.GetDriverUpgradePlan(c.Request.Context(), actor, project, c.Param("planId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
	r.POST("/api/v1/projects/:id/driver/plans/:planId/precheck", func(c *gin.Context) {
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
		detail, e := a.store.PrecheckDriverUpgradePlan(c.Request.Context(), actor, project, c.Param("planId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
	r.POST("/api/v1/projects/:id/driver/plans/:planId/start", func(c *gin.Context) {
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
		detail, e := a.store.StartDriverUpgradePlan(c.Request.Context(), actor, project, c.Param("planId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
	r.POST("/api/v1/projects/:id/driver/plans/:planId/advance", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.DriverAdvanceInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		detail, e := a.store.AdvanceDriverUpgradePlan(c.Request.Context(), actor, project, c.Param("planId"), input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
	r.POST("/api/v1/projects/:id/driver/plans/:planId/targets/:machineId/rollback", func(c *gin.Context) {
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
		detail, e := a.store.RollbackDriverUpgradeTarget(c.Request.Context(), actor, project, c.Param("planId"), c.Param("machineId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
	r.POST("/api/v1/projects/:id/driver/plans/:planId/targets/:machineId/manual", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input struct {
			Note string `json:"note"`
		}
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		detail, e := a.store.ManualRecoverDriverUpgradeTarget(c.Request.Context(), actor, project, c.Param("planId"), c.Param("machineId"), input.Note, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
	r.POST("/api/v1/projects/:id/driver/plans/:planId/abort", func(c *gin.Context) {
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
		detail, e := a.store.AbortDriverUpgradePlan(c.Request.Context(), actor, project, c.Param("planId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, detail)
	})
}
