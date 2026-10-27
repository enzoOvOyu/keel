package platform

import (
	"context"
	"errors"
	"slices"
	"time"
)

type TopologyFleetRow struct {
	AssetID           string     `json:"asset_id"`
	MachineID         string     `json:"machine_id"`
	Hostname          string     `json:"hostname"`
	Serial            string     `json:"serial"`
	SnapshotID        string     `json:"snapshot_id"`
	SnapshotVersion   int64      `json:"snapshot_version"`
	HistoricalVerdict string     `json:"historical_verdict"`
	EffectiveVerdict  string     `json:"effective_verdict"`
	Validity          string     `json:"validity"`
	Reason            string     `json:"reason"`
	ValidUntil        *time.Time `json:"valid_until"`
}

// Bounded project Fleet projection. Filtering precedes pagination and consumes
// current scope validity, never only the historical snapshot verdict. It does
// not replace the later unified global Fleet/ServerDetailDrawer work package.
func (s *Store) TopologyFleet(ctx context.Context, a *AuthSession, project, verdict string, page Pagination) (Page[TopologyFleetRow], error) {
	out := NewPage[TopologyFleetRow](page)
	if e := page.Validate(); e != nil {
		return out, e
	}
	if !slices.Contains([]string{"", "PASS", "FAIL", "BLOCKED", "UNAVAILABLE"}, verdict) {
		return out, apiError(400, "VALIDATION_ERROR", "Invalid topology verdict filter")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return out, e
	}
	var total int
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assets WHERE project_id=$1`, project).Scan(&total); e != nil {
		return out, e
	}
	if total > 200 {
		return out, apiError(409, "TOPOLOGY_FLEET_SCOPE_LIMIT", "Topology overview supports at most 200 project devices")
	}
	rows, e := tx.QueryContext(ctx, `SELECT a.id,a.machine_id,a.hostname,m.serial_number,coalesce(t.id,'') FROM project_assets a JOIN machines m ON m.id=a.machine_id LEFT JOIN LATERAL(SELECT id FROM pci_topology_snapshots WHERE project_id=a.project_id AND asset_id=a.id ORDER BY version DESC LIMIT 1)t ON true WHERE a.project_id=$1 ORDER BY a.hostname,a.id`, project)
	if e != nil {
		return out, e
	}
	items := []TopologyFleetRow{}
	for rows.Next() {
		var row TopologyFleetRow
		if e = rows.Scan(&row.AssetID, &row.MachineID, &row.Hostname, &row.Serial, &row.SnapshotID); e != nil {
			rows.Close()
			return out, e
		}
		items = append(items, row)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	filtered := []TopologyFleetRow{}
	for _, row := range items {
		row.EffectiveVerdict = "UNAVAILABLE"
		row.Validity = "uncollected"
		row.Reason = "TOPOLOGY_CAPTURE_REQUIRED"
		if row.SnapshotID != "" {
			snapshot, e := scanTopology(tx.QueryRowContext(ctx, `SELECT `+topologyFields+` FROM pci_topology_snapshots WHERE id=$1 AND project_id=$2 AND asset_id=$3`, row.SnapshotID, project, row.AssetID))
			if e != nil {
				return out, e
			}
			if e = s.topologyValidityTx(ctx, tx, snapshot); e != nil {
				return out, e
			}
			row.SnapshotVersion = snapshot.Version
			row.HistoricalVerdict = snapshot.Verdict
			row.EffectiveVerdict = snapshot.EffectiveVerdict
			row.Validity = snapshot.Validity
			row.Reason = snapshot.InvalidationReason
			row.ValidUntil = &snapshot.ValidUntil
		}
		if verdict == "" || row.EffectiveVerdict == verdict {
			filtered = append(filtered, row)
		}
	}
	out.Total = len(filtered)
	start := page.Offset()
	if start >= len(filtered) {
		return out, nil
	}
	end := start + page.PageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	out.Items = filtered[start:end]
	return out, nil
}

func topologyReadInvalidation(e error) (string, bool) {
	var domain *Error
	if errors.As(e, &domain) && domain.Status == 409 {
		return domain.Code, true
	}
	return "", false
}
