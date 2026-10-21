package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (a *authAPI) registerZTPMockKea(r *gin.Engine) {
	base := "/api/v1/projects/:id/ztp/mock-kea"
	r.POST(base+"/plans/stage", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input fabricboot.DHCPOptions
		if !decodeBody(c, &input) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.StageZTPMockKeaPlan(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
	r.POST(base+"/plans/commit", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.ZTPMockKeaCommitInput
		if !decodeBody(c, &input) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.CommitZTPMockKeaPlan(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
	r.POST(base+"/dora", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		var input fabricboot.DORARequest
		if !decodeBody(c, &input) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.SimulateZTPDORA(c.Request.Context(), actor, project, input)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
}
