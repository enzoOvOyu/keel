package platform

import (
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

func TestNetworkReadinessRequiresReadableModeledPortsWithoutConfirmingWiring(t *testing.T) {
	now := time.Now().UTC()
	down := false
	cables := []fabricmon.Cable{{UID: "cable", A: fabricmon.Endpoint{Switch: true, Serial: "SW", Port: "swp1"}}}
	sample := fabricmon.Snapshot{Version: "fabric-snapshot/v1", Source: "switch", Provider: "fixture", Serial: "SW", CollectedAt: now, Capabilities: map[string]bool{"lldp": true}, Ports: []fabricmon.Port{{Name: "swp1", LinkUp: &down, LLDPReadable: true}}}
	// A readable port with no neighbor is a wiring fault for T1, not an unreadable
	// LLDP service. NP-4 never stands in for the missing NP-3 wiring assertion.
	if got := networkLLDPReadiness("SW", &sample, cables, now); got.State != "PASS" || got.ID != "NP-4" {
		t.Fatalf("readability confused with wiring: %+v", got)
	}
	for _, tc := range []struct {
		name   string
		change func(*fabricmon.Snapshot)
	}{
		{"unreadable", func(s *fabricmon.Snapshot) { s.Ports = []fabricmon.Port{{Name: "swp1", LLDPReadable: false}} }},
		{"wrong port", func(s *fabricmon.Snapshot) { s.Ports = []fabricmon.Port{{Name: "swp2", LLDPReadable: true}} }},
		{"no capability", func(s *fabricmon.Snapshot) { s.Capabilities = map[string]bool{} }},
		{"stale", func(s *fabricmon.Snapshot) { s.CollectedAt = now.Add(-6 * time.Minute) }},
		{"foreign serial", func(s *fabricmon.Snapshot) { s.Serial = "OTHER" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := sample
			tc.change(&candidate)
			if got := networkLLDPReadiness("SW", &candidate, cables, now); got.State != "BLOCKED" {
				t.Fatalf("unreadable or missing scope admitted: %+v", got)
			}
		})
	}
}
