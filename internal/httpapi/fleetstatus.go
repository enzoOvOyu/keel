package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"strconv"
)

// registerFleetStatus wires the status-pill aggregate and the drawer's
// monitoring event stream (WP-2.11). Members only.
func (a *authAPI) registerFleetStatus(r *gin.Engine) {
	r.GET("/api/v1/projects/:id/fleet-status", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		pills, e := a.store.GetFleetStatusPills(c.Request.Context(), actor, project)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, pills)
	})
	r.GET("/api/v1/projects/:id/machines/:machineId/events", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
		events, e := a.store.ListRecentPortEvents(c.Request.Context(), actor, project, c.Param("machineId"), limit)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(http.StatusOK, gin.H{"items": events})
	})
}
