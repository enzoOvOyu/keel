package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// registerDashboard wires the gate board aggregation (WP-2.8). Readable by
// every project member; the underlying mutations stay on their own
// endpoints with admin authority.
func (a *authAPI) registerDashboard(r *gin.Engine) {
	r.GET("/api/v1/projects/:id/dashboard", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		dashboard, e := a.store.GetGateDashboard(c.Request.Context(), actor, project)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, dashboard)
	})
}
