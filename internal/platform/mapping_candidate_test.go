package platform

import (
	"encoding/csv"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

// Synthetic predicate inputs only; no simulated hardware result is elevated to
// a real G2 confirmation or persisted as a physical acceptance in these tests.
func mappingFixture(t *testing.T) (*MappingCandidate, []MappingPortPlan, []PrecollectComponent, *TopologyRelationTable, map[string]MappingBinding) {
	t.Helper()
	record, frozen, report := relationFixture(t)
	relations := buildTopologyRelations(record, frozen, report)
	row := relations.Rows[0]
	plans := []MappingPortPlan{{PhysicalPortID: row.PhysicalPortID, ComponentID: row.NICComponentID, ComponentSerial: "NIC-original", PortIndex: "1", Name: "nic-r1-p1", Role: "compute", Rail: "r1"}}
	components := []PrecollectComponent{{ID: row.NICComponentID, Category: "nic"}}
	seen := map[string]bool{}
	for _, r := range relations.Rows {
		if !seen[r.GPUComponentID] {
			components = append(components, PrecollectComponent{ID: r.GPUComponentID, Category: "gpu"})
			seen[r.GPUComponentID] = true
		}
	}
	proofs := map[string]MappingBinding{row.PhysicalPortID: {Observed: wiring.ExpectedPort{PhysicalPortID: row.PhysicalPortID, ComponentID: row.NICComponentID, PortIndex: "1", Interface: row.Interface, PermanentMAC: row.PermanentMAC, BDF: row.NICNode.BDF, Rail: "r1"}, SwitchPortID: strings.Repeat("b", 32), ConfirmationID: strings.Repeat("c", 32)}}
	return &MappingCandidate{RuleVersion: mappingRuleVersion, Ready: true, Verdict: "PASS", Bindings: []MappingBinding{}, Conflicts: []MappingConflict{}}, plans, components, relations, proofs
}

func TestMappingRequiresCompleteIndependentGPUAndPortCoverage(t *testing.T) {
	out, plans, components, relations, proofs := mappingFixture(t)
	buildMappingBindings(out, plans, components, relations, proofs)
	if !out.Ready || out.Coverage.RequiredGPUs != 2 || out.Coverage.MappedGPUs != 2 || out.Coverage.RequiredPorts != 1 || len(out.Bindings[0].GPUs) != 2 {
		t.Fatalf("valid independent mapping failed: %+v", out)
	}
	if out.Bindings[0].LogicalName != plans[0].Name || out.Bindings[0].Plan.ComponentSerial != "NIC-original" {
		t.Fatal("BOM naming or physical serial history was lost")
	}
	for _, kind := range []string{"no_expected_relations", "different_BDF", "missing_GPU", "missing_management_port", "unconfirmed_port", "unsupported_role"} {
		t.Run(kind, func(t *testing.T) {
			out, plans, components, relations, proofs := mappingFixture(t)
			switch kind {
			case "no_expected_relations":
				relations.Rows = nil
			case "different_BDF":
				p := proofs[plans[0].PhysicalPortID]
				p.Observed.BDF = "0000:ff:00.0"
				proofs[plans[0].PhysicalPortID] = p
			case "missing_GPU":
				components = append(components, PrecollectComponent{ID: strings.Repeat("f", 32), Category: "gpu"})
			case "missing_management_port":
				plans = append(plans, MappingPortPlan{PhysicalPortID: strings.Repeat("e", 32), ComponentID: strings.Repeat("d", 32), Role: "management", PortIndex: "2"})
			case "unconfirmed_port":
				delete(proofs, plans[0].PhysicalPortID)
			case "unsupported_role":
				plans[0].Role = "unknown"
			}
			buildMappingBindings(out, plans, components, relations, proofs)
			if out.Ready || out.Verdict != "BLOCKED" || len(out.Conflicts) == 0 {
				t.Fatal("incomplete/conflicting scope passed G3")
			}
		})
	}
}

func TestMappingUnusedPortDoesNotWaiveAnyEnabledPort(t *testing.T) {
	out, plans, components, relations, proofs := mappingFixture(t)
	plans = append(plans, MappingPortPlan{PhysicalPortID: strings.Repeat("d", 32), ComponentID: strings.Repeat("e", 32), Role: "unused"})
	buildMappingBindings(out, plans, components, relations, proofs)
	if !out.Ready || out.Coverage.RequiredPorts != 1 || out.Coverage.RequiredNICs != 1 {
		t.Fatal("explicit unused port changed enabled-role denominator")
	}
}

func TestMappingExportPinsRevisionAndPreservesCurrentBlock(t *testing.T) {
	out, plans, components, relations, proofs := mappingFixture(t)
	plans[0].Name = "=untrusted-BOM-name"
	buildMappingBindings(out, plans, components, relations, proofs)
	out.seal()
	revision := &MappingRevision{ID: "original-revision", Version: 7, Candidate: *out, EffectiveVerdict: "BLOCKED", InvalidationReason: "MAPPING_DEPENDENCIES_CHANGED"}
	raw, e := MappingRevisionCSV(revision)
	if e != nil {
		t.Fatal(e)
	}
	rows, e := csv.NewReader(strings.NewReader(raw)).ReadAll()
	if e != nil {
		t.Fatal(e)
	}
	if len(rows) != 2 || rows[1][0] != revision.ID || rows[1][1] != "7" || rows[1][2] != out.ScopeHash || rows[1][3] != "BLOCKED" || rows[1][8] != "'=untrusted-BOM-name" {
		t.Fatal("export changed version, lost validity or allowed a spreadsheet formula")
	}
}
