package fabricdelivery

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func deliveryTestFacts(now time.Time) Facts {
	ref := Reference{ID: "bom:x", Kind: "bom_revision", ObjectID: "switch", Version: "1", SHA256: strings.Repeat("a", 64), EvidenceSource: "design_bom"}
	f := Facts{ProjectID: "project", PackageID: "package", References: []Reference{ref}, Objects: []Object{{ID: "switch", Category: "switch", Status: "installed", BOMRef: ref.ID, AssetRef: ref.ID, SampleRefs: []string{}, State: "observed", Source: "switch"}}, Stages: []Stage{}, RuleVersions: []string{"LL-01@1"}, HardBlocks: []string{}, ValidUntil: now.Add(30 * time.Second)}
	for i := 0; i < 8; i++ {
		f.Stages = append(f.Stages, Stage{ID: "N-S" + string(rune('0'+i)), State: "PASS", Reasons: []string{}, EvidenceRefs: []string{ref.ID}})
	}
	return f
}
func TestDeliveryAdmissionAndDraftIsolation(t *testing.T) {
	now := time.Now().UTC()
	f := deliveryTestFacts(now)
	empty := Review{Waivers: []Waiver{}, Code: "not_configured"}
	p := Evaluate(f, empty, now)
	if p.DeliveryAllowed || p.Stages[8].State != "BLOCKED" {
		t.Fatal("missing prior CMDB acceptance passed")
	}
	draft, b, e := Build("draft", 1, "draft", f, p, now)
	if e != nil || draft.Delivery || !strings.Contains(string(b), "NOT A NETWORK DELIVERY") {
		t.Fatal("draft misrepresented as delivery", e)
	}
	if _, _, e = Build("delivery", 2, "delivery", f, p, now); e == nil {
		t.Fatal("formal export bypassed missing acceptance")
	}
	accepted := Review{Acceptance: &Acceptance{ID: "receipt", ScopeHash: ScopeHash(f), ApprovalRef: "approval:1", CMDBSnapshotRef: "cmdb:snapshot:1", AcceptedAt: now, EvidenceSource: "cmdb"}, Waivers: []Waiver{}}
	p = Evaluate(f, accepted, now)
	if !p.DeliveryAllowed || p.ConsistencyCheckRate != 1 {
		t.Fatalf("valid exact-scope evidence blocked: %+v", p)
	}
	if _, _, e = Build("delivery", 2, "delivery", f, p, now); e != nil {
		t.Fatal(e)
	}
	accepted.Acceptance.EvidenceSource = "protocol_fixture"
	if Evaluate(f, accepted, now).DeliveryAllowed {
		t.Fatal("fixture acceptance became production PASS")
	}
}
func TestDeliveryWaiverPreservesOriginalAndCannotWaiveIntegrity(t *testing.T) {
	now := time.Now().UTC()
	f := deliveryTestFacts(now)
	f.Stages[7].State = "FAIL"
	f.Stages[7].Reasons = []string{"optic_alarm"}
	scope := ScopeHash(f)
	review := Review{Acceptance: &Acceptance{ID: "receipt", ScopeHash: scope, ApprovalRef: "accepted", CMDBSnapshotRef: "cmdb:prior", AcceptedAt: now, EvidenceSource: "cmdb"}, Waivers: []Waiver{{ID: "waiver", Stage: "N-S7", ScopeHash: scope, Approved: true, ApprovalRef: "approval", Reason: "approved maintenance exception", Compensation: "isolate the affected link", ValidUntil: now.Add(time.Hour), EvidenceSource: "cmdb"}}}
	p := Evaluate(f, review, now)
	if !p.DeliveryAllowed || p.State != "WAIVED" || p.Stages[7].OriginalState != "FAIL" || p.Stages[7].Reasons[0] != "optic_alarm" {
		t.Fatal("waiver erased or bypassed original conclusion")
	}
	review.Waivers[0].ValidUntil = now.Add(-time.Second)
	if Evaluate(f, review, now).DeliveryAllowed {
		t.Fatal("expired waiver passed")
	}
	review.Waivers[0].ValidUntil = now.Add(time.Hour)
	f.HardBlocks = []string{"unknown_write_quarantine"}
	review.Acceptance.ScopeHash = ScopeHash(f)
	review.Waivers[0].ScopeHash = ScopeHash(f)
	if Evaluate(f, review, now).DeliveryAllowed {
		t.Fatal("waiver bypassed immutable integrity gate")
	}
}
func TestDeliveryReferencesAndScopeBinding(t *testing.T) {
	now := time.Now().UTC()
	f := deliveryTestFacts(now)
	before := ScopeHash(f)
	f.ValidUntil = now.Add(time.Second)
	if ScopeHash(f) != before {
		t.Fatal("wall-clock TTL changed evidence identity")
	}
	f.Objects[0].ParentID = "foreign-parent"
	p := Evaluate(f, Review{}, now)
	if p.DeliveryAllowed || len(p.IntegrityErrors) == 0 {
		t.Fatal("dangling BOM parent accepted")
	}
	f = deliveryTestFacts(now)
	f.References = append(f.References, f.References[0])
	if Evaluate(f, Review{}, now).ConsistencyCheckRate == 1 {
		t.Fatal("duplicate reference falsely claimed 100 percent")
	}
	var schema map[string]any
	if json.Unmarshal(JSONSchema, &schema) != nil || schema["$id"] != "urn:keel:network-delivery:v1" {
		t.Fatal("embedded JSON schema unavailable")
	}
}

func TestDeliveryUnwaivableEvidenceAndStageIntegrity(t *testing.T) {
	now := time.Now().UTC()
	for _, scenario := range []string{"fixture", "expired", "future", "duplicate_stage", "empty_stage"} {
		t.Run(scenario, func(t *testing.T) {
			f := deliveryTestFacts(now)
			switch scenario {
			case "fixture":
				f.References[0].EvidenceSource = "protocol_fixture"
			case "expired":
				until := now.Add(-time.Second)
				f.References[0].ValidUntil = &until
			case "future":
				at := now.Add(time.Hour)
				f.References[0].ObservedAt = &at
			case "duplicate_stage":
				f.Stages = append(f.Stages, f.Stages[0])
			case "empty_stage":
				f.Stages[0].EvidenceRefs = []string{}
			}
			review := Review{Acceptance: &Acceptance{ID: "receipt", ScopeHash: ScopeHash(f), ApprovalRef: "approved", CMDBSnapshotRef: "snapshot", AcceptedAt: now, EvidenceSource: "cmdb"}, Waivers: []Waiver{}}
			p := Evaluate(f, review, now)
			if p.DeliveryAllowed {
				t.Fatal("invalid production evidence passed", scenario)
			}
			if _, _, err := Build("archive", 1, "delivery", f, p, now); err == nil {
				t.Fatal("invalid formal archive created")
			}
		})
	}
}

func TestDeliveryBuildReevaluatesExpiryAndOriginalConclusions(t *testing.T) {
	now := time.Now().UTC()
	f := deliveryTestFacts(now)
	until := now.Add(time.Second)
	f.References[0].ValidUntil = &until
	review := Review{Acceptance: &Acceptance{ID: "receipt", ScopeHash: ScopeHash(f), ApprovalRef: "approved", CMDBSnapshotRef: "snapshot", AcceptedAt: now, EvidenceSource: "cmdb"}, Waivers: []Waiver{}}
	p := Evaluate(f, review, now)
	if !p.DeliveryAllowed || !p.ValidUntil.Equal(until) {
		t.Fatal("preflight ignored the earliest evidence expiry")
	}
	if _, _, err := Build("archive", 1, "delivery", f, p, now.Add(2*time.Second)); err == nil {
		t.Fatal("expired evidence created a formal archive")
	}
	f.Objects[0].State = "changed"
	if _, _, err := Build("archive", 1, "draft", f, p, now); err == nil {
		t.Fatal("changed evidence reused a reviewed scope")
	}
}
