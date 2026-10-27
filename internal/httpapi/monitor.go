package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (a *authAPI) registerMonitor(r *gin.Engine) {
	base := "/api/v1/projects/:id/monitor"
	r.GET(base+"/policies", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.MonitorPolicies(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/policies", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.MonitorPolicyInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := a.store.SaveMonitorPolicy(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/alerts", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		filter := platform.MonitorAlertFilter{State: c.Query("state"), Severity: c.Query("severity"), DType: c.Query("dtype"), MachineID: c.Query("machine_id"), Source: c.Query("source")}
		out, e := a.store.MonitorAlerts(c.Request.Context(), actor, c.Param("id"), filter, page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/alerts/:alertId/suppress", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.MonitorSuppressInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := a.store.SuppressMonitorAlert(c.Request.Context(), actor, c.Param("id"), c.Param("alertId"), in, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	asset := "/api/v1/projects/:id/assets/:assetId/monitor"
	r.GET(asset, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		cursor, e := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
		if e != nil || cursor < 0 {
			a.respondError(c, &platform.Error{Status: 400, Code: "VALIDATION_ERROR", Message: "Invalid monitoring cursor"})
			return
		}
		out, e := a.store.MonitorSnapshot(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), cursor)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(asset+"/trends", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.MonitorTrend(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Query("key"), c.Query("entity"), c.Query("source"), c.Query("method"), c.DefaultQuery("origin", "real"), c.DefaultQuery("window", "1h"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
