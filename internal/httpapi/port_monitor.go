package httpapi

import (
	"context"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerPortMonitor(r *gin.Engine) {
	r.GET("/api/v1/projects/:id/ports", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.PortMonitorFleet(c.Request.Context(), actor, c.Param("id"), strings.Split(c.Query("asset_ids"), ","))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	base := "/api/v1/projects/:id/assets/:assetId/ports"
	r.GET(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.PortMonitorSnapshot(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:portId/events", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.PortMonitorEvents(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("portId"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/stream", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.PortMonitorSnapshot(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.Header("Content-Type", "text/event-stream; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		c.Header("X-Accel-Buffering", "no")
		// Each frame is a complete bounded snapshot. Reconnect explicitly resyncs;
		// an event ID never pretends to acknowledge missed physical observations.
		if !streamFrame(c, "snapshot", 0, out) {
			return
		}
		work, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
		defer cancel()
		tick := time.NewTicker(10 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-work.Done():
				streamFrame(c, "rotate", 0, gin.H{"resync_required": true})
				return
			case <-tick.C:
			}
			actor, e = a.store.Authenticate(work, cookieToken(c))
			if e != nil {
				streamFrame(c, "access_revoked", 0, gin.H{"reason": "session_expired_or_revoked"})
				return
			}
			out, e = a.store.PortMonitorSnapshot(work, actor, c.Param("id"), c.Param("assetId"))
			if e != nil {
				streamFrame(c, "unavailable", 0, gin.H{"resync_required": true})
				return
			}
			if !streamFrame(c, "snapshot", 0, out) {
				return
			}
		}
	})
}
