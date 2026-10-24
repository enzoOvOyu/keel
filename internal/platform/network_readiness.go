package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
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
	out := &NetworkReadiness{Version: "network-readiness/v1", ProjectID: project, ReadinessAt: health.ReadinessAt, TTLSeconds: 30, ValidUntil: health.ValidUntil, EvidenceSource: health.EvidenceSource, Health: health, Gates: []NetworkGate{}}
	np1 := networkGate("NP-1", "PASS", "")
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
		np1 = networkGate("NP-1", "BLOCKED", "current_network_collection_required")
	}
	if collection != nil && collection.EvidenceSource != "device" {
		mergeNetworkGate(&np1, networkGate("source", "BLOCKED", "device_evidence_required"))
	}
	rows, e := tx.QueryContext(ctx, `SELECT DISTINCT batch_ref FROM ztp_management_assignments WHERE project_id=$1 ORDER BY batch_ref`, project)
	if e != nil {
		return nil, e
	}
	batches := []string{}
	for rows.Next() {
		var batch string
		if e = rows.Scan(&batch); e != nil {
			rows.Close()
			return nil, e
		}
		batches = append(batches, batch)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(batches) == 0 {
		mergeNetworkGate(&np1, networkGate("plan", "BLOCKED", "approved_management_plan_required"))
	}
	for _, batch := range batches {
		plan, err := currentManagementPlanTx(ctx, tx, project, batch)
		if err != nil {
			var business *Error
			if errors.As(err, &business) && business.Status == 409 {
				mergeNetworkGate(&np1, networkGate(batch, "BLOCKED", "management_plan_stale"))
				continue
			}
			return nil, err
		}
		var report []byte
		err = tx.QueryRowContext(ctx, `SELECT report FROM ztp_environment_checks WHERE project_id=$1 AND plan_id=$2 AND batch_ref=$3 AND boot_channel='239' ORDER BY checked_at DESC,id DESC LIMIT 1`, project, plan.ID, batch).Scan(&report)
		if errors.Is(err, sql.ErrNoRows) {
			mergeNetworkGate(&np1, networkGate(batch, "BLOCKED", "environment_checks_required"))
			continue
		}
		if err != nil {
			return nil, err
		}
		var environment ZTPEnvironmentReport
		if err = json.Unmarshal(report, &environment); err != nil {
			return nil, err
		}
		if !environment.Ready || !environment.ExpiresAt.After(out.ReadinessAt) || s.FabricProbes == nil || environment.ConfigHash != s.FabricProbes.Fingerprint {
			mergeNetworkGate(&np1, networkGate(batch, "BLOCKED", "environment_checks_stale_or_failed"))
		} else {
			np1.EvidenceRefs = append(np1.EvidenceRefs, environment.ID)
		}
	}
	for _, device := range health.Devices {
		if device.Device.State == "unknown" && device.SampleRef == "" {
			mergeNetworkGate(&np1, networkGate(device.Device.ID, "BLOCKED", "switch_management_or_lldp_unknown"))
		}
		if device.SampleRef != "" {
			var raw []byte
			if e = tx.QueryRowContext(ctx, `SELECT sample FROM fabric_switch_samples WHERE run_id=$1 AND switch_id=$2`, collection.ID, device.Device.ID).Scan(&raw); e != nil {
				return nil, e
			}
			var snapshot struct {
				Capabilities map[string]bool `json:"capabilities"`
			}
			if e = json.Unmarshal(raw, &snapshot); e != nil {
				return nil, e
			}
			if !snapshot.Capabilities["lldp"] {
				mergeNetworkGate(&np1, networkGate(device.Device.ID, "BLOCKED", "lldp_unreadable"))
			}
		}
		// A simulator's managed state and a fixture's successful push cannot satisfy
		// production NP-2. Real progress must include authenticated manageability.
		var managed bool
		e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ztp_sessions WHERE project_id=$1 AND switch_id=$2 AND mode='device' AND state='managed')`, project, device.Device.ID).Scan(&managed)
		if e != nil {
			return nil, e
		}
		if !managed {
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
			if state != "VERIFIED" || proof.Source != "device" || proof.RunningHash == "" || proof.ObservedAt.IsZero() || out.ReadinessAt.Sub(proof.ObservedAt) > 5*time.Minute {
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
