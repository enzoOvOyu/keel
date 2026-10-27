package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerMapping(r *gin.Engine) {
	base := "/api/v1/projects/:id/assets/:assetId/mapping"
	r.GET(base+"/preview", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.PreviewMapping(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/assessments", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListMappingHistory(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/assessments/:assessmentId", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.GetMappingAssessment(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("assessmentId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/revisions", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var in platform.MappingPublishInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := a.store.PublishMapping(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/revisions/:revisionId", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.GetMappingRevision(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("revisionId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/revisions/:revisionId/export", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		format := c.DefaultQuery("format", "json")
		if format != "json" && format != "csv" {
			a.respondError(c, &platform.Error{Status: 400, Code: "VALIDATION_ERROR", Message: "format must be json or csv"})
			return
		}
		out, e := a.store.GetMappingRevision(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), c.Param("revisionId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		if format == "csv" {
			body, e := platform.MappingRevisionCSV(out)
			if e != nil {
				a.respondError(c, e)
				return
			}
			c.Header("Content-Disposition", "attachment; filename=keel-mapping.csv")
			c.Data(200, "text/csv; charset=utf-8", []byte(body))
			return
		}
		c.Header("Content-Disposition", "attachment; filename=keel-mapping.json")
		c.JSON(200, out)
	})
}
