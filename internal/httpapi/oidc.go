package httpapi

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

func (a *authAPI) registerOIDC(r *gin.Engine) {
	r.GET("/api/v1/auth/oidc/:provider/start", func(c *gin.Context) {
		if !a.host(c) {
			return
		}
		if strings.EqualFold(c.GetHeader("Sec-Fetch-Site"), "cross-site") || (c.GetHeader("Origin") != "" && !a.config.AllowedOrigin(c.GetHeader("Origin"))) {
			failure(c, 403, "ORIGIN_FORBIDDEN", "Request origin is not allowed")
			return
		}
		if !a.store.OIDC.Has(c.Param("provider")) {
			failure(c, 503, "AUTH_NOT_CONFIGURED", "Organization sign-in is not enabled")
			return
		}
		if !a.allowPre(c) {
			c.Header("Retry-After", "60")
			failure(c, 429, "RATE_LIMITED", "Too many authentication requests")
			return
		}
		ready, err := a.store.HasAdministrator(c.Request.Context())
		if err != nil {
			a.respondError(c, err)
			return
		}
		if !ready {
			failure(c, 503, "SETUP_REQUIRED", "Create the first administrator on the server")
			return
		}
		token := cookieToken(c)
		err = a.store.OIDCPreSession(c.Request.Context(), token)
		if err != nil {
			if !errors.Is(err, platform.ErrUnauthenticated) {
				a.respondError(c, err)
				return
			}
			var expiry time.Time
			token, expiry, err = a.store.NewPreSession(c.Request.Context())
			if err != nil {
				a.respondError(c, err)
				return
			}
			a.cookie(c, token, expiry)
		}
		destination, err := a.store.OIDC.Start(c.Param("provider"), platform.TokenHash(token), c.Query("next"))
		if err != nil {
			failure(c, 401, "INVALID_CREDENTIALS", "Organization sign-in failed")
			return
		}
		c.Header("Referrer-Policy", "no-referrer")
		c.Redirect(http.StatusFound, destination)
	})
	r.GET("/api/v1/auth/oidc/:provider/callback", func(c *gin.Context) {
		if !a.host(c) {
			return
		}
		c.Header("Referrer-Policy", "no-referrer")
		redirectFailure := func() { c.Redirect(http.StatusSeeOther, "/auth/callback?status=failed") }
		fail := func() {
			_ = a.store.RecordOIDCFailure(c.Request.Context(), c.Param("provider"), sourceIP(c), c.GetString("trace_id"))
			redirectFailure()
		}
		if !a.store.OIDC.Has(c.Param("provider")) {
			redirectFailure()
			return
		}
		if !a.allowPre(c) {
			c.Header("Retry-After", "60")
			redirectFailure()
			return
		}
		query, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil || len(query["state"]) != 1 || len(query["error"]) > 1 || len(query["iss"]) > 1 {
			fail()
			return
		}
		token := cookieToken(c)
		if query.Get("error") != "" || !a.store.OIDC.AcceptIssuer(c.Param("provider"), query.Get("iss")) {
			a.store.OIDC.Abort(c.Param("provider"), platform.TokenHash(token), query.Get("state"))
			fail()
			return
		}
		if len(query["code"]) != 1 {
			fail()
			return
		}
		if a.store.OIDCPreSession(c.Request.Context(), token) != nil {
			fail()
			return
		}
		name, next, err := a.store.OIDC.Complete(c.Request.Context(), c.Param("provider"), platform.TokenHash(token), query.Get("state"), query.Get("code"))
		if err != nil {
			fail()
			return
		}
		fresh, _, err := a.store.LoginOIDC(c.Request.Context(), token, name, c.Param("provider"), sourceIP(c), c.GetString("trace_id"))
		if err != nil {
			fail()
			return
		}
		a.cookie(c, fresh, time.Now().UTC().Add(platform.AbsoluteTimeout))
		c.Redirect(http.StatusSeeOther, "/auth/callback?status=ok&next="+url.QueryEscape(next))
	})
}
