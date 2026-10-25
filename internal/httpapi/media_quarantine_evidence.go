package httpapi

import "github.com/gin-gonic/gin"

func (a *authAPI) registerMediaQuarantineEvidence(r *gin.Engine, base string, global bool) {
	r.GET(base+"/:mediaId/quarantine-evidence", func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, false, global)
		if !ok {
			return
		}
		evidence, err := a.store.GetMediaQuarantineEvidence(c.Request.Context(), actor, project, c.Param("mediaId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.JSON(200, evidence)
	})
}
