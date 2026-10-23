package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerNetworkPush(r *gin.Engine) {
	base := "/api/v1/projects/:id/fabric/push-jobs"
	r.GET(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		rows, e := a.store.ListNetworkPush(c.Request.Context(), actor, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		client, e := a.store.FabricProbes.NetworkClient()
		c.JSON(200, gin.H{"jobs": rows, "writes_enabled": e == nil && client.WriteEnabled()})
	})
	r.POST(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var input platform.NetworkPlanInput
		if !decodeBody(c, &input) {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.CreateNetworkPush(c.Request.Context(), actor, p, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
	for _, phase := range []string{"prepare", "push"} {
		phase := phase
		r.POST(base+"/:jobId/"+phase, func(c *gin.Context) {
			actor, service, ok := a.executionProject(c, true)
			if !ok {
				return
			}
			service.NetworkStore = a.store
			out, e := service.CreateNetwork(c.Request.Context(), actor, c.Param("id"), execution.NetworkExecutionInput{JobID: c.Param("jobId"), Phase: phase}, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
			if e != nil {
				a.respondError(c, e)
				return
			}
			c.JSON(201, out)
		})
	}
	r.POST(base+"/:jobId/archive", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.ArchiveNetworkPush(c.Request.Context(), actor, p, c.Param("jobId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})

	r.POST(base+"/:jobId/decision", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var input struct {
			ReviewHash string `json:"review_hash"`
			Comment    string `json:"comment"`
			Decision   string `json:"decision"`
		}
		if !decodeBody(c, &input) {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.DecideNetworkPush(c.Request.Context(), actor, p, c.Param("jobId"), input.ReviewHash, input.Comment, input.Decision, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
