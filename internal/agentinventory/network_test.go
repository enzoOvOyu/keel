package agentinventory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestAgentNetworkDeltaAndExplicitRemovals(t *testing.T) {
	now := time.Now().UTC()
	a := Interface{Index: 1, Name: "eth0", MAC: "02:00:00:00:00:01", MTU: 1500, AdminUp: true, OperState: "up"}
	b := Interface{Index: 2, Name: "eth1", MAC: "02:00:00:00:00:02", MTU: 9000, OperState: "down"}
	initial := NetworkDelta(nil, []Interface{b, a}, now)
	if !initial.Full || initial.Validate(now) != nil {
		t.Fatal("initial snapshot rejected", initial)
	}
	baseline, err := MergeNetwork(nil, initial)
	if err != nil || len(baseline) != 2 || baseline[0] != a {
		t.Fatal("initial inventory not sorted", baseline, err)
	}
	unchanged := NetworkDelta(baseline, baseline, now)
	if unchanged.Full || len(unchanged.Interfaces) != 0 || len(unchanged.Removed) != 0 {
		t.Fatal("unchanged inventory was resent")
	}
	kept, err := MergeNetwork(baseline, unchanged)
	if err != nil || len(kept) != 2 {
		t.Fatal("empty delta deleted existing interfaces")
	}
	a.OperState = "down"
	delta := NetworkDelta(baseline, []Interface{a}, now)
	if delta.Full || len(delta.Interfaces) != 1 || len(delta.Removed) != 1 || delta.Removed[0] != 2 {
		t.Fatal("explicit changed/removed delta lost", delta)
	}
	merged, err := MergeNetwork(baseline, delta)
	if err != nil || len(merged) != 1 || merged[0] != a {
		t.Fatal("delta merge failed", merged, err)
	}
	reset, err := MergeNetwork(baseline, NetworkDelta(nil, []Interface{}, now))
	if err != nil || len(reset) != 0 {
		t.Fatal("new full empty observation did not reset old baseline")
	}
}

func TestAgentNetworkRejectsInvalidAndStaleFrames(t *testing.T) {
	now := time.Now().UTC()
	valid := func() NetworkFrame {
		return NetworkFrame{Version: NetworkVersion, State: "ready", Full: true, CapturedAt: now, Interfaces: []Interface{{Index: 1, Name: "eth0", MAC: "02:00:00:00:00:01", MTU: 1500, OperState: "up"}}, Removed: []int{}}
	}
	for _, change := range []func(*NetworkFrame){
		func(f *NetworkFrame) { f.Version = "other" }, func(f *NetworkFrame) { f.CapturedAt = now.Add(-31 * time.Second) }, func(f *NetworkFrame) { f.CapturedAt = now.Add(6 * time.Second) },
		func(f *NetworkFrame) { f.Removed = []int{1} }, func(f *NetworkFrame) { f.Interfaces = append(f.Interfaces, f.Interfaces[0]) },
		func(f *NetworkFrame) { f.Interfaces[0].Name = "../eth0" }, func(f *NetworkFrame) { f.Interfaces[0].MAC = "invalid" }, func(f *NetworkFrame) { f.Interfaces[0].Index = 0 },
		func(f *NetworkFrame) { f.Interfaces[0].OperState = "healthy" }, func(f *NetworkFrame) { f.State = "unavailable"; f.FailureCode = "interface_read_failed" },
	} {
		f := valid()
		change(&f)
		if f.Validate(now) == nil {
			t.Fatal("invalid frame admitted", f)
		}
	}
	raw, err := json.Marshal(valid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeNetwork(append(raw, raw...), now); err == nil {
		t.Fatal("trailing inventory accepted")
	}
	if _, err = DecodeNetwork([]byte(strings.TrimSuffix(string(raw), "}")+`,"secret":"forbidden"}`), now); err == nil {
		t.Fatal("unknown inventory field accepted")
	}
	if _, err = DecodeNetwork(make([]byte, MaxNetworkFrameBytes+1), now); err == nil {
		t.Fatal("oversized frame accepted")
	}
	if frame, err := DecodeNetwork(nil, now); err != nil || frame != nil {
		t.Fatal("legacy empty inventory was not disabled")
	}
	unknown := NetworkFrame{Version: NetworkVersion, State: "unavailable", CapturedAt: now, FailureCode: "interface_state_unavailable"}
	if unknown.Validate(now) != nil {
		t.Fatal("explicit collection failure rejected")
	}
	unknown.Interfaces = valid().Interfaces
	if unknown.Validate(now) == nil {
		t.Fatal("partial failed inventory accepted as a delta")
	}
}
