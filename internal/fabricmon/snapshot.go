// Package fabricmon owns normalized switch observations and pure network-side
// judgements. NOS-specific command parsing stays at the collector boundary.
package fabricmon

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"math"
	"strings"
	"time"
)

type Neighbor struct {
	ChassisID string `json:"chassis_id"`
	PortID    string `json:"port_id"`
	Sysname   string `json:"sysname"`
	Sysdesc   string `json:"sysdesc"`
	Serial    string `json:"serial,omitempty"`
}
type Optics struct {
	Vendor       string   `json:"vendor"`
	Model        string   `json:"model"`
	Serial       string   `json:"serial"`
	MediaType    string   `json:"media_type"`
	RxDBM        *float64 `json:"rx_dbm"`
	TxDBM        *float64 `json:"tx_dbm"`
	TemperatureC *float64 `json:"temperature_c"`
	VoltageV     *float64 `json:"voltage_v"`
	BiasMA       *float64 `json:"bias_ma"`
	Supported    *bool    `json:"supported"`
}
type Port struct {
	CounterEpoch     string             `json:"counter_epoch,omitempty"`
	Duplex           string             `json:"duplex,omitempty"`
	MediaClass       string             `json:"media_class,omitempty"`
	OpticsApplicable *bool              `json:"optics_applicable,omitempty"`
	Name             string             `json:"name"`
	LinkUp           *bool              `json:"link_up"`
	LLDPReadable     bool               `json:"lldp_readable"`
	Neighbor         *Neighbor          `json:"neighbor"`
	SpeedMbps        *int64             `json:"speed_mbps"`
	MTU              *int               `json:"mtu"`
	CRC              *uint64            `json:"crc"`
	Flaps            *uint64            `json:"flaps"`
	Optics           *Optics            `json:"optics"`
	Metrics          map[string]float64 `json:"metrics"`
}
type FDB struct {
	MAC  string `json:"mac"`
	Port string `json:"port"`
	VLAN int    `json:"vlan"`
}
type ARP struct {
	Address string `json:"address"`
	MAC     string `json:"mac"`
	Port    string `json:"port"`
}
type Snapshot struct {
	AppliedInterfaces map[string]InterfaceConfiguration `json:"applied_interfaces,omitempty"`
	Logs              []LogEntry                        `json:"logs,omitempty"`
	LogWindowStart    *time.Time                        `json:"log_window_start,omitempty"`
	MetricPoints      []MetricPoint                     `json:"metric_points,omitempty"`
	AppliedLossless   map[string]map[string]any         `json:"applied_lossless,omitempty"`
	Version           string                            `json:"version"`
	Source            string                            `json:"source"`
	Provider          string                            `json:"provider"`
	Serial            string                            `json:"serial"`
	NOS               string                            `json:"nos"`
	CollectedAt       time.Time                         `json:"collected_at"`
	Capabilities      map[string]bool                   `json:"capabilities"`
	Ports             []Port                            `json:"ports"`
	FDB               []FDB                             `json:"fdb"`
	ARP               []ARP                             `json:"arp"`
	BGPUp             *bool                             `json:"bgp_up"`
	PTPLocked         *bool                             `json:"ptp_locked"`
}

func (s Snapshot) Validate(serial string, now time.Time) error {
	if s.Version != "fabric-snapshot/v1" || s.Source != "switch" || s.Serial != serial || strings.TrimSpace(s.Provider) == "" || len(s.Provider) > 128 {
		return errors.New("collector provenance or device identity is invalid")
	}
	if s.CollectedAt.IsZero() || s.CollectedAt.After(now.Add(5*time.Second)) || s.CollectedAt.Before(now.Add(-5*time.Minute)) {
		return errors.New("collector sample is stale or from the future")
	}
	if len(s.Ports) > 8192 || len(s.FDB) > 100000 || len(s.ARP) > 100000 {
		return errors.New("collector sample exceeds limits")
	}
	if s.BGPUp != nil && !s.Capabilities["bgp"] || s.PTPLocked != nil && !s.Capabilities["ptp"] {
		return errors.New("protocol status contradicts collector capability")
	}
	if len(s.Logs) > 20000 {
		return errors.New("log observations exceed limits")
	}
	if len(s.Logs) > 0 || s.LogWindowStart != nil {
		if !s.Capabilities["logs"] || s.LogWindowStart == nil || s.LogWindowStart.After(s.CollectedAt) || s.LogWindowStart.Before(s.CollectedAt.Add(-72*time.Hour)) {
			return errors.New("log window or capability is invalid")
		}
		for _, entry := range s.Logs {
			if e := entry.Validate(*s.LogWindowStart, s.CollectedAt); e != nil {
				return e
			}
		}
	}
	if len(s.MetricPoints) > 100000 || len(s.AppliedLossless) > 8192 {
		return errors.New("lossless observations exceed limits")
	}
	if len(s.MetricPoints) > 0 && !s.Capabilities["lossless_metrics"] {
		return errors.New("lossless metrics contradict collector capability")
	}
	for _, configuration := range s.AppliedLossless {
		raw, e := json.Marshal(configuration)
		if e != nil || netbaseline.ValidateConfiguration(raw) != nil {
			return errors.New("normalized NVUE configuration is invalid or contains secrets")
		}
	}
	if len(s.AppliedLossless) > 0 && !s.Capabilities["nvue"] {
		return errors.New("NVUE configuration contradicts collector capability")
	}
	if len(s.MetricPoints) > 0 {
		catalog, e := DefaultLosslessCatalog()
		if e != nil {
			return e
		}
		registry, e := catalog.Registry()
		if e != nil {
			return e
		}
		if _, e = NewMetricEvaluation(registry, s.MetricPoints, nil, now); e != nil {
			return e
		}
	}
	if len(s.AppliedInterfaces) > 8192 {
		return errors.New("interface configuration exceeds limits")
	}
	if len(s.AppliedInterfaces) > 0 && !s.Capabilities["nvue"] {
		return errors.New("applied interface state requires NVUE capability")
	}
	for name, c := range s.AppliedInterfaces {
		if strings.TrimSpace(name) == "" || len(name) > 128 {
			return errors.New("invalid applied interface port")
		}
		if e := c.Validate(); e != nil {
			return e
		}
	}
	seen := map[string]bool{}
	for _, p := range s.Ports {
		if len(p.CounterEpoch) > 128 || strings.ContainsAny(p.CounterEpoch, "\r\n\x00") {
			return errors.New("invalid interface counter epoch")
		}
		if (p.Duplex != "" || p.MediaClass != "") && !s.Capabilities["physical_link"] {
			return errors.New("physical link facts contradict collector capability")
		}
		if p.Duplex != "" && p.Duplex != "full" && p.Duplex != "half" {
			return errors.New("invalid duplex observation")
		}
		switch p.MediaClass {
		case "", "optical", "copper", "dac", "aoc":
		default:
			return errors.New("invalid normalized physical media class")
		}

		if p.Optics != nil && p.OpticsApplicable != nil && !*p.OpticsApplicable {
			return errors.New("optics contradict applicability flag")
		}
		if strings.TrimSpace(p.Name) == "" || len(p.Name) > 128 || strings.ContainsAny(p.Name, "\r\n\x00") || seen[p.Name] {
			return errors.New("invalid or duplicate collector port")
		}
		seen[p.Name] = true
		if p.Neighbor != nil && (!p.LLDPReadable || p.LinkUp == nil || !*p.LinkUp) {
			return errors.New("LLDP neighbor contradicts interface availability")
		}
		if p.SpeedMbps != nil && *p.SpeedMbps <= 0 {
			return errors.New("invalid interface speed")
		}
		for _, v := range p.Metrics {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return errors.New("invalid metric value")
			}
		}
	}
	return nil
}

type Endpoint struct {
	UID        string
	Switch     bool
	Serial     string
	Port       string
	Hostname   string
	ChassisIDs []string
}
type Cable struct {
	UID string
	A   Endpoint
	B   Endpoint
}

const CableComparisonRuleRef = "PRECHECK-LLDP@1"

type Finding struct {
	CableUID  string `json:"cable_uid"`
	A         string `json:"endpoint_a_uid"`
	B         string `json:"endpoint_b_uid"`
	Verdict   string `json:"verdict"`
	IssueType string `json:"issue_type,omitempty"`
	Reason    string `json:"reason"`
	Source    string `json:"source"`
	RuleRef   string `json:"rule_ref"`
}

func EvaluateCable(c Cable, samples map[string]Snapshot) Finding {
	return EvaluateCables([]Cable{c}, samples)[0]
}
func EvaluateCables(cables []Cable, samples map[string]Snapshot) []Finding {
	ports := map[string]map[string]Port{}
	for serial, sample := range samples {
		ports[serial] = map[string]Port{}
		for _, p := range sample.Ports {
			ports[serial][p.Name] = p
		}
	}
	out := make([]Finding, 0, len(cables))
	for _, c := range cables {
		out = append(out, evaluateIndexedCable(c, samples, ports))
	}
	return out
}
func evaluateIndexedCable(c Cable, samples map[string]Snapshot, ports map[string]map[string]Port) Finding {
	f := Finding{CableUID: c.UID, A: c.A.UID, B: c.B.UID, Verdict: "pending", Reason: "server_side_pending", Source: "switch", RuleRef: CableComparisonRuleRef}
	if !c.A.Switch || !c.B.Switch {
		return f
	}
	f.Reason = "switch_evidence_missing"
	for _, pair := range [][2]Endpoint{{c.A, c.B}, {c.B, c.A}} {
		local, remote := pair[0], pair[1]
		sample, ok := samples[local.Serial]
		if !ok || !sample.Capabilities["interfaces"] {
			return f
		}
		observed, portFound := ports[local.Serial][local.Port]
		if !portFound || observed.LinkUp == nil {
			return f
		}
		if !*observed.LinkUp {
			f.Verdict = "anomaly"
			f.IssueType = "T1"
			f.Reason = "switch_link_down"
			return f
		}
		if !sample.Capabilities["lldp"] || !observed.LLDPReadable {
			return f
		}
		if observed.Neighbor == nil {
			f.Verdict = "anomaly"
			f.IssueType = "T1"
			f.Reason = "switch_lldp_absent"
			return f
		}
		n := observed.Neighbor
		// A sysname alone can be duplicated or stale. Prefer serial or an exact
		// SoT chassis MAC; a name-only match remains pending until PoC mapping exists.
		identified := n.Serial != "" && n.Serial == remote.Serial
		if n.Serial == "" {
			for _, id := range remote.ChassisIDs {
				if strings.EqualFold(strings.ReplaceAll(id, "-", ":"), strings.ReplaceAll(n.ChassisID, "-", ":")) {
					identified = true
					break
				}
			}
		}
		if !identified {
			if n.Serial != "" || len(remote.ChassisIDs) > 0 {
				f.Verdict = "anomaly"
				f.IssueType = "T2"
				f.Reason = "switch_neighbor_mismatch"
			}
			return f
		}
		if n.PortID != remote.Port {
			f.Verdict = "anomaly"
			f.IssueType = "T2"
			f.Reason = "switch_port_mismatch"
			return f
		}
		if observed.Optics != nil && observed.Optics.Supported != nil && !*observed.Optics.Supported {
			f.Verdict = "anomaly"
			f.IssueType = "T3"
			f.Reason = "unsupported_optics"
			return f
		}
	}
	f.Verdict = "precheck_pass"
	f.Reason = "reciprocal_switch_lldp"
	return f
}

// Missing approved thresholds or telemetry remains unknown; thresholds are
// supplied by the parameter catalog, never hardcoded in the UI/adapter.
type Range struct {
	WarnMin     *float64 `json:"warn_min"`
	WarnMax     *float64 `json:"warn_max"`
	CriticalMin *float64 `json:"critical_min"`
	CriticalMax *float64 `json:"critical_max"`
}

func JudgeOptics(o *Optics, limits map[string]Range) (string, []string) {
	if o == nil {
		return "unknown", []string{"ddm_not_available"}
	}
	state := "normal"
	reasons := []string{}
	if o.Supported != nil && !*o.Supported {
		state = "critical"
		reasons = append(reasons, "unsupported_optics")
	}
	values := map[string]*float64{"rx_dbm": o.RxDBM, "tx_dbm": o.TxDBM, "temperature_c": o.TemperatureC, "voltage_v": o.VoltageV, "bias_ma": o.BiasMA}
	for _, key := range []string{"rx_dbm", "tx_dbm", "temperature_c", "voltage_v", "bias_ma"} {
		value := values[key]
		r, ok := limits[key]
		if !ok || value == nil || r.CriticalMin == nil && r.CriticalMax == nil && r.WarnMin == nil && r.WarnMax == nil {
			if state == "normal" {
				state = "unknown"
			}
			if value == nil {
				reasons = append(reasons, key+"_telemetry_unknown")
			} else {
				reasons = append(reasons, key+"_threshold_unknown")
			}
			continue
		}
		if r.CriticalMin != nil && *value < *r.CriticalMin || r.CriticalMax != nil && *value > *r.CriticalMax {
			state = "critical"
			reasons = append(reasons, fmt.Sprintf("%s_critical", key))
		} else if r.WarnMin != nil && *value < *r.WarnMin || r.WarnMax != nil && *value > *r.WarnMax {
			if state != "critical" {
				state = "warning"
			}
			reasons = append(reasons, key+"_warning")
		}
	}
	return state, reasons
}
