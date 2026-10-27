package platform

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/pcitopology"
	"github.com/enzoOvOyu/keel/internal/precollect"
)

func relationFixture(t *testing.T) (*PciTopologySnapshot, *PrecollectScope, *precollect.Report) {
	t.Helper()
	raw, e := os.ReadFile("../../sim/fixtures/nvidia-smi-topo/baseline.json")
	if e != nil {
		t.Fatal(e)
	}
	var f struct {
		Snapshot    pcitopology.Snapshot    `json:"snapshot"`
		Expectation pcitopology.Expectation `json:"expectation"`
		Bindings    []pcitopology.Binding   `json:"bindings"`
	}
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	record := &PciTopologySnapshot{ID: strings.Repeat("a", 32), Version: 1, Verdict: "PASS", EffectiveVerdict: "PASS", Snapshot: &f.Snapshot, Scope: &TopologyScope{Expectation: &f.Expectation, Bindings: f.Bindings}}
	frozen := &PrecollectScope{Expectation: precollect.Expectation{Ports: []precollect.ExpectedPort{{ID: strings.Repeat("4", 32), ComponentID: f.Bindings[2].ComponentID, MAC: "02:00:00:00:00:01", PortIndex: "1"}}}}
	report := &precollect.Report{NICs: []precollect.NIC{{Name: "eth0", BDF: f.Bindings[2].BDF, PermanentMAC: "02:00:00:00:00:01"}}}
	return record, frozen, report
}
func TestGPUToNICRelationsConsumeIndependentMapAndStablePorts(t *testing.T) {
	r, f, p := relationFixture(t)
	out := buildTopologyRelations(r, f, p)
	if out.Step1Published || out.EffectiveVerdict != "PASS" || len(out.Rows) != 2 {
		t.Fatalf("%+v", out)
	}
	for _, row := range out.Rows {
		if row.PermanentMAC != "02:00:00:00:00:01" || row.PhysicalPortID != strings.Repeat("4", 32) || row.GPUNode == nil || row.NICNode == nil || row.Verdict != "PASS" {
			t.Fatalf("%+v", row)
		}
	}
	// No unplanned association is synthesized from NUMA proximity or BDF rank.
	r.Scope.Expectation.GPUNICMap = r.Scope.Expectation.GPUNICMap[:1]
	out = buildTopologyRelations(r, f, p)
	if len(out.Rows) != 1 || out.Rows[0].GPUKey != "GPU-A" {
		t.Fatalf("observations invented a map: %+v", out)
	}
}
func TestRelationsRetainMissingExpectedRowsAndCurrentBlock(t *testing.T) {
	r, f, p := relationFixture(t)
	p.NICs[0].PermanentMAC = "02:ff:ff:ff:ff:ff"
	out := buildTopologyRelations(r, f, p)
	if out.EffectiveVerdict != "BLOCKED" || len(out.Rows) != 2 {
		t.Fatalf("missing ports disappeared: %+v", out)
	}
	for _, row := range out.Rows {
		if row.PhysicalPortID != "" || row.Verdict != "BLOCKED" {
			t.Fatal(row)
		}
	}
	r.EffectiveVerdict = "FAIL"
	out = buildTopologyRelations(r, f, p)
	if out.EffectiveVerdict != "FAIL" {
		t.Fatal("relation projection hid whole-topology failure")
	}
	r.EffectiveVerdict = "BLOCKED"
	r.Scope.Expectation.GPUNICMap[0].Affinity = "PHB"
	out = buildTopologyRelations(r, f, p)
	if out.EffectiveVerdict != "BLOCKED" {
		t.Fatal("invalidated snapshot became an effective topology verdict")
	}
}
