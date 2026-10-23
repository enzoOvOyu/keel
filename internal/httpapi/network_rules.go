package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerNetworkRules(r *gin.Engine) {
	base := "/api/v1/projects/:id/fabric/rules"
	project := func(c *gin.Context, write bool) (*platform.AuthSession, *platform.ProjectSummary, bool) {
		actor, ok := a.authenticated(c)
		if !ok || write && !a.writeAllowed(c) {
			return nil, nil, false
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return nil, nil, false
		}
		return actor, p, true
	}
	r.GET(base, func(c *gin.Context) {
		actor, p, ok := project(c, false)
		if !ok {
			return
		}
		out, e := a.store.ListNetworkRules(c.Request.Context(), actor, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/preview", func(c *gin.Context) {
		actor, p, ok := project(c, true)
		if !ok {
			return
		}
		var input platform.NetworkRuleInput
		if !decodeBody(c, &input) {
			return
		}
		out, e := a.store.PreviewNetworkRule(c.Request.Context(), actor, p, input)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base, func(c *gin.Context) {
		actor, p, ok := project(c, true)
		if !ok {
			return
		}
		var input platform.NetworkRuleInput
		if !decodeBody(c, &input) {
			return
		}
		out, e := a.store.CreateNetworkRule(c.Request.Context(), actor, p, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
	r.POST(base+"/:ruleVersionId/approve", func(c *gin.Context) {
		actor, p, ok := project(c, true)
		if !ok {
			return
		}
		var input struct {
			Role        string `json:"approval_role"`
			PreviewHash string `json:"preview_hash"`
		}
		if !decodeBody(c, &input) {
			return
		}
		out, e := a.store.ApproveNetworkRule(c.Request.Context(), actor, p, c.Param("ruleVersionId"), input.Role, input.PreviewHash, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/:ruleVersionId/publish", func(c *gin.Context) {
		actor, p, ok := project(c, true)
		if !ok {
			return
		}
		var input struct {
			PreviewHash string `json:"preview_hash"`
		}
		if !decodeBody(c, &input) {
			return
		}
		out, e := a.store.PublishNetworkRule(c.Request.Context(), actor, p, c.Param("ruleVersionId"), input.PreviewHash, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:ruleVersionId/rollback-candidate", func(c *gin.Context) {
		actor, p, ok := project(c, false)
		if !ok {
			return
		}
		target := c.DefaultQuery("target", "default")
		out, e := a.store.NetworkRuleRollbackInput(c.Request.Context(), actor, p, c.Param("ruleVersionId"), target)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
