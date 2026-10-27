package platform

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/enzoOvOyu/keel/internal/pcitopology"
	"github.com/enzoOvOyu/keel/internal/precollect"
)

type TopologyRelationRow struct {
	GPUKey           string            `json:"gpu_key"`
	NICKey           string            `json:"nic_key"`
	GPUComponentID   string            `json:"gpu_component_id"`
	NICComponentID   string            `json:"nic_component_id"`
	GPUUUID          string            `json:"gpu_uuid"`
	PermanentMAC     string            `json:"permanent_mac"`
	PhysicalPortID   string            `json:"physical_port_id"`
	Interface        string            `json:"interface"`
	GPUNode          *pcitopology.Node `json:"gpu_node"`
	NICNode          *pcitopology.Node `json:"nic_node"`
	ExpectedAffinity string            `json:"expected_affinity"`
	ObservedAffinity string            `json:"observed_affinity"`
	ComputedAffinity string            `json:"computed_affinity"`
	Verdict          string            `json:"verdict"`
}
type TopologyRelationTable struct {
	SnapshotID        string                `json:"snapshot_id"`
	Version           int64                 `json:"version"`
	ModelVersionID    string                `json:"model_version_id"`
	G1ID              string                `json:"g1_id"`
	ScopeHash         string                `json:"scope_hash"`
	HistoricalVerdict string                `json:"historical_verdict"`
	EffectiveVerdict  string                `json:"effective_verdict"`
	Step1Published    bool                  `json:"step1_published"`
	Rows              []TopologyRelationRow `json:"rows"`
}

// This versioned relation projection is the input for future G2 Step 1. It does
// not start a G2 batch or waive its network handshake (NP-1/2/4). Associations
// are declared by the model, never chosen by NIC/GPU enumeration or proximity.
func (s *Store) TopologyRelations(ctx context.Context, a *AuthSession, project, asset, id string) (*TopologyRelationTable, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	record, e := s.topologyDetailTx(ctx, tx, project, asset, id)
	if e != nil {
		return nil, e
	}
	var scopeRaw, reportRaw []byte
	if e = tx.QueryRowContext(ctx, `SELECT q.scope,r.report FROM validation_precollect_requests q JOIN validation_precollect_records r ON r.id=q.id WHERE q.id=$1 AND q.project_id=$2 AND q.asset_id=$3`, record.PrecollectID, project, asset).Scan(&scopeRaw, &reportRaw); e != nil {
		return nil, e
	}
	var frozen PrecollectScope
	var report precollect.Report
	if e = json.Unmarshal(scopeRaw, &frozen); e != nil {
		return nil, e
	}
	if e = json.Unmarshal(reportRaw, &report); e != nil {
		return nil, e
	}
	return buildTopologyRelations(record, &frozen, &report), nil
}

func buildTopologyRelations(record *PciTopologySnapshot, frozen *PrecollectScope, report *precollect.Report) *TopologyRelationTable {
	out := &TopologyRelationTable{SnapshotID: record.ID, Version: record.Version, ModelVersionID: record.ModelVersionID, G1ID: record.G1ID, ScopeHash: record.ScopeHash, HistoricalVerdict: record.Verdict, EffectiveVerdict: record.EffectiveVerdict, Rows: []TopologyRelationRow{}}
	if record.Scope == nil || record.Scope.Expectation == nil || record.Snapshot == nil || frozen == nil || report == nil {
		out.EffectiveVerdict = "BLOCKED"
		return out
	}
	bindings := map[string]pcitopology.Binding{}
	nodes := map[string]pcitopology.Node{}
	devices := map[string]pcitopology.Device{}
	affinity := map[string]pcitopology.Affinity{}
	for _, binding := range record.Scope.Bindings {
		bindings[binding.Key] = binding
	}
	for _, node := range record.Snapshot.Nodes {
		nodes[node.BDF] = node
	}
	for _, device := range record.Snapshot.Devices {
		devices[device.BDF] = device
	}
	for _, cell := range record.Snapshot.Affinity {
		affinity[cell.From+":"+cell.To] = cell
	}
	ports := map[string][]precollect.ExpectedPort{}
	observations := map[string][]precollect.NIC{}
	for _, p := range frozen.Expectation.Ports {
		ports[p.ComponentID] = append(ports[p.ComponentID], p)
	}
	for _, n := range report.NICs {
		if !n.VirtualFunction {
			observations[n.PermanentMAC] = append(observations[n.PermanentMAC], n)
		}
	}
	for _, pair := range record.Scope.Expectation.GPUNICMap {
		gpu, nic := bindings[pair.GPU], bindings[pair.NIC]
		g, n := devices[gpu.BDF], devices[nic.BDF]
		cell := affinity[g.Label+":"+n.Label]
		base := TopologyRelationRow{GPUKey: pair.GPU, NICKey: pair.NIC, GPUComponentID: gpu.ComponentID, NICComponentID: nic.ComponentID, GPUUUID: g.Identity, ExpectedAffinity: pair.Affinity, ObservedAffinity: cell.Observed, ComputedAffinity: cell.Computed, Verdict: "PASS"}
		if node, ok := nodes[gpu.BDF]; ok {
			base.GPUNode = &node
		}
		if node, ok := nodes[nic.BDF]; ok {
			base.NICNode = &node
		}
		if base.GPUNode == nil || base.NICNode == nil || g.Kind != "gpu" || n.Kind != "nic" || gpu.ComponentID == "" || nic.ComponentID == "" || cell.Observed == "" || cell.Computed == "unknown" {
			base.Verdict = "BLOCKED"
		} else if cell.Observed != pair.Affinity {
			base.Verdict = "FAIL"
		}
		count := 0
		for _, port := range ports[nic.ComponentID] {
			for _, observed := range observations[port.MAC] {
				if observed.BDF != nic.BDF {
					continue
				}
				row := base
				row.PermanentMAC = port.MAC
				row.PhysicalPortID = port.ID
				row.Interface = observed.Name
				out.Rows = append(out.Rows, row)
				count++
			}
		}
		if count == 0 {
			base.Verdict = "BLOCKED"
			out.Rows = append(out.Rows, base)
		}
	}
	for _, row := range out.Rows {
		if record.EffectiveVerdict != "PASS" {
			break
		}
		if row.Verdict == "FAIL" {
			out.EffectiveVerdict = "FAIL"
		} else if row.Verdict != "PASS" && out.EffectiveVerdict == "PASS" {
			out.EffectiveVerdict = "BLOCKED"
		}
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if a.GPUKey != b.GPUKey {
			return a.GPUKey < b.GPUKey
		}
		if a.NICKey != b.NICKey {
			return a.NICKey < b.NICKey
		}
		return a.PhysicalPortID < b.PhysicalPortID
	})
	return out
}
