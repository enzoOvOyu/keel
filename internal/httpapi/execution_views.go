package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

func viewPagination(c *gin.Context, max int) (int, int, bool) {
	page, e := strconv.Atoi(c.DefaultQuery("page", "1"))
	if e != nil || page < 1 || page > 1000000 {
		failure(c, 400, "VALIDATION_ERROR", "Invalid view page")
		return 0, 0, false
	}
	size, e := strconv.Atoi(c.DefaultQuery("page_size", "100"))
	if e != nil || size < 1 || size > max {
		failure(c, 400, "VALIDATION_ERROR", "Invalid view page size")
		return 0, 0, false
	}
	return page, size, true
}
func streamCursor(c *gin.Context) (int64, bool) {
	value := c.Query("after")
	if header := c.GetHeader("Last-Event-ID"); header != "" {
		value = header
	}
	if value == "" {
		return 0, true
	}
	cursor, e := strconv.ParseInt(value, 10, 64)
	if e != nil || cursor < 0 {
		failure(c, 400, "VALIDATION_ERROR", "Invalid event cursor")
		return 0, false
	}
	return cursor, true
}
func streamFrame(c *gin.Context, kind string, cursor int64, data any) bool {
	raw, e := json.Marshal(data)
	if e != nil {
		return false
	}
	_, e = fmt.Fprintf(c.Writer, "id: %d\nevent: %s\ndata: %s\n\n", cursor, kind, raw)
	if e != nil {
		return false
	}
	c.Writer.Flush()
	return true
}
func (a *authAPI) registerExecutionViews(r *gin.Engine) {
	base := "/api/v1/projects/:id/executions/:executionId"
	r.GET(base+"/overview", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := s.Overview(c.Request.Context(), c.Param("id"), c.Param("executionId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/matrix", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, size, ok := viewPagination(c, 500)
		if !ok {
			return
		}
		out, e := s.Matrix(c.Request.Context(), c.Param("id"), c.Param("executionId"), page, size, c.Query("state"), c.Query("q"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/journal", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		cursor, ok := streamCursor(c)
		if !ok {
			return
		}
		out, e := s.Journal(c.Request.Context(), c.Param("id"), c.Param("executionId"), cursor)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/outputs", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		page, size, ok := viewPagination(c, 100)
		if !ok {
			return
		}
		out, e := s.Outputs(c.Request.Context(), c.Param("id"), c.Param("executionId"), page, size)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/outputs/:attemptId/download", func(c *gin.Context) {
		actor, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := s.ExportOutput(c.Request.Context(), actor, c.Param("id"), c.Param("executionId"), c.Param("attemptId"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.Header("Content-Disposition", "attachment; filename=execution-output.json")
		c.JSON(200, out)
	})
	r.GET(base+"/relations", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := s.Relations(c.Request.Context(), c.Param("id"), c.Param("executionId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/attempts/:attemptId", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		out, e := s.AttemptDetail(c.Request.Context(), c.Param("id"), c.Param("executionId"), c.Param("attemptId"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/stream", func(c *gin.Context) {
		_, s, ok := a.executionProject(c, false)
		if !ok {
			return
		}
		cursor, ok := streamCursor(c)
		if !ok {
			return
		}
		initial, e := s.Journal(c.Request.Context(), c.Param("id"), c.Param("executionId"), cursor)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.Header("Content-Type", "text/event-stream; charset=utf-8")
		c.Header("Cache-Control", "no-store")
		c.Header("X-Accel-Buffering", "no")
		if initial.ResyncRequired {
			streamFrame(c, "resync", initial.NextCursor, initial)
			return
		}
		if !streamFrame(c, "snapshot", initial.NextCursor, initial) {
			return
		}
		cursor = initial.NextCursor
		// Rotate before the API's 30s write deadline. Reconnection resumes from cursor.
		streamContext, cancel := context.WithTimeout(c.Request.Context(), 25*time.Second)
		defer cancel()
		deadline := time.NewTimer(25 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-streamContext.Done():
				return
			case <-deadline.C:
				streamFrame(c, "rotate", cursor, gin.H{"next_cursor": cursor})
				return
			case <-tick.C:
			}
			actor, e := a.store.Authenticate(streamContext, cookieToken(c))
			if e != nil {
				streamFrame(c, "access_revoked", cursor, gin.H{"reason": "session_expired_or_revoked"})
				return
			}
			if _, e = a.store.GetProject(streamContext, actor, c.Param("id")); e != nil {
				streamFrame(c, "access_revoked", cursor, gin.H{"reason": "project_access_changed"})
				return
			}
			for {
				select {
				case <-streamContext.Done():
					return
				default:
				}
				page, e := s.Journal(streamContext, c.Param("id"), c.Param("executionId"), cursor)
				if e != nil {
					streamFrame(c, "unavailable", cursor, gin.H{"reason": "execution_stream_unavailable"})
					return
				}
				if page.ResyncRequired {
					streamFrame(c, "resync", page.NextCursor, page)
					return
				}
				for _, event := range page.Items {
					if !streamFrame(c, "execution", event.Sequence, event) {
						return
					}
					cursor = event.Sequence
				}
				if !page.HasMore {
					break
				}
				actor, e := a.store.Authenticate(streamContext, cookieToken(c))
				if e != nil {
					streamFrame(c, "access_revoked", cursor, gin.H{"reason": "session_expired_or_revoked"})
					return
				}
				if _, e = a.store.GetProject(streamContext, actor, c.Param("id")); e != nil {
					streamFrame(c, "access_revoked", cursor, gin.H{"reason": "project_access_changed"})
					return
				}
			}
			if _, e = fmt.Fprint(c.Writer, ": keepalive\n\n"); e != nil {
				return
			}
			c.Writer.Flush()
		}
	})
}
