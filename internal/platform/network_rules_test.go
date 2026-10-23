package platform

import (
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"testing"
)

func TestRuleQuorumCannotDowngradeCritical(t *testing.T) {
	candidate := &NetworkRuleVersion{RuleID: "LL-01", Rule: fabricmon.LosslessRule{Severity: "warning", State: "disabled"}}
	if !ruleNeedsDual(candidate, nil) {
		t.Fatal("critical built-in downgraded to single signature")
	}
	candidate.RuleID = "LL-04"
	parent := &NetworkRuleVersion{Rule: fabricmon.LosslessRule{Severity: "critical"}}
	if !ruleNeedsDual(candidate, parent) {
		t.Fatal("critical effective parent downgraded to single signature")
	}
	parent.Rule.Severity = "warning"
	if ruleNeedsDual(candidate, parent) {
		t.Fatal("warning-only change incorrectly needs critical quorum")
	}
}
