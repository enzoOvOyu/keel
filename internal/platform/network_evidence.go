package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

type NetworkLLDPEvidence struct {
	Version        string              `json:"version"`
	ProjectID      string              `json:"project_id"`
	DeviceID       string              `json:"device_id"`
	Port           string              `json:"port"`
	Gate           NetworkGate         `json:"gate"`
	Neighbor       *fabricmon.Neighbor `json:"neighbor"`
	Source         string              `json:"source"`
	EvidenceSource string              `json:"evidence_source"`
	CollectedAt    *time.Time          `json:"collected_at"`
	ValidUntil     time.Time           `json:"valid_until"`
	SampleRef      string              `json:"sample_ref"`
}
type NetworkCableEvidence struct {
	CableID        string      `json:"cable_id"`
	EndpointA      string      `json:"endpoint_a_uid"`
	EndpointB      string      `json:"endpoint_b_uid"`
	Gate           NetworkGate `json:"gate"`
	RuleRef        string      `json:"rule_ref"`
	Source         string      `json:"source"`
	EvidenceSource string      `json:"evidence_source"`
	Stage          string      `json:"stage"`
	NP3            string      `json:"np3"`
	CollectedAt    *time.Time  `json:"collected_at"`
	ValidUntil     time.Time   `json:"valid_until"`
}
type NetworkCablePage struct {
	Version   string                 `json:"version"`
	ProjectID string                 `json:"project_id"`
	PackageID string                 `json:"package_id"`
	Page      int                    `json:"page"`
	PageSize  int                    `json:"page_size"`
	Total     int                    `json:"total"`
	Gate      NetworkGate            `json:"gate"`
	Items     []NetworkCableEvidence `json:"items"`
}

func (s *Store) NetworkLLDP(ctx context.Context, a *AuthSession, p *ProjectSummary, device, port, trace string) (*NetworkLLDPEvidence, error) {
	if port == "" || len(port) > 128 {
		return nil, apiError(400, "FABRIC_PORT_REQUIRED", "Select a modeled switch port")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	pkg, _, _, e := fabricInventoryTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	var exists bool
	e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid WHERE r.package_id=$1 AND b.category='port' AND b.parent_uid=$2 AND b.port_name=$3 AND b.status<>'retired')`, pkg, device, port).Scan(&exists)
	if e != nil {
		return nil, e
	}
	if !exists {
		return nil, ErrNotFound
	}
	out := &NetworkLLDPEvidence{Version: "network-lldp/v1", ProjectID: p.ID, DeviceID: device, Port: port, Source: "switch", Gate: networkGate("LLDP", "BLOCKED", "current_switch_evidence_missing"), ValidUntil: time.Now().UTC().Add(30 * time.Second)}
	collection, e := latestFabricCollectionTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	if collection != nil {
		out.EvidenceSource = collection.EvidenceSource
		out.CollectedAt = &collection.CollectedAt
		if collection.ValidUntil.Before(out.ValidUntil) {
			out.ValidUntil = collection.ValidUntil
		}
		if !collection.Stale {
			var raw []byte
			var sample fabricmon.Snapshot
			e = tx.QueryRowContext(ctx, `SELECT content_hash,sample FROM fabric_switch_samples WHERE run_id=$1 AND switch_id=$2`, collection.ID, device).Scan(&out.SampleRef, &raw)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			if e == nil {
				if e = json.Unmarshal(raw, &sample); e != nil {
					return nil, e
				}
				out.CollectedAt = &sample.CollectedAt
				for _, observed := range sample.Ports {
					if observed.Name != port {
						continue
					}
					switch {
					case !sample.Capabilities["lldp"] || !observed.LLDPReadable:
						out.Gate = networkGate("LLDP", "BLOCKED", "lldp_unreadable")
					case observed.LinkUp == nil:
						out.Gate = networkGate("LLDP", "BLOCKED", "link_state_unknown")
					case !*observed.LinkUp:
						out.Gate = networkGate("LLDP", "FAIL", "link_down")
					case observed.Neighbor == nil:
						out.Gate = networkGate("LLDP", "FAIL", "lldp_neighbor_absent")
					default:
						out.Gate = networkGate("LLDP", "PASS", "")
						out.Neighbor = observed.Neighbor
						out.Gate.EvidenceRefs = append(out.Gate.EvidenceRefs, out.SampleRef)
					}
					break
				}
			}
		} else {
			out.Gate = networkGate("LLDP", "BLOCKED", "evidence_stale")
		}
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.lldp.read", device, trace, map[string]any{"port": port, "sample_ref": out.SampleRef, "state": out.Gate.State}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) NetworkCables(ctx context.Context, a *AuthSession, p *ProjectSummary, page, size int, trace string) (*NetworkCablePage, error) {
	if page < 1 || page > 1000000 || size < 1 || size > 1000 {
		return nil, apiError(400, "FABRIC_PAGE_INVALID", "Use page >=1 and page_size 1 through 1000")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	pkg, _, cables, e := fabricInventoryTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	report, e := latestFabricCollectionTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	out := &NetworkCablePage{Version: "network-cables/v1", ProjectID: p.ID, PackageID: pkg, Page: page, PageSize: size, Total: len(cables), Gate: networkGate("cables", "BLOCKED", "current_cable_evidence_required"), Items: []NetworkCableEvidence{}}
	findings := map[string]fabricmon.Finding{}
	if report != nil && !report.Stale {
		for _, f := range report.Findings {
			findings[f.CableUID] = f
		}
		out.Gate = networkGate("cables", "PASS", "")
	}
	for i, cable := range cables {
		item := NetworkCableEvidence{CableID: cable.UID, EndpointA: cable.A.UID, EndpointB: cable.B.UID, Gate: networkGate(cable.UID, "BLOCKED", "cable_evidence_missing"), Source: "switch", Stage: "N-S5", NP3: "BLOCKED", ValidUntil: time.Now().UTC().Add(30 * time.Second), RuleRef: "PRECHECK-LLDP@1"}
		if report != nil {
			item.EvidenceSource = report.EvidenceSource
			item.CollectedAt = &report.CollectedAt
			if report.ValidUntil.Before(item.ValidUntil) {
				item.ValidUntil = report.ValidUntil
			}
		}
		if f, ok := findings[cable.UID]; ok {
			item.RuleRef = f.RuleRef
			item.Gate.Reasons = []string{f.Reason}
			switch f.Verdict {
			case "precheck_pass":
				item.Gate.State = "PASS"
			case "anomaly":
				item.Gate.State = "FAIL"
			default:
				item.Gate.State = "BLOCKED"
			}
			item.Gate.EvidenceRefs = []string{report.ID}
		}
		mergeNetworkGate(&out.Gate, item.Gate)
		if i >= (page-1)*size && i < page*size {
			out.Items = append(out.Items, item)
		}
	}
	if len(cables) == 0 {
		out.Gate = networkGate("cables", "BLOCKED", "cable_inventory_empty")
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.cables.read", p.ID, trace, map[string]any{"package_id": pkg, "page": page, "page_size": size, "state": out.Gate.State}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
