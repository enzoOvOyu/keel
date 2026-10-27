package platform

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

// The query lifetime may be shortened by catalog policy, never extended beyond
// the PRD's thirty-second admission window. A retired definition fails closed.
func networkReadinessTTLTx(ctx context.Context, tx *sql.Tx, project string) (int, error) {
	values, e := catalogDefaultsTx(ctx, tx, project, "gate.readiness_ttl")
	if e != nil {
		return 0, e
	}
	raw, ok := values["gate.readiness_ttl"]
	if !ok {
		return 30, nil
	}
	seconds, e := strconv.Atoi(raw)
	if e != nil || seconds < 1 || seconds > 30 {
		return 0, apiError(409, "NETWORK_READINESS_TTL_INVALID", "gate.readiness_ttl must be 1 through 30 seconds")
	}
	return seconds, nil
}

// NP-1 describes the approved model. Missing telemetry and incomplete ZTP must
// not rewrite model existence into a different assertion.
func networkModelReadinessTx(ctx context.Context, tx *sql.Tx, pkg string, inventory []FabricDeviceSummary, cables []fabricmon.Cable) (NetworkGate, error) {
	gate := networkGate("NP-1", "PASS", "")
	gate.EvidenceRefs = []string{pkg}
	if len(inventory) == 0 {
		return networkGate("NP-1", "BLOCKED", "switch_inventory_empty"), nil
	}
	rows, e := tx.QueryContext(ctx, `SELECT b.item_uid,b.parent_uid,coalesce(nullif(b.port_name,''),b.port_index,''),p.id IS NOT NULL FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid JOIN bom_items parent ON parent.item_uid=b.parent_uid AND parent.category='switch' JOIN bom_item_revisions pr ON pr.package_id=r.package_id AND pr.item_uid=parent.item_uid LEFT JOIN physical_ports p ON p.id=b.item_uid AND p.switch_id=b.parent_uid WHERE r.package_id=$1 AND b.category='port' AND b.status<>'retired' AND parent.status<>'retired'`, pkg)
	if e != nil {
		return gate, e
	}
	stable := map[string]bool{}
	counts := map[string]int{}
	for rows.Next() {
		var id, parent, name string
		var physical bool
		if e = rows.Scan(&id, &parent, &name, &physical); e != nil {
			rows.Close()
			return gate, e
		}
		if physical && name != "" {
			stable[id] = true
			counts[parent]++
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return gate, e
	}
	for _, device := range inventory {
		if device.ID == "" || device.Serial == "" || counts[device.ID] == 0 {
			mergeNetworkGate(&gate, networkGate(device.ID, "BLOCKED", "switch_or_ports_unmodeled"))
		}
	}
	var declared int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid WHERE r.package_id=$1 AND b.category='cable' AND b.status<>'retired'`, pkg).Scan(&declared); e != nil {
		return gate, e
	}
	if declared == 0 || declared != len(cables) {
		mergeNetworkGate(&gate, networkGate("links", "BLOCKED", "modeled_cable_endpoints_required"))
	}
	modeled := map[string]bool{}
	for _, device := range inventory {
		modeled[device.Serial] = true
	}
	for _, cable := range cables {
		for _, endpoint := range []fabricmon.Endpoint{cable.A, cable.B} {
			if !endpoint.Switch {
				continue
			}
			if !modeled[endpoint.Serial] || endpoint.UID == "" || endpoint.Port == "" {
				mergeNetworkGate(&gate, networkGate(cable.UID, "BLOCKED", "switch_or_ports_unmodeled"))
				continue
			}
			if !stable[endpoint.UID] {
				mergeNetworkGate(&gate, networkGate(cable.UID, "BLOCKED", "switch_or_ports_unmodeled"))
			}
		}
	}
	return gate, nil
}

// LLDP readability is distinct from a correct neighbor or a link-up verdict.
// This gate cannot confirm wiring and never produces NP-3.
func networkLLDPReadiness(serial string, sample *fabricmon.Snapshot, cables []fabricmon.Cable, now time.Time) NetworkGate {
	gate := networkGate("NP-4", "PASS", "")
	if sample == nil || sample.Validate(serial, now) != nil {
		return networkGate("NP-4", "BLOCKED", "current_switch_evidence_missing")
	}
	if !sample.Capabilities["lldp"] {
		return networkGate("NP-4", "BLOCKED", "lldp_unreadable")
	}
	needed := map[string]bool{}
	for _, cable := range cables {
		for _, endpoint := range []fabricmon.Endpoint{cable.A, cable.B} {
			if endpoint.Switch && endpoint.Serial == serial {
				needed[endpoint.Port] = true
			}
		}
	}
	if len(needed) == 0 {
		return networkGate("NP-4", "BLOCKED", "modeled_connected_ports_required")
	}
	for _, port := range sample.Ports {
		if !needed[port.Name] {
			continue
		}
		delete(needed, port.Name)
		if !port.LLDPReadable {
			mergeNetworkGate(&gate, networkGate(port.Name, "BLOCKED", "lldp_unreadable"))
		}
	}
	if len(needed) > 0 {
		mergeNetworkGate(&gate, networkGate(serial, "BLOCKED", "modeled_connected_port_sample_missing"))
	}
	return gate
}
