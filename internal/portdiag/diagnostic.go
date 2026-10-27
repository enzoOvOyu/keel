// Package portdiag evaluates bounded, adapter-owned post-repair observations.
// It never consumes caller-supplied PASS flags or advances delivery gates.
package portdiag

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
)

const Version = "keel.port-diagnostic/v1"
const MaxBytes = 256 << 10

type Scope struct {
	ProjectID                 string                   `json:"project_id"`
	IssueID                   string                   `json:"issue_id"`
	MachineID                 string                   `json:"machine_id"`
	Serial                    string                   `json:"serial"`
	PhysicalPortID            string                   `json:"physical_port_id"`
	Interface                 string                   `json:"interface"`
	PermanentMAC              string                   `json:"permanent_mac"`
	BDF                       string                   `json:"bdf"`
	SwitchID                  string                   `json:"switch_id"`
	SwitchSerial              string                   `json:"switch_serial"`
	SwitchPort                string                   `json:"switch_port"`
	Category                  string                   `json:"category"`
	RuleID                    string                   `json:"rule_id"`
	Source                    string                   `json:"source"`
	Method                    string                   `json:"method"`
	Origin                    string                   `json:"origin"`
	ExpectedConfigurationHash string                   `json:"expected_configuration_hash"`
	RepairedAt                time.Time                `json:"repaired_at"`
	Limits                    map[string]monitor.Limit `json:"limits"`
}
type Binding struct {
	ExecutionID  string `json:"execution_id"`
	AttemptID    string `json:"attempt_id"`
	CommandID    string `json:"command_id"`
	FencingToken int64  `json:"fencing_token"`
	ScopeHash    string `json:"scope_hash"`
}

// A path probe is limited to the frozen physical link. Destination identity is
// read back by the adapter rather than accepted as an arbitrary network target.
type PathProbe struct {
	PhysicalPortID string    `json:"physical_port_id"`
	Interface      string    `json:"interface"`
	PeerSerial     string    `json:"peer_serial"`
	PeerPort       string    `json:"peer_port"`
	Sent           int       `json:"sent"`
	Received       int       `json:"received"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	RawRef         string    `json:"raw_ref"`
}
type Capture struct {
	CounterEpoch        string           `json:"counter_epoch"`
	CounterEpochBefore  string           `json:"counter_epoch_before"`
	Version             string           `json:"version"`
	Binding             Binding          `json:"binding"`
	Scope               Scope            `json:"scope"`
	StartedAt           time.Time        `json:"started_at"`
	FinishedAt          time.Time        `json:"finished_at"`
	Stopped             bool             `json:"stopped"`
	LLDPReadable        bool             `json:"lldp_readable"`
	HostPeerSerial      string           `json:"host_peer_serial"`
	HostPeerPort        string           `json:"host_peer_port"`
	SwitchPeerSerial    string           `json:"switch_peer_serial"`
	SwitchPeerMAC       string           `json:"switch_peer_mac"`
	SwitchPeerInterface string           `json:"switch_peer_interface"`
	Observations        []monitor.Sample `json:"observations"`
	Path                *PathProbe       `json:"path"`
	ConfigurationHash   string           `json:"configuration_hash"`
	RawRefs             []string         `json:"raw_refs"`
}
type Check struct {
	ID      string `json:"id"`
	Verdict string `json:"verdict"`
	Code    string `json:"code"`
}
type Result struct {
	Verdict string  `json:"verdict"`
	Checks  []Check `json:"checks"`
}

func Evaluate(scope Scope, binding Binding, capture Capture, now time.Time) (Result, error) {
	out := Result{Verdict: "PASS", Checks: []Check{}}
	a, _ := json.Marshal(scope)
	b, _ := json.Marshal(capture.Scope)
	raw, _ := json.Marshal(capture)
	if len(raw) > MaxBytes || capture.Version != Version || string(a) != string(b) || capture.Binding != binding || binding.FencingToken < 1 || binding.CommandID == "" || !capture.Stopped || !capture.StartedAt.After(scope.RepairedAt) || capture.FinishedAt.Before(capture.StartedAt) || capture.FinishedAt.After(now.Add(time.Second)) || now.Sub(capture.FinishedAt) > 5*time.Minute || capture.FinishedAt.Sub(capture.StartedAt) > 11*time.Minute || len(capture.Observations) > 2048 || len(capture.RawRefs) < 1 || len(capture.RawRefs) > 32 {
		return out, errors.New("bounded original stopped capture for the exact repair scope required")
	}
	for _, ref := range capture.RawRefs {
		if ref == "" || len(ref) > 512 {
			return out, errors.New("bounded original raw references required")
		}
	}
	add := func(id, verdict, code string) {
		out.Checks = append(out.Checks, Check{id, verdict, code})
		if verdict == "FAIL" {
			out.Verdict = "FAIL"
		} else if verdict != "PASS" && out.Verdict != "FAIL" {
			out.Verdict = "BLOCKED"
		}
	}
	if !capture.LLDPReadable {
		add("lldp_single_link", "BLOCKED", "lldp_unreadable")
	} else if capture.HostPeerSerial != scope.SwitchSerial || capture.HostPeerPort != scope.SwitchPort || capture.SwitchPeerSerial != scope.Serial || capture.SwitchPeerMAC != scope.PermanentMAC || capture.SwitchPeerInterface != scope.Interface {
		add("lldp_single_link", "FAIL", "physical_peer_mismatch")
	} else {
		add("lldp_single_link", "PASS", "reciprocal_peer_matches")
	}
	add("switch_readback", out.Checks[0].Verdict, out.Checks[0].Code)
	matching := []monitor.Sample{}
	for _, sample := range capture.Observations {
		if sample.Validate(now) != nil || (monitor.Metrics[sample.Key].DType != "D1" && monitor.Metrics[sample.Key].DType != "D2" && sample.Key != "rdma.pfc_pause") || sample.ObservedAt.Before(capture.StartedAt) || sample.ObservedAt.After(capture.FinishedAt) || sample.Interface != scope.Interface || sample.PermanentMAC != scope.PermanentMAC || sample.BDF != scope.BDF {
			return out, errors.New("observation identity or original capture window mismatch")
		}
		matching = append(matching, sample)
	}
	slices.SortFunc(matching, func(a, b monitor.Sample) int { return a.ObservedAt.Compare(b.ObservedAt) })
	metric := map[string]string{"link-down": "link.state", "link-flapping": "link.state", "optics-rx_dbm": "dom.rx_power", "optics-tx_dbm": "dom.tx_power", "optics-temperature_c": "dom.temperature", "pfc-pause-growth": "rdma.pfc_pause"}[scope.RuleID]
	var latest, prior, lastLink *monitor.Sample
	transitions := 0
	firstGood, lastGood := time.Time{}, time.Time{}
	complete := true
	seen := map[string]bool{}
	for i := range matching {
		sample := &matching[i]
		identity := sample.Key + ":" + sample.ObservedAt.Format(time.RFC3339Nano)
		if seen[identity] {
			return out, errors.New("duplicate diagnostic observation identity")
		}
		seen[identity] = true
		if sample.Key == metric {
			prior = latest
			latest = sample
		}
		if sample.Key != "link.state" {
			continue
		}
		if sample.Quality != "good" {
			complete = false
		} else {
			if firstGood.IsZero() {
				firstGood = sample.ObservedAt
			}
			if !lastGood.IsZero() && sample.ObservedAt.Sub(lastGood) > 30*time.Second {
				complete = false
			}
			lastGood = sample.ObservedAt
		}
		if lastLink != nil && lastLink.Quality == "good" && sample.Quality == "good" && lastLink.State == "up" && sample.State == "down" {
			transitions++
		}
		lastLink = sample
	}
	verdict, code := "BLOCKED", "matching_observation_missing"
	if latest != nil {
		for _, finding := range monitor.Evaluate(*latest, prior, true, transitions, scope.Limits) {
			if finding.RuleID != scope.RuleID {
				continue
			}
			code = finding.State
			if finding.State == "firing" {
				verdict = "FAIL"
			} else if finding.State == "resolved" {
				verdict = "PASS"
			}
		}
	}
	if latest != nil && capture.FinishedAt.Sub(latest.ObservedAt) > 30*time.Second {
		verdict, code = "BLOCKED", "fresh_post_repair_observation_required"
	}
	if scope.RuleID == "pfc-pause-growth" && (capture.CounterEpoch == "" || capture.CounterEpochBefore == "" || capture.CounterEpoch != capture.CounterEpochBefore) {
		verdict, code = "BLOCKED", "counter_epoch_unknown_or_changed"
	}
	if scope.RuleID == "link-flapping" {
		if !complete || firstGood.IsZero() || lastGood.Sub(firstGood) < 10*time.Minute {
			verdict, code = "BLOCKED", "complete_ten_minute_window_required"
		}
		add("continuous_post_repair_flapping_window", verdict, code)
	}
	if latest != nil && latest.Key == "link.state" && latest.Quality == "good" && latest.State != "up" && verdict == "PASS" {
		verdict, code = "BLOCKED", "link_not_up"
	}
	add("original_monitor_rule", verdict, code)
	if scope.Category == "T3" {
		add("ddm_readback", verdict, code)
	}
	if slices.Contains([]string{"T3", "T4", "T5", "T6"}, scope.Category) {
		probe := capture.Path
		if probe == nil {
			add("connectivity_single_path", "BLOCKED", "path_probe_missing")
		} else if probe.PhysicalPortID != scope.PhysicalPortID || probe.Interface != scope.Interface || probe.PeerSerial != scope.SwitchSerial || probe.PeerPort != scope.SwitchPort || probe.StartedAt.Before(capture.StartedAt) || probe.FinishedAt.After(capture.FinishedAt) || probe.FinishedAt.Before(probe.StartedAt) || capture.FinishedAt.Sub(probe.FinishedAt) > 30*time.Second || !slices.Contains(capture.RawRefs, probe.RawRef) || probe.Sent < 1 || probe.Sent > 100 || probe.Received < 0 || probe.Received > probe.Sent {
			return out, errors.New("bounded probe of the frozen physical path required")
		} else if probe.Received < probe.Sent {
			add("connectivity_single_path", "FAIL", "path_packet_loss")
		} else {
			add("connectivity_single_path", "PASS", "path_packets_received")
		}
	}
	if scope.Category == "T6" {
		if capture.ConfigurationHash == "" || scope.ExpectedConfigurationHash == "" {
			add("configuration_readback", "BLOCKED", "approved_configuration_readback_missing")
		} else if capture.ConfigurationHash != scope.ExpectedConfigurationHash {
			add("configuration_readback", "FAIL", "configuration_mismatch")
		} else {
			add("configuration_readback", "PASS", "configuration_matches")
		}
	}
	if !slices.Contains([]string{"T1", "T3", "T4", "T6"}, scope.Category) || !slices.Contains([]string{"host", "switch"}, scope.Source) || scope.Origin != "real" && scope.Origin != "sim" {
		return out, errors.New("supported port incident scope required")
	}
	for _, limit := range scope.Limits {
		for _, value := range []*float64{limit.WarnMin, limit.WarnMax, limit.CriticalMin, limit.CriticalMax} {
			if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
				return out, errors.New("finite approved thresholds required")
			}
		}
	}
	return out, nil
}
