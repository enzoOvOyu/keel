package platform

import (
	"context"
	"time"
)

// Fleet status pills (WP-2.11, DESIGN-SYSTEM 4.12a). Two compact per-machine
// badges computed in one aggregate read for the whole page: the mapping
// column (ok when a mapping revision exists, error on unresolved T7
// topology diffs, unset before any mapping) and the network column (ok when
// every observed port is up with no open alerts or network issues, error on
// down/flapping/open issues, unknown when no fresh observation exists — the
// network column never depends on a mapping existing, per the SSOT note).

type MachineStatusPill struct {
	MachineID string `json:"machine_id"`
	Mapping   string `json:"mapping"` // ok / error / unset
	Network   string `json:"network"` // ok / error / unknown
}

type FleetStatusPills struct {
	Items []MachineStatusPill `json:"items"`
}

// GetFleetStatusPills computes the badge pair for every machine bound to
// the project (single read, page-friendly).
func (s *Store) GetFleetStatusPills(ctx context.Context, a *AuthSession, project *ProjectSummary) (*FleetStatusPills, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT machine_id FROM project_assets WHERE project_id=$1 ORDER BY hostname", project.ID)
	if e != nil {
		return nil, e
	}
	machineIDs := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		machineIDs = append(machineIDs, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	out := &FleetStatusPills{Items: []MachineStatusPill{}}
	for _, machineID := range machineIDs {
		pill := MachineStatusPill{MachineID: machineID, Mapping: "unset", Network: "unknown"}
		// Mapping column: a published revision means ok; unresolved T7 diffs
		// mean error even when a revision exists (conflict semantics).
		var mapped bool
		var t7Open int
		if e = tx.QueryRowContext(ctx, `SELECT
				EXISTS(SELECT 1 FROM mapping_revisions WHERE project_id=$1 AND machine_id=$2),
				(SELECT COUNT(*) FROM pcie_diffs WHERE project_id=$1 AND machine_id=$2 AND resolved_at IS NULL)`,
			project.ID, machineID).Scan(&mapped, &t7Open); e != nil {
			return nil, e
		}
		if t7Open > 0 {
			pill.Mapping = "error"
		} else if mapped {
			pill.Mapping = "ok"
		}
		// Network column: independent of the mapping. Latest link sample per
		// port decides ok/error; no samples at all means unknown. Open
		// network issues or alerts force error regardless of link state.
		var observed bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM mon_samples WHERE project_id=$1 AND machine_id=$2)`,
			project.ID, machineID).Scan(&observed); e != nil {
			return nil, e
		}
		var openIssues, openAlerts int
		if e = tx.QueryRowContext(ctx, `SELECT
				(SELECT COUNT(*) FROM issues WHERE project_id=$1 AND machine_id=$2 AND status NOT IN('closed') AND category<>'T7'),
				(SELECT COUNT(*) FROM mon_alerts WHERE project_id=$1 AND machine_id=$2 AND closed_at IS NULL)`,
			project.ID, machineID).Scan(&openIssues, &openAlerts); e != nil {
			return nil, e
		}
		if !observed && openIssues == 0 && openAlerts == 0 {
			pill.Network = "unknown"
		} else {
			pill.Network = "ok"
			if openIssues > 0 || openAlerts > 0 {
				pill.Network = "error"
			} else {
				// Any latest-down port flips to error.
				var downs int
				if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM (
					SELECT DISTINCT ON (port) port, value->>'state' AS state FROM mon_samples
					WHERE project_id=$1 AND machine_id=$2 AND metric='link'
					ORDER BY port, ingested_at DESC) latest WHERE state='down'`,
					project.ID, machineID).Scan(&downs); e != nil {
					return nil, e
				}
				if downs > 0 {
					pill.Network = "error"
				}
			}
		}
		out.Items = append(out.Items, pill)
	}
	return out, nil
}

// PortEvent is a single port observation for the drawer's event stream.
type PortEvent struct {
	Port      string    `json:"port"`
	Metric    string    `json:"metric"`
	Value     string    `json:"value"`
	Source    string    `json:"source"`
	IngestedAt time.Time `json:"ingested_at"`
}

// ListRecentPortEvents answers the newest monitoring samples for one
// machine (the drawer's monitoring tab).
func (s *Store) ListRecentPortEvents(ctx context.Context, a *AuthSession, project *ProjectSummary, machineID string, limit int) ([]PortEvent, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var bound bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM project_assets WHERE project_id=$1 AND machine_id=$2)", project.ID, machineID).Scan(&bound); e != nil {
		return nil, e
	}
	if !bound {
		return nil, apiError(409, "VALIDATION_ERROR", "machine not bound to this project")
	}
	rows, e := tx.QueryContext(ctx, `SELECT port,metric,value::text,source,ingested_at FROM mon_samples
		WHERE project_id=$1 AND machine_id=$2 ORDER BY ingested_at DESC LIMIT $3`, project.ID, machineID, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []PortEvent{}
	for rows.Next() {
		var event PortEvent
		if e = rows.Scan(&event.Port, &event.Metric, &event.Value, &event.Source, &event.IngestedAt); e != nil {
			return nil, e
		}
		out = append(out, event)
	}
	return out, rows.Err()
}
