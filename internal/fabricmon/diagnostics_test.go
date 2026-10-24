package fabricmon

import (
	"testing"
	"time"
)

func TestPhysicalCountersRequireContinuityAndCoveredWindow(t *testing.T) {
	now := time.Now().UTC()
	window := 60
	maximum := uint64(0)
	oldCount, count := uint64(9007199254740992), uint64(9007199254740993)
	old := Snapshot{Capabilities: map[string]bool{"interface_counters": true}, Source: "switch", Provider: "a", Serial: "s", CollectedAt: now.Add(-time.Minute), Ports: []Port{{Name: "p", CRC: &oldCount, Flaps: &oldCount, CounterEpoch: "boot-reset-1"}}}
	current := old
	current.CollectedAt = now
	current.Ports = []Port{{Name: "p", CRC: &count, Flaps: &count, CounterEpoch: "boot-reset-1"}}
	run := func() DiagnosticCheck {
		return DiagnosticPorts(current, &old, "p", true, nil, nil, CounterPolicy{&window, &maximum, &maximum}).Checks[0]
	}
	if c := run(); c.State != "anomaly" || c.Observed["growth"] != "1" || c.IssueType != "T4" {
		t.Fatalf("precise growth: %+v", c)
	}
	old.Provider = "other"
	if c := run(); c.State != "unknown" || c.Reason != "counter_source_changed" {
		t.Fatalf("source change: %+v", c)
	}
	old.Provider = "a"
	current.Ports[0].CounterEpoch = "reset-2"
	if c := run(); c.State != "unknown" || c.Reason != "counter_reset" {
		t.Fatal(c)
	}
	current.Ports[0].CounterEpoch = "boot-reset-1"
	old.CollectedAt = now.Add(-5 * time.Second)
	if c := run(); c.State != "unknown" || c.Reason != "counter_window_incomplete" {
		t.Fatal(c)
	}
	if c := DiagnosticPorts(current, &old, "p", false, nil, nil, CounterPolicy{}); len(c.Checks) != 1 || c.Checks[0].State != "not_applicable" {
		t.Fatal(c)
	}
}
func TestPhysicalAndConfigurationEvidenceStayDistinct(t *testing.T) {
	speed, peerSpeed, configured := int64(100000), int64(25000), int64(40000)
	mtu := 9216
	vlans := []int{}
	priorities := []int{6}
	auto := true
	current := Snapshot{Capabilities: map[string]bool{"interfaces": true, "physical_link": true, "nvue": true}, Ports: []Port{{Name: "p", SpeedMbps: &speed, Duplex: "full", MediaClass: "optical"}}, AppliedInterfaces: map[string]InterfaceConfiguration{"p": {SpeedMbps: &configured, MTU: &mtu, VLANs: &vlans, PFCPriorities: &priorities, Autoneg: &auto}}}
	peer := &PeerObservation{Snapshot: Snapshot{Capabilities: current.Capabilities}, Port: Port{SpeedMbps: &peerSpeed, Duplex: "full", MediaClass: "copper"}, TrustedSerial: "leaf-2"}
	intent := &PortIntent{Configuration: InterfaceConfiguration{SpeedMbps: &speed, MTU: &mtu, VLANs: &vlans, PFCPriorities: &priorities, Autoneg: &auto}, RailID: "rail-customer-blue", LeafSerial: "leaf-1"}
	out := DiagnosticPorts(current, nil, "p", true, peer, intent, CounterPolicy{})
	seen := map[string]bool{}
	for _, c := range out.Checks {
		if c.State == "anomaly" {
			seen[c.IssueType+":"+c.Subtype] = true
		}
	}
	if !seen["T4:"] || !seen["T6:"] || !seen["T2:rail"] {
		t.Fatalf("distinct classes: %+v", out)
	}
	out = DiagnosticPorts(current, nil, "p", true, peer, nil, CounterPolicy{})
	for _, c := range out.Checks {
		if c.IssueType == "T6" || c.Subtype == "rail" {
			if c.State != "unknown" {
				t.Fatalf("missing intent: %+v", c)
			}
		}
	}
}

func TestDiagnosticUnknownGuardsAndTypedInterfaceInput(t *testing.T) {
	now := time.Now().UTC()
	one := uint64(1)
	two := uint64(2)
	window := 60
	maximum := uint64(0)
	old := Snapshot{Source: "switch", Provider: "test", Serial: "s", CollectedAt: now.Add(-time.Minute), Capabilities: map[string]bool{"interface_counters": true}, Ports: []Port{{Name: "p", CRC: &one, Flaps: &one, CounterEpoch: "boot1-reset1"}}}
	current := old
	current.CollectedAt = now
	current.Ports = []Port{{Name: "p", CRC: &two, Flaps: &two, CounterEpoch: "boot1-reset1"}}
	missing := DiagnosticPorts(current, &old, "p", true, nil, nil, CounterPolicy{}).Checks[0]
	if missing.State != "unknown" || missing.Reason != "counter_threshold_or_window_missing" {
		t.Fatalf("missing catalog must not assume zero: %+v", missing)
	}
	current.Capabilities = map[string]bool{}
	missing = DiagnosticPorts(current, &old, "p", true, nil, nil, CounterPolicy{&window, &maximum, &maximum}).Checks[0]
	if missing.State != "unknown" || missing.Reason != "counter_capability_unavailable" {
		t.Fatalf("missing capability must not imply available counters: %+v", missing)
	}
	invalid := []int{6, 6}
	if (InterfaceConfiguration{PFCPriorities: &invalid}).Validate() == nil {
		t.Fatal("duplicate priority accepted")
	}
	invalid = []int{4095}
	if (InterfaceConfiguration{VLANs: &invalid}).Validate() == nil {
		t.Fatal("reserved VLAN accepted")
	}
	empty := []int{}
	if e := (InterfaceConfiguration{VLANs: &empty, PFCPriorities: &empty}).Validate(); e != nil {
		t.Fatal(e)
	}
}
