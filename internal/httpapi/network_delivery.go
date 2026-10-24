package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/fabricdelivery"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"strconv"
)

func (a *authAPI) networkDeliveryProject(c *gin.Context) (*platform.AuthSession, *platform.ProjectSummary, bool) {
	actor, ok := a.authenticated(c)
	if !ok {
		return nil, nil, false
	}
	p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
	if e != nil {
		a.respondError(c, e)
		return nil, nil, false
	}
	return actor, p, true
}
func (a *authAPI) registerNetworkDelivery(r *gin.Engine) {
	base := "/api/v1/projects/:id/fabric/delivery"
	r.GET(base+"/preflight", func(c *gin.Context) {
		actor, p, ok := a.networkDeliveryProject(c)
		if !ok {
			return
		}
		out, e := a.store.PreviewNetworkDelivery(c.Request.Context(), actor, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/schema", func(c *gin.Context) {
		_, _, ok := a.networkDeliveryProject(c)
		if !ok {
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Data(200, "application/schema+json", fabricdelivery.JSONSchema)
	})
	r.GET(base+"/references", func(c *gin.Context) {
		actor, p, ok := a.networkDeliveryProject(c)
		if !ok {
			return
		}
		page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
		if e != nil {
			failure(c, 400, "NETWORK_DELIVERY_PAGE_INVALID", "Invalid page")
			return
		}
		size, e := strconv.Atoi(c.DefaultQuery("page_size", "200"))
		if e != nil {
			failure(c, 400, "NETWORK_DELIVERY_PAGE_INVALID", "Invalid page size")
			return
		}
		out, e := a.store.NetworkDeliveryReferences(c.Request.Context(), actor, p, page, size)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/exports", func(c *gin.Context) {
		actor, p, ok := a.networkDeliveryProject(c)
		if !ok {
			return
		}
		out, e := a.store.ListNetworkDeliveryExports(c.Request.Context(), actor, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, gin.H{"items": out})
	})
	r.POST(base+"/exports", func(c *gin.Context) {
		actor, p, ok := a.networkDeliveryProject(c)
		if !ok || !a.writeAllowed(c) {
			return
		}
		var input platform.NetworkDeliveryExportInput
		if !decodeBody(c, &input) {
			return
		}
		out, e := a.store.CreateNetworkDeliveryExport(c.Request.Context(), actor, p, input, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
	download := func(c *gin.Context) {
		actor, p, ok := a.networkDeliveryProject(c)
		if !ok {
			return
		}
		id := c.Param("exportId")
		if id == "" {
			id = c.Query("export_id")
		}
		if format := c.Query("format"); format != "" && format != "json" {
			failure(c, 400, "NETWORK_DELIVERY_FORMAT_INVALID", "This version exports the defined JSON schema")
			return
		}
		out, body, e := a.store.DownloadNetworkDeliveryExport(c.Request.Context(), actor, p, id, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Keel-Content-SHA256", out.ContentHash)
		c.Header("Content-Disposition", "attachment; filename=keel-network-"+out.Kind+"-"+out.ID+".json")
		c.Data(200, "application/json", body)
	}
	r.GET(base+"/exports/:exportId/download", download)
	// A documented federation alias downloads an explicitly selected immutable
	// archive; it never silently substitutes a draft or stale live baseline.
	r.GET("/api/v1/projects/:id/fabric/baseline/export", download)
}
