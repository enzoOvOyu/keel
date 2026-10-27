package httpapi

import (
	"io"
	"strconv"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerBMCInventory(r *gin.Engine) {
	base := "/api/v1/projects/:id/assets/:assetId/bmc/collections"
	r.GET(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		page, pe := strconv.Atoi(c.DefaultQuery("page", "1"))
		size, se := strconv.Atoi(c.DefaultQuery("page_size", "20"))
		if pe != nil || se != nil {
			a.respondError(c, &platform.Error{Status: 400, Code: "VALIDATION_ERROR", Message: "Page and page_size must be integers"})
			return
		}
		out, e := a.store.ListBMCCollections(c.Request.Context(), actor, p, c.Param("assetId"), platform.Pagination{Page: page, PageSize: size})
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:collectionId", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.GetBMCCollection(c.Request.Context(), actor, p, c.Param("assetId"), c.Param("collectionId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:collectionId/raw", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		body, e := a.store.OpenBMCRaw(c.Request.Context(), actor, p, c.Param("assetId"), c.Param("collectionId"), c.Query("path"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		defer body.Close()
		c.Header("Content-Type", "application/json")
		c.Header("Cache-Control", "private, no-store")
		c.Header("Content-Disposition", `attachment; filename="bmc-resource.json"`)
		_, _ = io.Copy(c.Writer, body)
	})
}
