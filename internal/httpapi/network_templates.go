package httpapi

import (
	"context"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"time"
)

func (a *authAPI) registerNetworkTemplates(r *gin.Engine) {
	base := "/api/v1/projects/:id/fabric/templates"
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
		rows, e := a.store.ListNetworkTemplates(c.Request.Context(), actor, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		_, e = a.store.FabricProbes.NetworkClient()
		c.JSON(200, gin.H{"templates": rows, "adapter_configured": e == nil})
	})
	r.POST(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var input platform.NetworkTemplateInput
		if !decodeBody(c, &input) {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.CreateNetworkTemplate(c.Request.Context(), actor, p, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
	r.POST(base+"/:templateId/test", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var input platform.NetworkPlanInput
		if !decodeBody(c, &input) {
			return
		}
		input.TemplateID = c.Param("templateId")
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
		defer cancel()
		out, e := a.store.TestNetworkTemplate(ctx, actor, p, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/:templateId/release", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.ReleaseNetworkTemplate(c.Request.Context(), actor, p, c.Param("templateId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
