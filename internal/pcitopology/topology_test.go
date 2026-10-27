package pcitopology

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type topologyFixture struct {
	Expectation      Expectation `json:"expectation"`
	Snapshot         Snapshot    `json:"snapshot"`
	Bindings         []Binding   `json:"bindings"`
	ExpectedCategory string      `json:"expected_category"`
	Raw              struct {
		Identity string `json:"gpu_identity"`
		Matrix   string `json:"gpu_topology"`
		NVLink   string `json:"nvlink"`
	} `json:"raw"`
}

func fixture(t *testing.T, name string) topologyFixture {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "..", "sim", "fixtures", "nvidia-smi-topo", name+".json"))
	if e != nil {
		t.Fatal(e)
	}
	var f topologyFixture
	if e = json.Unmarshal(raw, &f); e != nil {
		t.Fatal(e)
	}
	return f
}
func hasCode(e Evaluation, code string) bool {
	for _, d := range e.Diffs {
		if d.Code == code {
			return true
		}
	}
	return false
}

// Authored for the next centralized milestone; no real hardware claim is made
// by exercising pure evaluation with real=true on synthetic input.
func TestFourDimensionFixtureClassification(t *testing.T) {
	for _, name := range []string{"baseline", "pcie-misplaced", "numa-anomaly", "nvlink-missing", "lnk-degraded"} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t, name)
			if e := f.Expectation.Validate(); e != nil {
				t.Fatal(e)
			}
			if e := f.Snapshot.Validate(); e != nil {
				t.Fatal(e)
			}
			got := Evaluate(&f.Snapshot, &f.Expectation, f.Bindings, true)
			if name == "baseline" {
				if got.Verdict != "PASS" {
					t.Fatalf("%+v", got)
				}
				return
			}
			if got.Verdict != "FAIL" {
				t.Fatalf("%+v", got)
			}
			found := false
			for _, d := range got.Diffs {
				if d.Category == f.ExpectedCategory && d.Verdict == "FAIL" {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing category %s: %+v", f.ExpectedCategory, got)
			}
		})
	}
}
func TestRawNvidiaSemanticsAndExplicitIdentity(t *testing.T) {
	f := fixture(t, "baseline")
	s := Normalize(f.Snapshot.Nodes, map[string]string{"eth0": "0000:05:00.0"}, f.Raw.Identity, f.Raw.Matrix, f.Raw.NVLink, nil)
	if e := s.Validate(); e != nil {
		t.Fatal(e)
	}
	got := Evaluate(s, &f.Expectation, f.Bindings, true)
	if got.Verdict != "PASS" {
		t.Fatalf("%+v", got)
	}
	// Collection order is immaterial: GPU0 is index 0, not the smallest BDF.
	rows := strings.Split(strings.TrimSpace(f.Raw.Identity), "\n")
	s = Normalize(f.Snapshot.Nodes, map[string]string{"eth0": "0000:05:00.0"}, rows[1]+"\n"+rows[0]+"\n", f.Raw.Matrix, f.Raw.NVLink, nil)
	if got := Evaluate(s, &f.Expectation, f.Bindings, true); got.Verdict != "PASS" {
		t.Fatalf("reordered identity: %+v", got)
	}
	// Swapping actual component locations cannot manufacture a new baseline.
	f.Bindings[0].BDF, f.Bindings[1].BDF = f.Bindings[1].BDF, f.Bindings[0].BDF
	if got := Evaluate(s, &f.Expectation, f.Bindings, true); got.Verdict != "FAIL" || !hasCode(got, "TOPOLOGY_SLOT_MISMATCH") {
		t.Fatalf("swapped BDF self-validated: %+v", got)
	}
}
func TestTopologyUnknownAndSimulationNeverPass(t *testing.T) {
	for _, kind := range []string{"sim", "provenance", "binding", "numa", "link", "status", "matrix", "bridge"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, "baseline")
			real := true
			switch kind {
			case "sim":
				real = false
			case "provenance":
				f.Expectation.Provenance.Kind = "observed"
			case "binding":
				f.Bindings = f.Bindings[:2]
			case "numa":
				f.Snapshot.Nodes[3].NUMA = nil
			case "link":
				f.Snapshot.Nodes[3].Width = nil
			case "status":
				f.Snapshot.NVLinks = nil
			case "matrix":
				f.Snapshot.Affinity = nil
			case "bridge":
				f.Snapshot.Nodes[1].PCIeType = "unknown"
			}
			if got := Evaluate(&f.Snapshot, &f.Expectation, f.Bindings, real); got.Verdict != "BLOCKED" {
				t.Fatalf("%s: %+v", kind, got)
			}
		})
	}
}
func TestSlotDesignAndCapabilityAreSeparate(t *testing.T) {
	f := fixture(t, "baseline")
	f.Expectation.Devices[0].Width = 8
	*f.Snapshot.Nodes[3].Width = 8
	if got := Evaluate(&f.Snapshot, &f.Expectation, f.Bindings, true); got.Verdict != "PASS" {
		t.Fatalf("designed x8 slot: %+v", got)
	}
	f.Expectation.Devices[0].Width = 16
	*f.Snapshot.Nodes[3].MaxWidth = 8
	if got := Evaluate(&f.Snapshot, &f.Expectation, f.Bindings, true); !hasCode(got, "TOPOLOGY_CAPABILITY_BELOW_MODEL") {
		t.Fatalf("%+v", got)
	}
}

func TestGPUToNICAssociationMustBeIndependentlyDeclared(t *testing.T) {
	f := fixture(t, "baseline")
	f.Expectation.GPUNICMap[0].Affinity = "PHB"
	if got := Evaluate(&f.Snapshot, &f.Expectation, f.Bindings, true); got.Verdict != "FAIL" || !hasCode(got, "TOPOLOGY_GPU_NIC_AFFINITY_MISMATCH") {
		t.Fatalf("observed proximity replaced the model map: %+v", got)
	}
	f.Expectation.GPUNICMap = nil
	if f.Expectation.Validate() == nil {
		t.Fatal("missing model association accepted")
	}
}
func TestIndependentModelDecoderRejectsIncompleteAndUnknownFields(t *testing.T) {
	f := fixture(t, "baseline")
	raw, _ := json.Marshal(f.Expectation)
	if _, e := DecodeExpectation(raw); e != nil {
		t.Fatal(e)
	}
	for _, raw := range []string{`{}`, `{"version":"keel.pcie-topology/v1","observed_baseline":true}`, string(raw) + " {}"} {
		if _, e := DecodeExpectation([]byte(raw)); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	f.Expectation.NVLinks = f.Expectation.NVLinks[:1]
	if f.Expectation.Validate() == nil {
		t.Fatal("missing GPU pair accepted")
	}
}
func TestSnapshotRejectsForeignAncestorDuplicateAndForgedSelf(t *testing.T) {
	for _, kind := range []string{"ancestor", "duplicate", "self", "cycle"} {
		t.Run(kind, func(t *testing.T) {
			f := fixture(t, "baseline")
			switch kind {
			case "ancestor":
				f.Snapshot.Nodes[3].Upstream = []string{"0000:ff:00.0"}
			case "duplicate":
				f.Snapshot.Devices = append(f.Snapshot.Devices, f.Snapshot.Devices[0])
			case "self":
				f.Snapshot.Affinity[0].Observed = "PIX"
			case "cycle":
				f.Snapshot.Nodes[0].Upstream = []string{f.Snapshot.Nodes[0].BDF}
			}
			if f.Snapshot.Validate() == nil {
				t.Fatal("invalid graph accepted")
			}
		})
	}
}
