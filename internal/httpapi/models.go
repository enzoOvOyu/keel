package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

// The legacy list and both management scopes share the same model service.
func (a *authAPI) registerModels(r *gin.Engine) {
	r.GET("/api/v1/models", func(c *gin.Context) {
		actor, _, ok := a.modelScope(c, false, true)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		result, err := a.store.ListDeviceModels(c.Request.Context(), actor, nil, p)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, result)
	})
	a.registerModelsScope(r, "/api/v1/projects/:id/models", false)
	a.registerModelsScope(r, "/api/v1/admin/models", true)
}

func (a *authAPI) modelScope(c *gin.Context, write, global bool) (*platform.AuthSession, *platform.ProjectSummary, bool) {
	actor, ok := a.authenticated(c)
	if !ok {
		return nil, nil, false
	}
	if global && !actor.User.IsAdmin {
		a.respondError(c, platform.ErrForbidden)
		return nil, nil, false
	}
	if write && !a.writeAllowed(c) {
		return nil, nil, false
	}
	if global {
		return actor, nil, true
	}
	project, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id"))
	if err != nil {
		a.respondError(c, err)
		return nil, nil, false
	}
	return actor, project, true
}

func (a *authAPI) registerModelsScope(r *gin.Engine, base string, global bool) {
	r.GET(base, func(c *gin.Context) {
		actor, project, ok := a.modelScope(c, false, global)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		result, err := a.store.ListDeviceModels(c.Request.Context(), actor, project, p)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, result)
	})
	r.POST(base, func(c *gin.Context) {
		actor, project, ok := a.modelScope(c, true, global)
		if !ok {
			return
		}
		var input platform.CreateDeviceModelInput
		if !decodeBody(c, &input) {
			return
		}
		result, err := a.store.CreateDeviceModel(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(201, result)
	})
	r.POST(base+"/:modelId/retire", func(c *gin.Context) {
		actor, project, ok := a.modelScope(c, true, global)
		if !ok {
			return
		}
		result, err := a.store.RetireDeviceModel(c.Request.Context(), actor, project, c.Param("modelId"), c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, result)
	})
	r.GET(base+"/:modelId/versions", func(c *gin.Context) {
		actor, project, ok := a.modelScope(c, false, global)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		result, err := a.store.ListModelVersions(c.Request.Context(), actor, project, c.Param("modelId"), p)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, result)
	})
	r.POST(base+"/:modelId/versions", func(c *gin.Context) {
		actor, project, ok := a.modelScope(c, true, global)
		if !ok {
			return
		}
		var input platform.PublishModelVersionInput
		if !decodeExecutionBody(c, &input) {
			return
		}
		result, err := a.store.PublishModelVersion(c.Request.Context(), actor, project, c.Param("modelId"), input, c.GetString("trace_id"))
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(201, result)
	})
}
