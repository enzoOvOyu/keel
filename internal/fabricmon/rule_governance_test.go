package fabricmon

import (
	"encoding/json"
	"testing"
)

func TestGovernedRulePreservesUnknownAndUnits(t *testing.T) {
	c, e := DefaultLosslessCatalog()
	if e != nil {
		t.Fatal(e)
	}
	base := c.Rules[3]
	candidate := base
	candidate.Threshold = json.Number("0.1")
	if e = ValidateGovernedLossless(candidate, base, "ratio", "", false); e != nil {
		t.Fatal(e)
	}
	if ValidateGovernedLossless(candidate, base, "percent", "", false) == nil {
		t.Fatal("percent accepted as ratio")
	}
	candidate.UnknownConstraints = []string{"disabled"}
	if ValidateGovernedLossless(candidate, base, "ratio", "", false) == nil {
		t.Fatal("unknown guard removed")
	}
	candidate = base
	candidate.Threshold = "2"
	if ValidateGovernedLossless(candidate, base, "ratio", "", false) == nil {
		t.Fatal("unsafe range not confirmed")
	}
	if e = ValidateGovernedLossless(candidate, base, "ratio", "Customer approved range override", true); e != nil {
		t.Fatal(e)
	}
	candidate = base
	candidate.Expression = MetricExpression{Op: "literal", Value: "0"}
	if ValidateGovernedLossless(candidate, base, "ratio", "", false) == nil {
		t.Fatal("fabricated no-evidence normal accepted")
	}
}
func TestRulePreviewDiffKeepsUnknownVisible(t *testing.T) {
	u := MetricUnit{Device: "d", Port: "p", Priority: 6}
	before := []LosslessFinding{{Unit: u, RuleID: "LL-01", State: "critical", Weight: 15}}
	after := []LosslessFinding{{Unit: u, RuleID: "LL-01", State: "unknown", Reason: "rule_disabled"}}
	diff := DiffLosslessFindings(before, after)
	if len(diff.ResolvedFindings) != 1 || len(diff.SeverityChanges) != 1 || len(diff.Warnings) != 1 || diff.DeviceMinBefore != 85 || diff.DeviceMinAfter != 100 || diff.FabricAfter.State != "warning" || !diff.FabricAfter.Unknown {
		t.Fatalf("diff %+v", diff)
	}
}
