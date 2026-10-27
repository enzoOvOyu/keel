package wiring

import (
	"strings"
	"testing"
	"time"
)

// Synthetic predicate inputs only; these are never persisted as real evidence.
func coverageFixture(t *testing.T) (Expectation, string, string, []PortProof, time.Time) {
	t.Helper()
	_, expected := fixtureDocument(t, "dual-ended")
	now := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	parent, policy := strings.Repeat("e", 32), strings.Repeat("f", 64)
	proofs := []PortProof{}
	for i, p := range expected.Ports {
		digit := string(rune('1' + i))
		proofs = append(proofs, PortProof{PhysicalPortID: p.PhysicalPortID, ParentID: parent, Step2ID: strings.Repeat(digit, 32), Step3ID: strings.Repeat("a", 31) + digit, CommandID: strings.Repeat("b", 31) + digit, RawSHA256: strings.Repeat("c", 64), ScopeHash: expected.ScopeHash, PolicyHash: policy, HostOrigin: "real", SwitchOrigin: "device", Expected: p, Step2: "PASS", Step3: "PASS", Findings: []Finding{}, ObservedAt: now.Add(-time.Minute), ValidUntil: now.Add(time.Hour)})
	}
	return expected, parent, policy, proofs, now
}
func TestIncrementalCoverageRetainsUntouchedOriginalExpiry(t *testing.T) {
	expected, parent, policy, proofs, now := coverageFixture(t)
	proofs[0].Step3 = "FAIL"
	before, e := ComposeCoverage(expected, parent, policy, proofs, now)
	if e != nil || before.Step3 != "FAIL" {
		t.Fatalf("initial failure: %v %+v", e, before)
	}
	fixed := proofs[0]
	fixed.CommandID = strings.Repeat("d", 32)
	fixed.Step2ID = strings.Repeat("9", 32)
	fixed.Step3ID = strings.Repeat("8", 32)
	fixed.Step3 = "PASS"
	fixed.ObservedAt = now
	fixed.ValidUntil = now.Add(2 * time.Hour)
	after, e := ComposeCoverage(expected, parent, policy, append(proofs, fixed), now)
	if e != nil || after.Step2 != "PASS" || after.Step3 != "PASS" || len(after.Proofs) != 2 {
		t.Fatalf("partial repair: %v %+v", e, after)
	}
	if after.ValidUntil == nil || !after.ValidUntil.Equal(proofs[1].ValidUntil) || after.ParentID != parent {
		t.Fatal("rerunning one port renewed untouched evidence or replaced Step 1")
	}
	expired, e := ComposeCoverage(expected, parent, policy, append(proofs, fixed), proofs[1].ValidUntil.Add(time.Second))
	if e != nil || expired.Step2 != "BLOCKED" || expired.Step3 != "BLOCKED" {
		t.Fatal("fresh repaired port hid expired untouched port")
	}
}
func TestCoverageMissingNewBlockedAndSimCannotBorrowOldPass(t *testing.T) {
	expected, parent, policy, proofs, now := coverageFixture(t)
	missing, e := ComposeCoverage(expected, parent, policy, proofs[:1], now)
	if e != nil || missing.Step2 != "BLOCKED" || len(missing.MissingPortIDs) != 1 {
		t.Fatal("partial selection shrank the denominator")
	}
	newer := proofs[0]
	newer.CommandID = strings.Repeat("d", 32)
	newer.Step2 = "BLOCKED"
	newer.Step3 = "BLOCKED"
	newer.Step3ID = ""
	newer.ObservedAt = now
	blocked, e := ComposeCoverage(expected, parent, policy, append(proofs, newer), now)
	if e != nil || blocked.Step2 != "BLOCKED" {
		t.Fatal("new BLOCKED observation fell back to old PASS")
	}
	sim := append([]PortProof(nil), proofs...)
	sim[0].HostOrigin = "sim"
	sim[0].SwitchOrigin = "protocol_fixture"
	result, e := ComposeCoverage(expected, parent, policy, sim, now)
	if e != nil || result.Step2 != "BLOCKED" {
		t.Fatal("simulation contributed real coverage")
	}
}
func TestCoverageRejectsConflictingOriginalProofAndOtherStepOne(t *testing.T) {
	expected, parent, policy, proofs, now := coverageFixture(t)
	conflicting := proofs[0]
	conflicting.Step3 = "FAIL"
	if _, e := ComposeCoverage(expected, parent, policy, append(proofs, conflicting), now); e == nil {
		t.Fatal("same original command had conflicting terminal proof")
	}
	other := append([]PortProof(nil), proofs...)
	other[1].ParentID = strings.Repeat("7", 32)
	result, e := ComposeCoverage(expected, parent, policy, other, now)
	if e != nil || len(result.MissingPortIDs) != 1 || result.Step2 != "BLOCKED" {
		t.Fatal("different topology predecessor contributed coverage")
	}
}
func TestPerPortEvaluationIsolatesOnlyKnownLocalParserFailures(t *testing.T) {
	expected, host, switches, now := wireFixture()
	id := expected.Ports[0].PhysicalPortID
	host.Missing = []string{"lldp_neighbors_unavailable:" + strings.Repeat("6", 32)}
	// An unknown/out-of-scope suffix is a global integrity blocker, not discardable.
	result := evaluatePorts(expected, host, switches, now)
	if result[id].Step2 != "BLOCKED" {
		t.Fatal("unknown scoped failure was discarded")
	}
	host.Missing = []string{"switch_readback_not_coordinated"}
	if evaluatePorts(expected, host, switches, now)[id].Step2 != "BLOCKED" {
		t.Fatal("global collector failure was discarded")
	}
}
