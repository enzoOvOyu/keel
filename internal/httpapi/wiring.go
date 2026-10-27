package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerWiring(r *gin.Engine) {
	base := "/api/v1/projects/:id/validation/wiring"
	r.POST(base+"/executions", func(c *gin.Context) {
		actor, service, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.WiringRunInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		service.WiringStore = a.store
		out, e := service.CreateWiringExecution(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/batches", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.WiringBatchInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := a.store.CreateWiringBatch(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/batches/:batchId", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.GetWiringBatch(c.Request.Context(), actor, c.Param("id"), c.Param("batchId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})

	r.POST(base+"/scope", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in struct {
			Targets []platform.WiringTargetInput `json:"targets"`
		}
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := a.store.PreviewWiring(c.Request.Context(), actor, c.Param("id"), in.Targets)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/handshakes", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in platform.WiringHandshakeInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		out, e := a.store.CreateWiringHandshake(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/handshakes", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListWiringHandshakes(c.Request.Context(), actor, c.Param("id"), page)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/handshakes/:handshakeId", func(c *gin.Context) {
		actor, _, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := a.store.GetWiringHandshake(c.Request.Context(), actor, c.Param("id"), c.Param("handshakeId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
