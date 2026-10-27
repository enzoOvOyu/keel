package platform

import (
	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/wiring"
	"testing"
	"time"
)

func TestWiringAdmissionIdentityAllowsFreshSamplesButRejectsPhysicalChanges(t *testing.T) {
	original := &WiringFacts{ProjectID: "project", NetworkPackageID: "bom", CollectorHash: "collector", CollectionID: "old", Targets: []WiringHost{{AssetID: "asset", MachineID: "machine", G1ID: "g1", TopologyID: "topology", Ports: []WiringPort{{PhysicalPortID: "port", PermanentMAC: "02:00:00:00:00:01", SwitchID: "switch"}}}}, Switches: []WiringSwitch{{ID: "switch", Serial: "serial", GoldenHash: "golden", ManagementHash: "management", Ports: []string{"swp1"}, SampleHash: "old"}}}
	raw, _ := json.Marshal(original)
	clone := func() *WiringFacts {
		var f WiringFacts
		if e := json.Unmarshal(raw, &f); e != nil {
			t.Fatal(e)
		}
		return &f
	}
	fresh := clone()
	fresh.CollectionID = "fresh"
	fresh.Switches[0].SampleHash = "fresh"
	fresh.EvidenceValidUntil = time.Now().Add(time.Hour)
	fresh.FactsHash = "new"
	if wiringIdentityHash(original) != wiringIdentityHash(fresh) {
		t.Fatal("fresh collection changed frozen physical identity")
	}
	for name, change := range map[string]func(*WiringFacts){
		"topology":    func(f *WiringFacts) { f.Targets[0].TopologyID = "new" },
		"port":        func(f *WiringFacts) { f.Targets[0].Ports[0].PermanentMAC = "02:00:00:00:00:02" },
		"golden":      func(f *WiringFacts) { f.Switches[0].GoldenHash = "new" },
		"credentials": func(f *WiringFacts) { f.Switches[0].ManagementHash = "new" },
		"collector":   func(f *WiringFacts) { f.CollectorHash = "new" },
		"bom":         func(f *WiringFacts) { f.NetworkPackageID = "new" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := clone()
			change(changed)
			if wiringIdentityHash(original) == wiringIdentityHash(changed) {
				t.Fatal("changed identity accepted")
			}
		})
	}
}
func TestWiringResourceSetLocksEveryDistinctHostAndSwitch(t *testing.T) {
	scope := &WiringRunScope{MachineID: "host", Expectation: wiring.Expectation{Ports: []wiring.ExpectedPort{{SwitchID: "b"}, {SwitchID: "a"}, {SwitchID: "a"}}}}
	got := wiringRunResources(scope)
	want := []string{"device:machine:host", "device:switch:a", "device:switch:b", "ssh:machine:host", "ssh:switch:a", "ssh:switch:b"}
	if string(networkJSON(got)) != string(networkJSON(want)) {
		t.Fatalf("resources %v", got)
	}
}
func TestWiringNewFailedStepTwoInvalidatesOldStepThree(t *testing.T) {
	b := &WiringBatch{Targets: []WiringBatchTarget{{Reports: []WiringStepReport{
		{ID: "one", Step: 1, Version: 1, EffectiveVerdict: "PASS"},
		{ID: "old-two", Step: 2, Version: 1, ParentID: "one", EffectiveVerdict: "PASS"},
		{ID: "new-two", Step: 2, Version: 2, ParentID: "one", EffectiveVerdict: "FAIL"},
		{ID: "three", Step: 3, Version: 1, ParentID: "old-two", EffectiveVerdict: "PASS"},
	}}}}
	projectWiringBatch(b, true)
	if b.NextStep == 4 || b.Targets[0].Reports[3].EffectiveVerdict != "BLOCKED" {
		t.Fatal("obsolete passing Step 3 remained confirmable")
	}
	b.Targets[0].Reports[0].EffectiveVerdict = "BLOCKED"
	projectWiringBatch(b, true)
	if b.CanStartStep2 || b.NextStep != 1 {
		t.Fatal("invalid Step 1 still permits dispatch")
	}
}

func TestWiringConfirmationStageDoesNotClaimMappingPublication(t *testing.T) {
	b := &WiringBatch{Targets: []WiringBatchTarget{{Reports: []WiringStepReport{
		{ID: "one", Step: 1, Version: 1, EffectiveVerdict: "PASS"},
		{ID: "two", Step: 2, Version: 1, ParentID: "one", EffectiveVerdict: "PASS"},
		{ID: "three", Step: 3, Version: 1, ParentID: "two", EffectiveVerdict: "PASS"},
		{ID: "four", Step: 4, Version: 1, ParentID: "three", EffectiveVerdict: "PASS"},
	}}}}
	projectWiringBatch(b, true)
	if b.State != "confirmed" || b.NextStep != 0 {
		t.Fatal("complete observation/confirmation chain not projected")
	}
	b.Targets[0].Reports[1].EffectiveVerdict = "BLOCKED"
	projectWiringBatch(b, true)
	if b.State == "confirmed" || b.Targets[0].Reports[3].EffectiveVerdict != "BLOCKED" {
		t.Fatal("obsolete Step 4 survived invalid Step 2")
	}
}
