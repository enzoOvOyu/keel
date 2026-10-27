package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (a *authAPI) registerAssetViews(r *gin.Engine) {
	base := "/api/v1/projects/:id/asset-views"
	r.GET(base, func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListAssetViews(c.Request.Context(), actor, c.Param("id"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:viewId", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.GetAssetView(c.Request.Context(), actor, c.Param("id"), c.Param("viewId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.PUT(base+"/:viewId", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.AssetViewInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := a.store.SaveAssetView(c.Request.Context(), actor, c.Param("id"), c.Param("viewId"), in, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	for _, suffix := range []string{"/data", "/export.csv"} {
		export := suffix == "/export.csv"
		r.GET(base+"/:viewId"+suffix, func(c *gin.Context) {
			actor, _, ok := a.executionProject(c, false)
			if !ok {
				return
			}
			page, ok := readPagination(c)
			if !ok {
				return
			}
			version, e := strconv.ParseInt(c.Query("version"), 10, 64)
			if e != nil || version < 1 {
				a.respondError(c, &platform.Error{Status: 400, Code: "ASSET_VIEW_VERSION_REQUIRED", Message: "Exact view version required"})
				return
			}
			out, e := a.store.AssetViewData(c.Request.Context(), actor, c.Param("id"), c.Param("viewId"), version, page, export)
			if e != nil {
				a.respondError(c, e)
				return
			}
			if !export {
				c.JSON(200, out)
				return
			}
			csv, e := platform.AssetViewCSV(out)
			if e != nil {
				a.respondError(c, e)
				return
			}
			c.Header("Content-Disposition", `attachment; filename="asset-view.csv"`)
			c.Header("X-Source-Fingerprint", out.SourceFingerprint)
			c.Data(200, "text/csv; charset=utf-8", []byte("\ufeff"+csv))
		})
	}
}
