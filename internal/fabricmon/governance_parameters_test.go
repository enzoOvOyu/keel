package fabricmon

import (
	"encoding/json"
	"testing"
	"time"
)

func TestGovernedTypedConditionChangesActualStormThreshold(t *testing.T) {
	catalog, e := DefaultLosslessCatalog()
	if e != nil {
		t.Fatal(e)
	}
	base := catalog.Rules[1]
	candidate, e := ApplyLosslessConditionParameters(base, map[string]json.Number{"xoff_duration_ms": "120000", "pause_frames_per_minute": "2000"})
	if e != nil {
		t.Fatal(e)
	}
	if e = ValidateGovernedLossless(candidate, base, "boolean", "", false); e != nil {
		t.Fatal(e)
	}
	registry, _ := catalog.Registry()
	now := time.Now().UTC()
	priority := 6
	points := []MetricPoint{}
	for i := 0; i <= 5; i++ {
		value := json.Number(stringInt(i * 20000))
		points = append(points, MetricPoint{Key: "pfc_xoff_duration_ms", Unit: "ms", Port: "swp1", Priority: &priority, Source: "events", Provider: "test", At: now.Add(time.Duration(i-5) * time.Minute), Value: &value})
	}
	evaluation, e := NewMetricEvaluation(registry, points, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	unit := MetricUnit{Device: "d", Port: "swp1", Priority: 6}
	before, after := EvaluateLosslessUnit(evaluation, unit, []LosslessRule{base}), EvaluateLosslessUnit(evaluation, unit, []LosslessRule{candidate})
	if before[0].State != "critical" || after[0].State != "normal" {
		t.Fatalf("actual 100000ms must cross 90000 and stay below 120000: before=%+v after=%+v", before, after)
	}
	candidate.Threshold = "0"
	if ValidateGovernedLossless(candidate, base, "boolean", "approved override", true) == nil {
		t.Fatal("boolean wrapper threshold can bypass actual condition")
	}
	candidate, _ = ApplyLosslessConditionParameters(base, map[string]json.Number{"xoff_duration_ms": "999999999"})
	if ValidateGovernedLossless(candidate, base, "boolean", "", false) == nil {
		t.Fatal("unsafe typed threshold accepted")
	}
	candidate = base
	candidate.Escalation = &MetricExpression{Op: "exec"}
	if candidate.Validate(evaluation) == nil {
		t.Fatal("optional AST not validated")
	}
}
func stringInt(v int) string { b, _ := json.Marshal(v); return string(b) }
