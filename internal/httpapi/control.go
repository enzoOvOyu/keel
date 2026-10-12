package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"strconv"
	"time"
)

func readPagination(c *gin.Context) (platform.Pagination, bool) {
	p := platform.Pagination{Page: 1, PageSize: 20}
	for name, destination := range map[string]*int{"page": &p.Page, "page_size": &p.PageSize} {
		if raw, ok := c.GetQuery(name); ok {
			value, e := strconv.Atoi(raw)
			if e != nil {
				failure(c, 400, "VALIDATION_ERROR", "Invalid pagination")
				return p, false
			}
			*destination = value
		}
	}
	if e := p.Validate(); e != nil {
		failure(c, 400, "VALIDATION_ERROR", e.(*platform.Error).Message)
		return p, false
	}
	return p, true
}

// registerControl is mounted by the root router only when a configured store is present.
func (a *authAPI) registerControl(r *gin.Engine) {
	r.GET("/api/v1/admin/users", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		result, e := a.store.ListUsers(c.Request.Context(), s, p, c.Query("q"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.POST("/api/v1/admin/users", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.CreateUserInput
		if !decodeBody(c, &input) {
			return
		}
		result, e := a.store.ControlCreateUser(c.Request.Context(), s, input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, result)
	})
	r.PATCH("/api/v1/admin/users/:userId", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.UpdateUserInput
		if !decodeBody(c, &input) {
			return
		}
		result, e := a.store.ControlUpdateUser(c.Request.Context(), s, c.Param("userId"), input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.POST("/api/v1/admin/users/:userId/password", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.ResetPasswordInput
		if !decodeBody(c, &input) {
			return
		}
		result, e := a.store.ControlResetPassword(c.Request.Context(), s, c.Param("userId"), input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.POST("/api/v1/admin/users/:userId/unlock", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.UnlockUserInput
		if !decodeBody(c, &input) {
			return
		}
		result, e := a.store.ControlUnlockUser(c.Request.Context(), s, c.Param("userId"), input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.POST("/api/v1/auth/password", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.ChangePasswordInput
		if !decodeBody(c, &input) {
			return
		}
		if e := a.store.ChangeOwnPassword(c.Request.Context(), s, input, sourceIP(c), c.GetString("trace_id")); e != nil {
			a.respondError(c, e)
			return
		}
		a.cookie(c, "", time.Time{})
		c.Status(204)
	})
	r.GET("/api/v1/projects/:id/members", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		result, e := a.store.ListMembers(c.Request.Context(), s, c.Param("id"), p)
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.POST("/api/v1/projects/:id/members", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.CreateMemberInput
		if !decodeBody(c, &input) {
			return
		}
		result, e := a.store.ControlCreateMember(c.Request.Context(), s, c.Param("id"), input, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(201, result)
	})
	r.PATCH("/api/v1/projects/:id/members/:userId", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.UpdateMemberInput
		if !decodeBody(c, &input) {
			return
		}
		result, e := a.store.ControlChangeMember(c.Request.Context(), s, c.Param("id"), c.Param("userId"), input, false, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.DELETE("/api/v1/projects/:id/members/:userId", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		if !a.writeAllowed(c) {
			return
		}
		var input platform.VersionInput
		if !decodeBody(c, &input) {
			return
		}
		_, e := a.store.ControlChangeMember(c.Request.Context(), s, c.Param("id"), c.Param("userId"), platform.UpdateMemberInput{Version: input.Version}, true, c.GetString("trace_id"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.Status(204)
	})
	r.GET("/api/v1/admin/audits", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		result, e := a.store.ListAudits(c.Request.Context(), s, "", p, c.Query("action"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
	r.GET("/api/v1/projects/:id/audits", func(c *gin.Context) {
		s, ok := a.authenticated(c)
		if !ok {
			return
		}
		p, ok := readPagination(c)
		if !ok {
			return
		}
		result, e := a.store.ListAudits(c.Request.Context(), s, c.Param("id"), p, c.Query("action"))
		if e != nil {
			a.respondError(c, e)
			return
		}
		c.JSON(200, result)
	})
}
