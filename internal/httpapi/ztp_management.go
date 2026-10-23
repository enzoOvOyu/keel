package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"net/http"
)

func (a *authAPI) registerZTPManagement(r *gin.Engine) {
	base := "/api/v1/projects/:id/ztp/management-plan"
	for _, op := range []string{"preview", "commit", "latest"} {
		operation := op
		handler := func(c *gin.Context) {
			actor, ok := a.authenticated(c)
			if !ok {
				return
			}
			if operation != "latest" && !a.writeAllowed(c) {
				return
			}
			input := platform.ZTPManagementPlanInput{}
			if operation != "latest" && !decodeBody(c, &input) {
				return
			}
			project, e := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
			if e != nil {
				a.respondError(c, e)
				return
			}
			var out *platform.ZTPManagementPlan
			switch operation {
			case "preview":
				out, e = a.store.PreviewZTPManagementPlan(c.Request.Context(), actor, project, input)
			case "commit":
				out, e = a.store.CommitZTPManagementPlan(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
			default:
				out, e = a.store.LatestZTPManagementPlan(c.Request.Context(), actor, project)
			}
			if e != nil {
				a.respondError(c, e)
				return
			}
			c.JSON(http.StatusOK, out)
		}
		if operation == "latest" {
			r.GET(base, handler)
		} else {
			r.POST(base+"/"+operation, handler)
		}
	}
}
