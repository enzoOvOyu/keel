package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	"net/netip"
)

// The 239 endpoint is intentionally unauthenticated for Cumulus bootstrap.
// Its source check uses the actual TCP peer, never X-Forwarded-For; deployments
// with a reverse proxy must expose it through a dedicated management listener.
func directBootPeer(remote string) (string, bool) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return "", false
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.Is4() {
		return "", false
	}
	return ip.String(), true
}
func singleCumulusHeader(c *gin.Context, name string) (string, bool) {
	values := c.Request.Header.Values(name)
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}

func (a *authAPI) registerZTP239Boot(r *gin.Engine) {
	r.GET("/api/v1/projects/:id/ztp/isolation-script", func(c *gin.Context) {
		actor, ok := a.authenticated(c)
		if !ok {
			return
		}
		if _, err := a.store.GetProject(c.Request.Context(), actor, c.Param("id")); err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, gin.H{"type": "ztp_script", "version": "isolation-v1", "sha256": fabricboot.IsolationScriptDigest(), "size": len(fabricboot.IsolationScriptV1), "os_scope": "cumulus-linux-5.x", "script": fabricboot.IsolationScriptV1})
	})
	r.GET("/api/v1/boot/ztp", func(c *gin.Context) {
		peer, ok := directBootPeer(c.Request.RemoteAddr)
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}
		headers := []string{"CUMULUS-SERIAL", "CUMULUS-BASE-MAC", "CUMULUS-MGMT-MAC", "CUMULUS-PRODUCTNAME", "CUMULUS-VERSION", "CUMULUS-BUILD"}
		values := make([]string, len(headers))
		for i, name := range headers {
			value, unique := singleCumulusHeader(c, name)
			if !unique {
				c.JSON(http.StatusBadRequest, gin.H{"code": "ZTP_239_HEADERS_INVALID", "message": "Exactly one of each Cumulus identity header is required"})
				return
			}
			values[i] = value
		}
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		script, err := a.store.ServeZTP239(c.Request.Context(), c.Query("project"), c.Query("batch"), c.Query("stage"), c.Query("policy"), peer, scheme, c.Request.Host, c.Request.URL.Path, c.Request.URL.RawQuery, c.GetString("trace_id"), platform.ZTP239Claim{
			Serial: values[0], BaseMAC: values[1], MgmtMAC: values[2], ProductName: values[3], Version: values[4], Build: values[5],
		})
		if err != nil {
			a.respondError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Keel-Script-SHA256", script.SHA256)
		c.Data(http.StatusOK, "text/x-shellscript; charset=utf-8", []byte(script.Body))
	})
}
