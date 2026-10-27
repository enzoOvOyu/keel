package wiring

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

func healthyWire(t *testing.T) (Expectation, *Capture, map[string]SwitchEvidence, time.Time) {
	t.Helper()
	expected, host, switches, now := wireFixture()
	ptr := func(v float64) *float64 { return &v }
	window := 10
	crc, flaps := uint64(0), uint64(1)
	expected.HealthPolicy = &HealthPolicy{Version: HealthVersion, Counters: fabricmon.CounterPolicy{WindowSeconds: &window, CRCMax: &crc, FlapMax: &flaps}, Optics: map[string]fabricmon.Range{
		"rx_dbm": {WarnMin: ptr(-10), WarnMax: ptr(3)}, "tx_dbm": {WarnMin: ptr(-10), WarnMax: ptr(3)}, "temperature_c": {WarnMin: ptr(0), WarnMax: ptr(70)}, "voltage_v": {WarnMin: ptr(3), WarnMax: ptr(3.6)}, "bias_ma": {WarnMin: ptr(1), WarnMax: ptr(15)},
	}}
	raw, e := os.ReadFile("../../sim/fixtures/lldp/health/healthy.json")
	if e != nil {
		t.Fatal(e)
	}
	h, e := decodeHealthProbe(raw, expected.Ports[0], host.StartedAt, host.AdvertisingStoppedAt)
	if e != nil {
		t.Fatal(e)
	}
	host.Ports[0].Health = h
	return expected, host, switches, now
}

// Synthetic rule inputs only. These are never published as real G2 evidence.
func TestHealthTenPhysicalFaultVariantsKeepCategoryAndPort(t *testing.T) {
	cases := []struct {
		name, category string
		mutate         func(*Expectation, *Capture, map[string]SwitchEvidence)
	}{
		{"host link down", "T1", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) { v := false; h.Ports[0].LinkUp = &v }},
		{"readable empty neighbor", "T1", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) { h.Ports[0].Neighbors = nil }},
		{"wrong peer switch", "T2", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			h.Ports[0].Neighbors[0].ChassisID = "02:00:00:00:ff:01"
		}},
		{"wrong peer port", "T2", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			h.Ports[0].Neighbors[0].PortID = "swp99"
		}},
		{"unsupported module", "T3", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			v := false
			h.Ports[0].Health.Optics.Supported = &v
		}},
		{"optical temperature alarm", "T3", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			v := 95.0
			h.Ports[0].Health.Optics.TemperatureC = &v
		}},
		{"CRC growth", "T4", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			v := uint64(12)
			h.Ports[0].Health.CRC = &v
		}},
		{"link flapping growth", "T4", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			v := uint64(3)
			h.Ports[0].Health.Flaps = &v
		}},
		{"PCIe training failed", "T5", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			v := true
			h.Ports[0].Health.PCITrainingFailed = &v
		}},
		{"firmware crash", "T5", func(_ *Expectation, h *Capture, _ map[string]SwitchEvidence) {
			v := true
			h.Ports[0].Health.FirmwareCrashed = &v
		}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			expected, h, sw, now := healthyWire(t)
			test.mutate(&expected, h, sw)
			result := Evaluate(expected, h, sw, now)
			seen := false
			for _, finding := range result.Findings {
				if finding.Verdict == "FAIL" {
					if finding.Category != test.category || finding.PhysicalPortID != expected.Ports[0].PhysicalPortID || finding.CableID != expected.Ports[0].CableID {
						t.Fatal("misclassified physical failure", finding)
					}
					seen = true
				}
			}
			if !seen || result.Step2 == "PASS" && result.Step3 == "PASS" {
				t.Fatal("failure escaped gate", result)
			}
		})
	}
}
func TestHealthUnknownCountersAndMissingDiagnosticsCannotCloseRepair(t *testing.T) {
	cases := []struct {
		name, category string
		mutate         func(*Expectation, *Capture)
	}{
		{"missing entire probe", "T3", func(_ *Expectation, h *Capture) { h.Ports[0].Health = nil }},
		{"counter epoch reset", "T4", func(_ *Expectation, h *Capture) { h.Ports[0].Health.Epoch = "new-boot" }},
		{"counter decrease", "T4", func(_ *Expectation, h *Capture) { v := uint64(0); h.Ports[0].Health.CRC = &v }},
		{"incomplete counter window", "T4", func(_ *Expectation, h *Capture) { h.Ports[0].Health.BeforeAt = h.Ports[0].Health.ObservedAt }},
		{"missing approved threshold", "T4", func(e *Expectation, _ *Capture) { e.HealthPolicy.Counters.CRCMax = nil }},
		{"missing DOM", "T3", func(_ *Expectation, h *Capture) { h.Ports[0].Health.Optics = nil }},
		{"missing driver state", "T5", func(_ *Expectation, h *Capture) { h.Ports[0].Health.FirmwareCrashed = nil }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			expected, h, sw, now := healthyWire(t)
			test.mutate(&expected, h)
			r := Evaluate(expected, h, sw, now)
			if r.Step2 != "PASS" || r.Step3 != "PASS" {
				t.Fatal("optional diagnostics changed the declared LLDP identity scope", r)
			}
			if HealthVerdict(r.HealthChecks, test.category) != "BLOCKED" {
				t.Fatal("unknown health could close repair", r.HealthChecks)
			}
			for _, f := range r.Findings {
				if f.Category == "T5" {
					t.Fatal("unknown or down link invented an RMA", f)
				}
			}
		})
	}
	expected, h, sw, now := healthyWire(t)
	result := Evaluate(expected, h, sw, now)
	for _, category := range []string{"T3", "T4", "T5"} {
		if HealthVerdict(result.HealthChecks, category) != "PASS" {
			t.Fatal("explicit healthy observations did not pass health recheck", result)
		}
	}
}
func TestHealthRawProbeCannotReplaceFrozenIdentityOrInventProjection(t *testing.T) {
	expected, h, _, _ := healthyWire(t)
	original := h.Ports[0].Health
	for name, mutate := range map[string]func(*HealthObservation){"interface": func(v *HealthObservation) { v.Interface = "eth99" }, "BDF": func(v *HealthObservation) { v.BDF = "0000:ff:00.0" }, "MAC": func(v *HealthObservation) { v.PermanentMAC = "02:00:00:00:ff:99" }, "outside window": func(v *HealthObservation) { v.ObservedAt = h.AdvertisingStoppedAt.Add(time.Second) }} {
		t.Run(name, func(t *testing.T) {
			value := *original
			mutate(&value)
			raw, _ := json.Marshal(value)
			if _, e := decodeHealthProbe(raw, expected.Ports[0], h.StartedAt, h.AdvertisingStoppedAt); e == nil {
				t.Fatal("unscoped health accepted")
			}
		})
	}
}

func TestHealthProjectionCannotChangeWithoutOriginalRawProbe(t *testing.T) {
	driver := newCaptureDriver()
	_, h, _, _ := healthyWire(t)
	policy := &HealthPolicy{Version: HealthVersion}
	driver.expected.HealthPolicy = policy
	archive, e := CaptureWithDriver(context.Background(), driver.expected, strings.Repeat("e", 32), driver)
	if e != nil {
		t.Fatal(e)
	}
	observation := *h.Ports[0].Health
	port := driver.expected.Ports[0]
	observation.Interface, observation.BDF, observation.PermanentMAC = port.Interface, port.BDF, port.PermanentMAC
	observation.ObservedAt = archive.Observation.CollectedAt
	observation.BeforeAt = observation.ObservedAt
	raw, _ := json.Marshal(observation)
	archive.Observation.Probes = append(archive.Observation.Probes, RawProbe{PhysicalPortID: port.PhysicalPortID, Kind: "health", State: "success", Stdout: string(raw)})
	projected, e := projectArchive(archive)
	if e != nil {
		t.Fatal(e)
	}
	stored, _ := json.Marshal(projected)
	if _, e = DecodeArchive(stored); e != nil {
		t.Fatal("original health projection rejected", e)
	}
	if projected.Capture.Ports[0].Health == nil {
		t.Fatal("retained health raw probe was discarded")
	}
	altered := true
	projected.Capture.Ports[0].Health.FirmwareCrashed = &altered
	stored, _ = json.Marshal(projected)
	if _, e = DecodeArchive(stored); e == nil {
		t.Fatal("invented firmware failure overrode original raw probe")
	}
}
