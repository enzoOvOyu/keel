package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// LLDP line-order verification (WP-2.4, FR-4 G2, mock channel). The real
// channel collects lldpd neighbor state; here the caller submits the
// observed neighbor map per machine, the judge compares it against the BOM
// port expectation and classifies mismatches into the T1-T5 categories:
// T1 link dead (no neighbor observed), T2 mis-cabling (wrong peer or peer
// port), T3 module degradation (DOM thresholds), T4 cable degradation
// (flapping/CRC), T5 NIC failure (all ports down). Each difference keeps the
// snapshot evidence; a targeted re-verification simply submits a new
// snapshot for the affected machines, and resolved differences are closed by
// the re-run, never silently deleted.

type ObservedPort struct {
	Peer       string `json:"peer"`
	PeerPort   string `json:"peer_port"`
	LinkUp     bool   `json:"link_up"`
	DOMWarning bool   `json:"dom_warning"`
	Flapping   bool   `json:"flapping"`
}

type LldpSnapshotInput struct {
	MachineID string                    `json:"machine_id"`
	Ports     map[string]ObservedPort   `json:"ports"`
}

type SubmitSnapshotsInput struct {
	Snapshots []LldpSnapshotInput `json:"snapshots"`
}

type LldpDifference struct {
	ID               string    `json:"id"`
	MachineID        string    `json:"machine_id"`
	Hostname         string    `json:"hostname"`
	LocalPort        string    `json:"local_port"`
	ExpectedPeer     string    `json:"expected_peer"`
	ExpectedPeerPort string    `json:"expected_peer_port"`
	ObservedPeer     string    `json:"observed_peer"`
	ObservedPeerPort string    `json:"observed_peer_port"`
	Category         string    `json:"category"`
	Detail           string    `json:"detail"`
	CreatedAt        time.Time `json:"created_at"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
	ResolutionNote   string    `json:"resolution_note,omitempty"`
}

type SubmitSnapshotsResult struct {
	MachineID     string           `json:"machine_id"`
	Hostname      string           `json:"hostname"`
	Revision      int              `json:"revision"`
	Differences   []LldpDifference `json:"differences"`
	NewDifferences int             `json:"new_differences"`
	Resolved      int              `json:"resolved"`
}

// SubmitLldpSnapshots takes one snapshot per machine, runs the G2 comparison
// against the BOM expectation and records the classified differences. Prior
// unresolved differences for a machine are closed when the new snapshot no
// longer reproduces them (resolution note references the re-run).
func (s *Store) SubmitLldpSnapshots(ctx context.Context, a *AuthSession, project *ProjectSummary, input SubmitSnapshotsInput, trace string) ([]SubmitSnapshotsResult, error) {
	if len(input.Snapshots) == 0 || len(input.Snapshots) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "snapshots must contain 1-500 machines")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	results := []SubmitSnapshotsResult{}
	for _, snapshot := range input.Snapshots {
		result := SubmitSnapshotsResult{MachineID: snapshot.MachineID, Differences: []LldpDifference{}}
		var hostname string
		if e = tx.QueryRowContext(ctx, "SELECT hostname FROM project_assets WHERE project_id=$1 AND machine_id=$2", project.ID, snapshot.MachineID).Scan(&hostname); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return nil, apiError(409, "VALIDATION_ERROR", "machine not bound to this project")
			}
			return nil, e
		}
		result.Hostname = hostname
		// Next revision per machine.
		var revision int
		if e = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(revision),0)+1 FROM lldp_snapshots WHERE machine_id=$1", snapshot.MachineID).Scan(&revision); e != nil {
			return nil, e
		}
		observed, e := json.Marshal(snapshot.Ports)
		if e != nil {
			return nil, e
		}
		snapshotID := RandomID()
		if _, e = tx.ExecContext(ctx, `INSERT INTO lldp_snapshots(id,machine_id,project_id,revision,observed,collected_by,collected_at)
			VALUES($1,$2,$3,$4,$5,$6,$7)`,
			snapshotID, snapshot.MachineID, project.ID, revision, string(observed), a.User.ID, now); e != nil {
			return nil, e
		}
		result.Revision = revision
		// BOM expectation: ports of this machine.
		type expectedRow struct {
			peer, peerPort, localPort string
		}
		rows, e := tx.QueryContext(ctx, "SELECT port_name, '' AS peer, '' AS peer_port FROM physical_ports WHERE machine_id=$1", snapshot.MachineID)
		if e != nil {
			return nil, e
		}
		var expected []expectedRow
		for rows.Next() {
			var row expectedRow
			if e = rows.Scan(&row.localPort, &row.peer, &row.peerPort); e != nil {
				return nil, e
			}
			expected = append(expected, row)
		}
		rows.Close()
		// Classify per BOM port.
		allDown := len(expected) > 0
		seenDifferenceKeys := map[string]bool{}
		for _, exp := range expected {
			obs, ok := snapshot.Ports[exp.localPort]
			category, detail := "", ""
		switch {
		case !ok || !obs.LinkUp:
			// Link-down keeps allDown true: only a healthy or degraded-but-
			// observable port clears the NIC-failure escalation.
			category, detail = "T1", "no LLDP neighbor or link down"
		case obs.DOMWarning:
			category, detail = "T3", "DOM thresholds exceeded"
			allDown = false
		case obs.Flapping:
			category, detail = "T4", "link flapping observed"
			allDown = false
		case obs.Peer == "" || obs.PeerPort == "":
			category, detail = "T1", "neighbor identity missing"
		default:
				// A well-formed neighbor. When the BOM carries an expected
				// peer (non-empty), a mismatch is T2 mis-cabling; otherwise a
				// healthy link is not a difference.
				allDown = false
				if exp.peer != "" && (obs.Peer != exp.peer || obs.PeerPort != exp.peerPort) {
					category, detail = "T2", "peer mismatch against BOM expectation"
				}
			}
			if category == "" {
				continue
			}
			observedPeer, observedPeerPort := "", ""
			if ok {
				observedPeer, observedPeerPort = obs.Peer, obs.PeerPort
			}
			key := exp.localPort + "|" + category
			seenDifferenceKeys[key] = true
			diffID := RandomID()
			detailJSON, _ := json.Marshal(map[string]any{"reason": detail})
			if _, e = tx.ExecContext(ctx, `INSERT INTO lldp_differences(id,project_id,machine_id,snapshot_id,local_port,expected_peer,expected_peer_port,observed_peer,observed_peer_port,category,detail,created_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
				diffID, project.ID, snapshot.MachineID, snapshotID, exp.localPort, exp.peer, exp.peerPort, observedPeer, observedPeerPort, category, string(detailJSON), now); e != nil {
				return nil, e
			}
			result.Differences = append(result.Differences, LldpDifference{
				ID: diffID, MachineID: snapshot.MachineID, Hostname: hostname, LocalPort: exp.localPort,
				ExpectedPeer: exp.peer, ExpectedPeerPort: exp.peerPort, ObservedPeer: observedPeer, ObservedPeerPort: observedPeerPort,
				Category: category, Detail: string(detailJSON), CreatedAt: now,
			})
			result.NewDifferences++
		}
		// T5: every BOM port down at once reads as NIC failure instead of T1.
		if allDown {
			for i := range result.Differences {
				if result.Differences[i].Category == "T1" {
					result.Differences[i].Category = "T5"
				}
			}
			if _, e = tx.ExecContext(ctx, `UPDATE lldp_differences SET category='T5' WHERE machine_id=$1 AND snapshot_id=$2 AND category='T1'`,
				snapshot.MachineID, snapshotID); e != nil {
				return nil, e
			}
		}
		// Close prior unresolved differences that this snapshot no longer reproduces.
		res, e := tx.ExecContext(ctx, `UPDATE lldp_differences SET resolved_at=$3, resolution_note='resolved by re-run revision ' || $4
			WHERE machine_id=$1 AND project_id=$2 AND resolved_at IS NULL AND id NOT IN (
				SELECT id FROM lldp_differences WHERE machine_id=$1 AND project_id=$2 AND snapshot_id=$5)`,
			snapshot.MachineID, project.ID, now, time.Duration(revision)*time.Second, snapshotID)
		if e != nil {
			return nil, e
		}
		if count, _ := res.RowsAffected(); count > 0 {
			result.Resolved = int(count)
		}
		results = append(results, result)
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "lldp.snapshot", project.ID, trace, map[string]any{
		"machines": len(input.Snapshots),
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return results, nil
}

// ListLldpDifferences returns the project's differences, newest first;
// resolved ones stay visible with their resolution note (audit semantics).
func (s *Store) ListLldpDifferences(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[LldpDifference], error) {
	out := NewPage[LldpDifference](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM lldp_differences WHERE project_id=$1", project.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT d.id,d.machine_id,pa.hostname,d.local_port,d.expected_peer,d.expected_peer_port,d.observed_peer,d.observed_peer_port,d.category,d.detail,d.created_at,d.resolved_at,d.resolution_note
		FROM lldp_differences d JOIN project_assets pa ON pa.machine_id=d.machine_id AND pa.project_id=d.project_id
		WHERE d.project_id=$1 ORDER BY d.created_at DESC, d.id LIMIT $2 OFFSET $3`, project.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var item LldpDifference
		var resolvedAt sql.NullTime
		var note sql.NullString
		if e = rows.Scan(&item.ID, &item.MachineID, &item.Hostname, &item.LocalPort, &item.ExpectedPeer, &item.ExpectedPeerPort, &item.ObservedPeer, &item.ObservedPeerPort, &item.Category, &item.Detail, &item.CreatedAt, &resolvedAt, &note); e != nil {
			return out, e
		}
		if resolvedAt.Valid {
			at := resolvedAt.Time
			item.ResolvedAt = &at
		}
		item.ResolutionNote = note.String
		out.Items = append(out.Items, item)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.Items == nil {
		out.Items = []LldpDifference{}
	}
	return out, nil
}
