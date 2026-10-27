package platform

import (
	"strings"
	"testing"
)

func TestMappingCorrectionPreservesPhysicalProofAndOriginalCandidate(t *testing.T) {
	source, plans, components, relations, proofs := mappingFixture(t)
	buildMappingBindings(source, plans, components, relations, proofs)
	source.seal()
	source.BaseScopeHash = source.ScopeHash
	original := string(networkJSON(source))
	change := []MappingNameChange{{source.Bindings[0].Plan.PhysicalPortID, "nic-renamed-p1"}}
	corrected, e := applyMappingNames(source, change, "approved-correction")
	if e != nil {
		t.Fatal(e)
	}
	if !corrected.Ready || corrected.ScopeHash == source.ScopeHash || corrected.BaseScopeHash != source.BaseScopeHash || corrected.CorrectionID != "approved-correction" {
		t.Fatal("correction lost scope provenance")
	}
	corrected.Bindings[0].LogicalName = source.Bindings[0].LogicalName
	if string(networkJSON(corrected.Bindings)) != string(networkJSON(source.Bindings)) || string(networkJSON(source)) != original {
		t.Fatal("logical correction changed independent physical evidence or source")
	}
	for _, changes := range [][]MappingNameChange{nil, {{"unknown-port", "valid-name"}}, {{source.Bindings[0].Plan.PhysicalPortID, "=formula"}}, {{source.Bindings[0].Plan.PhysicalPortID, ""}}, {{source.Bindings[0].Plan.PhysicalPortID, "a"}, {source.Bindings[0].Plan.PhysicalPortID, "b"}}} {
		if _, e = applyMappingNames(source, changes, "correction"); e == nil {
			t.Fatal("invalid or ambiguous label correction accepted")
		}
	}
	source.block("MAPPING_CURRENT_G2_PORT_REQUIRED", plans[0].PhysicalPortID, plans[0].ComponentID)
	corrected, e = applyMappingNames(source, change, "correction")
	if e != nil {
		t.Fatal(e)
	}
	if corrected.Ready || corrected.Verdict != "BLOCKED" || len(corrected.Conflicts) != 1 {
		t.Fatal("approval waived physical evidence failure")
	}
}
func TestMappingCorrectionRepairsOnlyLabelConflicts(t *testing.T) {
	source, plans, components, relations, proofs := mappingFixture(t)
	plans[0].Name = "=bad-name"
	buildMappingBindings(source, plans, components, relations, proofs)
	source.seal()
	source.BaseScopeHash = source.ScopeHash
	corrected, e := applyMappingNames(source, []MappingNameChange{{plans[0].PhysicalPortID, "nic-valid"}}, "approval")
	if e != nil {
		t.Fatal(e)
	}
	if !corrected.Ready || len(corrected.Conflicts) != 0 || corrected.Bindings[0].Plan.Name != "=bad-name" {
		t.Fatal("label correction lost original BOM name or failed to resolve naming conflict")
	}
	corrected.Bindings = append(corrected.Bindings, corrected.Bindings[0])
	corrected.Bindings[1].Plan.PhysicalPortID = strings.Repeat("f", 32)
	if _, e = applyMappingNames(corrected, []MappingNameChange{{plans[0].PhysicalPortID, "nic-valid"}}, "approval"); e == nil {
		t.Fatal("duplicate final names accepted")
	}
}
func TestMappingDerivedBaseHashDoesNotChangeUncorrectedScope(t *testing.T) {
	source, plans, components, relations, proofs := mappingFixture(t)
	buildMappingBindings(source, plans, components, relations, proofs)
	source.seal()
	hash := source.ScopeHash
	source.BaseScopeHash = hash
	source.seal()
	if source.ScopeHash != hash {
		t.Fatal("derived base hash changed immutable original scope")
	}
}
