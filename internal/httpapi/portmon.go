package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// registerPortmon wires the port panel read (WP-2.6). Members only.
func (a *authAPI) registerPortmon(r *gin.Engine) {
	r.GET("/api/v1/projects/:id/machines/:machineId/port-panel", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		panel, e := a.store.GetPortPanel(c.Request.Context(), actor, project, c.Param("machineId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, panel)
	})
}
