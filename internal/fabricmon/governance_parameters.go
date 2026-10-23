package fabricmon

import (
	_ "embed"
	"encoding/json"
	"errors"
	"reflect"
)

//go:embed governance-parameters.json
var governanceParameterData []byte

type LosslessConditionParameter struct {
	RuleID  string      `json:"rule_id"`
	Key     string      `json:"key"`
	Unit    string      `json:"unit"`
	Default json.Number `json:"default"`
	Minimum json.Number `json:"safe_min"`
	Maximum json.Number `json:"safe_max"`
	Root    string      `json:"root"`
	Paths   [][]int     `json:"paths"`
}

func LosslessConditionParameters() []LosslessConditionParameter {
	var out []LosslessConditionParameter
	_ = json.Unmarshal(governanceParameterData, &out)
	return out
}
func conditionParameterNode(rule *LosslessRule, definition LosslessConditionParameter, path []int) (*MetricExpression, error) {
	node := &rule.Expression
	if definition.Root == "escalation" {
		node = rule.Escalation
	}
	if node == nil {
		return nil, errors.New("registered condition expression is missing")
	}
	for _, i := range path {
		if i < 0 || i >= len(node.Args) {
			return nil, errors.New("registered condition expression shape changed")
		}
		node = &node.Args[i]
	}
	if node.Op != "literal" {
		return nil, errors.New("condition parameter must remain a literal")
	}
	return node, nil
}
func LosslessConditionValues(rule LosslessRule) (map[string]json.Number, error) {
	out := map[string]json.Number{}
	for _, definition := range LosslessConditionParameters() {
		if definition.RuleID != rule.ID {
			continue
		}
		for _, path := range definition.Paths {
			node, e := conditionParameterNode(&rule, definition, path)
			if e != nil {
				return nil, e
			}
			if old, ok := out[definition.Key]; ok && old != node.Value {
				return nil, errors.New("sustained windows must use the same registered threshold")
			}
			out[definition.Key] = node.Value
		}
	}
	return out, nil
}
func ApplyLosslessConditionParameters(rule LosslessRule, values map[string]json.Number) (LosslessRule, error) {
	raw, e := json.Marshal(rule)
	if e != nil {
		return rule, e
	}
	var out LosslessRule
	if e = json.Unmarshal(raw, &out); e != nil {
		return rule, e
	}
	known := map[string]bool{}
	for _, definition := range LosslessConditionParameters() {
		if definition.RuleID != rule.ID {
			continue
		}
		known[definition.Key] = true
		value, ok := values[definition.Key]
		if !ok {
			continue
		}
		if _, valid := decimal(value); !valid {
			return rule, errors.New("condition threshold must be a finite decimal")
		}
		for _, path := range definition.Paths {
			node, e := conditionParameterNode(&out, definition, path)
			if e != nil {
				return rule, e
			}
			node.Value = value
		}
	}
	for key := range values {
		if !known[key] {
			return rule, errors.New("condition parameter is not registered for this rule")
		}
	}
	return out, nil
}
func validateConditionParameters(rule, base LosslessRule, override string, confirm bool) error {
	values, e := LosslessConditionValues(rule)
	if e != nil {
		return e
	}
	expected, e := ApplyLosslessConditionParameters(base, values)
	if e != nil {
		return e
	}
	if !reflect.DeepEqual(rule.Expression, expected.Expression) || !reflect.DeepEqual(rule.Escalation, expected.Escalation) || !reflect.DeepEqual(rule.WeakEvidence, expected.WeakEvidence) {
		return errors.New("derived metric AST shape, metric keys, fact paths and unknown branches are immutable")
	}
	for _, definition := range LosslessConditionParameters() {
		if definition.RuleID != rule.ID {
			continue
		}
		v, ok := decimal(values[definition.Key])
		if !ok {
			return errors.New("invalid condition threshold")
		}
		min, _ := decimal(definition.Minimum)
		max, _ := decimal(definition.Maximum)
		outside := v.Cmp(min) < 0 || v.Cmp(max) > 0
		if definition.Unit == "frames" || definition.Unit == "bytes" || definition.Unit == "ms" {
			if !v.IsInt() {
				return errors.New("counter and byte/millisecond thresholds must be integral")
			}
		}
		if outside && (!confirm || len(override) < 8) {
			return errors.New("condition threshold outside safe range requires confirmation and override_reason")
		}
	}
	return nil
}
