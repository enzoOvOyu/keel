package httpapi

import (
	"github.com/gin-gonic/gin"
	"strconv"
)

func (a *authAPI) registerFabricDiagnostics(r *gin.Engine) {
	base := "/api/v1/projects/:id/fabric/devices/:deviceId"
	r.GET(base+"/diagnostics", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.FabricDiagnostics(c.Request.Context(), actor, p, c.Param("deviceId"), c.Query("port"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/ddm-trends", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		hours, e := strconv.Atoi(c.DefaultQuery("hours", "24"))
		if e != nil {
			failure(c, 400, "VALIDATION_ERROR", "Invalid DDM window")
			return
		}
		out, e := a.store.FabricDDMTrends(c.Request.Context(), actor, p, c.Param("deviceId"), c.Query("port"), hours, "", c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/ddm-trends/export", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var input struct {
			Port        string `json:"port"`
			Hours       int    `json:"hours"`
			ContentHash string `json:"content_hash"`
		}
		if !decodeBody(c, &input) {
			return
		}
		if len(input.ContentHash) != 64 {
			failure(c, 400, "VALIDATION_ERROR", "Chart content_hash is required")
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.FabricDDMTrends(c.Request.Context(), actor, p, c.Param("deviceId"), input.Port, input.Hours, input.ContentHash, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
