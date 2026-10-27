package httpapi

import (
	"strconv"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerNetworkDeviceConfiguration(r *gin.Engine) {
	base := "/api/v1/projects/:id/fabric/devices/:deviceId"
	r.GET(base+"/configuration", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.NetworkDeviceConfiguration(c.Request.Context(), actor, p, c.Param("deviceId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/configuration-events", func(c *gin.Context) {
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
			a.respondError(c, &platform.Error{Status: 400, Code: "FABRIC_PAGE_INVALID", Message: "Page and page_size must be integers"})
			return
		}
		out, e := a.store.NetworkDeviceConfigurationEvents(c.Request.Context(), actor, p, c.Param("deviceId"), page, size)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
