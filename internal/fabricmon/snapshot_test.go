package fabricmon

import (
	"testing"
	"time"
)

func TestFabricCableEvidenceBoundaries(t *testing.T) {
	up := true
	a := Endpoint{UID: "a", Switch: true, Serial: "A", Port: "swp1"}
	b := Endpoint{UID: "b", Switch: true, Serial: "B", Port: "swp2"}
	samples := map[string]Snapshot{"A": {Capabilities: map[string]bool{"interfaces": true, "lldp": true}, Ports: []Port{{Name: "swp1", LinkUp: &up, LLDPReadable: true, Neighbor: &Neighbor{Serial: "B", PortID: "swp2"}}}}, "B": {Capabilities: map[string]bool{"interfaces": true, "lldp": true}, Ports: []Port{{Name: "swp2", LinkUp: &up, LLDPReadable: true, Neighbor: &Neighbor{Serial: "A", PortID: "swp1"}}}}}
	cable := Cable{UID: "c", A: a, B: b}
	if f := EvaluateCable(cable, samples); f.Verdict != "precheck_pass" {
		t.Fatal(f)
	}
	b.Switch = false
	if f := EvaluateCable(Cable{UID: "c", A: a, B: b}, samples); f.Verdict != "pending" || f.Reason != "server_side_pending" {
		t.Fatal("server side incorrectly confirmed", f)
	}
	b.Switch = true
	sample := samples["A"]
	sample.Ports[0].Neighbor.PortID = "swp99"
	samples["A"] = sample
	if f := EvaluateCable(cable, samples); f.IssueType != "T2" {
		t.Fatal(f)
	}
	sample.Ports[0].Neighbor = nil
	sample.Ports[0].LLDPReadable = false
	samples["A"] = sample
	if f := EvaluateCable(cable, samples); f.Verdict != "pending" {
		t.Fatal("missing capability became a pass", f)
	}
	sample.Ports[0].LLDPReadable = true
	samples["A"] = sample
	if f := EvaluateCable(cable, samples); f.IssueType != "T1" {
		t.Fatal(f)
	}
}
func TestFabricSnapshotProvenanceAndDDM(t *testing.T) {
	now := time.Now()
	s := Snapshot{Version: "fabric-snapshot/v1", Source: "switch", Serial: "A", Provider: "fixture/v1", CollectedAt: now}
	if e := s.Validate("B", now); e == nil {
		t.Fatal("foreign switch sample accepted")
	}
	s.CollectedAt = now.Add(-6 * time.Minute)
	if e := s.Validate("A", now); e == nil {
		t.Fatal("stale sample accepted")
	}
	if state, _ := JudgeOptics(&Optics{}, nil); state != "unknown" {
		t.Fatal(state)
	}
	rx := -20.0
	limit := -10.0
	if state, _ := JudgeOptics(&Optics{RxDBM: &rx}, map[string]Range{"rx_dbm": {CriticalMin: &limit}}); state != "critical" {
		t.Fatal(state)
	}
}
