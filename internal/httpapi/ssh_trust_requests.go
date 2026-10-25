package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerSSHTrustRequests(r *gin.Engine) {
	base := "/api/v1/projects/:id/ssh-trust-requests"
	r.GET(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.ListSSHTrustRequests(c.Request.Context(), actor, project, p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.POST(base, func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var in platform.CreateSSHTrustRequestInput
		if !decodeExecutionBody(c, &in) {
			return
		}
		project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		out, e := a.store.CreateSSHTrustRequest(c.Request.Context(), actor, project, in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
	for _, action := range []string{"approve", "revoke"} {
		r.POST(base+"/:requestId/"+action, func(c *gin.Context) {
			actor, ok := a.authenticated(c)
			if !ok {
				return
			}
			if !a.writeAllowed(c) {
				return
			}
			var in platform.SSHTrustRequestActionInput
			if action == "approve" {
				var approval struct {
					ExpectedVersion int64 `json:"expected_version"`
				}
				if !decodeExecutionBody(c, &approval) {
					return
				}
				in.ExpectedVersion = approval.ExpectedVersion
			} else if !decodeExecutionBody(c, &in) {
				return
			}
			project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
			if e != nil {
				a.respondError(c, e)
				return
			}
			out, e := a.store.ActSSHTrustRequest(c.Request.Context(), actor, project, c.Param("requestId"), action, in, c.GetHeader("Idempotency-Key"), c.GetString("trace_id"))
			if e != nil {
				a.respondError(c, e)
				return
			}
			c.JSON(200, out)
		})
	}
}
