package platform

import (
	"slices"
	"testing"
	"time"

	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/portdiag"
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

func TestPortMonitorScopePinsRepairTransportMappingAndThresholds(t *testing.T) {
	scope := IssueVerificationScope{Version: "issue-verification-scope/v1", IssueID: "issue", ProjectID: "project", IssueVersion: 7, RepairVersion: 6, PortMonitor: &PortMonitorVerificationScope{AssetID: "asset", AssetVersion: 4, MappingRevisionID: "mapping", HostTrustHash: "trust", Diagnostic: portdiag.Scope{Source: "switch", Method: "collector", Origin: "real", PhysicalPortID: "port", Interface: "eth0", SwitchPort: "swp1", RepairedAt: time.Now().UTC()}}}
	base := finishIssueVerificationScope(&scope).ScopeHash
	for _, change := range []func(*PortMonitorVerificationScope){
		func(v *PortMonitorVerificationScope) { v.AssetVersion++ }, func(v *PortMonitorVerificationScope) { v.MappingRevisionID = "replacement" }, func(v *PortMonitorVerificationScope) { v.HostTrustHash = "revoked" }, func(v *PortMonitorVerificationScope) { v.Diagnostic.Source = "host" }, func(v *PortMonitorVerificationScope) { v.Diagnostic.Method = "agent" }, func(v *PortMonitorVerificationScope) { v.Diagnostic.PhysicalPortID = "other" }, func(v *PortMonitorVerificationScope) { v.Diagnostic.SwitchPort = "swp2" }, func(v *PortMonitorVerificationScope) { v.Diagnostic.ExpectedConfigurationHash = "new-policy" },
	} {
		raw, _ := json.Marshal(scope)
		var changed IssueVerificationScope
		_ = json.Unmarshal(raw, &changed)
		change(changed.PortMonitor)
		if finishIssueVerificationScope(&changed).ScopeHash == base {
			t.Fatal("changed physical repair reused the accepted scope")
		}
	}
}
