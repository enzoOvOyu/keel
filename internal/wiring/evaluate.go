package wiring

import (
	"slices"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

// Every expected port is evaluated. A missing observation stays BLOCKED and
// never disappears from the denominator. This evaluates wire identity only;
// a frozen health policy additionally requires retained T3/T4/T5 diagnostics.
func Evaluate(expected Expectation, host *Capture, switches map[string]SwitchEvidence, now time.Time) Result {
	out := Result{Version: Version, RuleVersion: RuleVersion, Step2: "PASS", Step3: "PASS", Findings: []Finding{}}
	add := func(step int, p ExpectedPort, verdict, category, code string, want, got any) {
		out.Findings = append(out.Findings, Finding{step, p.PhysicalPortID, p.CableID, verdict, category, code, want, got})
		status := &out.Step2
		if step == 3 {
			status = &out.Step3
		}
		if verdict == "FAIL" || *status == "PASS" {
			*status = verdict
		}
	}
	finish := func() Result {
		if out.Step2 != "PASS" {
			out.Step3 = "BLOCKED"
		}
		return out
	}
	if expected.Validate() != nil {
		add(2, ExpectedPort{}, "BLOCKED", "", "expected_scope_invalid", nil, nil)
		return finish()
	}
	if host == nil || host.Version != Version || host.ProjectID != expected.ProjectID || host.MachineID != expected.MachineID || host.ScopeHash != expected.ScopeHash || host.Serial != expected.Serial || len(host.Ports) > MaxPorts {
		add(2, ExpectedPort{}, "BLOCKED", "", "host_capture_scope_required", nil, nil)
		return finish()
	}
	if host.StartedAt.IsZero() || host.AdvertisingAt.Before(host.StartedAt) || host.CollectedAt.Before(host.AdvertisingAt) || host.AdvertisingStoppedAt.Before(host.CollectedAt) || host.AdvertisingStoppedAt.After(now.Add(5*time.Second)) || host.CollectedAt.After(now.Add(5*time.Second)) || !host.CollectedAt.After(now.Add(-5*time.Minute)) || host.CollectedAt.Sub(host.StartedAt) > 2*time.Minute {
		add(2, ExpectedPort{}, "BLOCKED", "", "host_capture_window_invalid", nil, nil)
		return finish()
	}
	if !host.Stopped || !host.AdminRestored || !host.ConfigurationPreserved {
		add(2, ExpectedPort{}, "BLOCKED", "", "host_lldp_cleanup_not_proven", nil, nil)
	}
	if host.Origin != "real" {
		add(2, ExpectedPort{}, "BLOCKED", "", "real_host_evidence_required", "real", host.Origin)
	}
	if len(host.Missing) > 0 {
		add(2, ExpectedPort{}, "BLOCKED", "", "host_capture_incomplete", nil, host.Missing)
	}
	ports := map[string]HostPort{}
	interfaces := map[string]bool{}
	for _, p := range host.Ports {
		if _, duplicate := ports[p.PhysicalPortID]; duplicate || interfaces[p.Interface] {
			add(2, ExpectedPort{}, "BLOCKED", "", "host_capture_ambiguous", nil, nil)
			return finish()
		}
		ports[p.PhysicalPortID] = p
		interfaces[p.Interface] = true
	}
	expectedIDs := map[string]bool{}
	for _, p := range expected.Ports {
		expectedIDs[p.PhysicalPortID] = true
	}
	for _, p := range host.Ports {
		if !expectedIDs[p.PhysicalPortID] {
			add(2, ExpectedPort{}, "BLOCKED", "", "host_capture_outside_frozen_ports", nil, p.PhysicalPortID)
			return finish()
		}
	}
	switchPorts := map[string]map[string]fabricmon.Port{}
	validSwitch := map[string]bool{}
	for _, p := range expected.Ports {
		if _, seen := validSwitch[p.SwitchSerial]; seen {
			continue
		}
		proof, ok := switches[p.SwitchSerial]
		validSwitch[p.SwitchSerial] = ok && proof.Origin == "device" && proof.Snapshot != nil && proof.Snapshot.Validate(p.SwitchSerial, now) == nil && proof.Snapshot.Capabilities["lldp"] && proof.Snapshot.Capabilities["interfaces"] && !proof.Snapshot.CollectedAt.Before(host.AdvertisingAt) && !proof.Snapshot.CollectedAt.After(host.AdvertisingStoppedAt)
		if validSwitch[p.SwitchSerial] {
			index := map[string]fabricmon.Port{}
			for _, port := range proof.Snapshot.Ports {
				index[port.Name] = port
			}
			switchPorts[p.SwitchSerial] = index
		}
	}
	for _, p := range expected.Ports {
		actual, ok := ports[p.PhysicalPortID]
		if !ok {
			add(2, p, "BLOCKED", "", "host_port_observation_missing", p, nil)
			continue
		}
		if actual.Interface != p.Interface || actual.BDF != p.BDF || !sameMAC(actual.PermanentMAC, p.PermanentMAC) {
			add(2, p, "BLOCKED", "", "host_permanent_identity_changed", p, actual)
			continue
		}
		out.HealthChecks = append(out.HealthChecks, evaluateHealth(p, actual.Health, expected.HealthPolicy, add)...)
		if canonicalMAC(actual.CurrentMAC) == "" {
			add(2, p, "BLOCKED", "", "host_current_mac_missing", p.PermanentMAC, actual.CurrentMAC)
		} else if !sameMAC(actual.CurrentMAC, p.PermanentMAC) {
			add(2, p, "FAIL", "T6", "host_current_mac_mismatch", p.PermanentMAC, actual.CurrentMAC)
		}
		if actual.LinkUp == nil {
			add(2, p, "BLOCKED", "", "host_link_unknown", true, nil)
		} else if !*actual.LinkUp {
			add(2, p, "FAIL", "T1", "host_link_down", true, false)
		}
		if !actual.LLDPReadable || !actual.WindowComplete {
			add(2, p, "BLOCKED", "", "host_lldp_read_or_window_incomplete", nil, nil)
		} else if len(actual.Neighbors) == 0 {
			add(2, p, "FAIL", "T1", "host_lldp_absent", p.SwitchSerial, nil)
		} else if len(actual.Neighbors) > 1 {
			add(2, p, "BLOCKED", "", "host_lldp_multiple_neighbors", 1, len(actual.Neighbors))
		}
		want := Identity{p.PermanentMAC, p.PhysicalPortID, expected.Hostname, Description(expected.Serial, p.Rail, p.PortIndex)}
		if actual.Advertised == nil {
			add(2, p, "BLOCKED", "", "host_advertised_identity_missing", want, nil)
		} else if !sameMAC(actual.Advertised.ChassisID, want.ChassisID) || actual.Advertised.PortID != want.PortID || actual.Advertised.Sysname != want.Sysname || actual.Advertised.Sysdesc != want.Sysdesc {
			add(2, p, "FAIL", "T6", "host_advertised_identity_mismatch", want, actual.Advertised)
		}
		if !validSwitch[p.SwitchSerial] {
			add(2, p, "BLOCKED", "", "current_device_switch_readback_required", p.SwitchSerial, nil)
			continue
		}
		peer, ok := switchPorts[p.SwitchSerial][p.SwitchPort]
		if !ok || peer.LinkUp == nil || !peer.LLDPReadable {
			add(2, p, "BLOCKED", "", "switch_port_readback_missing", p.SwitchPort, nil)
			continue
		}
		if !*peer.LinkUp {
			add(2, p, "FAIL", "T1", "switch_link_down", true, false)
			continue
		}
		if peer.Neighbor == nil {
			add(2, p, "FAIL", "T1", "switch_lldp_absent", want, nil)
			continue
		}
	}
	if out.Step2 != "PASS" {
		return finish()
	}
	// Step 3 is unreachable until every selected Step 2 port has complete,
	// readable dual-sided evidence and its own advertised identity is correct.
	for _, p := range expected.Ports {
		actual := ports[p.PhysicalPortID]
		peer := switchPorts[p.SwitchSerial][p.SwitchPort]
		want := Identity{p.PermanentMAC, p.PhysicalPortID, expected.Hostname, Description(expected.Serial, p.Rail, p.PortIndex)}

		// A host-reported sysname or serial alone cannot authenticate the switch.
		// Chassis MAC aliases come only from approved BOM switch bindings.
		if len(actual.Neighbors) == 1 {
			neighbor := actual.Neighbors[0]
			if neighbor.ChassisID == "" {
				add(3, p, "BLOCKED", "", "host_neighbor_chassis_missing", p.SwitchChassisIDs, nil)
			} else if len(p.SwitchChassisIDs) == 0 {
				add(3, p, "BLOCKED", "", "approved_switch_chassis_binding_required", nil, nil)
			} else if !slices.ContainsFunc(p.SwitchChassisIDs, func(mac string) bool { return sameMAC(mac, neighbor.ChassisID) }) {
				add(3, p, "FAIL", "T2", "host_neighbor_switch_mismatch", p.SwitchChassisIDs, neighbor)
			}
			if neighbor.PortID == "" {
				add(3, p, "BLOCKED", "", "host_neighbor_port_missing", p.SwitchPort, nil)
			} else if neighbor.PortID != p.SwitchPort {
				add(3, p, "FAIL", "T2", "host_neighbor_port_mismatch", p.SwitchPort, neighbor.PortID)
			}
			if neighbor.Serial != "" && neighbor.Serial != p.SwitchSerial {
				add(3, p, "FAIL", "T2", "host_neighbor_serial_mismatch", p.SwitchSerial, neighbor.Serial)
			}
		}
		observed := peer.Neighbor
		if observed.ChassisID == "" || observed.PortID == "" || observed.Sysname == "" || observed.Sysdesc == "" {
			add(3, p, "BLOCKED", "", "switch_neighbor_tlvs_incomplete", want, observed)
			continue
		}
		if !sameMAC(observed.ChassisID, p.PermanentMAC) || observed.PortID != p.PhysicalPortID || observed.Sysname != expected.Hostname || observed.Sysdesc != want.Sysdesc {
			add(3, p, "FAIL", "T2", "switch_neighbor_host_identity_mismatch", want, observed)
		}
		if strings.TrimSpace(observed.Serial) != "" && observed.Serial != expected.Serial {
			add(3, p, "FAIL", "T2", "switch_neighbor_host_serial_mismatch", expected.Serial, observed.Serial)
		}
	}
	return finish()
}
