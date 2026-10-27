package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/enzoOvOyu/keel/internal/pcitopology"
	"github.com/enzoOvOyu/keel/internal/precollect"
)

type TopologyScope struct {
	Version             string                   `json:"version"`
	RuleVersion         string                   `json:"rule_version"`
	ProjectID           string                   `json:"project_id"`
	AssetID             string                   `json:"asset_id"`
	MachineID           string                   `json:"machine_id"`
	PrecollectID        string                   `json:"precollect_id"`
	PrecollectSHA256    string                   `json:"precollect_sha256"`
	PrecollectScopeHash string                   `json:"precollect_scope_hash"`
	G1ID                string                   `json:"g1_id"`
	ModelVersionID      string                   `json:"model_version_id"`
	ModelContentHash    string                   `json:"model_content_hash"`
	Expectation         *pcitopology.Expectation `json:"expectation"`
	Bindings            []pcitopology.Binding    `json:"bindings"`
	ValidUntil          time.Time                `json:"valid_until"`
	ScopeHash           string                   `json:"scope_hash"`
	Ready               bool                     `json:"ready"`
	Code                string                   `json:"code"`
}

func topologyScopeHash(scope *TopologyScope) string {
	copy := *scope
	copy.ScopeHash = ""
	return digest(string(networkJSON(copy)))
}

// Current G1 selects the source record. This API cannot upload observations,
// select another machine's source or bootstrap G1 from topology success.
func (s *Store) topologyScopeTx(ctx context.Context, tx *sql.Tx, project, asset string, lock bool) (*TopologyScope, *precollect.Report, error) {
	out := &TopologyScope{Version: pcitopology.Version, RuleVersion: pcitopology.RuleVersion, ProjectID: project, AssetID: asset, Bindings: []pcitopology.Binding{}, Ready: true, Code: "TOPOLOGY_READY"}
	block := func(code string) {
		out.Ready = false
		if out.Code == "TOPOLOGY_READY" {
			out.Code = code
		}
	}
	finish := func(report *precollect.Report) (*TopologyScope, *precollect.Report, error) {
		out.ScopeHash = topologyScopeHash(out)
		return out, report, nil
	}
	var scopeRaw, reportRaw []byte
	e := tx.QueryRowContext(ctx, `SELECT g.id,g.precollect_id,g.valid_until,q.machine_id,q.scope,q.scope_hash,r.raw_sha256,r.report FROM validation_gate_evidence g JOIN validation_precollect_requests q ON q.id=g.precollect_id JOIN validation_precollect_records r ON r.id=q.id WHERE g.project_id=$1 AND g.asset_id=$2 AND g.gate='G1' ORDER BY g.created_at DESC,g.id DESC LIMIT 1`, project, asset).Scan(&out.G1ID, &out.PrecollectID, &out.ValidUntil, &out.MachineID, &scopeRaw, &out.PrecollectScopeHash, &out.PrecollectSHA256, &reportRaw)
	if errors.Is(e, sql.ErrNoRows) {
		block("TOPOLOGY_G1_REQUIRED")
		return finish(nil)
	}
	if e != nil {
		return nil, nil, e
	}
	var frozen PrecollectScope
	if e = json.Unmarshal(scopeRaw, &frozen); e != nil {
		return nil, nil, e
	}
	current, e := s.PreparePrecollectScopeTx(ctx, tx, project, asset, &frozen, lock)
	if e != nil {
		return nil, nil, e
	}
	gate, e := s.precollectGateTx(ctx, tx, project, asset, out.PrecollectID, &frozen)
	if e != nil {
		return nil, nil, e
	}
	if gate == nil || gate.Verdict != "PASS" || gate.Validity != "fresh" || !current.Ready || current.ScopeHash != out.PrecollectScopeHash {
		block("TOPOLOGY_G1_NOT_CURRENT")
		return finish(nil)
	}
	out.ModelVersionID = current.ModelVersion
	out.ModelContentHash = current.ModelContentHash
	var expectedRaw []byte
	if e = tx.QueryRowContext(ctx, `SELECT expected_topology FROM device_model_versions WHERE id=$1 AND content_hash=$2`, out.ModelVersionID, out.ModelContentHash).Scan(&expectedRaw); e != nil {
		return nil, nil, e
	}
	out.Expectation, e = pcitopology.DecodeExpectation(expectedRaw)
	if e != nil {
		block("TOPOLOGY_INDEPENDENT_MODEL_REQUIRED")
		return finish(nil)
	}
	var report precollect.Report
	if e = json.Unmarshal(reportRaw, &report); e != nil {
		return nil, nil, e
	}
	if report.Topology == nil || report.Topology.Validate() != nil {
		block("TOPOLOGY_CAPTURE_REQUIRED")
		return finish(&report)
	}
	// ModelSlot aliases are independent, immutable published facts. A numeric
	// BOM slot and its pcie_slot resolve to one model slot, never a BDF rank.
	slots := map[string]string{}
	rows, e := tx.QueryContext(ctx, `SELECT slot_no,pcie_slot FROM device_model_slots WHERE model_version_id=$1 ORDER BY slot_no`, out.ModelVersionID)
	if e != nil {
		return nil, nil, e
	}
	for rows.Next() {
		var number int
		var name string
		if e = rows.Scan(&number, &name); e != nil {
			rows.Close()
			return nil, nil, e
		}
		slots[strconv.Itoa(number)] = name
		slots[name] = name
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, nil, e
	}
	resolved := map[string]pcitopology.ExpectedDevice{}
	for _, d := range out.Expectation.Devices {
		resolved[d.PCIESlot] = d
	}
	componentBDF := map[string]string{}
	ambiguous := map[string]bool{}
	associate := func(component, bdf string) {
		if previous := componentBDF[component]; previous != "" && previous != bdf {
			ambiguous[component] = true
		}
		componentBDF[component] = bdf
	}
	for _, c := range current.Components {
		if c.Category == "gpu" && c.Serial != "" {
			for _, d := range report.Topology.Devices {
				if d.Kind == "gpu" && d.Serial != "" && d.Serial == c.Serial {
					associate(c.ID, d.BDF)
				}
			}
		}
		if c.Category == "nic" {
			for _, p := range current.Expectation.Ports {
				if p.ComponentID != c.ID {
					continue
				}
				for _, n := range report.NICs {
					if !n.VirtualFunction && n.PermanentMAC == p.MAC {
						associate(c.ID, n.BDF)
					}
				}
			}
		}
		if c.Category != "nic" && c.Category != "gpu" {
			continue
		}
		slot := c.Slot
		if alias := slots[slot]; alias != "" {
			slot = alias
		}
		expected, ok := resolved[slot]
		if !ok || expected.Kind != c.Category || componentBDF[c.ID] == "" || ambiguous[c.ID] {
			block("TOPOLOGY_COMPONENT_BINDING_REQUIRED")
			continue
		}
		out.Bindings = append(out.Bindings, pcitopology.Binding{Key: expected.Key, ComponentID: c.ID, Kind: c.Category, BDF: componentBDF[c.ID]})
	}
	if len(out.Bindings) != len(out.Expectation.Devices) {
		block("TOPOLOGY_COMPONENT_SCOPE_INCOMPLETE")
	}
	return finish(&report)
}

func (s *Store) PreviewTopology(ctx context.Context, a *AuthSession, project, asset string) (*TopologyScope, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	out, _, e := s.topologyScopeTx(ctx, tx, project, asset, false)
	return out, e
}
