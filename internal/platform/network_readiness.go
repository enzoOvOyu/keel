package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

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
		configured, e := s.networkConfigReadinessTx(ctx, tx, project, device.Device, out.ReadinessAt, true)
		if e != nil {
			return nil, e
		}
		mergeNetworkGate(&np2, configured.Gate)
		if configured.ValidUntil != nil && configured.ValidUntil.Before(out.ValidUntil) {
			out.ValidUntil = *configured.ValidUntil
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
	if seconds := int(out.ValidUntil.Sub(out.ReadinessAt) / time.Second); seconds < out.TTLSeconds {
		out.TTLSeconds = seconds
	}
	out.Gates = []NetworkGate{np1, np2, np3, np4, np5, np6}
	out.G2Admission = np1.State == "PASS" && np2.State == "PASS" && np4.State == "PASS"
	return out, nil
}
