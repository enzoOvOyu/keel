package httpapi

import (
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"testing"
)

func TestDirectBootPeerIgnoresForwardedHeader(t *testing.T) {
	req := httptest.NewRequest("GET", "http://boot.example.invalid/api/v1/boot/ztp", nil)
	req.RemoteAddr = "192.0.2.8:4567"
	req.Header.Set("X-Forwarded-For", "198.51.100.9")
	got, ok := directBootPeer(req.RemoteAddr)
	if !ok || got != "192.0.2.8" {
		t.Fatalf("TCP peer changed by forwarded header: %q", got)
	}
	for _, bad := range []string{"198.51.100.9", "[2001:db8::1]:1234", "not-an-ip:1234"} {
		if _, ok := directBootPeer(bad); ok {
			t.Fatalf("bad TCP peer accepted: %q", bad)
		}
	}
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	req.Header.Add("CUMULUS-SERIAL", "SW-1")
	req.Header.Add("CUMULUS-SERIAL", "SW-2")
	if _, ok := singleCumulusHeader(c, "CUMULUS-SERIAL"); ok {
		t.Fatal("ambiguous identity header accepted")
	}
}
