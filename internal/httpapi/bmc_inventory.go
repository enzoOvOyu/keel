package httpapi

import (
	"io"
	"strconv"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerBMCInventory(r *gin.Engine) {
	a.registerBMCPolicies(r)
	a.registerBMCCredentialVerification(r)
	base := "/api/v1/projects/:id/assets/:assetId/bmc/collections"
	r.POST(base, func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		service.BMCStore = a.store
		var in platform.BMCCollectionInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := service.CreateBMCCollection(c.Request.Context(), actor, c.Param("id"), c.Param("assetId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(202, out)
	})
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
	r.GET(base+"/:collectionId/manifest", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.GetBMCCollectionManifest(c.Request.Context(), actor, &platform.ProjectSummary{ID: c.Param("id")}, c.Param("assetId"), c.Param("collectionId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:collectionId/resources", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListBMCResources(c.Request.Context(), actor, &platform.ProjectSummary{ID: c.Param("id")}, c.Param("assetId"), c.Param("collectionId"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/:collectionId/resource", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.GetBMCResource(c.Request.Context(), actor, &platform.ProjectSummary{ID: c.Param("id")}, c.Param("assetId"), c.Param("collectionId"), c.Query("path"))
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
