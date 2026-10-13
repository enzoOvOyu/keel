package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/enzoOvOyu/keel/internal/platform"
)

// registerNettest wires the RoCE connectivity module (WP-3.3, FR-11): the
// platform handshake, the run lifecycle (plan → ingest → finish), the rail
// heat matrix and the FR-11.6 NCCL admission view. Planning, ingesting and
// finishing need project-admin; reads are member-visible.
func (a *authAPI) registerNettest(r *gin.Engine) {
	r.PUT("/api/v1/projects/:id/nettest/platform", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.NettestPlatformInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		platformState, e := a.store.RefreshNettestPlatform(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, platformState)
	})
	r.GET("/api/v1/projects/:id/nettest/platform", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		state, e := a.store.GetNettestPlatform(c.Request.Context(), actor, project)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, state)
	})
	r.POST("/api/v1/projects/:id/nettest/runs", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.NettestPlanInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		run, e := a.store.PlanNettestRun(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusCreated, run)
	})
	r.GET("/api/v1/projects/:id/nettest/runs", func(c *gin.Context) {
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
		runs, e := a.store.ListNettestRuns(c.Request.Context(), actor, project, pagination)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, runs)
	})
	r.GET("/api/v1/projects/:id/nettest/runs/:runId", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		run, pairs, e := a.store.GetNettestRun(c.Request.Context(), actor, project, c.Param("runId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"run": run, "pairs": pairs})
	})
	r.POST("/api/v1/projects/:id/nettest/runs/:runId/results", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.NettestIngestInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		run, e := a.store.IngestNettestResults(c.Request.Context(), actor, project, c.Param("runId"), input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, run)
	})
	r.POST("/api/v1/projects/:id/nettest/runs/:runId/finish", func(c *gin.Context) {
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
		run, e := a.store.FinishNettestRun(c.Request.Context(), actor, project, c.Param("runId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, run)
	})
	r.GET("/api/v1/projects/:id/nettest/runs/:runId/matrix", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		cells, e := a.store.GetNettestMatrix(c.Request.Context(), actor, project, c.Param("runId"), c.Query("layer"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": cells})
	})
	r.GET("/api/v1/projects/:id/nettest/admission", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		machineIDs, ok := readMachineIDs(c, "machine_ids")
		if !ok {
			return
		}
		admission, e := a.store.NettestAdmission(c.Request.Context(), actor, project, machineIDs)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": admission})
	})
}

// readMachineIDs parses a repeated or comma-separated machine_ids query
// parameter (1-500 entries), matching the batch endpoints' contract.
func readMachineIDs(c *gin.Context, name string) ([]string, bool) {
	values := c.QueryArray(name)
	if len(values) == 1 && strings.Contains(values[0], ",") {
		values = strings.Split(values[0], ",")
	}
	if len(values) == 0 || len(values) > 500 {
		failure(c, 400, "VALIDATION_ERROR", name+" must contain 1-500 entries")
		return nil, false
	}
	return values, true
}
