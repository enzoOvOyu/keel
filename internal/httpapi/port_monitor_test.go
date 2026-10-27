package httpapi

import (
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/gin-gonic/gin"
	"testing"
)

func TestPortMonitorRoutesCoexistWithAssetMappingAndMonitoring(t *testing.T) {
	router := newRouter("port-monitor-fixture", t.TempDir(), &platform.Store{}, platform.Config{}).(*gin.Engine)
	paths := map[string]bool{}
	for _, route := range router.Routes() {
		if route.Method == "GET" {
			paths[route.Path] = true
		}
	}
	for _, path := range []string{"/api/v1/projects/:id/assets/:assetId", "/api/v1/projects/:id/ports", "/api/v1/projects/:id/assets/:assetId/ports", "/api/v1/projects/:id/assets/:assetId/ports/stream", "/api/v1/projects/:id/assets/:assetId/ports/:portId/events", "/api/v1/projects/:id/assets/:assetId/monitor", "/api/v1/projects/:id/assets/:assetId/mapping/preview"} {
		if !paths[path] {
			t.Fatal("missing scoped route", path)
		}
	}
}
