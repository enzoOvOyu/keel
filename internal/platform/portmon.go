package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// Port panel (WP-2.6, FR-4 3.4.5 PortModel). A read-only query view around
// the stable PhysicalPort ID: each port answers its planned OS interface
// (from the latest mapping revision), its latest observed link state and
// DOM summary (from the monitoring samples, with source and timestamp —
// unknown stays unknown with its provenance, never fabricated), its LLDP
// peer (from the latest healthy snapshot), and its alert state (open
// flapping alerts). Maintenance machines keep their last data with the
// panel marked stopped, per the FR-13.2 maintenance semantics.

type PortPanelEntry struct {
	PortID      string     `json:"port_id"`
	PortName    string     `json:"port_name"`
	MACAddress  string     `json:"mac_address"`
	Rail        int        `json:"rail"`
	OSInterface string     `json:"os_interface,omitempty"`
	LinkState   string     `json:"link_state"` // up / down / unknown
	LinkSource  string     `json:"link_source,omitempty"`
	LinkAt      *time.Time `json:"link_at,omitempty"`
	DOMSummary  string     `json:"dom_summary,omitempty"`
	DOMSource   string     `json:"dom_source,omitempty"`
	LLDPPeer    string     `json:"lldp_peer,omitempty"`
	LLDPAt      *time.Time `json:"lldp_at,omitempty"`
	AlertRule   string     `json:"alert_rule,omitempty"`
	// NettestFailure is the newest failed connectivity pair touching this
	// port (directly or through its rail's L5 result): the failed path's
	// ports light up red in the panel (FR-11.4).
	NettestFailure string     `json:"nettest_failure,omitempty"`
	NettestAt      *time.Time `json:"nettest_at,omitempty"`
}

type PortPanel struct {
	MachineID string           `json:"machine_id"`
	Hostname  string           `json:"hostname"`
	Monitored bool             `json:"monitored"`
	Ports     []PortPanelEntry `json:"ports"`
}

// GetPortPanel aggregates one machine's port panel for project members.
func (s *Store) GetPortPanel(ctx context.Context, a *AuthSession, project *ProjectSummary, machineID string) (*PortPanel, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var hostname string
	if e = tx.QueryRowContext(ctx, "SELECT hostname FROM project_assets WHERE project_id=$1 AND machine_id=$2", project.ID, machineID).Scan(&hostname); e != nil {
		if e == sql.ErrNoRows {
			return nil, apiError(409, "VALIDATION_ERROR", "machine not bound to this project")
		}
		return nil, e
	}
	panel := &PortPanel{MachineID: machineID, Hostname: hostname, Ports: []PortPanelEntry{}}
	// Any monitoring samples at all => the machine is under dynamic monitoring.
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM mon_samples WHERE project_id=$1 AND machine_id=$2)", project.ID, machineID).Scan(&panel.Monitored); e != nil {
		return nil, e
	}
	// Ports of the machine.
	rows, e := tx.QueryContext(ctx, "SELECT id,port_name,mac_address,rail FROM physical_ports WHERE machine_id=$1 ORDER BY port_name", machineID)
	if e != nil {
		return nil, e
	}
	type portRow struct {
		id, name, mac string
		rail          int
	}
	var ports []portRow
	for rows.Next() {
		var p portRow
		if e = rows.Scan(&p.id, &p.name, &p.mac, &p.rail); e != nil {
			rows.Close()
			return nil, e
		}
		ports = append(ports, p)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	for _, p := range ports {
		entry := PortPanelEntry{PortID: p.id, PortName: p.name, MACAddress: p.mac, Rail: p.rail, LinkState: "unknown"}
		// Planned OS interface from the latest mapping revision.
		_ = tx.QueryRowContext(ctx, `SELECT e.os_interface FROM mapping_entries e
			JOIN mapping_revisions r ON r.id=e.revision_id
			WHERE r.project_id=$1 AND r.machine_id=$2 AND e.port_id=$3
			ORDER BY r.sequence DESC LIMIT 1`, project.ID, machineID, p.id).Scan(&entry.OSInterface)
		// Latest link sample (state + source + time; unknown keeps provenance).
		var state, source string
		var at time.Time
		var hasLink bool
		if e = tx.QueryRowContext(ctx, `SELECT value->>'state', source, ingested_at FROM mon_samples
			WHERE project_id=$1 AND machine_id=$2 AND port=$3 AND metric='link'
			ORDER BY ingested_at DESC LIMIT 1`, project.ID, machineID, p.name).Scan(&state, &source, &at); e == nil {
			entry.LinkState, entry.LinkSource, entry.LinkAt, hasLink = state, source, &at, true
		} else if e != sql.ErrNoRows {
			return nil, e
		}
		_ = hasLink
		// Latest DOM summary.
		var dom string
		var domAt time.Time
		if e = tx.QueryRowContext(ctx, `SELECT value::text, ingested_at FROM mon_samples
			WHERE project_id=$1 AND machine_id=$2 AND port=$3 AND metric='dom'
			ORDER BY ingested_at DESC LIMIT 1`, project.ID, machineID, p.name).Scan(&dom, &domAt); e == nil {
			entry.DOMSummary, entry.DOMSource, _ = dom, "agent", &domAt
		} else if e != sql.ErrNoRows {
			return nil, e
		}
		// LLDP peer from the latest snapshot revision.
		var observed string
		if e = tx.QueryRowContext(ctx, `SELECT observed FROM lldp_snapshots
			WHERE project_id=$1 AND machine_id=$2 ORDER BY revision DESC LIMIT 1`, project.ID, machineID).Scan(&observed); e == nil {
			entry.LLDPPeer, entry.LLDPAt = extractPeer(observed, p.name), snapshotTime(tx, project.ID, machineID)
		} else if e != sql.ErrNoRows {
			return nil, e
		}
		// Open alert on this port.
		var rule string
		if e = tx.QueryRowContext(ctx, `SELECT rule FROM mon_alerts
			WHERE project_id=$1 AND machine_id=$2 AND port=$3 AND closed_at IS NULL
			ORDER BY opened_at DESC LIMIT 1`, project.ID, machineID, p.name).Scan(&rule); e == nil {
			entry.AlertRule = rule
		} else if e != sql.ErrNoRows {
			return nil, e
		}
		// Newest failed connectivity pair on this port — a direct pair
		// failure or the rail-level L5 result anchored on the same rail.
		var layer, classification string
		var measuredAt time.Time
		if e = tx.QueryRowContext(ctx, `SELECT p.layer, p.classification, MAX(p.measured_at) FROM nettest_pairs p
			LEFT JOIN physical_ports anchor ON anchor.id=p.port_id
			WHERE p.project_id=$1 AND p.state='fail'
				AND (p.port_id=$2 OR p.peer_port_id=$2 OR (p.layer='L5' AND p.machine_id=$3 AND anchor.rail=$4))
			GROUP BY p.layer, p.classification ORDER BY MAX(p.measured_at) DESC LIMIT 1`,
			project.ID, p.id, machineID, p.rail).Scan(&layer, &classification, &measuredAt); e == nil {
			entry.NettestFailure = layer + ":" + classification
			entry.NettestAt = &measuredAt
		} else if e != sql.ErrNoRows {
			return nil, e
		}
		panel.Ports = append(panel.Ports, entry)
	}
	return panel, nil
}

// extractPeer pulls the peer identity for one port out of a snapshot's
// observed JSON ({"port1":{"peer":"sw1","peer_port":"Eth1/1",...}}).
func extractPeer(observedJSON, portName string) string {
	var observed map[string]struct {
		Peer     string `json:"peer"`
		PeerPort string `json:"peer_port"`
	}
	if json.Unmarshal([]byte(observedJSON), &observed) != nil {
		return ""
	}
	if entry, ok := observed[portName]; ok && entry.Peer != "" {
		return entry.Peer + " " + entry.PeerPort
	}
	return ""
}

func snapshotTime(tx *sql.Tx, projectID, machineID string) *time.Time {
	var at time.Time
	if tx.QueryRowContext(context.Background(), `SELECT collected_at FROM lldp_snapshots
		WHERE project_id=$1 AND machine_id=$2 ORDER BY revision DESC LIMIT 1`, projectID, machineID).Scan(&at) == nil {
		return &at
	}
	return nil
}
