package platform

import (
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"testing"
)

func TestNetworkOpticsUsesModeledConnectedPorts(t *testing.T) {
	no := false
	cables := []fabricmon.Cable{{A: fabricmon.Endpoint{Switch: true, Serial: "s", Port: "swp1"}}}
	sample := &fabricmon.Snapshot{Ports: []fabricmon.Port{{Name: "swp1", OpticsApplicable: &no}, {Name: "unused"}}}
	if got := networkOptics(sample, "s", cables, nil); got.State != "PASS" {
		t.Fatalf("explicit copper and unused ports: %+v", got)
	}
	sample.Ports = nil
	if got := networkOptics(sample, "s", cables, nil); got.State != "BLOCKED" {
		t.Fatalf("missing connected port: %+v", got)
	}
	sample.Ports = []fabricmon.Port{{Name: "swp1"}}
	if got := networkOptics(sample, "s", cables, nil); got.State != "BLOCKED" {
		t.Fatalf("unknown applicability/DDM: %+v", got)
	}
}
