package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

func (s *Store) mappingCandidateTx(ctx context.Context, tx *sql.Tx, project, asset string, lock bool) (*MappingCandidate, error) {
	out := &MappingCandidate{RuleVersion: mappingRuleVersion, ProjectID: project, AssetID: asset, Ready: true, Verdict: "PASS", Bindings: []MappingBinding{}, Conflicts: []MappingConflict{}}
	var state string
	query := `SELECT machine_id,version,coalesce(model_version_id,''),state FROM project_assets WHERE project_id=$1 AND id=$2`
	if lock {
		query += " FOR SHARE"
	}
	e := tx.QueryRowContext(ctx, query, project, asset).Scan(&out.MachineID, &out.AssetVersion, &out.ModelVersionID, &state)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	finish := func() (*MappingCandidate, error) { out.seal(); return out, nil }
	if !s.WiringEnabled {
		out.block("WIRING_DISABLED", "", "")
	}
	if state != "lldp" && state != "mapped" && state != "configured" && state != "netverified" && state != "tested" {
		out.block("MAPPING_G2_ASSET_STAGE_REQUIRED", "", "")
	}
	var topologyID string
	e = tx.QueryRowContext(ctx, `SELECT id FROM pci_topology_snapshots WHERE project_id=$1 AND asset_id=$2 ORDER BY version DESC LIMIT 1`, project, asset).Scan(&topologyID)
	if errors.Is(e, sql.ErrNoRows) {
		out.block("MAPPING_TOPOLOGY_REQUIRED", "", "")
		return finish()
	}
	if e != nil {
		return nil, e
	}
	topology, e := s.topologyDetailTx(ctx, tx, project, asset, topologyID)
	if e != nil {
		return nil, e
	}
	out.TopologyID, out.G1ID = topology.ID, topology.G1ID
	var openTopology bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM topology_issue_bindings b JOIN issues i ON i.id=b.issue_id WHERE b.project_id=$1 AND b.asset_id=$2 AND b.origin='real' AND i.state<>'closed')`, project, asset).Scan(&openTopology); e != nil {
		return nil, e
	}
	if openTopology {
		out.block("MAPPING_OPEN_TOPOLOGY_ISSUES", "", "")
	}

	out.ValidUntil = &topology.ValidUntil
	if topology.EffectiveVerdict != "PASS" || topology.Origin != "real" {
		out.block("MAPPING_CURRENT_REAL_TOPOLOGY_REQUIRED", "", "")
	}
	var frozenRaw, modelRoles []byte
	if e = tx.QueryRowContext(ctx, `SELECT scope FROM validation_precollect_requests WHERE id=$1 AND project_id=$2 AND asset_id=$3`, topology.PrecollectID, project, asset).Scan(&frozenRaw); e != nil {
		return nil, e
	}
	var frozen PrecollectScope
	if e = json.Unmarshal(frozenRaw, &frozen); e != nil {
		return nil, e
	}
	out.BOMPackageID, out.ModelContentHash = frozen.BOMPackageID, frozen.ModelContentHash
	if out.ModelVersionID != frozen.ModelVersion || out.AssetVersion != frozen.AssetVersion {
		out.block("MAPPING_ASSET_MODEL_CHANGED", "", "")
	}
	if e = tx.QueryRowContext(ctx, `SELECT default_role_plan FROM device_model_versions WHERE id=$1`, out.ModelVersionID).Scan(&modelRoles); errors.Is(e, sql.ErrNoRows) {
		out.block("MAPPING_MODEL_REQUIRED", "", "")
		return finish()
	} else if e != nil {
		return nil, e
	}
	relations, e := s.topologyRelationsTx(ctx, tx, project, asset, topologyID)
	if e != nil {
		return nil, e
	}
	if relations.EffectiveVerdict != "PASS" {
		out.block("MAPPING_MODEL_RELATIONS_REQUIRED", "", "")
	}
	rows, e := tx.QueryContext(ctx, `SELECT p.id,c.id,coalesce(c.serial_number,''),coalesce(r.row_snapshot#>>'{values,port_index}',''),coalesce(r.row_snapshot#>>'{values,port_name}',''),coalesce(r.row_snapshot#>>'{values,port_role}',''),coalesce(r.row_snapshot#>>'{values,rail}','') FROM physical_ports p JOIN components c ON c.id=p.parent_uid AND c.category='nic' JOIN bom_items i ON i.item_uid=p.id AND i.status<>'retired' JOIN bom_items parent ON parent.item_uid=c.id AND parent.status<>'retired' JOIN bom_item_revisions r ON r.item_uid=i.item_uid AND r.project_id=$1 AND r.package_id=$2 WHERE p.machine_id=$3 ORDER BY p.id`, project, frozen.BOMPackageID, out.MachineID)
	if e != nil {
		return nil, e
	}
	plans := []MappingPortPlan{}
	for rows.Next() {
		var p MappingPortPlan
		if e = rows.Scan(&p.PhysicalPortID, &p.ComponentID, &p.ComponentSerial, &p.PortIndex, &p.Name, &p.Role, &p.Rail); e != nil {
			rows.Close()
			return nil, e
		}
		plans = append(plans, p)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	// Changing the role plan, even for a previously unused port, changes scope.
	out.RolePlanVersion = digest(string(networkJSON(struct {
		ModelRoles json.RawMessage   `json:"model_roles"`
		Ports      []MappingPortPlan `json:"ports"`
	}{modelRoles, plans})))
	expected := map[string]bool{}
	for _, p := range frozen.Expectation.Ports {
		expected[p.ID] = true
	}
	for _, p := range plans {
		if !expected[p.PhysicalPortID] {
			out.block("MAPPING_PORT_OUTSIDE_G1", p.PhysicalPortID, p.ComponentID)
		}
		delete(expected, p.PhysicalPortID)
	}
	for id := range expected {
		out.block("MAPPING_BOM_PORT_REMOVED", id, "")
	}
	type observed struct {
		port, peer, confirmation string
		expected                 []byte
	}
	rows, e = tx.QueryContext(ctx, `SELECT DISTINCT ON(p.physical_port_id) p.physical_port_id,p.switch_port_id,g.id,p.expected FROM validation_wiring_confirmed_ports p JOIN validation_wiring_confirmations g ON g.id=p.confirmation_id WHERE g.project_id=$1 AND g.asset_id=$2 ORDER BY p.physical_port_id,g.created_at DESC,g.id DESC`, project, asset)
	if e != nil {
		return nil, e
	}
	proofs := map[string]observed{}
	for rows.Next() {
		var p observed
		if e = rows.Scan(&p.port, &p.peer, &p.confirmation, &p.expected); e != nil {
			rows.Close()
			return nil, e
		}
		proofs[p.port] = p
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	confirmed := map[string]MappingBinding{}
	cache := map[string]*WiringConfirmation{}
	for _, plan := range plans {
		if plan.Role == "unused" {
			continue
		}
		p, exists := proofs[plan.PhysicalPortID]
		if !exists {
			continue
		}
		g, seen := cache[p.confirmation]
		if !seen {
			g, e = s.wiringConfirmationTx(ctx, tx, project, p.confirmation)
			if e != nil {
				return nil, e
			}
			cache[p.confirmation] = g
		}
		if g.EffectiveVerdict != "PASS" {
			out.block(g.InvalidationReason, plan.PhysicalPortID, plan.ComponentID)
			continue
		}
		var port wiring.ExpectedPort
		if e = json.Unmarshal(p.expected, &port); e != nil {
			return nil, e
		}
		confirmed[p.port] = MappingBinding{Observed: port, SwitchPortID: p.peer, ConfirmationID: p.confirmation}
		if out.ValidUntil == nil || g.ValidUntil.Before(*out.ValidUntil) {
			until := g.ValidUntil
			out.ValidUntil = &until
		}
	}
	buildMappingBindings(out, plans, frozen.Components, relations, confirmed)
	return finish()
}

func (s *Store) PreviewMapping(ctx context.Context, a *AuthSession, project, asset string) (*MappingCandidate, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	out, e := s.mappingCandidateTx(ctx, tx, project, asset, false)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
