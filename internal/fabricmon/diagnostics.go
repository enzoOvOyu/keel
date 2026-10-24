package fabricmon

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"time"
)

// InterfaceConfiguration is the normalized applied NVUE state, not an inferred
// value from negotiated link speed. Nil and an explicitly empty set differ.
type InterfaceConfiguration struct {
	SpeedMbps     *int64 `json:"speed_mbps,omitempty"`
	MTU           *int   `json:"mtu,omitempty"`
	VLANs         *[]int `json:"vlans,omitempty"`
	PFCPriorities *[]int `json:"pfc_priorities,omitempty"`
	Autoneg       *bool  `json:"autoneg,omitempty"`
}

func (c InterfaceConfiguration) Validate() error {
	if c.SpeedMbps != nil && *c.SpeedMbps <= 0 || c.MTU != nil && (*c.MTU < 64 || *c.MTU > 65535) {
		return errors.New("invalid normalized interface speed or MTU")
	}
	for _, set := range []struct {
		Values   *[]int
		Min, Max int
	}{{c.VLANs, 1, 4094}, {c.PFCPriorities, 0, 7}} {
		if set.Values == nil {
			continue
		}
		if len(*set.Values) > set.Max-set.Min+1 {
			return errors.New("interface set exceeds limits")
		}
		seen := map[int]bool{}
		for _, v := range *set.Values {
			if v < set.Min || v > set.Max || seen[v] {
				return errors.New("invalid or duplicate interface VLAN/PFC member")
			}
			seen[v] = true
		}
	}
	return nil
}

type PortIntent struct {
	Configuration InterfaceConfiguration `json:"configuration"`
	RailID        string                 `json:"rail_id,omitempty"`
	LeafSerial    string                 `json:"leaf_serial,omitempty"`
}
type CounterPolicy struct {
	WindowSeconds *int    `json:"window_seconds"`
	CRCMax        *uint64 `json:"crc_growth_max,string"`
	FlapMax       *uint64 `json:"flap_growth_max,string"`
}
type DiagnosticCheck struct {
	RuleRef      string            `json:"rule_ref"`
	IssueType    string            `json:"issue_type"`
	Subtype      string            `json:"subtype,omitempty"`
	State        string            `json:"state"`
	Reason       string            `json:"reason"`
	Observed     map[string]string `json:"observed"`
	ThresholdRef string            `json:"threshold_ref,omitempty"`
}
type PortDiagnostics struct {
	Port   string            `json:"port"`
	InUse  bool              `json:"in_use"`
	Checks []DiagnosticCheck `json:"checks"`
}
type PeerObservation struct {
	Snapshot      Snapshot
	Port          Port
	TrustedSerial string
}

func DiagnosticPorts(current Snapshot, previous *Snapshot, portName string, inUse bool, peer *PeerObservation, intent *PortIntent, policy CounterPolicy) PortDiagnostics {
	out := PortDiagnostics{Port: portName, InUse: inUse, Checks: []DiagnosticCheck{}}
	add := func(rule, kind, subtype, state, reason string, observed map[string]string, threshold string) {
		if observed == nil {
			observed = map[string]string{}
		}
		out.Checks = append(out.Checks, DiagnosticCheck{rule, kind, subtype, state, reason, observed, threshold})
	}
	if !inUse {
		add("PRECHECK-PORT-USAGE@1", "", "", "not_applicable", "unused_port_not_evaluated", nil, "")
		return out
	}
	var currentPort *Port
	for i := range current.Ports {
		if current.Ports[i].Name == portName {
			currentPort = &current.Ports[i]
			break
		}
	}
	if currentPort == nil {
		add("PRECHECK-PORT-EVIDENCE@1", "", "", "unknown", "modeled_connected_port_sample_missing", nil, "")
		return out
	}
	var oldPort *Port
	if previous != nil {
		for i := range previous.Ports {
			if previous.Ports[i].Name == portName {
				oldPort = &previous.Ports[i]
				break
			}
		}
	}
	for _, counter := range []struct {
		Name                  string
		Current, Old, Maximum *uint64
	}{{"crc", currentPort.CRC, nil, policy.CRCMax}, {"flap", currentPort.Flaps, nil, policy.FlapMax}} {
		if oldPort != nil {
			if counter.Name == "crc" {
				counter.Old = oldPort.CRC
			} else {
				counter.Old = oldPort.Flaps
			}
		}
		reason := ""
		state := "unknown"
		observed := map[string]string{}
		key := "fabric.precheck." + counter.Name + "_growth_max"
		switch {
		case !current.Capabilities["interface_counters"] || previous != nil && !previous.Capabilities["interface_counters"]:
			reason = "counter_capability_unavailable"
		case policy.WindowSeconds == nil || counter.Maximum == nil:
			reason = "counter_threshold_or_window_missing"
		case currentPort.CRC == nil && counter.Name == "crc" || currentPort.Flaps == nil && counter.Name == "flap" || counter.Current == nil:
			reason = "counter_unavailable"
		case previous == nil || oldPort == nil || counter.Old == nil:
			reason = "counter_baseline_missing"
		case currentPort.CounterEpoch == "" || oldPort.CounterEpoch == "":
			reason = "counter_epoch_unknown"
		case previous.Provider != current.Provider || previous.Source != current.Source || previous.Serial != current.Serial:
			reason = "counter_source_changed"
		case oldPort.CounterEpoch != currentPort.CounterEpoch:
			reason = "counter_reset"
		case *counter.Current < *counter.Old:
			reason = "counter_reset"
		default:
			elapsed := current.CollectedAt.Sub(previous.CollectedAt)
			window := time.Duration(*policy.WindowSeconds) * time.Second
			if elapsed < window || elapsed > window+window/2 || elapsed <= 0 {
				reason = "counter_window_incomplete"
			} else {
				delta := *counter.Current - *counter.Old
				observed["growth"] = strconv.FormatUint(delta, 10)
				observed["maximum"] = strconv.FormatUint(*counter.Maximum, 10)
				observed["elapsed_seconds"] = strconv.FormatFloat(elapsed.Seconds(), 'f', 3, 64)
				state = "normal"
				reason = "counter_growth_within_limit"
				if delta > *counter.Maximum {
					state = "anomaly"
					reason = "counter_growth_exceeds_limit"
				}
			}
		}
		add("PRECHECK-T4-"+counter.Name+"@1", "T4", "", state, reason, observed, key)
	}
	for _, name := range []string{"speed", "duplex", "media"} {
		state, reason := "unknown", "physical_peer_evidence_missing"
		observed := map[string]string{}
		if peer != nil {
			var a, b string
			switch name {
			case "speed":
				if current.Capabilities["interfaces"] && peer.Snapshot.Capabilities["interfaces"] && currentPort.SpeedMbps != nil && peer.Port.SpeedMbps != nil {
					a = strconv.FormatInt(*currentPort.SpeedMbps, 10)
					b = strconv.FormatInt(*peer.Port.SpeedMbps, 10)
				}
			case "duplex":
				if current.Capabilities["physical_link"] && peer.Snapshot.Capabilities["physical_link"] {
					a = currentPort.Duplex
					b = peer.Port.Duplex
				}
			case "media":
				if current.Capabilities["physical_link"] && peer.Snapshot.Capabilities["physical_link"] {
					a = currentPort.MediaClass
					b = peer.Port.MediaClass
				}
			}
			if a != "" && b != "" {
				state = "normal"
				reason = "physical_link_parameters_match"
				observed["local"] = a
				observed["peer"] = b
				if a != b {
					state = "anomaly"
					reason = "physical_link_parameter_mismatch"
				}
			}
		}
		add("PRECHECK-T4-"+name+"@1", "T4", "", state, reason, observed, "")
	}
	for _, field := range []string{"speed_mbps", "mtu", "vlans", "pfc_priorities", "autoneg"} {
		state, reason := "unknown", "approved_interface_intent_missing"
		observed := map[string]string{}
		if intent != nil {
			applied, ok := current.AppliedInterfaces[portName]
			if !current.Capabilities["nvue"] || !ok {
				reason = "applied_interface_configuration_missing"
			} else {
				want, have := configurationField(intent.Configuration, field), configurationField(applied, field)
				if want == nil {
					reason = "approved_interface_field_missing"
				} else if have == nil {
					reason = "applied_interface_field_missing"
				} else {
					state = "normal"
					reason = "applied_configuration_matches_intent"
					observed["intended"] = fmt.Sprint(want)
					observed["applied"] = fmt.Sprint(have)
					if !reflect.DeepEqual(want, have) {
						state = "anomaly"
						reason = "applied_configuration_mismatch"
					}
				}
			}
		}
		add("PRECHECK-T6-"+field+"@1", "T6", "", state, reason, observed, "")
	}
	state, reason := "unknown", "approved_rail_intent_missing"
	observed := map[string]string{}
	if intent != nil && intent.RailID != "" && intent.LeafSerial != "" {
		observed["rail_id"] = intent.RailID
		observed["expected_leaf_serial"] = intent.LeafSerial
		if peer != nil && peer.TrustedSerial != "" {
			observed["observed_leaf_serial"] = peer.TrustedSerial
			state = "normal"
			reason = "approved_rail_peer_matches"
			if peer.TrustedSerial != intent.LeafSerial {
				state = "anomaly"
				reason = "approved_rail_peer_mismatch"
			}
		} else {
			reason = "rail_peer_identity_unknown"
		}
	}
	add("PRECHECK-T2-RAIL@1", "T2", "rail", state, reason, observed, "")
	return out
}
func configurationField(c InterfaceConfiguration, field string) any {
	switch field {
	case "speed_mbps":
		if c.SpeedMbps != nil {
			return *c.SpeedMbps
		}
	case "mtu":
		if c.MTU != nil {
			return *c.MTU
		}
	case "autoneg":
		if c.Autoneg != nil {
			return *c.Autoneg
		}
	case "vlans":
		if c.VLANs != nil {
			v := append([]int{}, (*c.VLANs)...)
			sort.Ints(v)
			return v
		}
	case "pfc_priorities":
		if c.PFCPriorities != nil {
			v := append([]int{}, (*c.PFCPriorities)...)
			sort.Ints(v)
			return v
		}
	}
	return nil
}
