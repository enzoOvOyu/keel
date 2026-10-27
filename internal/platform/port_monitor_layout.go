package platform

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Geometry is loaded from the asset's explicit immutable version, never the
// latest model. A cell acquires a lamp only with an unambiguous physical peer.
func portMonitorPanelTx(ctx context.Context, tx *sql.Tx, project, asset string, items []PortMonitorItem) (*PortMonitorPanel, error) {
	out := &PortMonitorPanel{State: "missing", Cells: []PortMonitorPanelCell{}}
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT version.id,version.version,version.content_hash,version.panel_layout FROM project_assets asset JOIN device_model_versions version ON version.id=asset.model_version_id WHERE asset.project_id=$1 AND asset.id=$2`, project, asset).Scan(&out.ModelVersionID, &out.ModelVersion, &out.ModelContentHash, &raw)
	if err == sql.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var metadata map[string]any
	if json.Unmarshal(raw, &metadata) != nil {
		out.State = "invalid"
		return out, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT slot_no,pcie_slot,expect_nic_model,panel_port,default_role FROM device_model_slots WHERE model_version_id=$1 ORDER BY slot_no`, out.ModelVersionID)
	if err != nil {
		return nil, err
	}
	slots := []ModelSlot{}
	for rows.Next() {
		var slot ModelSlot
		if err = rows.Scan(&slot.SlotNo, &slot.PcieSlot, &slot.ExpectNicModel, &slot.PanelPort, &slot.DefaultRole); err != nil {
			rows.Close()
			return nil, err
		}
		slots = append(slots, slot)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if validateModelPanelLayout(metadata, slots) != nil {
		out.State = "invalid"
		return out, nil
	}
	layoutRaw, exists := metadata["layout_v1"]
	if !exists {
		return out, nil
	}
	var grid modelPanelLayout
	if json.Unmarshal(networkJSON(layoutRaw), &grid) != nil {
		out.State = "invalid"
		return out, nil
	}
	componentSlots := map[string]string{}
	rows, err = tx.QueryContext(ctx, `SELECT component.id,coalesce(component.slot,'') FROM components component JOIN project_assets asset ON asset.machine_id=component.machine_id WHERE asset.project_id=$1 AND asset.id=$2 AND component.category='nic'`, project, asset)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, slot string
		if err = rows.Scan(&id, &slot); err != nil {
			rows.Close()
			return nil, err
		}
		componentSlots[id] = slot
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out.State = "ready"
	out.Rows = grid.Rows
	out.Columns = grid.Columns
	out.Cells = portMonitorPanelCells(grid, slots, componentSlots, items)
	return out, nil
}

func portMonitorPanelCells(grid modelPanelLayout, slots []ModelSlot, componentSlots map[string]string, items []PortMonitorItem) []PortMonitorPanelCell {
	cells := []PortMonitorPanelCell{}
	counts := map[int]int{}
	for _, cell := range grid.Ports {
		var no int
		if json.Unmarshal(cell.SlotNo, &no) == nil {
			counts[no]++
		}
	}
	for _, cell := range grid.Ports {
		projected := PortMonitorPanelCell{ID: cell.ID, Label: cell.Label, Row: cell.Row, Column: cell.Column, Reason: "physical_binding_missing"}
		var no int
		if json.Unmarshal(cell.SlotNo, &no) == nil && counts[no] == 1 {
			for _, slot := range slots {
				if slot.SlotNo != no || slot.PcieSlot == "" {
					continue
				}
				matches := []string{}
				for _, item := range items {
					if componentSlots[item.Plan.ComponentID] == slot.PcieSlot {
						matches = append(matches, item.PhysicalPortID)
					}
				}
				if len(matches) == 1 {
					projected.PhysicalPortID = matches[0]
					projected.Reason = ""
				} else if len(matches) > 1 {
					projected.Reason = "physical_binding_ambiguous"
				}
			}
		}
		cells = append(cells, projected)
	}
	return cells
}
