package fabricmon

import (
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"time"
)

// GovernanceMetric describes the derived output of a frozen lossless rule. It
// does not replace the raw sampling registry or alter collector scheduling.
type GovernanceMetric struct {
	Key     string      `json:"key"`
	Unit    string      `json:"unit"`
	Minimum json.Number `json:"safe_min"`
	Maximum json.Number `json:"safe_max"`
}

//go:embed governance-metrics.json
var governanceMetricData []byte

func LosslessGovernanceMetrics() []GovernanceMetric {
	var out []GovernanceMetric
	_ = json.Unmarshal(governanceMetricData, &out)
	return out
}
func ValidateGovernedLossless(rule, base LosslessRule, unit, overrideReason string, confirmOverride bool) error {
	if rule.ID != base.ID || rule.Unit != base.Unit || rule.MetricKey != base.MetricKey || !reflect.DeepEqual(rule.UnknownConstraints, base.UnknownConstraints) {
		return errors.New("rule_id, subject unit, metric key and unknown constraints are immutable")
	}
	if e := validateConditionParameters(rule, base, overrideReason, confirmOverride); e != nil {
		return e
	}

	if rule.State != "released" && rule.State != "disabled" {
		return errors.New("target state must be released or disabled")
	}
	if rule.Scope.Priority != nil && (*rule.Scope.Priority < 0 || *rule.Scope.Priority > 7) {
		return errors.New("priority must be 0 through 7")
	}
	catalog, e := DefaultLosslessCatalog()
	if e != nil {
		return e
	}
	registry, e := catalog.Registry()
	if e != nil {
		return e
	}
	if e = rule.Validate(&MetricEvaluation{Registry: registry}); e != nil {
		return e
	}
	for _, extra := range []*MetricExpression{rule.Escalation, rule.WeakEvidence} {
		if extra != nil {
			if e = (&MetricEvaluation{Registry: registry}).ValidateExpression(*extra); e != nil {
				return e
			}
		}
	}

	var metric *GovernanceMetric
	for _, m := range LosslessGovernanceMetrics() {
		if m.Key == rule.MetricKey {
			copy := m
			metric = &copy
			break
		}
	}
	if metric == nil || unit != metric.Unit {
		return errors.New("derived metric is unregistered or unit differs; percent and ratio are distinct")
	}
	if metric.Unit == "boolean" && (rule.Comparator != base.Comparator || rule.Threshold != base.Threshold || rule.ObserveThreshold != nil) {
		return errors.New("boolean derived output thresholds are fixed; edit the registered typed condition parameters")
	}

	min, _ := decimal(metric.Minimum)
	max, _ := decimal(metric.Maximum)
	hit, _ := decimal(rule.Threshold)
	outside := hit.Cmp(min) < 0 || hit.Cmp(max) > 0
	if rule.ObserveThreshold != nil {
		observe, _ := decimal(*rule.ObserveThreshold)
		outside = outside || observe.Cmp(min) < 0 || observe.Cmp(max) > 0
	}
	if outside && (!confirmOverride || len(overrideReason) < 8) {
		return errors.New("threshold outside safe range requires confirmation and override_reason of at least eight characters")
	}
	if len(rule.Advice) > 2000 || len(overrideReason) > 1000 {
		return errors.New("rule text exceeds limits")
	}
	return nil
}

type RuleSeverityChange struct {
	EvaluatedAt *time.Time `json:"evaluated_at,omitempty"`
	Unit        MetricUnit `json:"unit"`
	RuleID      string     `json:"rule_id"`
	Before      string     `json:"before"`
	After       string     `json:"after"`
}
type LosslessPreviewDiff struct {
	NewFindings      []LosslessFinding    `json:"new_findings"`
	ResolvedFindings []LosslessFinding    `json:"resolved_findings"`
	SeverityChanges  []RuleSeverityChange `json:"severity_changes"`
	AffectedBefore   int                  `json:"affected_units_before"`
	AffectedAfter    int                  `json:"affected_units_after"`
	DeviceMinBefore  int                  `json:"device_min_before"`
	DeviceMinAfter   int                  `json:"device_min_after"`
	FabricAfter      HealthRollup         `json:"fabric_after"`
	Warnings         []string             `json:"warnings"`
}

func DiffLosslessFindings(before, after []LosslessFinding) LosslessPreviewDiff {
	out := LosslessPreviewDiff{NewFindings: []LosslessFinding{}, ResolvedFindings: []LosslessFinding{}, SeverityChanges: []RuleSeverityChange{}, Warnings: []string{}}
	identity := func(f LosslessFinding) string {
		b, _ := json.Marshal([]any{f.Unit.Device, f.Unit.Port, f.Unit.Priority, f.RuleID, f.EvaluatedAt})
		return string(b)
	}
	active := func(f LosslessFinding) bool {
		return f.State != "normal" && f.State != "observe" && f.State != "unknown"
	}
	old, new := map[string]LosslessFinding{}, map[string]LosslessFinding{}
	oldUnits, newUnits := map[string]bool{}, map[string]bool{}
	for _, f := range before {
		old[identity(f)] = f
		if active(f) {
			b, _ := json.Marshal([]any{f.Unit.Device, f.Unit.Port, f.Unit.Priority})
			oldUnits[string(b)] = true
		}
	}
	for _, f := range after {
		new[identity(f)] = f
		if active(f) {
			b, _ := json.Marshal([]any{f.Unit.Device, f.Unit.Port, f.Unit.Priority})
			newUnits[string(b)] = true
		}
		if f.State == "unknown" {
			out.Warnings = append(out.Warnings, "unknown:"+f.Unit.Device+":"+f.Unit.Port+":"+strconv.Itoa(f.Unit.Priority)+":"+f.RuleID+":"+f.Reason)
		}
	}
	keys := []string{}
	for k := range new {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		n := new[k]
		o, ok := old[k]
		if active(n) && (!ok || !active(o)) {
			out.NewFindings = append(out.NewFindings, n)
		}
		if ok && o.State != n.State {
			out.SeverityChanges = append(out.SeverityChanges, RuleSeverityChange{n.EvaluatedAt, n.Unit, n.RuleID, o.State, n.State})
		}
	}
	keys = nil
	for k := range old {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		o := old[k]
		n, ok := new[k]
		if active(o) && (!ok || !active(n)) {
			out.ResolvedFindings = append(out.ResolvedFindings, o)
		}
	}
	expected := []MetricUnit{}
	seenUnits := map[string]bool{}
	for _, f := range append(append([]LosslessFinding{}, before...), after...) {
		raw, _ := json.Marshal([]any{f.Unit.Device, f.Unit.Port, f.Unit.Priority})
		key := string(raw)
		if !seenUnits[key] {
			expected = append(expected, f.Unit)
			seenUnits[key] = true
		}
	}
	b, a := RollupLossless(before, expected), RollupLossless(after, expected)
	out.DeviceMinBefore = b.Fabric.Score
	out.DeviceMinAfter = a.Fabric.Score
	out.FabricAfter = a.Fabric
	out.AffectedBefore = len(oldUnits)
	out.AffectedAfter = len(newUnits)
	sort.Strings(out.Warnings)
	uniqueWarnings := []string{}
	for _, warning := range out.Warnings {
		if len(uniqueWarnings) == 0 || uniqueWarnings[len(uniqueWarnings)-1] != warning {
			uniqueWarnings = append(uniqueWarnings, warning)
		}
	}
	out.Warnings = uniqueWarnings
	return out
}
