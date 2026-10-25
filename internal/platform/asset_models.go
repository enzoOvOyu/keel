package platform

import (
	"context"
	"database/sql"
	"errors"
)

// AssetModelBinding is a read of the asset's pinned version, never model latest
// or the machine's current projection. Retired model histories remain readable.
type AssetModelBinding struct {
	AssetID      string                   `json:"asset_id"`
	MachineID    string                   `json:"machine_id"`
	AssetVersion int64                    `json:"asset_version"`
	Model        DeviceModelSummary       `json:"model"`
	ModelVersion DeviceModelVersionDetail `json:"model_version"`
}

func (s *Store) GetAssetModelBinding(ctx context.Context, a *AuthSession, project *ProjectSummary, assetID string) (*AssetModelBinding, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	var out AssetModelBinding
	m, v := &out.Model, &out.ModelVersion
	err = tx.QueryRowContext(ctx, `SELECT pa.id,pa.machine_id,pa.version,
 m.id,m.scope_key,m.name,m.kind,m.state,(SELECT coalesce(max(version),0) FROM device_model_versions WHERE model_id=m.id),m.created_at,m.updated_at,
 v.id,v.model_id,v.version,v.hw_spec,v.expected_topology,v.capability_matrix,v.panel_layout,v.bom_expect,v.default_role_plan,v.content_hash,v.published_at
 FROM project_assets pa JOIN device_models m ON m.id=pa.model_id JOIN device_model_versions v ON v.id=pa.model_version_id AND v.model_id=m.id
 WHERE pa.id=$1 AND pa.project_id=$2 AND (m.scope_key='global' OR m.scope_key=$3)`, assetID, project.ID, "project:"+project.ID).Scan(&out.AssetID, &out.MachineID, &out.AssetVersion, &m.ID, &m.ScopeKey, &m.Name, &m.Kind, &m.State, &m.Version, &m.CreatedAt, &m.UpdatedAt, &v.ID, &v.ModelID, &v.Version, &v.HwSpec, &v.ExpectedTopology, &v.CapabilityMatrix, &v.PanelLayout, &v.BomExpect, &v.DefaultRolePlan, &v.ContentHash, &v.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT slot_no,pcie_slot,expect_nic_model,panel_port,default_role FROM device_model_slots WHERE model_version_id=$1 ORDER BY slot_no`, v.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	v.Slots = []ModelSlot{}
	for rows.Next() {
		var slot ModelSlot
		if err = rows.Scan(&slot.SlotNo, &slot.PcieSlot, &slot.ExpectNicModel, &slot.PanelPort, &slot.DefaultRole); err != nil {
			return nil, err
		}
		v.Slots = append(v.Slots, slot)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return &out, nil
}
