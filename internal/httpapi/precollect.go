package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerPrecollect(r *gin.Engine) {
	base := "/api/v1/projects/:id/assets/:assetId/precollect"
	r.GET(base+"/scope", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.PreviewPrecollect(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST("/api/v1/projects/:id/validation/precollect", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.PrecollectCreateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := service.CreatePrecollect(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(202, out)
	})
	r.GET(base, func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListPrecollect(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:precollectId", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.GetPrecollect(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("precollectId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:precollectId/raw", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		raw, sha, e := a.store.PrecollectRaw(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("precollectId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.Header("Content-Disposition", `attachment; filename="precollect.json"`)
		c.Header("X-Content-SHA256", sha)
		c.Data(200, "application/json", raw)
	})
	r.GET("/api/v1/projects/:id/assets/:assetId/gates/G1", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.CurrentG1(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
