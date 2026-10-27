package platform

import (
	"slices"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

func TestVerificationScopeIdentityExcludesReadTimeButPinsAdmissionFacts(t *testing.T) {
	original := IssueVerificationScope{Version: "issue-verification-scope/v1", IssueID: "issue", ProjectID: "project", IssueVersion: 4, RepairVersion: 4, PackageID: "bom", EvidenceOrigin: "protocol_fixture", Category: "T2", RuleRef: "PRECHECK-LLDP@1", Targets: []fabricboot.CollectorTarget{{SwitchID: "switch", Serial: "serial", Ports: []string{"swp1"}}}, PolicyHash: "policy", IntentHash: "intent", RequiredChecks: issueVerificationChecks("T2"), Blockers: []string{}, ValidUntil: time.Now().UTC()}
	base := finishIssueVerificationScope(&original).ScopeHash
	again := original
	again.ValidUntil = again.ValidUntil.Add(time.Minute)
	if finishIssueVerificationScope(&again).ScopeHash != base {
		t.Fatal("read time changed scope identity")
	}
	for _, tc := range []struct {
		name   string
		change func(*IssueVerificationScope)
	}{
		{"management identity", func(v *IssueVerificationScope) { v.ManagementHash = "changed-management" }},
		{"issue revision", func(v *IssueVerificationScope) { v.IssueVersion++ }},
		{"repair revision", func(v *IssueVerificationScope) { v.RepairVersion++ }},
		{"network BOM", func(v *IssueVerificationScope) { v.PackageID = "changed" }},
		{"origin", func(v *IssueVerificationScope) { v.EvidenceOrigin = "device" }},
		{"policy", func(v *IssueVerificationScope) { v.PolicyHash = "changed" }},
		{"intent", func(v *IssueVerificationScope) { v.IntentHash = "changed" }},
		{"rule", func(v *IssueVerificationScope) { v.RuleRef = "PRECHECK-LLDP@2" }},
		{"target", func(v *IssueVerificationScope) {
			v.Targets = []fabricboot.CollectorTarget{{SwitchID: "replacement", Serial: "other", Ports: []string{"swp2"}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := original
			tc.change(&v)
			if finishIssueVerificationScope(&v).ScopeHash == base {
				t.Fatal("changed admission facts reused an old scope digest")
			}
		})
	}
}
func TestVerificationChecksRequireConnectivityAndTopologyByCategory(t *testing.T) {
	for _, kind := range []string{"T3", "T4", "T5", "T6"} {
		if !slices.Contains(issueVerificationChecks(kind), "connectivity_single_path") {
			t.Fatalf("%s could close using switch-only observations", kind)
		}
	}
	for _, check := range []string{"pcie_topology", "host_precollect_incremental"} {
		if !slices.Contains(issueVerificationChecks("T7"), check) {
			t.Fatal("T7 omitted topology or precollection")
		}
	}
}
