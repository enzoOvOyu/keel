package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

type WiringTargetInput struct {
	AssetID         string   `json:"asset_id"`
	PhysicalPortIDs []string `json:"physical_port_ids"`
}
type WiringHandshakeInput struct {
	Targets []WiringTargetInput `json:"targets"`
	Reason  string              `json:"reason"`
}
type WiringPort struct {
	PhysicalPortID   string   `json:"physical_port_id"`
	ComponentID      string   `json:"component_id"`
	PortIndex        string   `json:"port_index"`
	Interface        string   `json:"interface"`
	PermanentMAC     string   `json:"permanent_mac"`
	BDF              string   `json:"bdf"`
	CableID          string   `json:"cable_id"`
	SwitchID         string   `json:"switch_id"`
	SwitchSerial     string   `json:"switch_serial"`
	SwitchChassisIDs []string `json:"switch_chassis_ids"`
	SwitchPortID     string   `json:"switch_port_id"`
	SwitchPort       string   `json:"switch_port"`
	Rail             string   `json:"rail"`
}
type WiringHost struct {
	AssetID           string       `json:"asset_id"`
	MachineID         string       `json:"machine_id"`
	Hostname          string       `json:"hostname"`
	Serial            string       `json:"serial"`
	AssetVersion      int64        `json:"asset_version"`
	BOMPackageID      string       `json:"bom_package_id"`
	G1WaiverID        string       `json:"g1_waiver_id,omitempty"`
	G1ID              string       `json:"g1_id"`
	PrecollectID      string       `json:"precollect_id"`
	TopologyID        string       `json:"topology_id"`
	TopologyScopeHash string       `json:"topology_scope_hash"`
	Ports             []WiringPort `json:"ports"`
}
type WiringSwitch struct {
	ID             string      `json:"id"`
	Serial         string      `json:"serial"`
	Ports          []string    `json:"ports"`
	ManagementHash string      `json:"management_hash"`
	GoldenHash     string      `json:"golden_hash"`
	SampleHash     string      `json:"sample_hash"`
	NP1            NetworkGate `json:"np1"`
	NP2            NetworkGate `json:"np2"`
	NP4            NetworkGate `json:"np4"`
}
type WiringFacts struct {
	Version            string         `json:"version"`
	ProjectID          string         `json:"project_id"`
	Targets            []WiringHost   `json:"targets"`
	Switches           []WiringSwitch `json:"switches"`
	NetworkPackageID   string         `json:"network_package_id"`
	CollectionID       string         `json:"collection_id"`
	CollectorHash      string         `json:"collector_hash"`
	TTLSeconds         int            `json:"ttl_seconds"`
	EvidenceValidUntil time.Time      `json:"evidence_valid_until"`
	FactsHash          string         `json:"facts_hash"`
	Ready              bool           `json:"ready"`
	Blockers           []string       `json:"blockers"`
}

func normalizeWiringTargets(targets []WiringTargetInput) ([]WiringTargetInput, error) {
	if len(targets) == 0 || len(targets) > 200 {
		return nil, apiError(400, "VALIDATION_ERROR", "One to 200 explicit host targets required")
	}
	out := make([]WiringTargetInput, len(targets))
	seen := map[string]bool{}
	total := 0
	for i, target := range targets {
		if !validEntityID(target.AssetID) || seen[target.AssetID] || len(target.PhysicalPortIDs) == 0 || len(target.PhysicalPortIDs) > 128 {
			return nil, apiError(400, "VALIDATION_ERROR", "Unique assets with one to 128 selected physical ports required")
		}
		seen[target.AssetID] = true
		out[i] = WiringTargetInput{AssetID: target.AssetID, PhysicalPortIDs: slices.Clone(target.PhysicalPortIDs)}
		sort.Strings(out[i].PhysicalPortIDs)
		for j, id := range out[i].PhysicalPortIDs {
			if !validEntityID(id) || j > 0 && id == out[i].PhysicalPortIDs[j-1] {
				return nil, apiError(400, "VALIDATION_ERROR", "Physical port IDs must be unique within each target")
			}
		}
		total += len(target.PhysicalPortIDs)
	}
	if total > 4096 {
		return nil, apiError(400, "VALIDATION_ERROR", "At most 4096 selected physical ports per batch")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].AssetID < out[j].AssetID })
	return out, nil
}

func (s *Store) wiringFactsTx(ctx context.Context, tx *sql.Tx, project string, targets []WiringTargetInput, lock bool) (*WiringFacts, error) {
	out := &WiringFacts{Version: "keel.wiring-scope/v1", ProjectID: project, Targets: []WiringHost{}, Switches: []WiringSwitch{}, Ready: true, Blockers: []string{}, EvidenceValidUntil: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)}
	block := func(code string) {
		out.Ready = false
		if !slices.Contains(out.Blockers, code) {
			out.Blockers = append(out.Blockers, code)
		}
	}
	finish := func() (*WiringFacts, error) {
		sort.Strings(out.Blockers)
		out.FactsHash = ""
		out.FactsHash = digest(string(networkJSON(out)))
		return out, nil
	}
	if !s.WiringEnabled {
		block("WIRING_DISABLED")
	}
	var e error
	out.TTLSeconds, e = networkReadinessTTLTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	var inventory []FabricDeviceSummary
	var cables []fabricmon.Cable
	out.NetworkPackageID, inventory, cables, e = fabricInventoryTx(ctx, tx, project)
	if e != nil {
		var domain *Error
		if !errors.As(e, &domain) || domain.Status != 409 {
			return nil, e
		}
		block(domain.Code)
	}
	if s.FabricProbes == nil || !s.FabricProbes.CollectorConfigured() {
		block("WIRING_COLLECTOR_REQUIRED")
	} else {
		out.CollectorHash = s.FabricProbes.Fingerprint
	}
	portsBySwitch := map[string]map[string]bool{}
	cablesByPort := map[string][]fabricmon.Cable{}
	for _, c := range cables {
		if !c.A.Switch {
			cablesByPort[c.A.UID] = append(cablesByPort[c.A.UID], c)
		}
		if !c.B.Switch {
			cablesByPort[c.B.UID] = append(cablesByPort[c.B.UID], c)
		}
	}
	selectedCables := []fabricmon.Cable{}
	selectedSwitchPorts := map[string]bool{}
	for _, target := range targets {
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		var host WiringHost
		host.AssetID = target.AssetID
		host.Ports = []WiringPort{}
		query := `SELECT a.machine_id,a.hostname,m.serial_number,a.version FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.project_id=$1 AND a.id=$2`
		if lock {
			query += " FOR UPDATE OF a,m"
		}
		e = tx.QueryRowContext(ctx, query, project, target.AssetID).Scan(&host.MachineID, &host.Hostname, &host.Serial, &host.AssetVersion)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		if e != nil {
			return nil, e
		}
		var topologyID string
		e = tx.QueryRowContext(ctx, `SELECT id FROM pci_topology_snapshots WHERE project_id=$1 AND asset_id=$2 ORDER BY version DESC LIMIT 1`, project, target.AssetID).Scan(&topologyID)
		if errors.Is(e, sql.ErrNoRows) {
			block("WIRING_TOPOLOGY_REQUIRED:" + target.AssetID)
			out.Targets = append(out.Targets, host)
			continue
		}
		if e != nil {
			return nil, e
		}
		topology, e := s.topologyDetailTx(ctx, tx, project, target.AssetID, topologyID)
		if e != nil {
			return nil, e
		}
		host.TopologyID = topology.ID
		host.TopologyScopeHash = topology.ScopeHash
		host.G1ID = topology.G1ID
		if topology.Scope != nil {
			host.G1WaiverID = topology.Scope.G1WaiverID
		}
		host.PrecollectID = topology.PrecollectID
		if topology.EffectiveVerdict != "PASS" {
			block("WIRING_CURRENT_G1_TOPOLOGY_REQUIRED:" + target.AssetID)
		}
		if topology.ValidUntil.Before(out.EvidenceValidUntil) {
			out.EvidenceValidUntil = topology.ValidUntil
		}
		var frozenRaw []byte
		if e = tx.QueryRowContext(ctx, `SELECT scope FROM validation_precollect_requests WHERE id=$1 AND project_id=$2`, host.PrecollectID, project).Scan(&frozenRaw); e != nil {
			return nil, e
		}
		var frozen PrecollectScope
		if e = json.Unmarshal(frozenRaw, &frozen); e != nil {
			return nil, e
		}
		host.BOMPackageID = frozen.BOMPackageID
		ports := map[string]WiringPort{}
		selected := map[string]bool{}
		for _, id := range target.PhysicalPortIDs {
			selected[id] = true
		}
		for _, p := range frozen.Expectation.Ports {
			if !selected[p.ID] {
				continue
			}
			var observed WiringPort
			observed.PhysicalPortID = p.ID
			observed.ComponentID = p.ComponentID
			observed.PortIndex = p.PortIndex
			observed.PermanentMAC = p.MAC
			if e = tx.QueryRowContext(ctx, `SELECT coalesce(observation->>'interface',''),coalesce(observation->>'bdf','') FROM validation_precollect_ports WHERE record_id=$1 AND physical_port_id=$2 AND component_id=$3`, host.PrecollectID, p.ID, p.ComponentID).Scan(&observed.Interface, &observed.BDF); e != nil {
				if !errors.Is(e, sql.ErrNoRows) {
					return nil, e
				}
				block("WIRING_INTERFACE_IDENTITY_REQUIRED:" + p.ID)
			}
			if observed.Interface == "" || observed.BDF == "" {
				block("WIRING_INTERFACE_IDENTITY_REQUIRED:" + p.ID)
			}
			ports[p.ID] = observed
		}
		for _, id := range target.PhysicalPortIDs {
			p, ok := ports[id]
			if !ok {
				return nil, apiError(409, "WIRING_PORT_SCOPE_CHANGED", "Selected port is outside the target's current G1/BOM")
			}
			links := cablesByPort[id]
			if len(links) != 1 {
				block("WIRING_SINGLE_BOM_CABLE_REQUIRED:" + id)
				host.Ports = append(host.Ports, p)
				continue
			}
			c := links[0]
			peer := c.B
			if c.B.UID == id {
				peer = c.A
			}
			if !peer.Switch {
				block("WIRING_SWITCH_ENDPOINT_REQUIRED:" + id)
				host.Ports = append(host.Ports, p)
				continue
			}
			p.CableID = c.UID
			p.SwitchSerial = peer.Serial
			p.SwitchChassisIDs = slices.Clone(peer.ChassisIDs)
			sort.Strings(p.SwitchChassisIDs)
			if p.SwitchChassisIDs == nil {
				p.SwitchChassisIDs = []string{}
			}
			p.SwitchPortID = peer.UID
			p.SwitchPort = peer.Port
			e = tx.QueryRowContext(ctx, `SELECT s.id,coalesce(i.rail,'') FROM physical_ports p JOIN switches s ON s.id=p.switch_id JOIN bom_items i ON i.item_uid=p.id AND i.parent_uid=s.id AND i.status<>'retired' JOIN bom_item_revisions r ON r.item_uid=p.id AND r.project_id=$1 AND r.package_id=$2 WHERE p.id=$3 AND s.serial_number=$4`, project, out.NetworkPackageID, peer.UID, peer.Serial).Scan(&p.SwitchID, &p.Rail)
			if errors.Is(e, sql.ErrNoRows) {
				block("WIRING_SWITCH_PORT_UNMODELED:" + id)
			} else if e != nil {
				return nil, e
			}
			if p.SwitchID != "" {
				if portsBySwitch[p.SwitchID] == nil {
					portsBySwitch[p.SwitchID] = map[string]bool{}
				}
				portsBySwitch[p.SwitchID][p.SwitchPort] = true
				if selectedSwitchPorts[peer.UID] {
					block("WIRING_SWITCH_PORT_AMBIGUOUS:" + peer.UID)
				}
				selectedSwitchPorts[peer.UID] = true
				selectedCables = append(selectedCables, c)
			}
			host.Ports = append(host.Ports, p)
		}
		out.Targets = append(out.Targets, host)
	}
	if len(portsBySwitch) == 0 {
		block("WIRING_MODELED_SWITCHES_REQUIRED")
		return finish()
	}
	collection, e := latestFabricCollectionTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	if collection == nil || collection.Stale || collection.EvidenceSource != "device" {
		block("WIRING_CURRENT_DEVICE_COLLECTION_REQUIRED")
	}
	if collection != nil {
		out.CollectionID = collection.ID
		if collection.ValidUntil.Before(out.EvidenceValidUntil) {
			out.EvidenceValidUntil = collection.ValidUntil
		}
	}
	known := map[string]FabricDeviceSummary{}
	for _, d := range inventory {
		known[d.ID] = d
	}
	ids := make([]string, 0, len(portsBySwitch))
	for id := range portsBySwitch {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		device, ok := known[id]
		if !ok {
			block("WIRING_SWITCH_NOT_IN_BOM:" + id)
			continue
		}
		result := WiringSwitch{ID: id, Serial: device.Serial, Ports: []string{}, NP1: networkGate("NP-1", "PASS", ""), NP2: networkGate("NP-2", "BLOCKED", "configuration_unknown"), NP4: networkGate("NP-4", "BLOCKED", "lldp_unreadable")}
		for port := range portsBySwitch[id] {
			if strings.TrimSpace(port) == "" {
				mergeNetworkGate(&result.NP1, networkGate(id, "BLOCKED", "switch_port_name_required"))
			}
			result.Ports = append(result.Ports, port)
		}
		sort.Strings(result.Ports)
		configured, e := s.networkConfigReadinessTx(ctx, tx, project, device, time.Now().UTC(), lock)
		if e != nil {
			return nil, e
		}
		result.NP2 = configured.Gate
		result.NP2.ID = "NP-2"
		result.ManagementHash = configured.ManagementHash
		result.GoldenHash = configured.GoldenHash
		if configured.ValidUntil != nil && configured.ValidUntil.Before(out.EvidenceValidUntil) {
			out.EvidenceValidUntil = *configured.ValidUntil
		}
		if collection != nil && !collection.Stale && collection.EvidenceSource == "device" {
			var sampleRaw []byte
			var sample fabricmon.Snapshot
			e = tx.QueryRowContext(ctx, `SELECT content_hash,sample FROM fabric_switch_samples WHERE run_id=$1 AND switch_id=$2`, collection.ID, id).Scan(&result.SampleHash, &sampleRaw)
			if e == nil {
				if e = json.Unmarshal(sampleRaw, &sample); e != nil {
					return nil, e
				}
				result.NP4 = networkLLDPReadiness(device.Serial, &sample, selectedCables, time.Now().UTC())
				result.NP4.EvidenceRefs = append(result.NP4.EvidenceRefs, collection.ID, result.SampleHash)
			} else if !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
		}
		for _, gate := range []NetworkGate{result.NP1, result.NP2, result.NP4} {
			if gate.State != "PASS" {
				block("WIRING_" + gate.ID + "_REQUIRED:" + id)
			}
		}
		out.Switches = append(out.Switches, result)
	}
	return finish()
}
