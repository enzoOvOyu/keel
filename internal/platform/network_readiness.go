package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

func (s *Store) NetworkReadiness(ctx context.Context, a *AuthSession, p *ProjectSummary, trace string) (*NetworkReadiness, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	out, e := s.networkReadinessTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.readiness.view", p.ID, trace, map[string]any{"package_id": out.Health.PackageID, "g2_admission": out.G2Admission}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

func (s *Store) networkReadinessTx(ctx context.Context, tx *sql.Tx, project string) (*NetworkReadiness, error) {
	health, e := networkHealthTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	out := &NetworkReadiness{Version: "network-readiness/v1", ProjectID: project, ReadinessAt: health.ReadinessAt, TTLSeconds: health.TTLSeconds, ValidUntil: health.ValidUntil, EvidenceSource: health.EvidenceSource, Health: health, Gates: []NetworkGate{}}
	pkg, inventory, cables, e := fabricInventoryTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	np1, e := networkModelReadinessTx(ctx, tx, pkg, inventory, cables)
	if e != nil {
		return nil, e
	}
	np2 := networkGate("NP-2", "PASS", "")
	np3 := networkGate("NP-3", "BLOCKED", "server_three_source_G2_evidence_required")
	np4 := networkGate("NP-4", "PASS", "")
	np5 := health.NS7
	np5.ID = "NP-5"
	np6 := networkGate("NP-6", "PASS", "")
	collection, e := latestFabricCollectionTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	if collection == nil || collection.Stale {
		mergeNetworkGate(&np4, networkGate("collection", "BLOCKED", "current_network_collection_required"))
	}
	if collection != nil && collection.EvidenceSource != "device" {
		mergeNetworkGate(&np4, networkGate("source", "BLOCKED", "device_evidence_required"))
	}
	for _, device := range health.Devices {
		var sample *fabricmon.Snapshot
		if collection != nil && !collection.Stale && device.SampleRef != "" {
			var raw []byte
			if e = tx.QueryRowContext(ctx, "SELECT sample FROM fabric_switch_samples WHERE run_id=$1 AND switch_id=$2", collection.ID, device.Device.ID).Scan(&raw); e != nil {
				return nil, e
			}
			if e = json.Unmarshal(raw, &sample); e != nil {
				return nil, e
			}
		}
		readable := networkLLDPReadiness(device.Device.Serial, sample, cables, out.ReadinessAt)
		if readable.State == "PASS" {
			readable.EvidenceRefs = append(readable.EvidenceRefs, collection.ID, device.SampleRef)
		}
		mergeNetworkGate(&np4, readable)
		scope := &IssueVerificationScope{ProjectID: project, EvidenceOrigin: "device", Targets: []fabricboot.CollectorTarget{{SwitchID: device.Device.ID, Serial: device.Device.Serial}}}
		if _, err := s.issueVerificationManagementHashTx(ctx, tx, scope); err != nil {
			var business *Error
			if !errors.As(err, &business) || business.Status != 409 {
				return nil, err
			}
			mergeNetworkGate(&np2, networkGate(device.Device.ID, "BLOCKED", "authenticated_ztp_manageability_required"))
		}
		var raw []byte
		var state string
		var jobID string
		e = tx.QueryRowContext(ctx, `SELECT d.state,coalesce(d.verify_evidence,'null'),j.id FROM network_push_devices d JOIN network_push_jobs j ON j.id=d.job_id WHERE j.project_id=$1 AND d.switch_id=$2 AND j.push_execution_id IS NOT NULL ORDER BY j.created_at DESC,j.id DESC LIMIT 1`, project, device.Device.ID).Scan(&state, &raw, &jobID)
		if errors.Is(e, sql.ErrNoRows) {
			mergeNetworkGate(&np2, networkGate(device.Device.ID, "BLOCKED", "golden_readback_required"))
		} else if e != nil {
			return nil, e
		} else {
			var proof struct {
				Source      string    `json:"evidence_source"`
				ObservedAt  time.Time `json:"observed_at"`
				RunningHash string    `json:"running_hash"`
			}
			if e = json.Unmarshal(raw, &proof); e != nil {
				return nil, e
			}
			if state == "VERIFIED" {
				j, err := loadNetworkPush(ctx, tx, project, jobID, false)
				if err != nil {
					return nil, err
				}
				if err = s.reviewNetworkPushFacts(ctx, tx, j); err != nil {
					var business *Error
					if !errors.As(err, &business) {
						return nil, err
					}
					state = "STALE"
				}
			}
			if state != "VERIFIED" || proof.Source != "device" || proof.RunningHash == "" || proof.ObservedAt.IsZero() || proof.ObservedAt.After(out.ReadinessAt) || out.ReadinessAt.Sub(proof.ObservedAt) > 5*time.Minute {
				mergeNetworkGate(&np2, networkGate(device.Device.ID, "BLOCKED", "current_device_golden_readback_required"))
			} else {
				np2.EvidenceRefs = append(np2.EvidenceRefs, jobID)
			}
		}
		if device.Lossless.Fabric.Unknown {
			mergeNetworkGate(&np6, networkGate(device.Device.ID, "BLOCKED", "lossless_counter_or_intent_unknown"))
		}
	}
	if len(health.Devices) == 0 {
		np4 = networkGate("NP-4", "BLOCKED", "network_inventory_empty")
		np2 = networkGate("NP-2", "BLOCKED", "network_inventory_empty")
		np6 = networkGate("NP-6", "BLOCKED", "network_inventory_empty")
	}
	if !out.ValidUntil.After(out.ReadinessAt) {
		np4 = networkGate("NP-4", "BLOCKED", "readiness_evidence_expired")
	}
	if out.EvidenceSource != "device" {
		mergeNetworkGate(&np6, networkGate("source", "BLOCKED", "device_evidence_required"))
	}
	out.Gates = []NetworkGate{np1, np2, np3, np4, np5, np6}
	out.G2Admission = np1.State == "PASS" && np2.State == "PASS" && np4.State == "PASS"
	return out, nil
}
