package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Mapping revisions (WP-2.5, FR-4 G3 / FR 3.4.4). Publishing a revision is
// the only write path and it is append-only: the revision freezes every
// physical port of one machine with its OS interface (planned naming, e.g.
// nic-r1-p3), MAC, BDF, switch port and GPU affinity; a second publish for
// the same machine creates a new sequence referencing the previous
// revision (part replacement keeps history). Publishing requires a valid,
// non-expired G1 PASS/WAIVED evidence covering the machine and no
// unresolved LLDP differences on it (the line order must be confirmed
// before mapping); entries themselves never change after publish.

type MappingEntryInput struct {
	PortID      string `json:"port_id"`
	OSInterface string `json:"os_interface"`
	MACAddress  string `json:"mac_address"`
	BDF         string `json:"bdf"`
	SwitchPort  string `json:"switch_port"`
	GPUAffinity string `json:"gpu_affinity"`
}

type PublishMappingInput struct {
	MachineID string                `json:"machine_id"`
	Entries   []MappingEntryInput   `json:"entries"`
	Note      string                `json:"note"`
}

type MappingEntry struct {
	PortID      string `json:"port_id"`
	PortName    string `json:"port_name"`
	OSInterface string `json:"os_interface"`
	MACAddress  string `json:"mac_address"`
	BDF         string `json:"bdf"`
	SwitchPort  string `json:"switch_port"`
	GPUAffinity string `json:"gpu_affinity"`
}

type MappingRevision struct {
	ID            string         `json:"id"`
	Sequence      int            `json:"sequence"`
	MachineID     string         `json:"machine_id"`
	Hostname      string         `json:"hostname"`
	GateEvidenceID string       `json:"gate_evidence_id,omitempty"`
	Note          string         `json:"note"`
	PublishedBy   string         `json:"published_by"`
	PublishedAt   time.Time      `json:"published_at"`
	PreviousRevision string      `json:"previous_revision,omitempty"`
	Entries       []MappingEntry `json:"entries"`
}

// PublishMappingRevision enforces the G3 contract and appends the revision.
func (s *Store) PublishMappingRevision(ctx context.Context, a *AuthSession, project *ProjectSummary, input PublishMappingInput, trace string) (*MappingRevision, error) {
	if input.MachineID == "" || len(input.Entries) == 0 || len(input.Entries) > 64 {
		return nil, apiError(400, "VALIDATION_ERROR", "machine_id and 1-64 entries are required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	var hostname string
	if e = tx.QueryRowContext(ctx, "SELECT hostname FROM project_assets WHERE project_id=$1 AND machine_id=$2", project.ID, input.MachineID).Scan(&hostname); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(409, "VALIDATION_ERROR", "machine not bound to this project")
		}
		return nil, e
	}
	// G1 evidence must hold for this machine (PASS or WAIVED, not expired).
	evidenceID, conclusion, e := latestG1ForMachine(ctx, tx, project.ID, input.MachineID)
	if e != nil {
		return nil, e
	}
	if evidenceID == "" || (conclusion != "PASS" && conclusion != "WAIVED") {
		return nil, apiError(409, "GATE_EVIDENCE", "a valid G1 PASS (or waiver) is required before publishing a mapping")
	}
	// No unresolved line-order differences on this machine.
	var openDiffs int
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM lldp_differences WHERE project_id=$1 AND machine_id=$2 AND resolved_at IS NULL", project.ID, input.MachineID).Scan(&openDiffs); e != nil {
		return nil, e
	}
	if openDiffs > 0 {
		return nil, apiError(409, "GATE_EVIDENCE", fmt.Sprintf("%d unresolved line-order differences; confirm the line order first", openDiffs))
	}
	// Every BOM port of the machine must be covered exactly once.
	coverage := map[string]bool{}
	for _, entry := range input.Entries {
		if entry.OSInterface == "" || entry.MACAddress == "" {
			return nil, apiError(400, "VALIDATION_ERROR", "every entry needs os_interface and mac_address")
		}
		var portName string
		if e = tx.QueryRowContext(ctx, "SELECT port_name FROM physical_ports WHERE id=$1 AND machine_id=$2", entry.PortID, input.MachineID).Scan(&portName); e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				return nil, apiError(400, "VALIDATION_ERROR", "entry references a port outside this machine")
			}
			return nil, e
		}
		if coverage[entry.PortID] {
			return nil, apiError(400, "VALIDATION_ERROR", "duplicate port in entries")
		}
		coverage[entry.PortID] = true
	}
	rows, e := tx.QueryContext(ctx, "SELECT id FROM physical_ports WHERE machine_id=$1", input.MachineID)
	if e != nil {
		return nil, e
	}
	var missing []string
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		if !coverage[id] {
			missing = append(missing, id)
		}
	}
	rows.Close()
	if len(missing) > 0 {
		return nil, apiError(409, "VALIDATION_ERROR", fmt.Sprintf("mapping must cover every BOM port; %d missing", len(missing)))
	}
	// Sequence and previous revision (part replacement keeps history).
	var sequence int
	var previous sql.NullString
	if e = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(sequence),0)+1, MAX(id) FROM mapping_revisions WHERE project_id=$1 AND machine_id=$2", project.ID, input.MachineID).Scan(&sequence, &previous); e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	revision := MappingRevision{Sequence: sequence, MachineID: input.MachineID, Hostname: hostname, GateEvidenceID: evidenceID, Note: input.Note, PublishedBy: a.User.ID, PublishedAt: now, PreviousRevision: previous.String, Entries: []MappingEntry{}}
	revision.ID = RandomID()
	if _, e = tx.ExecContext(ctx, `INSERT INTO mapping_revisions(id,project_id,sequence,machine_id,gate_evidence_id,note,published_by,published_at,previous_revision)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		revision.ID, project.ID, sequence, input.MachineID, evidenceID, input.Note, a.User.ID, now, previous); e != nil {
		return nil, e
	}
	for _, entry := range input.Entries {
		var portName string
		if e = tx.QueryRowContext(ctx, "SELECT port_name FROM physical_ports WHERE id=$1", entry.PortID).Scan(&portName); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO mapping_entries(id,revision_id,port_id,os_interface,mac_address,bdf,switch_port,gpu_affinity)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8)`,
			RandomID(), revision.ID, entry.PortID, entry.OSInterface, entry.MACAddress, entry.BDF, entry.SwitchPort, entry.GPUAffinity); e != nil {
			return nil, e
		}
		revision.Entries = append(revision.Entries, MappingEntry{PortID: entry.PortID, PortName: portName, OSInterface: entry.OSInterface, MACAddress: entry.MACAddress, BDF: entry.BDF, SwitchPort: entry.SwitchPort, GPUAffinity: entry.GPUAffinity})
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "mapping.publish", revision.ID, trace, map[string]any{
		"machine": input.MachineID, "sequence": sequence, "entries": len(input.Entries), "previous": previous.String,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &revision, nil
}

func latestG1ForMachine(ctx context.Context, tx *sql.Tx, projectID, machineID string) (string, string, error) {
	rows, e := tx.QueryContext(ctx, `SELECT id, conclusion, targets FROM gate_evidences
		WHERE project_id=$1 AND gate='G1' AND valid_until > now() ORDER BY created_at DESC LIMIT 20`, projectID)
	if e != nil {
		return "", "", e
	}
	defer rows.Close()
	for rows.Next() {
		var id, conclusion, targets string
		if e = rows.Scan(&id, &conclusion, &targets); e != nil {
			return "", "", e
		}
		var list []string
		if e = json.Unmarshal([]byte(targets), &list); e != nil {
			return "", "", e
		}
		for _, target := range list {
			if target == machineID {
				return id, conclusion, nil
			}
		}
	}
	return "", "", nil
}

// ListMappingRevisions returns a machine's immutable revision history.
func (s *Store) ListMappingRevisions(ctx context.Context, a *AuthSession, project *ProjectSummary, machineID string, p Pagination) (Page[MappingRevision], error) {
	out := NewPage[MappingRevision](p)
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
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM mapping_revisions r
		JOIN project_assets pa ON pa.machine_id=r.machine_id AND pa.project_id=r.project_id
		WHERE r.project_id=$1 AND r.machine_id=$2`, project.ID, machineID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT r.id,r.sequence,r.machine_id,pa.hostname,r.gate_evidence_id,r.note,r.published_by,r.published_at,r.previous_revision
		FROM mapping_revisions r JOIN project_assets pa ON pa.machine_id=r.machine_id AND pa.project_id=r.project_id
		WHERE r.project_id=$1 AND r.machine_id=$2 ORDER BY r.sequence DESC LIMIT $3 OFFSET $4`, project.ID, machineID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	revisions := []MappingRevision{}
	for rows.Next() {
		var revision MappingRevision
		var evidenceID, previous sql.NullString
		if e = rows.Scan(&revision.ID, &revision.Sequence, &revision.MachineID, &revision.Hostname, &evidenceID, &revision.Note, &revision.PublishedBy, &revision.PublishedAt, &previous); e != nil {
			return out, e
		}
		revision.GateEvidenceID, revision.PreviousRevision = evidenceID.String, previous.String
		revision.Entries = []MappingEntry{}
		revisions = append(revisions, revision)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	rows.Close()
	// Load entries per revision in separate queries after the outer rows are
	// fully consumed (nested open rows exhausted connections under load).
	for i := range revisions {
		entryRows, e := tx.QueryContext(ctx, `SELECT e.port_id,p.port_name,e.os_interface,e.mac_address,e.bdf,e.switch_port,e.gpu_affinity
			FROM mapping_entries e JOIN physical_ports p ON p.id=e.port_id WHERE e.revision_id=$1`, revisions[i].ID)
		if e != nil {
			return out, e
		}
		for entryRows.Next() {
			var entry MappingEntry
			if e = entryRows.Scan(&entry.PortID, &entry.PortName, &entry.OSInterface, &entry.MACAddress, &entry.BDF, &entry.SwitchPort, &entry.GPUAffinity); e != nil {
				entryRows.Close()
				return out, e
			}
			revisions[i].Entries = append(revisions[i].Entries, entry)
		}
		if e = entryRows.Err(); e != nil {
			entryRows.Close()
			return out, e
		}
		entryRows.Close()
		out.Items = append(out.Items, revisions[i])
	}
	if out.Items == nil {
		out.Items = []MappingRevision{}
	}
	return out, nil
}

// PhysicalPortView exposes a machine's BOM ports (FR-2.1: ports are
// referenceable before G3; the mapping UI and publish input need their ids).
type PhysicalPortView struct {
	ID        string `json:"id"`
	PortName  string `json:"port_name"`
	MACAddress string `json:"mac_address"`
	Rail      int    `json:"rail"`
}

// ListMachinePorts returns the machine's physical ports for project members.
func (s *Store) ListMachinePorts(ctx context.Context, a *AuthSession, project *ProjectSummary, machineID string) ([]PhysicalPortView, error) {
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
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(409, "VALIDATION_ERROR", "machine not bound to this project")
		}
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT id,port_name,mac_address,rail FROM physical_ports WHERE machine_id=$1 ORDER BY port_name", machineID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []PhysicalPortView{}
	for rows.Next() {
		var port PhysicalPortView
		if e = rows.Scan(&port.ID, &port.PortName, &port.MACAddress, &port.Rail); e != nil {
			return nil, e
		}
		out = append(out, port)
	}
	return out, rows.Err()
}
