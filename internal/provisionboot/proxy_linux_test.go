//go:build linux

package provisionboot

import (
	"context"
	"testing"
)

func TestProxyRequiresExplicitEnableAndReservedPorts(t *testing.T) {
	scope, authority := fixture()
	if err := ServeProxy(context.Background(), ProxyConfig{Scope: scope}, authority); err == nil {
		t.Fatal("disabled proxy attempted to listen")
	}
	if _, _, _, err := (ProxyConfig{Enabled: true, Scope: scope, DiscoverPort: 3067}).ports(); err == nil {
		t.Fatal("production arbitrary network port accepted")
	}
	scope.Development = true
	if _, _, _, err := (ProxyConfig{Enabled: true, Scope: scope, DiscoverPort: 3067, PXEPort: 3068, ClientPort: 3069}).ports(); err != nil {
		t.Fatal("isolated software ports rejected", err)
	}
}
