package httpapi

import "github.com/gin-gonic/gin"

func (a *authAPI) registerGateDashboard(r *gin.Engine) {
	r.GET("/api/v1/projects/:id/gates", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		out, e := a.store.GateDashboard(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
