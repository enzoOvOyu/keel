package httpapi

import (
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"io"
	"mime"
	"net/http"
	"strconv"
)

// Execution requests have their own bounded decoder: 2000 immutable target IDs
// exceed the 64 KiB auth payload limit. Authentication routes keep that lower cap.
func decodeExecutionBody(c *gin.Context, target any) bool {
	kind, _, e := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if e != nil || kind != "application/json" {
		failure(c, 415, "UNSUPPORTED_MEDIA_TYPE", "Content-Type must be application/json")
		return false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512*1024)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	e = dec.Decode(target)
	if e == nil {
		var extra any
		e = dec.Decode(&extra)
		if e == io.EOF {
			return true
		}
		if e == nil {
			e = errors.New("multiple JSON values")
		}
	}
	var size *http.MaxBytesError
	if errors.As(e, &size) {
		failure(c, 413, "REQUEST_TOO_LARGE", "Execution payload exceeds 512 KiB")
	} else {
		failure(c, 400, "VALIDATION_ERROR", "Invalid JSON or unsupported execution fields")
	}
	return false
}
func (a *authAPI) executionProject(c *gin.Context, write bool) (*platform.AuthSession, *execution.Service, bool) {
	actor, ok := a.authenticated(c)
	if !ok {
		return nil, nil, false
	}
	if write && !a.writeAllowed(c) {
		return nil, nil, false
	}
	if _, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id")); e != nil {
		a.respondError(c, e)
		return nil, nil, false
	}
	service := execution.New(a.store.DB)
	service.MediaStore = a.store
	service.ProvisionStore = a.store
	service.AgentAdmissionOnly = a.store.AgentTasksEnabled
	if a.store.SSHRunner != nil {
		service.SSHRunner = a.store.SSHRunner
	}
	return actor, service, true
}
func (a *authAPI) registerExecution(r *gin.Engine) {
	base := "/api/v1/projects/:id"
	a.registerSSHExecution(r, base)
	a.registerAgentExecution(r, base)
	r.GET(base+"/sim-targets", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		rows, e := s.ListTargets(c.Request.Context(), c.Param("id"), p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, rows)
	})
	r.POST(base+"/sim-targets", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.RegisterInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		rows, e := s.RegisterTargets(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, gin.H{"items": rows, "mode": "sim"})
	})
	r.GET(base+"/flows", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		rows, e := s.ListFlows(c.Request.Context(), c.Param("id"), "", p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, rows)
	})
	r.POST(base+"/flows", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.PublishInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		v, e := s.Publish(c.Request.Context(), actor, c.Param("id"), "", in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, v)
	})
	r.GET(base+"/flows/:flowId/versions", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		rows, e := s.ListFlows(c.Request.Context(), c.Param("id"), c.Param("flowId"), p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, rows)
	})
	r.POST(base+"/flows/:flowId/versions", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.PublishInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		v, e := s.Publish(c.Request.Context(), actor, c.Param("id"), c.Param("flowId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, v)
	})
	r.GET(base+"/executions", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		rows, e := s.List(c.Request.Context(), c.Param("id"), p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, rows)
	})
	r.POST(base+"/executions", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.CreateInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		x, e := s.Create(c.Request.Context(), actor, c.Param("id"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, x)
	})
	r.GET(base+"/executions/:executionId", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		x, e := s.Get(c.Request.Context(), c.Param("id"), c.Param("executionId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, x)
	})
	r.GET(base+"/executions/:executionId/attempts", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := s.Attempts(c.Request.Context(), c.Param("id"), c.Param("executionId"), p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base+"/executions/:executionId/control", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.ControlInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		x, e := s.Control(c.Request.Context(), actor, c.Param("id"), c.Param("executionId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, x)
	})
	r.POST(base+"/executions/:executionId/attempts/:attemptId/reconcile", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, true)
		if !ok {
			return
		}
		var in execution.ReconcileInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		x, e := s.Reconcile(c.Request.Context(), actor, c.Param("id"), c.Param("executionId"), c.Param("attemptId"), in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, x)
	})
	r.GET(base+"/executions/:executionId/events", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		var after int64
		var e error
		if text := c.Query("after"); text != "" {
			after, e = strconv.ParseInt(text, 10, 64)
			if e != nil || after < 0 {
				failure(c, 400, "VALIDATION_ERROR", "after must be a non-negative event sequence")
				return
			}
		}
		events, e := s.Events(c.Request.Context(), c.Param("id"), c.Param("executionId"), after)
		if e != nil {
			a.respondError(c, e)
			return
		}
		next := after
		if len(events) > 0 {
			next = events[len(events)-1].Sequence
		}
		mode, e := s.ExecutionMode(c.Request.Context(), c.Param("id"), c.Param("executionId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, gin.H{"items": events, "next_cursor": next, "mode": mode})
	})
}
