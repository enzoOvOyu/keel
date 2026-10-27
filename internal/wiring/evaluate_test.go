package wiring

import (
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

func wireFixture() (Expectation, *Capture, map[string]SwitchEvidence, time.Time) {
	now := time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)
	up := true
	id := func(s string) string { return strings.Repeat(s, 32) }
	p := ExpectedPort{PhysicalPortID: id("4"), ComponentID: id("5"), CableID: id("6"), SwitchID: id("7"), PortIndex: "1", Interface: "eth1", PermanentMAC: "02:00:00:00:01:01", BDF: "0000:41:00.0", Rail: "rail-1", SwitchSerial: "leaf-1-sn", SwitchPort: "swp1", SwitchChassisIDs: []string{"02:00:00:00:02:01"}}
	expected := Expectation{Version: Version, ProjectID: id("1"), AssetID: id("2"), MachineID: id("3"), Serial: "host-sn", Hostname: "host-1.example.test", ScopeHash: strings.Repeat("a", 64), Ports: []ExpectedPort{p}}
	local := &Identity{ChassisID: p.PermanentMAC, PortID: p.PhysicalPortID, Sysname: expected.Hostname, Sysdesc: Description(expected.Serial, p.Rail, p.PortIndex)}
	host := &Capture{Stopped: true, AdminRestored: true, ConfigurationPreserved: true, Version: Version, Origin: "real", ProjectID: expected.ProjectID, MachineID: expected.MachineID, ScopeHash: expected.ScopeHash, Serial: expected.Serial, StartedAt: now.Add(-20 * time.Second), AdvertisingAt: now.Add(-15 * time.Second), CollectedAt: now.Add(-time.Second), Missing: []string{}, Ports: []HostPort{{PhysicalPortID: p.PhysicalPortID, Interface: p.Interface, BDF: p.BDF, PermanentMAC: p.PermanentMAC, CurrentMAC: p.PermanentMAC, LinkUp: &up, LLDPReadable: true, WindowComplete: true, Advertised: local, Neighbors: []fabricmon.Neighbor{{ChassisID: p.SwitchChassisIDs[0], PortID: p.SwitchPort, Sysname: "leaf-1"}}}}}
	switches := map[string]SwitchEvidence{p.SwitchSerial: {Origin: "device", Snapshot: &fabricmon.Snapshot{Version: "fabric-snapshot/v1", Source: "switch", Provider: "synthetic-software-fixture", Serial: p.SwitchSerial, CollectedAt: now, Capabilities: map[string]bool{"lldp": true, "interfaces": true}, Ports: []fabricmon.Port{{Name: p.SwitchPort, LinkUp: &up, LLDPReadable: true, Neighbor: &fabricmon.Neighbor{ChassisID: p.PermanentMAC, PortID: p.PhysicalPortID, Sysname: expected.Hostname, Sysdesc: local.Sysdesc}}}}}}
	return expected, host, switches, now
}
func TestThreeSourceWireIdentityAndStepOrdering(t *testing.T) {
	tests := []struct {
		name               string
		mutate             func(*Expectation, *Capture, map[string]SwitchEvidence)
		step2, step3, code string
	}{
		{"reciprocal independent identities", func(*Expectation, *Capture, map[string]SwitchEvidence) {}, "PASS", "PASS", ""},
		{"missing host port", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) { h.Ports = nil }, "BLOCKED", "BLOCKED", "host_port_observation_missing"},
		{"unreadable is not absent", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			h.Ports[0].Neighbors = nil
			h.Ports[0].LLDPReadable = false
		}, "BLOCKED", "BLOCKED", "host_lldp_read_or_window_incomplete"},
		{"bounded empty readable table", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) { h.Ports[0].Neighbors = nil }, "FAIL", "BLOCKED", "host_lldp_absent"},
		{"multiple neighbors", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			h.Ports[0].Neighbors = append(h.Ports[0].Neighbors, h.Ports[0].Neighbors[0])
		}, "BLOCKED", "BLOCKED", "host_lldp_multiple_neighbors"},
		{"spoofed local mac", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			h.Ports[0].CurrentMAC = "02:00:00:00:ff:01"
		}, "FAIL", "BLOCKED", "host_current_mac_mismatch"},
		{"no switch source", func(e *Expectation, _ *Capture, s map[string]SwitchEvidence) { delete(s, e.Ports[0].SwitchSerial) }, "BLOCKED", "BLOCKED", "current_device_switch_readback_required"},
		{"fixture switch not real", func(e *Expectation, _ *Capture, s map[string]SwitchEvidence) {
			p := s[e.Ports[0].SwitchSerial]
			p.Origin = "protocol_fixture"
			s[e.Ports[0].SwitchSerial] = p
		}, "BLOCKED", "BLOCKED", "current_device_switch_readback_required"},
		{"old switch sample", func(e *Expectation, h *Capture, s map[string]SwitchEvidence) {
			s[e.Ports[0].SwitchSerial].Snapshot.CollectedAt = h.AdvertisingAt.Add(-time.Second)
		}, "BLOCKED", "BLOCKED", "current_device_switch_readback_required"},
		{"same name wrong chassis", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			h.Ports[0].Neighbors[0].ChassisID = "02:00:00:00:ff:02"
		}, "PASS", "FAIL", "host_neighbor_switch_mismatch"},
		{"wrong peer port", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) { h.Ports[0].Neighbors[0].PortID = "swp2" }, "PASS", "FAIL", "host_neighbor_port_mismatch"},
		{"missing independent switch binding", func(e *Expectation, _ *Capture, _ map[string]SwitchEvidence) { e.Ports[0].SwitchChassisIDs = nil }, "PASS", "BLOCKED", "approved_switch_chassis_binding_required"},
		{"reciprocal port mismatch", func(e *Expectation, _ *Capture, s map[string]SwitchEvidence) {
			s[e.Ports[0].SwitchSerial].Snapshot.Ports[0].Neighbor.PortID = strings.Repeat("9", 32)
		}, "PASS", "FAIL", "switch_neighbor_host_identity_mismatch"},
		{"serial prefix is not host identity", func(e *Expectation, _ *Capture, s map[string]SwitchEvidence) {
			s[e.Ports[0].SwitchSerial].Snapshot.Ports[0].Neighbor.Sysdesc = Description(e.Serial+"-other", e.Ports[0].Rail, e.Ports[0].PortIndex)
		}, "PASS", "FAIL", "switch_neighbor_host_identity_mismatch"},
		{"sim host cannot publish real pass", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) { h.Origin = "sim" }, "BLOCKED", "BLOCKED", "real_host_evidence_required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			e, h, s, now := wireFixture()
			test.mutate(&e, h, s)
			result := Evaluate(e, h, s, now)
			if result.Step2 != test.step2 || result.Step3 != test.step3 {
				t.Fatalf("%+v", result)
			}
			found := test.code == ""
			for _, finding := range result.Findings {
				if finding.Code == test.code {
					found = true
				}
				if result.Step2 != "PASS" && finding.Step == 3 {
					t.Fatal("step 3 ran before step 2 passed")
				}
			}
			if !found {
				t.Fatalf("missing reason %s: %+v", test.code, result)
			}
		})
	}
}
