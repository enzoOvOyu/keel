package httpapi

import "github.com/gin-gonic/gin"

func (a *authAPI) registerProvisionInstallationProgress(r *gin.Engine) {
	r.GET("/api/v1/projects/:id/provision/installations/:installationId/progress", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, err := a.store.ReadProvisionInstallationProgress(c.Request.Context(), actor, c.Param("id"), c.Param("installationId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
}
