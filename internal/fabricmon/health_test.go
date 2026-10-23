package fabricmon

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func metricNumber(value string) *json.Number { n := json.Number(value); return &n }
func TestLosslessWindowsAndCounterPrecision(t *testing.T) {
	catalog, e := DefaultLosslessCatalog()
	if e != nil {
		t.Fatal(e)
	}
	registry, _ := catalog.Registry()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	priority := 3
	unit := MetricUnit{Device: "leaf-1", Port: "swp1", Priority: priority}
	point := func(key, value string, at time.Time) MetricPoint {
		return MetricPoint{Key: key, Unit: "frames", Port: "swp1", Priority: &priority, Source: "ethtool", At: at, Value: metricNumber(value)}
	}
	points := []MetricPoint{point("pfc_tx_pause_frames", "9007199254740992", now.Add(-time.Minute)), point("pfc_tx_pause_frames", "9007199254740993", now), point("pfc_rx_pause_frames", "0", now.Add(-time.Minute)), point("pfc_rx_pause_frames", "0", now)}
	c, e := NewMetricEvaluation(registry, points, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	f := EvaluateLosslessUnit(c, unit, catalog.Rules)
	if f[0].RuleID != "LL-01" || f[0].State != "critical" {
		t.Fatalf("single-frame increment lost above 2^53: %+v", f[0])
	}
	for _, finding := range f {
		if finding.RuleID == "LL-03" && finding.State != "unknown" {
			t.Fatal("partial hour accepted")
		}
		if finding.RuleID == "LL-06" && finding.State != "unknown" {
			t.Fatal("missing WJH/drop counters reported normal")
		}
	}
	points[1].Value = metricNumber("1")
	c, e = NewMetricEvaluation(registry, points, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	f = EvaluateLosslessUnit(c, unit, catalog.Rules)
	if f[0].State != "unknown" || f[0].Reason != "counter_reset" {
		t.Fatal("counter reset accepted as normal")
	}
	points[1].Unit = "ratio"
	if _, e = NewMetricEvaluation(registry, points, nil, now); e == nil {
		t.Fatal("unit mismatch accepted")
	}
}
func TestLosslessScopeDisabledAndHealthRollup(t *testing.T) {
	catalog, e := DefaultLosslessCatalog()
	if e != nil {
		t.Fatal(e)
	}
	registry, _ := catalog.Registry()
	now := time.Now().UTC()
	c, _ := NewMetricEvaluation(registry, nil, nil, now)
	u := MetricUnit{Device: "leaf-1", Port: "swp1", Priority: 3, Pod: "pod-1"}
	rule := catalog.Rules[0]
	rule.Expression = MetricExpression{Op: "literal", Value: json.Number("9")}
	rule.Threshold = json.Number("10")
	rule.Severity = "warning"
	rule.Weight = 15
	strict := rule
	strict.Version = 2
	strict.Threshold = json.Number("5")
	findings := EvaluateLosslessUnit(c, u, []LosslessRule{rule, strict})
	if len(findings) != 1 || findings[0].State != "warning" || len(findings[0].AppliedRules) != 2 {
		t.Fatal("same-scope strict rule silently ignored")
	}
	override := rule
	override.Version = 3
	override.State = "disabled"
	override.Scope.Device = u.Device
	findings = EvaluateLosslessUnit(c, u, []LosslessRule{rule, strict, override})
	if findings[0].State != "unknown" || strings.Join(findings[0].AppliedRules, ",") != "LL-01@3" {
		t.Fatal("disabled highest scope fell back to normal")
	}
	one := LosslessFinding{Unit: u, RuleID: "LL-01", State: "critical", Weight: 15}
	two := one
	two.Unit.Priority = 4
	report := RollupLossless([]LosslessFinding{one, two}, []MetricUnit{u, two.Unit})
	if report.Fabric.Score != 78 {
		t.Fatalf("expected floor(100-15*sqrt(2))=78: %+v", report.Fabric)
	}
	healthy := LosslessFinding{Unit: MetricUnit{Device: "leaf-2", Port: "swp2", Priority: 3, Pod: "pod-1"}, RuleID: "LL-01", State: "normal"}
	report = RollupLossless([]LosslessFinding{one, two, healthy}, []MetricUnit{u, two.Unit, healthy.Unit})
	if report.Fabric.Score != 78 {
		t.Fatal("rollup averaged a degraded device away")
	}
	report = RollupLossless(findings, []MetricUnit{u})
	if report.Fabric.State == "healthy" {
		t.Fatal("unknown received healthy grade")
	}
}
func TestMetricExpressionCannotExecuteCode(t *testing.T) {
	catalog, _ := DefaultLosslessCatalog()
	registry, _ := catalog.Registry()
	c := &MetricEvaluation{Registry: registry}
	if c.ValidateExpression(MetricExpression{Op: "shell", Key: "rm"}) == nil {
		t.Fatal("executable expression accepted")
	}
	if c.ValidateExpression(MetricExpression{Op: "latest", Key: "unregistered"}) == nil {
		t.Fatal("unknown metric accepted")
	}
	if c.ValidateExpression(MetricExpression{Op: "mismatch", PathA: "../../private", PathB: "applied.x"}) == nil {
		t.Fatal("unbounded fact path accepted")
	}
}
