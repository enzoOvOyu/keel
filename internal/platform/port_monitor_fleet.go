package platform

import (
	"context"
	"encoding/hex"
	"sort"
	"time"
)

type PortMonitorFleetItem struct {
	AssetID           string             `json:"asset_id"`
	MappingState      string             `json:"mapping_state"`
	MappingValidUntil *time.Time         `json:"mapping_valid_until"`
	MappingRevisionID string             `json:"mapping_revision_id"`
	MappingReason     string             `json:"mapping_reason"`
	NetworkState      string             `json:"network_state"`
	Summary           PortMonitorSummary `json:"summary"`
}
type PortMonitorFleet struct {
	ProjectID string                 `json:"project_id"`
	AsOf      time.Time              `json:"as_of"`
	Items     []PortMonitorFleetItem `json:"items"`
}

// A bounded batch for visible Fleet rows; one consistent authenticated snapshot.
// The caller cannot obtain an asset from a different project by supplying its ID.
func (s *Store) PortMonitorFleet(ctx context.Context, a *AuthSession, project string, ids []string) (*PortMonitorFleet, error) {
	if len(ids) == 0 || len(ids) > 200 {
		return nil, apiError(400, "VALIDATION_ERROR", "Select 1 to 200 exact assets")
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if raw, e := hex.DecodeString(id); e != nil || len(raw) != 16 || seen[id] {
			return nil, apiError(400, "VALIDATION_ERROR", "Unique exact asset IDs required")
		}
		seen[id] = true
	}
	ids = append([]string{}, ids...)
	sort.Strings(ids)
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	// Validate the complete selected set before calculating any row.
	for _, id := range ids {
		var owned bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM project_assets WHERE project_id=$1 AND id=$2)`, project, id).Scan(&owned); e != nil {
			return nil, e
		}
		if !owned {
			return nil, ErrNotFound
		}
	}
	out := &PortMonitorFleet{ProjectID: project, AsOf: time.Now().UTC(), Items: []PortMonitorFleetItem{}}
	for _, id := range ids {
		snapshot, e := s.portMonitorSnapshotTx(ctx, tx, project, id)
		if e != nil {
			return nil, e
		}
		out.Items = append(out.Items, PortMonitorFleetItem{AssetID: id, MappingValidUntil: snapshot.MappingValidUntil, MappingState: snapshot.MappingState, MappingRevisionID: snapshot.MappingRevisionID, MappingReason: snapshot.MappingReason, NetworkState: snapshot.NetworkState, Summary: snapshot.Summary})
	}
	return out, tx.Commit()
}
