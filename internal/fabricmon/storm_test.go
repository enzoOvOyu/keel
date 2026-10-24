package fabricmon

import (
	"testing"
	"time"
)

func TestFabricStormCountsPortsNotPriorities(t *testing.T) {
	policy, e := DefaultStormPolicy()
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC()
	hit := func(device, port string, priority int) LosslessFinding {
		return LosslessFinding{RuleID: "LL-02", Unit: MetricUnit{Device: device, Port: port, Priority: priority}, State: "critical", AppliedRules: []string{"LL-02@2"}, EvaluatedAt: &now}
	}
	list := []LosslessFinding{hit("a", "swp1", 3), hit("a", "swp1", 6), hit("b", "swp1", 3)}
	if got := AggregateFabricStorm(list, policy); got.State != "normal" || got.AffectedPorts != 2 {
		t.Fatalf("priority overcount: %+v", got)
	}
	list = append(list, hit("a", "swp2", 6))
	got := AggregateFabricStorm(list, policy)
	if got.State != "escalated" || got.AffectedPorts != 3 || len(got.DeviceIDs) != 2 || len(got.AppliedRules) != 1 {
		t.Fatalf("expected three distinct ports: %+v", got)
	}
	unknown := hit("c", "swp1", 3)
	unknown.State = "unknown"
	got = AggregateFabricStorm(append(list, unknown), policy)
	if got.State != "escalated" || !got.Incomplete {
		t.Fatalf("known storm must retain missing evidence: %+v", got)
	}
	got = AggregateFabricStorm([]LosslessFinding{unknown}, policy)
	if got.State != "unknown" || !got.Incomplete {
		t.Fatalf("missing evidence reported normal: %+v", got)
	}
	old := now.Add(-time.Minute)
	list[0].EvaluatedAt = &old
	got = AggregateFabricStorm(list, policy)
	if got.State != "unknown" || got.AffectedPorts != 0 {
		t.Fatalf("historical hits were pooled: %+v", got)
	}
}

func TestFabricStormRejectsIncompleteEvidence(t *testing.T) {
	policy, err := DefaultStormPolicy()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	known := LosslessFinding{RuleID: "LL-02", Unit: MetricUnit{Device: "leaf", Port: "swp1"}, State: "normal", EvaluatedAt: &now}
	cases := []struct {
		name    string
		finding LosslessFinding
	}{
		{"missing boundary", func() LosslessFinding { f := known; f.EvaluatedAt = nil; return f }()},
		{"missing device", func() LosslessFinding { f := known; f.Unit.Device = ""; return f }()},
		{"missing port", func() LosslessFinding { f := known; f.Unit.Port = ""; return f }()},
		{"unknown window", func() LosslessFinding { f := known; f.State = "unknown"; return f }()},
		{"degraded normal", func() LosslessFinding { f := known; f.Flags = []string{"degraded"}; return f }()},
		{"unrelated rule", func() LosslessFinding { f := known; f.RuleID = "LL-01"; f.State = "critical"; return f }()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AggregateFabricStorm([]LosslessFinding{tc.finding}, policy)
			if got.State != "unknown" || !got.Incomplete || got.AffectedPorts != 0 {
				t.Fatalf("invalid evidence accepted: %+v", got)
			}
		})
	}
	for _, invalid := range []StormPolicy{{RuleID: "LL-01", Version: 1, ThresholdPorts: 3}, {RuleID: "LL-02", Version: 0, ThresholdPorts: 3}, {RuleID: "LL-02", Version: 1, ThresholdPorts: 0}} {
		if got := AggregateFabricStorm([]LosslessFinding{known}, invalid); got.State != "unknown" || !got.Incomplete {
			t.Fatalf("invalid policy accepted: %+v", got)
		}
	}
	if got := AggregateFabricStorm(nil, policy); got.State != "unknown" || !got.Incomplete {
		t.Fatalf("no evidence accepted: %+v", got)
	}
}

func TestFabricStormEvaluationBoundaryAndApproximation(t *testing.T) {
	catalog, err := DefaultLosslessCatalog()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := catalog.Registry()
	if err != nil {
		t.Fatal(err)
	}
	policy, err := DefaultStormPolicy()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	var findings []LosslessFinding
	priority := 6
	for _, port := range []string{"swp1", "swp2", "swp3"} {
		var points []MetricPoint
		for i, value := range []string{"0", "1000", "2000", "3000"} {
			points = append(points, MetricPoint{Key: "pfc_tx_pause_frames", Unit: "frames", Port: port, Priority: &priority, Source: "ethtool", At: now.Add(time.Duration(i-3) * time.Minute), Value: metricNumber(value)})
		}
		evaluation, err := NewMetricEvaluation(registry, points, nil, now)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range EvaluateLosslessUnit(evaluation, MetricUnit{Device: "leaf", Port: port, Priority: priority}, catalog.Rules) {
			if f.RuleID == policy.RuleID {
				findings = append(findings, f)
			}
		}
		evaluation.Now = now.Add(time.Hour)
	}
	got := AggregateFabricStorm(findings, policy)
	if got.State != "escalated" || got.AffectedPorts != 3 || !got.Approximation || !got.Incomplete || len(got.DeviceIDs) != 1 {
		t.Fatalf("three-window approximation not preserved: %+v; findings: %+v", got, findings)
	}
	for _, f := range findings {
		if f.EvaluatedAt == nil || !f.EvaluatedAt.Equal(now) {
			t.Fatalf("finding boundary changed: %+v", f)
		}
	}
}
