package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"strings"
)

// Media registry and private version-pinned artifact routes.
func (a *authAPI) registerMedia(r *gin.Engine) {
	a.registerMediaScope(r, "/api/v1/projects/:id/media", false)
	a.registerMediaScope(r, "/api/v1/admin/media", true)
}

func (a *authAPI) registerMediaScope(r *gin.Engine, base string, global bool) {
	a.registerMediaArtifacts(r, base, global)
	r.POST(base+"/compatibility/validate", func(c *gin.Context) {
		if _, _, ok := a.mediaScope(c, true, global); !ok {
			return
		}
		var input platform.Compatibility
		if !decodeBody(c, &input) {
			return
		}
		matrix, err := platform.ValidateMediaCompatibility(input)
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.JSON(200, matrix)
	})
	r.GET(base, func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, c.Request.Method != "GET", global)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		var filter platform.MediaListFilter
		for _, key := range []string{"search", "type", "state", "compatible_arch", "stored_only"} {
			if len(c.Request.URL.Query()[key]) > 1 {
				failure(c, 400, "VALIDATION_ERROR", "Media filters must be single values")
				return
			}
		}
		stored := c.Query("stored_only")
		if stored != "" && stored != "true" && stored != "false" {
			failure(c, 400, "VALIDATION_ERROR", "stored_only must be true or false")
			return
		}
		filter = platform.MediaListFilter{Search: strings.TrimSpace(c.Query("search")), Type: c.Query("type"), State: c.Query("state"), CompatibleArch: c.Query("compatible_arch"), StoredOnly: stored == "true"}
		result, e := a.store.ListMediaFiltered(c.Request.Context(), actor, project, p, filter)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.POST(base, func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, c.Request.Method != "GET", global)
		if !ok {
			return
		}
		var input platform.RegisterMediaInput
		if !decodeBody(c, &input) {
			return
		}
		result, e := a.store.RegisterMedia(c.Request.Context(), actor, project, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, result)
	})
	r.POST(base+"/:mediaId/transition", func(c *gin.Context) {
		actor, project, ok := a.mediaScope(c, c.Request.Method != "GET", global)
		if !ok {
			return
		}
		var input platform.MediaTransitionInput
		if !decodeBody(c, &input) {
			return
		}
		cancel := mediaDeadline(c)
		defer cancel()
		result, e := a.store.TransitionMediaChecked(c.Request.Context(), actor, project, c.Param("mediaId"), input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
}
