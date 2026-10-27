package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

type networkConfigReadiness struct {
	Gate           NetworkGate `json:"gate"`
	ManagementHash string      `json:"management_hash"`
	GoldenHash     string      `json:"golden_hash"`
	ValidUntil     *time.Time  `json:"valid_until"`
}

// Shared NP-2 predicate for project readiness and target-scoped G2 admission.
// Missing NP-5/6 telemetry is deliberately not part of this configuration gate.
func (s *Store) networkConfigReadinessTx(ctx context.Context, tx *sql.Tx, project string, device FabricDeviceSummary, now time.Time, lock bool) (networkConfigReadiness, error) {
	out := networkConfigReadiness{Gate: networkGate(device.ID, "PASS", "")}
	scope := &IssueVerificationScope{ProjectID: project, EvidenceOrigin: "device", Targets: []fabricboot.CollectorTarget{{SwitchID: device.ID, Serial: device.Serial}}}
	hash, e := s.issueVerificationManagementHashTx(ctx, tx, scope)
	if e != nil {
		var business *Error
		if !errors.As(e, &business) || business.Status != 409 {
			return out, e
		}
		mergeNetworkGate(&out.Gate, networkGate(device.ID, "BLOCKED", "authenticated_ztp_manageability_required"))
	} else {
		out.ManagementHash = hash
	}
	var raw, facts []byte
	var state, executionState, jobID string
	e = tx.QueryRowContext(ctx, latestNetworkTemplateDevicesCTE+` SELECT device_state,coalesce(verify_evidence,'null'),job_id,execution_state,facts FROM latest WHERE switch_id=$2`, project, device.ID).Scan(&state, &raw, &jobID, &executionState, &facts)
	if errors.Is(e, sql.ErrNoRows) {
		mergeNetworkGate(&out.Gate, networkGate(device.ID, "BLOCKED", "golden_readback_required"))
		return out, nil
	}
	if e != nil {
		return out, e
	}
	var proof *netbaseline.OperationResult
	var planned NetworkPlannedDevice
	if e = json.Unmarshal(facts, &planned); e != nil {
		return out, e
	}
	if e = json.Unmarshal(raw, &proof); e != nil {
		return out, e
	}
	if state == "VERIFIED" {
		job, e := loadNetworkPush(ctx, tx, project, jobID, false)
		if e != nil {
			return out, e
		}
		if e = s.reviewNetworkPushFactsWithLock(ctx, tx, job, lock); e != nil {
			var business *Error
			if !errors.As(e, &business) {
				return out, e
			}
			state = "STALE"
		}
	}
	golden := networkGoldenReadbackGate(project, planned, state, executionState, proof, now)
	if planned.ID != device.ID || planned.Serial != device.Serial {
		golden = networkGate(device.ID, "BLOCKED", "current_golden_identity_changed")
	}
	if golden.State == "PASS" {
		golden.EvidenceRefs = append(golden.EvidenceRefs, jobID)
		expiry := proof.ObservedAt.Add(networkGoldenMaxAge)
		out.ValidUntil = &expiry
		out.GoldenHash = digest(string(networkJSON([]any{jobID, planned, proof})))
	}
	mergeNetworkGate(&out.Gate, golden)
	return out, nil
}
