package httpapi

import (
	"net/http"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerNetworkBaselines(r *gin.Engine) {
	base := "/api/v1/projects/:id/network-baselines"
	r.POST(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.CreateNetworkBaselineInput
		if !decodeBody(c, &input) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.CreateNetworkBaseline(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusCreated, out)
	})
	r.POST(base+"/:versionRef/approve", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.ApproveNetworkBaselineInput
		if !decodeBody(c, &input) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ApproveNetworkBaseline(c.Request.Context(), actor, project, c.Param("versionRef"), input, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
	r.GET(base+"/:versionRef", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.GetNetworkBaseline(c.Request.Context(), actor, project, c.Param("versionRef"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(http.StatusOK, out)
	})
}
