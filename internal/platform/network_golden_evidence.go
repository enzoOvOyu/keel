package platform

import (
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

const networkGoldenMaxAge = 5 * time.Minute

// One retained result predicate is shared by impact previews and current
// readiness. It never chooses an older job when the latest attempt is unknown.
func networkGoldenProofMatches(project string, device NetworkPlannedDevice, state, executionState string, proof *netbaseline.OperationResult, now time.Time) bool {
	if state != "VERIFIED" || (executionState != "success" && executionState != "failed") || proof == nil {
		return false
	}
	if proof.Version != "network-operation/v1" || proof.ProjectID != project || proof.DeviceID != device.ID || proof.Serial != device.Serial || proof.RunningHash == "" || proof.RunningHash != device.IntendedHash || proof.ObservedAt.IsZero() || proof.ObservedAt.After(now) || proof.FencingToken < 1 || proof.Provider == "" || (proof.EvidenceSource != "device" && proof.EvidenceSource != "protocol_fixture") {
		return false
	}
	return netbaseline.RequireGates(proof, networkVerifyGates...) == nil
}
func networkGoldenReadbackGate(project string, device NetworkPlannedDevice, state, executionState string, proof *netbaseline.OperationResult, now time.Time) NetworkGate {
	if !networkGoldenProofMatches(project, device, state, executionState, proof, now) {
		return networkGate(device.ID, "BLOCKED", "current_golden_or_execution_unresolved")
	}
	if proof.EvidenceSource != "device" {
		return networkGate(device.ID, "BLOCKED", "device_golden_evidence_required")
	}
	if !now.Before(proof.ObservedAt.Add(networkGoldenMaxAge)) {
		return networkGate(device.ID, "BLOCKED", "device_golden_evidence_expired")
	}
	return networkGate(device.ID, "PASS", "")
}
