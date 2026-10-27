package wiring

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

const HealthVersion = "keel.wiring-health/v1"

// HealthPolicy is frozen by the server's approved parameter catalog together
// with Step 1. Neither a device observation nor a repair request sets limits.
type HealthPolicy struct {
	Version  string                     `json:"version"`
	Counters fabricmon.CounterPolicy    `json:"counters"`
	Optics   map[string]fabricmon.Range `json:"optics"`
}

// HealthObservation is an adapter-owned, retained raw probe. Counter epochs and
// both observations are required for growth; a lifetime counter is not growth.
// PCIe/firmware states are explicit driver observations, never inferred from a
// down link or an empty LLDP table. Nil means unavailable.
type HealthObservation struct {
	Version           string            `json:"version"`
	Interface         string            `json:"interface"`
	BDF               string            `json:"bdf"`
	PermanentMAC      string            `json:"permanent_mac"`
	BeforeAt          time.Time         `json:"before_at"`
	ObservedAt        time.Time         `json:"observed_at"`
	BeforeEpoch       string            `json:"before_epoch"`
	Epoch             string            `json:"epoch"`
	CRCBefore         *uint64           `json:"crc_before"`
	CRC               *uint64           `json:"crc"`
	FlapsBefore       *uint64           `json:"flaps_before"`
	Flaps             *uint64           `json:"flaps"`
	ModulePresent     *bool             `json:"module_present"`
	Optics            *fabricmon.Optics `json:"optics"`
	PCITrainingFailed *bool             `json:"pci_training_failed"`
	FirmwareCrashed   *bool             `json:"firmware_crashed"`
}

func decodeHealthProbe(raw []byte, expected ExpectedPort, started, stopped time.Time) (*HealthObservation, error) {
	if len(raw) == 0 || len(raw) > 32768 {
		return nil, ErrCaptureRejected
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSONValue(tokens, 0) != nil {
		return nil, ErrCaptureRejected
	}
	if _, e := tokens.Token(); e != io.EOF {
		return nil, ErrCaptureRejected
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var out HealthObservation
	if d.Decode(&out) != nil || out.Version != HealthVersion || out.Interface != expected.Interface || out.BDF != expected.BDF || !sameMAC(out.PermanentMAC, expected.PermanentMAC) || out.ObservedAt.Before(started) || out.ObservedAt.After(stopped) || out.BeforeAt.Before(started) || out.BeforeAt.After(out.ObservedAt) || len(out.Epoch) > 128 || len(out.BeforeEpoch) > 128 {
		return nil, ErrCaptureRejected
	}
	if out.Optics != nil {
		o := out.Optics
		if len(o.Vendor) > 256 || len(o.Model) > 256 || len(o.Serial) > 256 || len(o.MediaType) > 128 || strings.ContainsAny(o.Vendor+o.Model+o.Serial+o.MediaType, "\r\n\x00") {
			return nil, ErrCaptureRejected
		}
		for _, v := range []*float64{o.RxDBM, o.TxDBM, o.TemperatureC, o.VoltageV, o.BiasMA} {
			if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0)) {
				return nil, ErrCaptureRejected
			}
		}
	}
	return &out, nil
}

// Health checks retain their own unknown state. G2 still assesses its declared
// wire identity scope; only concrete physical failures add blocking findings.
// A T3/T4/T5 repair additionally requires PASS for its own health category.
type HealthCheck struct {
	PhysicalPortID string `json:"physical_port_id"`
	Category       string `json:"category"`
	Code           string `json:"code"`
	Verdict        string `json:"verdict"`
}

func evaluateHealth(p ExpectedPort, h *HealthObservation, policy *HealthPolicy, add func(int, ExpectedPort, string, string, string, any, any)) []HealthCheck {
	if policy == nil {
		return nil
	}
	out := []HealthCheck{}
	check := func(category, code, verdict string, want, got any) {
		out = append(out, HealthCheck{PhysicalPortID: p.PhysicalPortID, Category: category, Code: code, Verdict: verdict})
		if verdict == "FAIL" {
			add(2, p, verdict, category, code, want, got)
		}
	}
	if h == nil {
		for _, category := range []string{"T3", "T4", "T5"} {
			check(category, "host_health_observation_missing", "BLOCKED", nil, nil)
		}
		return out
	}
	if h.ModulePresent == nil || !*h.ModulePresent {
		if h.ModulePresent != nil {
			add(2, p, "FAIL", "T1", "host_module_absent", true, false)
		}
		check("T3", "host_module_presence_unknown", "BLOCKED", true, h.ModulePresent)
	} else {
		state, reasons := fabricmon.JudgeOptics(h.Optics, policy.Optics)
		switch state {
		case "critical", "warning":
			check("T3", "host_optics_alarm", "FAIL", "within approved limits", reasons)
		case "unknown":
			check("T3", "host_optics_health_unknown", "BLOCKED", nil, reasons)
		default:
			if h.Optics == nil || h.Optics.Supported == nil {
				check("T3", "host_module_support_unknown", "BLOCKED", nil, nil)
			} else {
				check("T3", "host_optics_normal", "PASS", nil, nil)
			}
		}
	}
	for _, v := range []struct {
		name                     string
		before, current, maximum *uint64
	}{{"crc", h.CRCBefore, h.CRC, policy.Counters.CRCMax}, {"flap", h.FlapsBefore, h.Flaps, policy.Counters.FlapMax}} {
		reason := ""
		elapsed := h.ObservedAt.Sub(h.BeforeAt)
		failed := false
		switch {
		case policy.Counters.WindowSeconds == nil || v.maximum == nil:
			reason = "policy_missing"
		case v.before == nil || v.current == nil:
			reason = "sample_missing"
		case h.BeforeEpoch == "" || h.Epoch == "":
			reason = "epoch_unknown"
		case h.BeforeEpoch != h.Epoch || *v.current < *v.before:
			reason = "counter_reset"
		default:
			window := time.Duration(*policy.Counters.WindowSeconds) * time.Second
			if elapsed < window || elapsed > window+window/2 || elapsed <= 0 {
				reason = "window_incomplete"
			} else if *v.current-*v.before > *v.maximum {
				check("T4", "host_"+v.name+"_growth", "FAIL", *v.maximum, *v.current-*v.before)
				failed = true
			}
		}
		if reason != "" {
			check("T4", "host_"+v.name+"_"+reason, "BLOCKED", nil, nil)
		} else if !failed {
			check("T4", "host_"+v.name+"_normal", "PASS", nil, nil)
		}
	}
	for _, v := range []struct {
		name   string
		failed *bool
	}{{"pci_training", h.PCITrainingFailed}, {"firmware", h.FirmwareCrashed}} {
		if v.failed == nil {
			check("T5", "host_"+v.name+"_state_unknown", "BLOCKED", false, nil)
		} else if *v.failed {
			check("T5", "host_"+v.name+"_failure", "FAIL", false, true)
		} else {
			check("T5", "host_"+v.name+"_normal", "PASS", nil, nil)
		}
	}
	return out
}

func HealthVerdict(checks []HealthCheck, category string) string {
	result := "PASS"
	count := 0
	for _, check := range checks {
		if check.Category != category {
			continue
		}
		count++
		if check.Verdict == "FAIL" {
			result = "FAIL"
		} else if check.Verdict != "PASS" && result != "FAIL" {
			result = "BLOCKED"
		}
	}
	if count == 0 {
		return "BLOCKED"
	}
	return result
}

func (p *HealthPolicy) Validate() error {
	if p == nil {
		return nil
	}
	if p.Version != HealthVersion || p.Counters.WindowSeconds != nil && (*p.Counters.WindowSeconds < 1 || *p.Counters.WindowSeconds > 3600) {
		return errors.New("bounded approved health counter policy required")
	}
	for _, name := range []string{"rx_dbm", "tx_dbm", "temperature_c", "voltage_v", "bias_ma"} {
		r := p.Optics[name]
		for _, v := range []*float64{r.WarnMin, r.WarnMax, r.CriticalMin, r.CriticalMax} {
			if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0)) {
				return errors.New("non-finite optical limit")
			}
		}
		if r.WarnMin != nil && r.WarnMax != nil && *r.WarnMin > *r.WarnMax || r.CriticalMin != nil && r.CriticalMax != nil && *r.CriticalMin > *r.CriticalMax {
			return errors.New("inverted optical limits")
		}
	}
	return nil
}
