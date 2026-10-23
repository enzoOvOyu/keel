package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerKeaDeployments(r *gin.Engine) {
	base := "/api/v1/projects/:id/ztp/kea-deployments"
	r.GET(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		rows, e := a.store.ListKeaDeployments(c.Request.Context(), actor, project)
		if e != nil {
			a.respondError(c, e)
			return
		}
		policies, e := a.store.KeaPolicyOptions(c.Request.Context(), actor, project)
		if e != nil {
			a.respondError(c, e)
			return
		}
		_, _, e = a.store.FabricProbes.DeploymentControl()
		c.JSON(200, gin.H{"enabled": e == nil, "deployments": rows, "policies": policies})
	})
	r.POST(base+"/stage", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var input platform.KeaDeploymentInput
		if !decodeBody(c, &input) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.StageKeaDeployment(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
	r.POST(base+"/:deploymentId/execute", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		service.KeaStore = a.store
		out, e := service.CreateKea(c.Request.Context(), actor, c.Param("id"), execution.KeaCreateInput{DeploymentID: c.Param("deploymentId")}, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
}
