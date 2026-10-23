package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
)

// A bootstrap peer is always the direct TCP peer. Forwarded identity is never
// used to authorize a device or select a management subnet.
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

func singleBootHeader(r *http.Request, name string) (string, bool) {
	values := r.Header.Values(name)
	return firstHeader(values)
}
func firstHeader(values []string) (string, bool) {
	if len(values) != 1 {
		return "", false
	}
	return values[0], true
}

// The manifest is an authenticated review artifact on the application API.
// Device script delivery is exposed only by NewZTP239Handler.
func (a *authAPI) registerZTP239Manifest(r *gin.Engine) {
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
}

// NewZTP239Handler must be mounted on a dedicated, direct TLS listener in the
// management network. It has no browser session or application API routes.
func NewZTP239Handler(store *platform.Store) http.Handler {
	if store == nil {
		panic("ZTP 239 requires a store")
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.URL.Path != "/api/v1/boot/ztp" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.TLS == nil {
			http.Error(w, "TLS required", http.StatusForbidden)
			return
		}
		if r.Header.Get("Forwarded") != "" || r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("X-Real-Ip") != "" {
			http.Error(w, "direct device connection required", http.StatusForbidden)
			return
		}
		peer, ok := directBootPeer(r.RemoteAddr)
		if !ok {
			http.NotFound(w, r)
			return
		}
		names := []string{"CUMULUS-SERIAL", "CUMULUS-BASE-MAC", "CUMULUS-MGMT-MAC", "CUMULUS-PRODUCTNAME", "CUMULUS-VERSION", "CUMULUS-BUILD"}
		values := make([]string, len(names))
		for i, name := range names {
			value, unique := singleBootHeader(r, name)
			if !unique {
				http.Error(w, "exactly one of each Cumulus identity header is required", http.StatusBadRequest)
				return
			}
			values[i] = value
		}
		var trace [16]byte
		if _, err := rand.Read(trace[:]); err != nil {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		query := r.URL.Query()
		result, err := store.ServeZTP239(r.Context(), query.Get("project"), query.Get("batch"), query.Get("stage"), query.Get("policy"), peer, "https", r.Host, r.URL.Path, r.URL.RawQuery, hex.EncodeToString(trace[:]), platform.ZTP239Claim{Serial: values[0], BaseMAC: values[1], MgmtMAC: values[2], ProductName: values[3], Version: values[4], Build: values[5]})
		if err != nil {
			var pe *platform.Error
			if errors.As(err, &pe) {
				http.Error(w, pe.Message, pe.Status)
			} else if errors.Is(err, platform.ErrNotFound) {
				http.NotFound(w, r)
			} else {
				http.Error(w, "ZTP service unavailable", http.StatusServiceUnavailable)
			}
			return
		}
		w.Header().Set("X-Request-ID", hex.EncodeToString(trace[:]))
		w.Header().Set("X-Keel-Script-SHA256", result.SHA256)
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(result.Body))
	})
}
