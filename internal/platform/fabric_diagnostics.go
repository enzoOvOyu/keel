package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"strconv"
	"strings"
	"time"
)

type FabricDiagnosticsReport struct {
	RecordID       string                          `json:"record_id,omitempty"`
	CollectionID   string                          `json:"collection_id"`
	IntentSnapshot map[string]fabricmon.PortIntent `json:"intent_snapshot"`
	DDMLimits      map[string]fabricmon.Range      `json:"ddm_limits"`
	OpticsSnapshot map[string]*fabricmon.Optics    `json:"optics_snapshot"`

	Version        string                      `json:"version"`
	ProjectID      string                      `json:"project_id"`
	PackageID      string                      `json:"package_id"`
	DeviceID       string                      `json:"device_id"`
	Source         string                      `json:"source"`
	EvidenceSource string                      `json:"evidence_source"`
	Stage          string                      `json:"stage"`
	NP3            string                      `json:"np3"`
	Gate           NetworkGate                 `json:"gate"`
	CollectedAt    *time.Time                  `json:"collected_at"`
	ValidUntil     time.Time                   `json:"valid_until"`
	Policy         fabricmon.CounterPolicy     `json:"policy"`
	PolicyHash     string                      `json:"policy_hash"`
	IntentHash     string                      `json:"intent_hash"`
	Ports          []fabricmon.PortDiagnostics `json:"ports"`
	EvidenceRefs   []string                    `json:"evidence_refs"`
}

func fabricCounterPolicyTx(ctx context.Context, tx *sql.Tx, project string) (fabricmon.CounterPolicy, error) {
	out := fabricmon.CounterPolicy{}
	values, e := catalogDefaultsTx(ctx, tx, project, "fabric.precheck.counter_window_seconds", "fabric.precheck.crc_growth_max", "fabric.precheck.flap_growth_max")
	if e != nil {
		return out, e
	}
	for key, raw := range values {
		value, e := strconv.ParseUint(raw, 10, 64)
		if e != nil {
			return out, apiError(409, "FABRIC_DIAGNOSTIC_POLICY_INVALID", "Counter thresholds must be unsigned catalog values")
		}
		switch key {
		case "fabric.precheck.counter_window_seconds":
			if value < 1 || value > 3600 {
				return out, apiError(409, "FABRIC_DIAGNOSTIC_POLICY_INVALID", "Counter window must be 1 through 3600 seconds")
			}
			v := int(value)
			out.WindowSeconds = &v
		case "fabric.precheck.crc_growth_max":
			out.CRCMax = &value
		case "fabric.precheck.flap_growth_max":
			out.FlapMax = &value
		}
	}
	return out, nil
}
func modeledFabricPortsTx(ctx context.Context, tx *sql.Tx, pkg, device string) ([]string, error) {
	rows, e := tx.QueryContext(ctx, `SELECT coalesce(nullif(b.port_name,''),b.port_index,'') FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid WHERE r.package_id=$1 AND b.category='port' AND b.parent_uid=$2 AND b.status<>'retired' ORDER BY b.port_index,b.item_uid`, pkg, device)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	ports := []string{}
	for rows.Next() {
		var p string
		if e = rows.Scan(&p); e != nil {
			return nil, e
		}
		if p != "" {
			ports = append(ports, p)
		}
	}
	return ports, rows.Err()
}
func usedFabricPorts(serial string, cables []fabricmon.Cable) map[string]bool {
	used := map[string]bool{}
	for _, c := range cables {
		for _, p := range []fabricmon.Endpoint{c.A, c.B} {
			if p.Switch && p.Serial == serial {
				used[p.Port] = true
			}
		}
	}
	return used
}
func approvedFabricPortIntents(variables map[string]any) (map[string]fabricmon.PortIntent, error) {
	out := map[string]fabricmon.PortIntent{}
	rawPorts, ok := variables["interfaces"].(map[string]any)
	if !ok {
		return out, nil
	}
	for port, raw := range rawPorts {
		encoded, e := json.Marshal(raw)
		if e != nil {
			return nil, e
		}
		var intent fabricmon.PortIntent
		if e = json.Unmarshal(encoded, &intent.Configuration); e != nil {
			return nil, apiError(409, "FABRIC_PORT_INTENT_INVALID", "Approved interface intent must use normalized numeric speed/MTU and typed VLAN/PFC sets")
		}
		var rail struct {
			ID   string `json:"rail_id"`
			Leaf string `json:"leaf_serial"`
		}
		if e = json.Unmarshal(encoded, &rail); e != nil {
			return nil, e
		}
		intent.RailID = rail.ID
		intent.LeafSerial = rail.Leaf
		if len(intent.RailID) > 128 || len(intent.LeafSerial) > 256 {
			return nil, apiError(409, "FABRIC_PORT_INTENT_INVALID", "Approved rail identity exceeds limits")
		}
		if e = intent.Configuration.Validate(); e != nil {
			return nil, apiError(409, "FABRIC_PORT_INTENT_INVALID", e.Error())
		}
		out[port] = intent
	}
	return out, nil
}
func (s *Store) FabricDiagnostics(ctx context.Context, a *AuthSession, p *ProjectSummary, id, port, trace string) (*FabricDiagnosticsReport, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	out, e := s.fabricDiagnosticsTx(ctx, tx, p, id, port)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.diagnostics.read", id, trace, map[string]any{"package_id": out.PackageID, "port": port, "policy_hash": out.PolicyHash, "intent_hash": out.IntentHash, "state": out.Gate.State, "evidence_refs": out.EvidenceRefs}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
func (s *Store) fabricDiagnosticsTx(ctx context.Context, tx *sql.Tx, p *ProjectSummary, id, port string) (*FabricDiagnosticsReport, error) {
	if len(port) > 128 {
		return nil, apiError(400, "FABRIC_PORT_INVALID", "Invalid port")
	}
	pkg, devices, cables, e := fabricInventoryTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	var device *FabricDeviceSummary
	for i := range devices {
		if devices[i].ID == id {
			device = &devices[i]
			break
		}
	}
	if device == nil {
		return nil, ErrNotFound
	}
	ports, e := modeledFabricPortsTx(ctx, tx, pkg, id)
	if e != nil {
		return nil, e
	}
	if port != "" {
		found := false
		for _, name := range ports {
			if name == port {
				found = true
				break
			}
		}
		if !found {
			return nil, ErrNotFound
		}
		ports = []string{port}
	}
	policy, e := fabricCounterPolicyTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	out := &FabricDiagnosticsReport{Version: "fabric-diagnostics/v1", ProjectID: p.ID, PackageID: pkg, DeviceID: id, Source: "switch", Stage: "N-S5", NP3: "BLOCKED", Gate: networkGate("diagnostics", "PASS", ""), ValidUntil: now.Add(30 * time.Second), Policy: policy, PolicyHash: netbaseline.ContentHash(policy), Ports: []fabricmon.PortDiagnostics{}, EvidenceRefs: []string{}}
	report, e := latestFabricCollectionTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	samples := map[string]fabricmon.Snapshot{}
	hashes := map[string]string{}
	if report != nil {
		out.CollectionID = report.ID
		out.EvidenceSource = report.EvidenceSource
		if report.ValidUntil.Before(out.ValidUntil) {
			out.ValidUntil = report.ValidUntil
		}
		if !report.Stale {
			rows, e := tx.QueryContext(ctx, `SELECT s.switch_id,s.content_hash,s.sample FROM fabric_switch_samples s WHERE s.run_id=$1`, report.ID)
			if e != nil {
				return nil, e
			}
			for rows.Next() {
				var sid, hash string
				var raw []byte
				var sample fabricmon.Snapshot
				if e = rows.Scan(&sid, &hash, &raw); e != nil {
					rows.Close()
					return nil, e
				}
				if e = json.Unmarshal(raw, &sample); e != nil {
					rows.Close()
					return nil, e
				}
				if now.Sub(sample.CollectedAt) <= 5*time.Minute {
					samples[sid] = sample
					hashes[sid] = hash
				}
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return nil, e
			}
		}
	}
	current, exists := samples[id]
	var previous *fabricmon.Snapshot
	if exists {
		out.CollectedAt = &current.CollectedAt
		if current.CollectedAt.Add(5 * time.Minute).Before(out.ValidUntil) {
			out.ValidUntil = current.CollectedAt.Add(5 * time.Minute)
		}
		out.EvidenceRefs = append(out.EvidenceRefs, hashes[id])
		if policy.WindowSeconds != nil {
			var raw []byte
			var hash string
			e = tx.QueryRowContext(ctx, `SELECT s.content_hash,s.sample FROM fabric_switch_samples s JOIN fabric_collection_runs r ON r.id=s.run_id WHERE r.project_id=$1 AND r.package_id=$2 AND s.switch_id=$3 AND r.report->>'evidence_source'=$4 AND (s.sample->>'collected_at')::timestamptz<=$5 AND (s.sample->>'collected_at')::timestamptz>=$6 ORDER BY (s.sample->>'collected_at')::timestamptz DESC LIMIT 1`, p.ID, pkg, id, out.EvidenceSource, current.CollectedAt.Add(-time.Duration(*policy.WindowSeconds)*time.Second), current.CollectedAt.Add(-time.Duration(*policy.WindowSeconds)*time.Second*3/2)).Scan(&hash, &raw)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			if e == nil {
				if e = json.Unmarshal(raw, &previous); e != nil {
					return nil, e
				}
				out.EvidenceRefs = append(out.EvidenceRefs, hash)
			}
		}
	}
	variables, e := loadLosslessIntentTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	out.IntentHash = netbaseline.ContentHash(variables)
	intents, e := approvedFabricPortIntents(variables[device.Serial])
	if e != nil {
		return nil, e
	}
	out.IntentSnapshot = intents
	limits, e := fabricDDMLimitsTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	out.DDMLimits = limits
	out.OpticsSnapshot = map[string]*fabricmon.Optics{}
	used := usedFabricPorts(device.Serial, cables)
	knownSerials := map[string]bool{}
	chassisSerials := map[string]string{}
	normalizeChassis := func(v string) string { return strings.ToLower(strings.ReplaceAll(v, "-", ":")) }
	for _, d := range devices {
		knownSerials[d.Serial] = true
	}
	for _, c := range cables {
		for _, endpoint := range []fabricmon.Endpoint{c.A, c.B} {
			if endpoint.Switch {
				for _, mac := range endpoint.ChassisIDs {
					chassisSerials[normalizeChassis(mac)] = endpoint.Serial
				}
			}
		}
	}
	resolveNeighbor := func(n *fabricmon.Neighbor) string {
		if n == nil {
			return ""
		}
		serial := n.Serial
		fromMAC := chassisSerials[normalizeChassis(n.ChassisID)]
		if serial != "" {
			if !knownSerials[serial] || fromMAC != "" && fromMAC != serial {
				return ""
			}
			return serial
		}
		return fromMAC
	}
	for _, intent := range intents {
		if intent.RailID != "" && intent.LeafSerial != "" && !knownSerials[intent.LeafSerial] {
			return nil, apiError(409, "FABRIC_RAIL_INTENT_INVALID", "Approved rail Leaf must resolve to the current project BOM")
		}
	}
	for _, name := range ports {
		var peer *fabricmon.PeerObservation
		if exists {
			for _, local := range current.Ports {
				if local.Name != name || !local.LLDPReadable || local.Neighbor == nil || local.LinkUp == nil || !*local.LinkUp {
					continue
				}
				n := local.Neighbor
				// Resolve the actually observed peer against current SoT. Never compare a
				// wrongly wired link to the physical properties of its expected peer.
				for _, candidate := range devices {
					if !current.Capabilities["lldp"] || candidate.Serial != resolveNeighbor(n) {
						continue
					}
					remote, ok := samples[candidate.ID]
					if !ok {
						continue
					}
					for _, rp := range remote.Ports {
						if rp.Name == n.PortID && remote.Capabilities["lldp"] && rp.LLDPReadable && rp.Neighbor != nil && rp.Neighbor.PortID == name && resolveNeighbor(rp.Neighbor) == device.Serial && rp.LinkUp != nil && *rp.LinkUp {
							peer = &fabricmon.PeerObservation{Snapshot: remote, Port: rp, TrustedSerial: candidate.Serial}
							out.EvidenceRefs = append(out.EvidenceRefs, hashes[candidate.ID])
							break
						}
					}
				}
			}
		}
		var intent *fabricmon.PortIntent
		if v, ok := intents[name]; ok {
			copy := v
			intent = &copy
		}
		checks := fabricmon.DiagnosticPorts(current, previous, name, used[name], peer, intent, policy)
		if used[name] {
			var optics *fabricmon.Optics
			applicable := true
			for _, v := range current.Ports {
				if v.Name == name {
					optics = v.Optics
					if v.OpticsApplicable != nil {
						applicable = *v.OpticsApplicable
					}
					break
				}
			}
			out.OpticsSnapshot[name] = optics
			state, reasons := fabricmon.JudgeOptics(optics, limits)
			status := "normal"
			if !applicable {
				state = "not_applicable"
				reasons = []string{"optics_not_applicable"}
				status = "not_applicable"
			}
			if state == "unknown" {
				status = "unknown"
			}
			if state == "warning" || state == "critical" {
				status = "anomaly"
			}
			checks.Checks = append(checks.Checks, fabricmon.DiagnosticCheck{RuleRef: "PRECHECK-DDM@1", IssueType: "T3", State: status, Reason: strings.Join(reasons, ","), Observed: map[string]string{"ddm_state": state}, ThresholdRef: "fabric.ddm.*"})
		}
		out.Ports = append(out.Ports, checks)
		for _, check := range checks.Checks {
			state := "PASS"
			if check.State == "unknown" {
				state = "BLOCKED"
			} else if check.State == "anomaly" {
				state = "FAIL"
			}
			mergeNetworkGate(&out.Gate, networkGate(check.RuleRef, state, func() string {
				if state != "PASS" {
					return check.Reason
				}
				return ""
			}()))
		}
	}
	if len(used) == 0 {
		out.Gate = networkGate("diagnostics", "BLOCKED", "no_connected_ports_to_evaluate")
	}
	if len(ports) == 0 {
		out.Gate = networkGate("diagnostics", "BLOCKED", "modeled_ports_missing")
	}
	if !exists {
		out.Gate = networkGate("diagnostics", "BLOCKED", "current_switch_evidence_missing")
	}
	unique := []string{}
	seen := map[string]bool{}
	for _, ref := range out.EvidenceRefs {
		if ref != "" && !seen[ref] {
			unique = append(unique, ref)
			seen[ref] = true
		}
	}
	out.EvidenceRefs = unique
	return out, nil
}
