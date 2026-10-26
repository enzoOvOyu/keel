package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerProvisionRequests(r *gin.Engine) {
	a.registerProvisionKeyRepairs(r)
	r.GET("/api/v1/projects/:id/provision/os-images/:mediaId/dependencies", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		out, err := a.store.ProvisionOSDependencies(c.Request.Context(), actor, project, c.Param("mediaId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
	r.GET("/api/v1/projects/:id/provision/requests/:requestId/rollout", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, err := a.store.PreviewProvisionRollout(c.Request.Context(), actor, c.Param("id"), c.Param("requestId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})

	const base = "/api/v1/projects/:id/provision/requests"
	r.GET("/api/v1/projects/:id/provision/public-keys", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		out, err := a.store.ProvisionPublicKeys(c.Request.Context(), actor, c.Param("id"), p)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
	r.GET(base, func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		out, err := a.store.ListProvisionRequests(c.Request.Context(), actor, c.Param("id"), p)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
	r.GET(base+"/:requestId", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, err := a.store.ReadProvisionRequest(c.Request.Context(), actor, c.Param("id"), c.Param("requestId"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(200, out)
	})
	r.POST("/api/v1/projects/:id/assets/:assetId/provision/requests", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.CreateProvisionRequestInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		out, err := a.store.CreateProvisionRequest(c.Request.Context(), actor, project, c.Param("assetId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(201, out)
	})
	for _, action := range []string{"approve", "cancel"} {
		action := action
		r.POST(base+"/:requestId/"+action, func(c *gin.Context) {
			actor, _, ok := a.executionProject(c, true)
			if !ok {
				return
			}
			var in platform.ProvisionRequestActionInput
			if !decodeExecutionBody(c, &in) {
				return
			}
			out, err := a.store.ChangeProvisionRequest(c.Request.Context(), actor, c.Param("id"), c.Param("requestId"), action, in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
			if err != nil {
				a.respondError(c, err)
				return
			}
			c.Header("Cache-Control", "private, no-store")
			c.JSON(200, out)
		})
	}
	r.POST(base+"/:requestId/prepare", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in struct {
			ExpectedVersion int64 `json:"expected_version"`
		}
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, err := service.CreateProvisionPreparation(c.Request.Context(), actor, c.Param("id"), c.Param("requestId"), in.ExpectedVersion, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "private, no-store")
		c.JSON(201, out)
	})
}
