package platform

import (
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

func TestGoldenReadbackCannotUseUnknownExpiredOrForeignEvidence(t *testing.T) {
	now := time.Date(2026, 10, 27, 5, 0, 0, 0, time.UTC)
	project := strings.Repeat("a", 32)
	device := NetworkPlannedDevice{ID: strings.Repeat("b", 32), Serial: "GOLDEN-DEVICE", IntendedHash: strings.Repeat("c", 64)}
	proof := netbaseline.OperationResult{Version: "network-operation/v1", ProjectID: project, DeviceID: device.ID, Serial: device.Serial, RunningHash: device.IntendedHash, FencingToken: 17, State: "completed", Provider: "synthetic-test", EvidenceSource: "device", ObservedAt: now.Add(-time.Minute)}
	for _, id := range networkVerifyGates {
		proof.Gates = append(proof.Gates, netbaseline.Gate{ID: id, State: "PASS"})
	}
	if g := networkGoldenReadbackGate(project, device, "VERIFIED", "success", &proof, now); g.State != "PASS" {
		t.Fatal(g)
	}
	cases := []struct {
		name, state, execution string
		mutate                 func(*netbaseline.OperationResult)
	}{
		{"unknown latest target", "UNKNOWN", "success", nil}, {"unfinished execution", "VERIFIED", "reconciling", nil}, {"cancelled execution", "VERIFIED", "killed", nil},
		{"wrong project", "VERIFIED", "success", func(p *netbaseline.OperationResult) { p.ProjectID = strings.Repeat("d", 32) }},
		{"wrong device", "VERIFIED", "success", func(p *netbaseline.OperationResult) { p.DeviceID = strings.Repeat("d", 32) }},
		{"wrong serial", "VERIFIED", "success", func(p *netbaseline.OperationResult) { p.Serial = "REPLACED" }},
		{"wrong readback", "VERIFIED", "success", func(p *netbaseline.OperationResult) { p.RunningHash = strings.Repeat("d", 64) }},
		{"missing owner fence", "VERIFIED", "success", func(p *netbaseline.OperationResult) { p.FencingToken = 0 }},
		{"unknown checks", "VERIFIED", "success", func(p *netbaseline.OperationResult) { p.Gates = nil }},
		{"future timestamp", "VERIFIED", "success", func(p *netbaseline.OperationResult) { p.ObservedAt = now.Add(time.Second) }},
		{"expiry boundary", "VERIFIED", "success", func(p *netbaseline.OperationResult) { p.ObservedAt = now.Add(-networkGoldenMaxAge) }},
		{"fixture is not device proof", "VERIFIED", "success", func(p *netbaseline.OperationResult) { p.EvidenceSource = "protocol_fixture" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copy := proof
			if tc.mutate != nil {
				tc.mutate(&copy)
			}
			if got := networkGoldenReadbackGate(project, device, tc.state, tc.execution, &copy, now); got.State == "PASS" {
				t.Fatal("unsafe readback accepted")
			}
		})
	}
	// Upgrade previews can still compare retained fixture/historical intent, but
	// this must not make the current device gate ready.
	proof.EvidenceSource = "protocol_fixture"
	proof.ObservedAt = now.Add(-time.Hour)
	if !networkGoldenProofMatches(project, device, "VERIFIED", "failed", &proof, now) {
		t.Fatal("retained valid comparison evidence lost")
	}
	if g := networkGoldenReadbackGate(project, device, "VERIFIED", "failed", &proof, now); g.State == "PASS" {
		t.Fatal("historical fixture became current device proof")
	}
}
