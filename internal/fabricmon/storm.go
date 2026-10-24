package fabricmon

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

//go:embed storm-policy.json
var stormPolicyData []byte

type StormPolicy struct {
	RuleID         string `json:"rule_id"`
	Version        int    `json:"version"`
	ThresholdPorts int    `json:"threshold_ports"`
}

type FabricStorm struct {
	RuleID         string   `json:"rule_id"`
	PolicyVersion  int      `json:"policy_version"`
	ThresholdPorts int      `json:"threshold_ports"`
	AffectedPorts  int      `json:"affected_ports"`
	State          string   `json:"state"`
	Incomplete     bool     `json:"incomplete"`
	Approximation  bool     `json:"approximation"`
	AppliedRules   []string `json:"applied_rule_ids"`
	DeviceIDs      []string `json:"affected_device_ids"`
}

func DefaultStormPolicy() (StormPolicy, error) {
	var p StormPolicy
	if err := json.Unmarshal(stormPolicyData, &p); err != nil {
		return p, err
	}
	if p.RuleID != "LL-02" || p.Version < 1 || p.ThresholdPorts < 1 || p.ThresholdPorts > 100000 {
		return p, errors.New("invalid fabric storm aggregation policy")
	}
	return p, nil
}

// AggregateFabricStorm consumes findings from one evaluation boundary. Multiple
// priorities on one physical port count once; historical hits cannot be pooled.
func AggregateFabricStorm(findings []LosslessFinding, p StormPolicy) FabricStorm {
	out := FabricStorm{
		RuleID: p.RuleID, PolicyVersion: p.Version, ThresholdPorts: p.ThresholdPorts,
		State: "unknown", AppliedRules: []string{}, DeviceIDs: []string{},
	}
	if p.RuleID != "LL-02" || p.Version < 1 || p.ThresholdPorts < 1 || p.ThresholdPorts > 100000 {
		out.Incomplete = true
		return out
	}
	ports, devices, rules := map[string]bool{}, map[string]bool{}, map[string]bool{}
	observed, mixedBoundary := false, false
	var boundary time.Time
	for _, f := range findings {
		if f.RuleID != p.RuleID {
			continue
		}
		observed = true
		for _, id := range f.AppliedRules {
			rules[id] = true
		}
		for _, flag := range f.Flags {
			out.Incomplete = out.Incomplete || flag == "degraded"
			out.Approximation = out.Approximation || flag == "approximation"
		}
		if f.EvaluatedAt == nil || f.EvaluatedAt.IsZero() {
			out.Incomplete = true
			continue
		}
		if !boundary.IsZero() && !f.EvaluatedAt.Equal(boundary) {
			mixedBoundary = true
		}
		boundary = *f.EvaluatedAt
		if f.Unit.Device == "" || f.Unit.Port == "" {
			out.Incomplete = true
			continue
		}
		switch f.State {
		case "critical", "warning", "info":
			ports[f.Unit.Device+"\x00"+f.Unit.Port] = true
			devices[f.Unit.Device] = true
		case "normal", "observe":
		default:
			out.Incomplete = true
		}
	}
	for id := range rules {
		out.AppliedRules = append(out.AppliedRules, id)
	}
	sort.Strings(out.AppliedRules)
	if !observed || mixedBoundary {
		out.Incomplete = true
		return out
	}
	out.AffectedPorts = len(ports)
	for id := range devices {
		out.DeviceIDs = append(out.DeviceIDs, id)
	}
	sort.Strings(out.DeviceIDs)
	if out.AffectedPorts >= p.ThresholdPorts {
		out.State = "escalated"
	} else if !out.Incomplete {
		out.State = "normal"
	}
	return out
}
