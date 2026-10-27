package platform

import (
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

func TestScopedLinkProofCannotOverrideNewerOrDifferentOriginCollection(t *testing.T) {
	now := time.Now().UTC()
	proof := scopedLinkEvidence{Request: &IssueVerificationRequest{ID: "request"}, EvidenceID: "evidence", ExecutionID: "execution", IssueID: "issue", Origin: "protocol_fixture", Finding: fabricmon.Finding{CableUID: "cable", Verdict: "precheck_pass", RuleRef: fabricmon.CableComparisonRuleRef}, CollectedAt: now, ValidUntil: now.Add(time.Minute)}
	collection := &FabricCollectionReport{EvidenceSource: "protocol_fixture", CollectedAt: now.Add(time.Second)}
	if newestScopedLinkEvidence([]scopedLinkEvidence{proof}, collection) != nil {
		t.Fatal("older targeted proof overrode a newer collection")
	}
	collection.CollectedAt = now.Add(-time.Second)
	collection.EvidenceSource = "device"
	if newestScopedLinkEvidence([]scopedLinkEvidence{proof}, collection) != nil {
		t.Fatal("fixture proof replaced device evidence")
	}
	collection.EvidenceSource = "protocol_fixture"
	chosen := newestScopedLinkEvidence([]scopedLinkEvidence{proof}, collection)
	if chosen == nil {
		t.Fatal("current scoped proof was lost")
	}
	item := NetworkCableEvidence{CableID: "cable", NP3: "BLOCKED", Stage: "N-S5"}
	applyScopedLinkEvidence(&item, chosen)
	if item.Gate.State != "PASS" || item.NP3 != "BLOCKED" || item.Stage != "N-S5" || item.EvidenceScope != "targeted_link" || item.ExecutionID != "execution" || item.CollectedAt == nil || !item.CollectedAt.Equal(now) || item.ValidUntil.After(proof.ValidUntil) {
		t.Fatalf("scoped projection broadened its proof: %+v", item)
	}
}
