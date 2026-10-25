package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) catalogAdmin(c *gin.Context, write bool) (*platform.AuthSession, bool) {
	actor, ok := a.authenticated(c)
	if !ok {
		return nil, false
	}
	if !actor.User.IsAdmin {
		a.respondError(c, platform.ErrForbidden)
		return nil, false
	}
	if write && !a.writeAllowed(c) {
		return nil, false
	}
	return actor, true
}

func (a *authAPI) registerCatalogAdmin(r *gin.Engine) {
	const base = "/api/v1/admin/params"
	r.GET(base, func(c *gin.Context) {
		actor, ok := a.catalogAdmin(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		retired := c.Query("include_retired")
		if retired != "" && retired != "true" && retired != "false" {
			a.respondError(c, &platform.Error{Status: 400, Code: "VALIDATION_ERROR", Message: "include_retired must be true or false"})
			return
		}
		out, e := a.store.ListParamKeysIncludingRetired(c.Request.Context(), actor, nil, p, c.Query("arch"), retired == "true")
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.GET(base+"/media-options", func(c *gin.Context) {
		actor, ok := a.catalogAdmin(c, false)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		out, e := a.store.ListMediaFiltered(c.Request.Context(), actor, nil, p, platform.MediaListFilter{State: "approved", StoredOnly: true})
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
	r.PUT(base, func(c *gin.Context) {
		actor, ok := a.catalogAdmin(c, true)
		if !ok {
			return
		}
		var input platform.UpsertParamKeyInput
		if !decodeBody(c, &input) {
			return
		}
		out, e := a.store.UpsertParamKey(c.Request.Context(), actor, nil, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, out)
	})
	r.POST(base+"/:keyId/retire", func(c *gin.Context) {
		actor, ok := a.catalogAdmin(c, true)
		if !ok {
			return
		}
		var input platform.RetireParamKeyInput
		if c.Request.ContentLength != 0 && !decodeBody(c, &input) {
			return
		}
		out, e := a.store.RetireParamKeyChecked(c.Request.Context(), actor, nil, c.Param("keyId"), input.ExpectedUpdatedAt, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, out)
	})
}
