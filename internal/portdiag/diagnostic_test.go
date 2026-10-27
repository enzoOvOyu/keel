package portdiag

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
)

func fixture(category, rule string) (Scope, Binding, Capture, time.Time) {
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	low, high := -15.0, 0.0
	scope := Scope{ProjectID: "project", IssueID: "issue", MachineID: "machine", Serial: "SERVER-A", PhysicalPortID: "physical", Interface: "eth0", PermanentMAC: "02:00:00:00:00:01", BDF: "0000:01:00.0", SwitchID: "switch", SwitchSerial: "SW-A", SwitchPort: "swp1", Category: category, RuleID: rule, Source: "host", Method: "agent", Origin: "sim", RepairedAt: now.Add(-12 * time.Minute), Limits: map[string]monitor.Limit{"rx_dbm": {CriticalMin: &low, CriticalMax: &high}, "pfc_pause_delta": {CriticalMax: &high}}}
	binding := Binding{ExecutionID: "execution", AttemptID: "attempt", CommandID: "command", FencingToken: 1, ScopeHash: "scope"}
	capture := Capture{Version: Version, Binding: binding, Scope: scope, StartedAt: now.Add(-10 * time.Minute), FinishedAt: now, Stopped: true, LLDPReadable: true, HostPeerSerial: scope.SwitchSerial, HostPeerPort: scope.SwitchPort, SwitchPeerSerial: scope.Serial, SwitchPeerMAC: scope.PermanentMAC, SwitchPeerInterface: scope.Interface, RawRefs: []string{"raw-lldp", "raw-metrics", "raw-path"}}
	capture.Observations = []monitor.Sample{{Key: "link.state", Entity: "eth0", State: "up", Quality: "good", ObservedAt: now, Interface: scope.Interface, PermanentMAC: scope.PermanentMAC, BDF: scope.BDF}}
	capture.Path = &PathProbe{PhysicalPortID: scope.PhysicalPortID, Interface: scope.Interface, PeerSerial: scope.SwitchSerial, PeerPort: scope.SwitchPort, Sent: 3, Received: 3, StartedAt: now.Add(-time.Second), FinishedAt: now, RawRef: "raw-path"}
	return scope, binding, capture, now
}
func TestDiagnosticRequiresCurrentPhysicalCaptureAndExplicitKnownReadings(t *testing.T) {
	scope, binding, good, now := fixture("T1", "link-down")
	for _, tc := range []struct {
		name, want string
		change     func(*Capture)
	}{
		{"healthy", "PASS", func(*Capture) {}},
		{"wrong peer", "FAIL", func(c *Capture) { c.HostPeerPort = "swp2" }},
		{"link still down", "FAIL", func(c *Capture) { c.Observations[0].State = "down" }},
		{"unknown", "BLOCKED", func(c *Capture) { c.Observations[0].State = ""; c.Observations[0].Quality = "unknown" }},
		{"degraded", "BLOCKED", func(c *Capture) { c.Observations[0].State = "degraded" }},
		{"LLDP unreadable", "BLOCKED", func(c *Capture) { c.LLDPReadable = false }},
		{"foreign original command", "ERROR", func(c *Capture) { c.Binding.CommandID = "foreign" }},
		{"wrong source", "ERROR", func(c *Capture) { c.Scope.Source = "switch" }},
		{"other physical identity", "ERROR", func(c *Capture) { c.Observations[0].BDF = "0000:02:00.0" }},
		{"unstopped", "ERROR", func(c *Capture) { c.Stopped = false }},
		{"duplicate observations", "ERROR", func(c *Capture) { c.Observations = append(c.Observations, c.Observations[0]) }},
		{"pre-repair capture", "ERROR", func(c *Capture) { c.StartedAt = scope.RepairedAt }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(good)
			var capture Capture
			_ = json.Unmarshal(raw, &capture)
			tc.change(&capture)
			out, e := Evaluate(scope, binding, capture, now)
			if tc.want == "ERROR" {
				if e == nil {
					t.Fatal("invalid original proof accepted")
				}
				return
			}
			if e != nil || out.Verdict != tc.want {
				t.Fatalf("want %s: %+v %v", tc.want, out, e)
			}
		})
	}
}
func TestDiagnosticOpticsRequiresBoundedPathAndApprovedLimits(t *testing.T) {
	scope, binding, capture, now := fixture("T3", "optics-rx_dbm")
	value := -5.0
	sample := capture.Observations[0]
	sample.Key = "dom.rx_power"
	sample.State = ""
	sample.Value = &value
	capture.Observations = []monitor.Sample{sample}
	for _, tc := range []struct {
		name, want string
		change     func(*Capture)
	}{
		{"healthy", "PASS", func(*Capture) {}},
		{"path missing", "BLOCKED", func(c *Capture) { c.Path = nil }},
		{"path packet loss", "FAIL", func(c *Capture) { c.Path.Received = 2 }},
		{"foreign path", "ERROR", func(c *Capture) { c.Path.PeerSerial = "OTHER" }},
		{"unknown raw path", "ERROR", func(c *Capture) { c.Path.RawRef = "not-retained" }},
		{"DOM unsupported", "BLOCKED", func(c *Capture) { c.Observations[0].Quality = "unsupported"; c.Observations[0].Value = nil }},
		{"DOM still failed", "FAIL", func(c *Capture) { bad := -20.0; c.Observations[0].Value = &bad }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(capture)
			var v Capture
			_ = json.Unmarshal(raw, &v)
			tc.change(&v)
			out, e := Evaluate(scope, binding, v, now)
			if tc.want == "ERROR" {
				if e == nil {
					t.Fatal("foreign path accepted")
				}
				return
			}
			if e != nil || out.Verdict != tc.want {
				t.Fatalf("want %s: %+v %v", tc.want, out, e)
			}
		})
	}
}
func TestDiagnosticFlappingRequiresCompletePostRepairWindow(t *testing.T) {
	scope, binding, capture, now := fixture("T4", "link-flapping")
	base := capture.Observations[0]
	capture.Observations = nil
	for i := 0; i <= 60; i++ {
		sample := base
		sample.ObservedAt = capture.StartedAt.Add(time.Duration(i) * 10 * time.Second)
		capture.Observations = append(capture.Observations, sample)
	}
	for _, tc := range []struct {
		name, want string
		change     func(*Capture)
	}{
		{"complete", "PASS", func(*Capture) {}},
		{"one sample", "BLOCKED", func(c *Capture) { c.Observations = c.Observations[len(c.Observations)-1:] }},
		{"hole", "BLOCKED", func(c *Capture) { c.Observations = append(c.Observations[:20], c.Observations[25:]...) }},
		{"unknown in window", "BLOCKED", func(c *Capture) { c.Observations[20].Quality = "unknown"; c.Observations[20].State = "" }},
		{"three transitions", "FAIL", func(c *Capture) {
			for _, i := range []int{10, 20, 30} {
				c.Observations[i].State = "down"
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(capture)
			var v Capture
			_ = json.Unmarshal(raw, &v)
			tc.change(&v)
			out, e := Evaluate(scope, binding, v, now)
			if e != nil || out.Verdict != tc.want {
				t.Fatalf("want %s: %+v %v", tc.want, out, e)
			}
		})
	}
}

func TestDiagnosticPFCRequiresOriginalCounterEpochAndIndependentConfiguration(t *testing.T) {
	scope, binding, capture, now := fixture("T6", "pfc-pause-growth")
	scope.ExpectedConfigurationHash = "approved-switch-configuration"
	capture.Scope = scope
	capture.ConfigurationHash = scope.ExpectedConfigurationHash
	capture.CounterEpoch = "boot-one"
	capture.CounterEpochBefore = "boot-one"
	sample := capture.Observations[0]
	sample.Key = "rdma.pfc_pause"
	sample.State = ""
	value := 10.0
	sample.Value = &value
	prior := sample
	prior.ObservedAt = now.Add(-time.Second)
	capture.Observations = []monitor.Sample{prior, sample}
	for _, tc := range []struct {
		name, want string
		change     func(*Capture)
	}{
		{"stable approved path", "PASS", func(*Capture) {}},
		{"unknown epoch", "BLOCKED", func(c *Capture) { c.CounterEpoch = "" }},
		{"new boot", "BLOCKED", func(c *Capture) { c.CounterEpoch = "boot-two" }},
		{"counter reset", "BLOCKED", func(c *Capture) { v := 9.0; c.Observations[1].Value = &v }},
		{"growth beyond approved limit", "FAIL", func(c *Capture) { v := 11.0; c.Observations[1].Value = &v }},
		{"configuration mismatch", "FAIL", func(c *Capture) { c.ConfigurationHash = "unexpected" }},
		{"missing configuration", "BLOCKED", func(c *Capture) { c.ConfigurationHash = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(capture)
			var v Capture
			_ = json.Unmarshal(raw, &v)
			tc.change(&v)
			result, e := Evaluate(scope, binding, v, now)
			if e != nil || result.Verdict != tc.want {
				t.Fatalf("want %s: %+v %v", tc.want, result, e)
			}
		})
	}
}
