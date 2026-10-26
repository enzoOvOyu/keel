package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerAgentExecution(r *gin.Engine, base string) {
	r.GET(base+"/agent-targets/:machineId/network-inventory", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, err := a.store.GetAgentNetworkInventory(c.Request.Context(), actor, c.Param("id"), c.Param("machineId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
	r.GET(base+"/agent-targets", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListAgentTaskTargets(c.Request.Context(), actor, c.Param("id"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/agent-script-executions", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.SSHScriptCreateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		x, err := service.CreateAgentScript(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(201, x)
	})
	r.POST(base+"/agent-executions", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.SSHCreateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		x, e := service.CreateAgent(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, x)
	})
}
