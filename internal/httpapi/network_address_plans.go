package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerNetworkAddressPlans(r *gin.Engine) {
	base := "/api/v1/projects/:id/fabric/address-plans"
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
		plans, e := a.store.ListNetworkAddressPlans(c.Request.Context(), actor, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, gin.H{"plans": plans})
	})
	r.GET(base+"/:planId", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		v, e := a.store.GetNetworkAddressPlan(c.Request.Context(), actor, p, c.Param("planId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, v)
	})
	for _, operation := range []string{"preview", "save", "approve"} {
		op := operation
		path := base + "/" + op
		if op == "save" {
			path = base
		}
		if op == "approve" {
			path = base + "/:planId/approve"
		}
		r.POST(path, func(c *gin.Context) {
			actor, ok := a.authenticated(c)
			if !ok || !a.writeAllowed(c) {
				return
			}
			var input platform.NetworkAddressPlanInput
			var approval platform.NetworkAddressPlanApproval
			if op == "approve" {
				if !decodeBody(c, &approval) {
					return
				}
			} else {
				if !decodeBody(c, &input) {
					return
				}
			}
			p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
			if e != nil {
				a.respondError(c, e)
				return
			}
			var out *platform.NetworkAddressPlan
			switch op {
			case "preview":
				out, e = a.store.PreviewNetworkAddressPlan(c.Request.Context(), actor, p, input)
			case "save":
				out, e = a.store.SaveNetworkAddressPlan(c.Request.Context(), actor, p, input, c.GetString("trace_id"))
			case "approve":
				out, e = a.store.ApproveNetworkAddressPlan(c.Request.Context(), actor, p, c.Param("planId"), approval, c.GetString("trace_id"))
			}
			if e != nil {
				a.respondError(c, e)
				return
			}
			status := 200
			if op == "save" {
				status = 201
			}
			c.JSON(status, out)
		})
	}
}
