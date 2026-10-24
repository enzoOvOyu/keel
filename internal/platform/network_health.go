package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

type NetworkGate struct {
	ID           string   `json:"id"`
	State        string   `json:"state"`
	Reasons      []string `json:"reasons"`
	EvidenceRefs []string `json:"evidence_refs"`
}
type NetworkDeviceHealth struct {
	Device         FabricDeviceSummary      `json:"device"`
	Protocol       NetworkGate              `json:"protocol"`
	Optics         NetworkGate              `json:"optics"`
	Lossless       fabricmon.LosslessHealth `json:"lossless"`
	Source         string                   `json:"evidence_source"`
	SampleRef      string                   `json:"sample_ref"`
	Warnings       []string                 `json:"warnings"`
	AppliedRuleIDs []string                 `json:"applied_rule_ids"`
	FindingCounts  map[string]int           `json:"finding_counts"`
}
type NetworkHealthReport struct {
	FabricStorm    fabricmon.FabricStorm  `json:"fabric_storm"`
	Version        string                 `json:"version"`
	ProjectID      string                 `json:"project_id"`
	PackageID      string                 `json:"package_id"`
	ReadinessAt    time.Time              `json:"readiness_at"`
	TTLSeconds     int                    `json:"ttl_seconds"`
	ValidUntil     time.Time              `json:"valid_until"`
	Devices        []NetworkDeviceHealth  `json:"devices"`
	Lossless       fabricmon.HealthRollup `json:"lossless"`
	NS7            NetworkGate            `json:"ns7"`
	EvidenceSource string                 `json:"evidence_source"`
}
type NetworkReadiness struct {
	Version        string               `json:"version"`
	ProjectID      string               `json:"project_id"`
	ReadinessAt    time.Time            `json:"readiness_at"`
	TTLSeconds     int                  `json:"ttl_seconds"`
	ValidUntil     time.Time            `json:"valid_until"`
	G2Admission    bool                 `json:"g2_admission"`
	Gates          []NetworkGate        `json:"gates"`
	EvidenceSource string               `json:"evidence_source"`
	Health         *NetworkHealthReport `json:"health"`
}

func networkGate(id, state, reason string) NetworkGate {
	g := NetworkGate{ID: id, State: state, Reasons: []string{}, EvidenceRefs: []string{}}
	if reason != "" {
		g.Reasons = append(g.Reasons, reason)
	}
	return g
}
func mergeNetworkGate(target *NetworkGate, part NetworkGate) {
	rank := func(state string) int {
		switch state {
		case "PASS":
			return 0
		case "WARN":
			return 1
		case "FAIL":
			return 3
		default:
			return 2
		}
	}
	if rank(part.State) > rank(target.State) {
		target.State = part.State
	}
	appendUnique := func(to []string, values []string) []string {
		for _, v := range values {
			found := false
			for _, old := range to {
				if old == v {
					found = true
					break
				}
			}
			if !found {
				to = append(to, v)
			}
		}
		return to
	}
	target.Reasons = appendUnique(target.Reasons, part.Reasons)
	target.EvidenceRefs = appendUnique(target.EvidenceRefs, part.EvidenceRefs)
}
func networkOptics(sample *fabricmon.Snapshot, serial string, cables []fabricmon.Cable, limits map[string]fabricmon.Range) NetworkGate {
	result := networkGate("optics", "PASS", "")
	used := map[string]bool{}
	for _, c := range cables {
		for _, endpoint := range []fabricmon.Endpoint{c.A, c.B} {
			if endpoint.Switch && endpoint.Serial == serial {
				used[endpoint.Port] = true
			}
		}
	}
	if len(used) == 0 {
		return networkGate("optics", "BLOCKED", "modeled_connected_ports_required")
	}
	for _, port := range sample.Ports {
		if !used[port.Name] {
			continue
		}
		delete(used, port.Name)
		if port.OpticsApplicable != nil && !*port.OpticsApplicable {
			continue
		}
		state, _ := fabricmon.JudgeOptics(port.Optics, limits)
		switch state {
		case "critical":
			mergeNetworkGate(&result, networkGate(port.Name, "FAIL", "optic_critical"))
		case "warning":
			mergeNetworkGate(&result, networkGate(port.Name, "WARN", "optic_warning"))
		case "unknown":
			mergeNetworkGate(&result, networkGate(port.Name, "BLOCKED", "optic_threshold_or_sample_unknown"))
		}
	}
	if len(used) > 0 {
		mergeNetworkGate(&result, networkGate("optics", "BLOCKED", "modeled_connected_port_sample_missing"))
	}
	return result
}
func loadLosslessIntentTx(ctx context.Context, tx *sql.Tx, project string) (map[string]map[string]any, error) {
	var batch string
	e := tx.QueryRowContext(ctx, `SELECT batch_ref FROM ztp_management_assignments WHERE project_id=$1 ORDER BY batch_ref LIMIT 1`, project).Scan(&batch)
	if errors.Is(e, sql.ErrNoRows) {
		return map[string]map[string]any{}, nil
	}
	if e != nil {
		return nil, e
	}
	plan, e := currentManagementPlanTx(ctx, tx, project, batch)
	if e != nil {
		var business *Error
		if errors.As(e, &business) && business.Status == 409 {
			return map[string]map[string]any{}, nil
		}
		return nil, e
	}
	var raw []byte
	e = tx.QueryRowContext(ctx, `SELECT engineering_limits FROM network_baseline_versions WHERE id=$1 AND project_id=$2 AND state='approved'`, plan.BaselineID, project).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return map[string]map[string]any{}, nil
	}
	if e != nil {
		return nil, e
	}
	var limits NetworkEngineeringLimits
	if e = json.Unmarshal(raw, &limits); e != nil {
		return nil, e
	}
	return limits.ConfigurationVariables, nil
}
func deviceLosslessUnits(d FabricDeviceSummary, variables map[string]any) ([]fabricmon.MetricUnit, map[string]map[string]any) {
	units := []fabricmon.MetricUnit{}
	facts := map[string]map[string]any{}
	lossless, _ := variables["lossless"].(map[string]any)
	ports, _ := lossless["ports"].(map[string]any)
	names := []string{}
	for port := range ports {
		names = append(names, port)
	}
	sort.Strings(names)
	for _, port := range names {
		intent, ok := ports[port].(map[string]any)
		if !ok {
			continue
		}
		priorities, _ := intent["no_drop_priorities"].([]any)
		for _, priority := range priorities {
			value, ok := priority.(float64)
			if !ok || value != float64(int(value)) || value < 0 || value > 7 {
				continue
			}
			u := fabricmon.MetricUnit{Device: d.ID, Pod: d.Pod, Role: d.Role, Port: port, Priority: int(value)}
			u.Pool, _ = intent["pool"].(string)
			u.Queue, _ = intent["queue"].(string)
			units = append(units, u)
		}
		facts[port] = intent
	}
	if len(units) == 0 {
		units = append(units, fabricmon.MetricUnit{Device: d.ID, Pod: d.Pod, Role: d.Role, Priority: -1})
	}
	return units, facts
}
func fabricMetricIdentity(p fabricmon.MetricPoint) string {
	priority := -1
	if p.Priority != nil {
		priority = *p.Priority
	}
	return fmt.Sprintf("%s\x00%s\x00%d\x00%s\x00%s\x00%s", p.Key, p.Port, priority, p.Queue, p.Pool, p.At.Format(time.RFC3339Nano))
}
func networkHealthTx(ctx context.Context, tx *sql.Tx, project string, onlyDevice ...string) (*NetworkHealthReport, error) {
	pkg, inventory, cables, e := fabricInventoryTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	collection, e := latestFabricCollectionTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	limits, e := fabricDDMLimitsTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	variables, e := loadLosslessIntentTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	detail := len(onlyDevice) > 0 && onlyDevice[0] != ""
	if detail {
		filtered := []FabricDeviceSummary{}
		for _, d := range inventory {
			if d.ID == onlyDevice[0] {
				filtered = append(filtered, d)
			}
		}
		if len(filtered) == 0 {
			return nil, ErrNotFound
		}
		inventory = filtered
	}
	catalog, e := fabricmon.DefaultLosslessCatalog()
	if e != nil {
		return nil, e
	}
	registry, e := catalog.Registry()
	if e != nil {
		return nil, e
	}
	// Governance previews and live health use the same effective catalog weights.
	catalog.Rules, e = networkRuleDefaultsTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	catalog.Rules, e = networkLosslessRulesTx(ctx, tx, project, catalog.Rules)
	if e != nil {
		return nil, e
	}
	stormPolicy, e := fabricmon.DefaultStormPolicy()
	if e != nil {
		return nil, e
	}
	stormFindings := []fabricmon.LosslessFinding{}
	now := time.Now().UTC()
	out := &NetworkHealthReport{Version: "network-health/v1", ProjectID: project, PackageID: pkg, ReadinessAt: now, TTLSeconds: 30, ValidUntil: now.Add(30 * time.Second), Devices: []NetworkDeviceHealth{}, NS7: networkGate("N-S7", "PASS", ""), Lossless: fabricmon.HealthRollup{ID: "fabric", Score: 100, State: "healthy", Unknown: len(inventory) == 0}}
	if collection != nil {
		out.EvidenceSource = collection.EvidenceSource
		if collection.ValidUntil.Before(out.ValidUntil) {
			out.ValidUntil = collection.ValidUntil
		}
	}
	for _, d := range inventory {
		h := NetworkDeviceHealth{Device: d, Protocol: networkGate("protocol", "PASS", ""), Optics: networkGate("optics", "PASS", ""), Warnings: []string{}, Source: out.EvidenceSource, FindingCounts: map[string]int{}, AppliedRuleIDs: []string{}}
		var sample *fabricmon.Snapshot
		if collection != nil && !collection.Stale {
			var raw []byte
			e = tx.QueryRowContext(ctx, `SELECT sample,content_hash FROM fabric_switch_samples WHERE run_id=$1 AND switch_id=$2`, collection.ID, d.ID).Scan(&raw, &h.SampleRef)
			if e == nil {
				if e = json.Unmarshal(raw, &sample); e != nil {
					return nil, e
				}
			} else if !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
		}
		if sample == nil {
			h.Protocol = networkGate("protocol", "BLOCKED", "current_switch_evidence_missing")
			h.Optics = networkGate("optics", "BLOCKED", "current_switch_evidence_missing")
		} else {
			for key, value := range map[string]*bool{"bgp": sample.BGPUp, "ptp": sample.PTPLocked} {
				if !sample.Capabilities[key] || value == nil {
					mergeNetworkGate(&h.Protocol, networkGate(key, "BLOCKED", key+"_unknown"))
				} else if !*value {
					mergeNetworkGate(&h.Protocol, networkGate(key, "FAIL", key+"_not_ready"))
				}
			}
			h.Optics = networkOptics(sample, d.Serial, cables, limits)
		}
		units, intents := deviceLosslessUnits(d, variables[d.Serial])
		all := []fabricmon.LosslessFinding{}
		points := []fabricmon.MetricPoint{}
		seen := map[string]string{}
		conflict := false
		if sample != nil {
			histories, e := tx.QueryContext(ctx, `SELECT s.sample FROM fabric_switch_samples s JOIN fabric_collection_runs r ON r.id=s.run_id WHERE r.project_id=$1 AND r.package_id=$2 AND s.switch_id=$3 AND r.collected_at>now()-interval '48 hours' AND r.report->>'evidence_source'=$4 ORDER BY r.collected_at DESC LIMIT 2881`, project, pkg, d.ID, out.EvidenceSource)
			if e != nil {
				return nil, e
			}
			for histories.Next() {
				var raw []byte
				var history fabricmon.Snapshot
				if e = histories.Scan(&raw); e != nil {
					histories.Close()
					return nil, e
				}
				if e = json.Unmarshal(raw, &history); e != nil {
					histories.Close()
					return nil, e
				}
				for _, p := range history.MetricPoints {
					if p.Provider == "" {
						p.Provider = history.Provider
					}
					identity := fabricMetricIdentity(p)
					encoded := string(networkJSON(p))
					if prior, exists := seen[identity]; exists {
						conflict = conflict || prior != encoded
						continue
					}
					seen[identity] = encoded
					points = append(points, p)
					if len(points) > 100000 {
						conflict = true
						break
					}
				}
				if conflict {
					break
				}
			}
			e = histories.Err()
			histories.Close()
			if e != nil {
				return nil, e
			}
		}
		if conflict {
			points = nil
			h.Warnings = append(h.Warnings, "metric_history_conflict_or_limit")
		}
		baseEvaluation, e := fabricmon.NewMetricEvaluation(registry, points, nil, now)
		if e != nil {
			h.Warnings = append(h.Warnings, "metric_evidence_invalid")
			baseEvaluation, _ = fabricmon.NewMetricEvaluation(registry, nil, nil, now)
		}
		for _, u := range units {
			facts := map[string]any{"intended": intents[u.Port]}
			if sample != nil {
				facts["applied"] = sample.AppliedLossless[u.Port]
			}
			evaluation := *baseEvaluation
			evaluation.Facts = facts
			all = append(all, fabricmon.EvaluateLosslessUnit(&evaluation, u, catalog.Rules)...)
		}
		h.Lossless = fabricmon.RollupLossless(all, units)
		appliedRules := map[string]bool{}
		for _, f := range all {
			for _, id := range f.AppliedRules {
				appliedRules[id] = true
			}
			h.FindingCounts[f.State]++
			if f.RuleID == stormPolicy.RuleID {
				stormFindings = append(stormFindings, f)
			}
		}
		for id := range appliedRules {
			h.AppliedRuleIDs = append(h.AppliedRuleIDs, id)
		}
		sort.Strings(h.AppliedRuleIDs)
		if !detail {
			h.Lossless.Findings = []fabricmon.LosslessFinding{}
		}
		out.Lossless.Score = min(out.Lossless.Score, h.Lossless.Fabric.Score)
		out.Lossless.Unknown = out.Lossless.Unknown || h.Lossless.Fabric.Unknown
		mergeNetworkGate(&out.NS7, h.Protocol)
		mergeNetworkGate(&out.NS7, h.Optics)
		out.Devices = append(out.Devices, h)
	}
	out.FabricStorm = fabricmon.AggregateFabricStorm(stormFindings, stormPolicy)
	if len(inventory) == 0 {
		out.NS7 = networkGate("N-S7", "BLOCKED", "network_inventory_empty")
	}
	out.Lossless.State = "healthy"
	if out.Lossless.Score < 70 {
		out.Lossless.State = "critical"
	} else if out.Lossless.Score < 90 || out.Lossless.Unknown {
		out.Lossless.State = "warning"
	}
	if out.EvidenceSource != "device" {
		mergeNetworkGate(&out.NS7, networkGate("source", "BLOCKED", "device_evidence_required"))
	}
	current, _, _, e := fabricInventoryTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	if current != pkg {
		return nil, apiError(409, "FABRIC_HEALTH_BOM_CHANGED", "BOM changed while calculating health")
	}
	return out, nil
}
func (s *Store) NetworkHealth(ctx context.Context, a *AuthSession, p *ProjectSummary, trace string) (*NetworkHealthReport, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	out, e := networkHealthTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.health.view", p.ID, trace, map[string]any{"package_id": out.PackageID}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

func (s *Store) NetworkDeviceHealth(ctx context.Context, a *AuthSession, p *ProjectSummary, id, trace string) (*NetworkDeviceHealth, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	out, e := networkHealthTx(ctx, tx, p.ID, id)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.health.device.view", id, trace, map[string]any{"package_id": out.PackageID}); e != nil {
		return nil, e
	}
	return &out.Devices[0], tx.Commit()
}
