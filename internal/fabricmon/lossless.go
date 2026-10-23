package fabricmon

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
)

//go:embed lossless-defaults.json
var defaultLosslessData []byte

type LosslessCatalog struct {
	Version string             `json:"version"`
	Metrics []MetricDefinition `json:"metrics"`
	Rules   []LosslessRule     `json:"rules"`
}
type RuleScope struct {
	Device   string `json:"device,omitempty"`
	Port     string `json:"port,omitempty"`
	Priority *int   `json:"priority,omitempty"`
	Pod      string `json:"pod,omitempty"`
	Role     string `json:"role,omitempty"`
	Fabric   bool   `json:"fabric,omitempty"`
}
type LosslessRule struct {
	Escalation         *MetricExpression `json:"escalation,omitempty"`
	WeakEvidence       *MetricExpression `json:"weak_evidence,omitempty"`
	ID                 string            `json:"rule_id"`
	Version            int               `json:"version"`
	State              string            `json:"state"`
	Unit               string            `json:"subject_unit"`
	MetricKey          string            `json:"metric_key"`
	Expression         MetricExpression  `json:"expression"`
	WindowSeconds      int               `json:"window_seconds"`
	Comparator         string            `json:"comparator"`
	Threshold          json.Number       `json:"hit_threshold"`
	ObserveThreshold   *json.Number      `json:"observe_threshold,omitempty"`
	Severity           string            `json:"severity"`
	Weight             float64           `json:"health_deduction_weight"`
	Scope              RuleScope         `json:"scope"`
	UnknownConstraints []string          `json:"degrade_to_unknown_when"`
	Advice             string            `json:"advice"`
}
type LosslessFinding struct {
	Unit         MetricUnit `json:"unit"`
	RuleID       string     `json:"rule_id"`
	AppliedRules []string   `json:"applied_rule_ids"`
	State        string     `json:"state"`
	Reason       string     `json:"reason"`
	Value        string     `json:"value,omitempty"`
	Flags        []string   `json:"flags"`
	Weight       float64    `json:"health_deduction_weight"`
	Advice       string     `json:"advice"`
}
type HealthRollup struct {
	ID      string `json:"id"`
	Score   int    `json:"score"`
	State   string `json:"state"`
	Unknown bool   `json:"unknown"`
}
type LosslessHealth struct {
	Findings []LosslessFinding `json:"findings"`
	Ports    []HealthRollup    `json:"ports"`
	Devices  []HealthRollup    `json:"devices"`
	Pods     []HealthRollup    `json:"pods"`
	Fabric   HealthRollup      `json:"fabric"`
}

func DefaultLosslessCatalog() (LosslessCatalog, error) {
	var c LosslessCatalog
	e := json.Unmarshal(defaultLosslessData, &c)
	if e != nil {
		return c, e
	}
	registry, e := c.Registry()
	if e != nil {
		return c, e
	}
	evaluation := &MetricEvaluation{Registry: registry}
	for _, r := range c.Rules {
		if e = r.Validate(evaluation); e != nil {
			return c, fmt.Errorf("%s: %w", r.ID, e)
		}
	}
	return c, nil
}
func (c LosslessCatalog) Registry() (map[string]MetricDefinition, error) {
	out := map[string]MetricDefinition{}
	for _, d := range c.Metrics {
		if d.Key == "" || out[d.Key].Key != "" || d.IntervalSeconds < 1 || d.IntervalSeconds > 900 || len(d.Sources) == 0 {
			return nil, errors.New("invalid metric registry entry")
		}
		if d.Kind != "counter" && d.Kind != "gauge" {
			return nil, errors.New("invalid registered metric kind")
		}
		switch d.Granularity {
		case "port_priority", "port_queue", "port", "device_pool", "device":
		default:
			return nil, errors.New("invalid metric granularity")
		}
		out[d.Key] = d
	}
	return out, nil
}
func (r LosslessRule) Validate(c *MetricEvaluation) error {
	if r.ID == "" || r.Version < 1 || r.Unit != "port_priority" || r.MetricKey == "" || r.Weight < 0 || r.Weight > 100 || math.IsNaN(r.Weight) || math.IsInf(r.Weight, 0) || len(r.UnknownConstraints) == 0 {
		return errors.New("invalid lossless rule identity, unit, weight or unknown constraints")
	}
	if r.State != "draft" && r.State != "released" && r.State != "disabled" {
		return errors.New("invalid rule state")
	}
	if r.Severity != "critical" && r.Severity != "warning" && r.Severity != "info" {
		return errors.New("invalid rule severity")
	}
	if r.WindowSeconds != 60 && r.WindowSeconds != 300 && r.WindowSeconds != 3600 {
		return errors.New("invalid rule window")
	}
	hit, ok := decimal(r.Threshold)
	if !ok {
		return errors.New("invalid rule threshold")
	}
	switch r.Comparator {
	case "gt", "gte", "lt", "lte", "eq":
	default:
		return errors.New("unsupported comparator")
	}
	if r.ObserveThreshold != nil {
		observe, ok := decimal(*r.ObserveThreshold)
		if !ok {
			return errors.New("invalid observe threshold")
		}
		if (r.Comparator == "gt" || r.Comparator == "gte") && observe.Cmp(hit) >= 0 || (r.Comparator == "lt" || r.Comparator == "lte") && observe.Cmp(hit) <= 0 {
			return errors.New("observe threshold must precede hit threshold")
		}
	}
	return c.ValidateExpression(r.Expression)
}
func (s RuleScope) match(u MetricUnit) (int, bool) {
	if s.Device != "" && s.Device != u.Device || s.Port != "" && s.Port != u.Port || s.Priority != nil && *s.Priority != u.Priority || s.Pod != "" && s.Pod != u.Pod || s.Role != "" && s.Role != u.Role {
		return 0, false
	}
	switch {
	case s.Device != "":
		return 6, true
	case s.Port != "":
		return 5, true
	case s.Priority != nil:
		return 4, true
	case s.Pod != "":
		return 3, true
	case s.Role != "":
		return 2, true
	case s.Fabric:
		return 1, true
	default:
		return 0, true
	}
}
func compareMetric(value, threshold *big.Rat, op string) bool {
	n := value.Cmp(threshold)
	switch op {
	case "gt":
		return n > 0
	case "gte":
		return n >= 0
	case "lt":
		return n < 0
	case "lte":
		return n <= 0
	case "eq":
		return n == 0
	}
	return false
}
func findingRank(state string) int {
	switch state {
	case "critical":
		return 4
	case "warning":
		return 3
	case "info":
		return 2
	case "observe":
		return 1
	default:
		return 0
	}
}

// Highest matching scope wins; every rule at that scope is evaluated. This
// applies the earliest-hit threshold rather than silently choosing a row.
func EvaluateLosslessUnit(c *MetricEvaluation, u MetricUnit, rules []LosslessRule) []LosslessFinding {
	groups := map[string][]LosslessRule{}
	ranks := map[string]int{}
	for _, r := range rules {
		if r.State == "draft" {
			continue
		}
		rank, match := r.Scope.match(u)
		if !match {
			continue
		}
		prior, exists := ranks[r.ID]
		if !exists || rank > prior {
			groups[r.ID] = []LosslessRule{r}
			ranks[r.ID] = rank
		} else if rank == prior {
			groups[r.ID] = append(groups[r.ID], r)
		}
	}
	ids := make([]string, 0, len(groups))
	for id := range groups {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	findings := []LosslessFinding{}
	for _, id := range ids {
		f := LosslessFinding{Unit: u, RuleID: id, AppliedRules: []string{}, State: "normal", Flags: []string{}}
		unknown := false
		for _, r := range groups[id] {
			f.AppliedRules = append(f.AppliedRules, fmt.Sprintf("%s@%d", r.ID, r.Version))
			if r.State == "disabled" {
				unknown = true
				f.Reason = "rule_disabled"
				f.Flags = append(f.Flags, "degraded")
				continue
			}
			if r.Validate(c) != nil {
				unknown = true
				f.Reason = "rule_invalid"
				continue
			}
			v := c.Evaluate(r.Expression, u, r.WindowSeconds)
			if v.Number == nil && r.WeakEvidence != nil {
				weak := c.Evaluate(*r.WeakEvidence, u, r.WindowSeconds)
				if weak.Number != nil {
					f.Flags = append(f.Flags, "weak_evidence_available", "degraded")
				}
			}

			f.Flags = append(f.Flags, v.Flags...)
			if v.Number == nil {
				unknown = true
				f.Reason = v.Reason
				continue
			}
			threshold, _ := decimal(r.Threshold)
			state := "normal"
			if compareMetric(v.Number, threshold, r.Comparator) {
				state = r.Severity
				if r.Escalation != nil {
					escalation := c.Evaluate(*r.Escalation, u, r.WindowSeconds)
					if escalation.Number != nil && escalation.Number.Sign() != 0 {
						f.Flags = append(f.Flags, "escalated")
					}
				}
			} else if r.ObserveThreshold != nil {
				observe, _ := decimal(*r.ObserveThreshold)
				if compareMetric(v.Number, observe, r.Comparator) {
					state = "observe"
				}
			}
			if findingRank(state) > findingRank(f.State) {
				f.State = state
				f.Value = v.Number.FloatString(8)
				f.Advice = r.Advice
				f.Weight = r.Weight
			} else if findingRank(state) == findingRank(f.State) && r.Weight > f.Weight {
				f.Weight = r.Weight
			}
		}
		if unknown {
			if findingRank(f.State) == 0 {
				f.State = "unknown"
			}
			f.Flags = append(f.Flags, "degraded")
		}
		sort.Strings(f.AppliedRules)
		sort.Strings(f.Flags)
		f.Flags = uniqueStrings(f.Flags)
		findings = append(findings, f)
	}
	return findings
}
func uniqueStrings(values []string) []string {
	out := []string{}
	for _, s := range values {
		if len(out) == 0 || out[len(out)-1] != s {
			out = append(out, s)
		}
	}
	return out
}
func grade(score int, unknown bool) string {
	if score < 70 {
		return "critical"
	}
	if score < 90 || unknown {
		return "warning"
	}
	return "healthy"
}
func scoreFindings(id string, findings []LosslessFinding) HealthRollup {
	unknown := len(findings) == 0
	affected := map[string]map[string]bool{}
	weights := map[string]float64{}
	for _, f := range findings {
		unknown = unknown || f.State == "unknown"
		for _, flag := range f.Flags {
			unknown = unknown || flag == "degraded"
		}
		if f.State != "critical" && f.State != "warning" && f.State != "info" {
			continue
		}
		if affected[f.RuleID] == nil {
			affected[f.RuleID] = map[string]bool{}
		}
		key := fmt.Sprintf("%s\x00%s\x00%d", f.Unit.Device, f.Unit.Port, f.Unit.Priority)
		affected[f.RuleID][key] = true
		if f.Weight > weights[f.RuleID] {
			weights[f.RuleID] = f.Weight
		}
	}
	deduction := 0.0
	for rule, units := range affected {
		deduction += weights[rule] * math.Sqrt(float64(len(units)))
	}
	score := int(math.Floor(math.Max(0, math.Min(100, 100-deduction))))
	return HealthRollup{ID: id, Score: score, State: grade(score, unknown), Unknown: unknown}
}
func RollupLossless(findings []LosslessFinding, expected []MetricUnit) LosslessHealth {
	report := LosslessHealth{Findings: findings, Ports: []HealthRollup{}, Devices: []HealthRollup{}, Pods: []HealthRollup{}}
	ports := map[string][]LosslessFinding{}
	devicePorts := map[string]map[string]bool{}
	podDevices := map[string]map[string]bool{}
	for _, u := range expected {
		port := u.Device + "/" + u.Port
		if ports[port] == nil {
			ports[port] = []LosslessFinding{}
		}
		if devicePorts[u.Device] == nil {
			devicePorts[u.Device] = map[string]bool{}
		}
		devicePorts[u.Device][port] = true
		if podDevices[u.Pod] == nil {
			podDevices[u.Pod] = map[string]bool{}
		}
		podDevices[u.Pod][u.Device] = true
	}
	for _, f := range findings {
		port := f.Unit.Device + "/" + f.Unit.Port
		ports[port] = append(ports[port], f)
	}
	portScores := map[string]HealthRollup{}
	deviceScores := map[string]HealthRollup{}
	for id, values := range ports {
		score := scoreFindings(id, values)
		report.Ports = append(report.Ports, score)
		portScores[id] = score
	}
	for id, children := range devicePorts {
		score := HealthRollup{ID: id, Score: 100}
		for child := range children {
			score.Score = min(score.Score, portScores[child].Score)
			score.Unknown = score.Unknown || portScores[child].Unknown
		}
		score.State = grade(score.Score, score.Unknown)
		report.Devices = append(report.Devices, score)
		deviceScores[id] = score
	}
	fabric := HealthRollup{ID: "fabric", Score: 100, State: "healthy", Unknown: len(expected) == 0}
	for id, children := range podDevices {
		score := HealthRollup{ID: id, Score: 100}
		for child := range children {
			score.Score = min(score.Score, deviceScores[child].Score)
			score.Unknown = score.Unknown || deviceScores[child].Unknown
		}
		score.State = grade(score.Score, score.Unknown)
		report.Pods = append(report.Pods, score)
		fabric.Score = min(fabric.Score, score.Score)
		fabric.Unknown = fabric.Unknown || score.Unknown
	}
	fabric.State = grade(fabric.Score, fabric.Unknown)
	report.Fabric = fabric
	sort.Slice(report.Ports, func(i, j int) bool { return report.Ports[i].ID < report.Ports[j].ID })
	sort.Slice(report.Devices, func(i, j int) bool { return report.Devices[i].ID < report.Devices[j].ID })
	sort.Slice(report.Pods, func(i, j int) bool { return report.Pods[i].ID < report.Pods[j].ID })
	return report
}
