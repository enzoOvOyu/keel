package platform

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Redfish collection (WP-2.1, FR-3, mock channel per ADR-14). The real
// channel is a BMC HTTP client; here the mock serves the same behavior
// contract: levels declare scope (L1 base, L2 network, L3 hardware detail),
// the raw snapshot and the structured view are both stored, the collection
// revision increments only on success, failures are classified (and the
// caller may inject a failure class to exercise retry paths), and freshness
// is derived at read time — stale/unknown never pass a gate by themselves.

type CollectionLevel string

const (
	LevelL1 CollectionLevel = "L1"
	LevelL2 CollectionLevel = "L2"
	LevelL3 CollectionLevel = "L3"
)

type CollectInput struct {
	MachineIDs []string `json:"machine_ids"`
	Level      string   `json:"level"`
	// InjectFailure simulates a BMC-side failure for the whole batch; empty
	// means a normal successful collection. Accepts the FR-3.2 classes.
	InjectFailure string `json:"inject_failure,omitempty"`
}

type CollectResult struct {
	MachineID   string    `json:"machine_id"`
	Serial      string    `json:"serial_number"`
	Level       string    `json:"level"`
	Status      string    `json:"status"`
	FailureClass string   `json:"failure_class,omitempty"`
	Revision    int       `json:"collection_revision"`
	RecordID    string    `json:"record_id,omitempty"`
	Freshness   string    `json:"freshness,omitempty"`
	CollectedAt time.Time `json:"collected_at"`
}

type CollectionRecord struct {
	ID                    string    `json:"id"`
	MachineID             string    `json:"machine_id"`
	Serial                string    `json:"serial_number"`
	Hostname              string    `json:"hostname"`
	Level                 string    `json:"level"`
	Status                string    `json:"status"`
	FailureClass          string    `json:"failure_class"`
	Structured            string    `json:"structured"`
	CollectionRevision    int       `json:"collection_revision"`
	Freshness             string    `json:"freshness"`
	FreshnessMaxAgeSeconds int     `json:"freshness_max_age_seconds"`
	CollectedAt           time.Time `json:"collected_at"`
}

// mockRedfishSnapshot returns the raw document the simulated BMC would serve
// for the level. FR-3.1: L1 covers identity basics, L2 adds network
// interfaces, L3 adds processor/memory/storage detail.
func mockRedfishSnapshot(level CollectionLevel, serial string) (raw, structured map[string]any) {
	raw = map[string]any{
		"@odata.type": "#ComputerSystem.v1_10_0.ComputerSystem",
		"Id":          serial,
		"SerialNumber": serial,
		"Manufacturer": "mock-bmc",
	}
	structured = map[string]any{"serial_number": serial, "manufacturer": "mock-bmc"}
	if level == LevelL2 || level == LevelL3 {
		raw["EthernetInterfaces"] = map[string]any{"@odata.count": 8}
		structured["ethernet_interfaces"] = 8
	}
	if level == LevelL3 {
		raw["Processors"] = map[string]any{"@odata.count": 2}
		raw["Memory"] = map[string]any{"@odata.count": 16}
		raw["Storage"] = map[string]any{"@odata.count": 2}
		structured["processors"] = 2
		structured["memory_dimms"] = 16
		structured["storage_drives"] = 2
	}
	return raw, structured
}

var validFailureClasses = map[string]bool{"auth_failed": true, "timeout": true, "version_unsupported": true, "partial": true, "credential_expired": true}

// RunCollection performs one mock collection pass over the given machines at
// the declared level. Every machine gets a record (success or failure) so
// the failure-class statistics of FR-3.2 have real data to count.
func (s *Store) RunCollection(ctx context.Context, a *AuthSession, project *ProjectSummary, input CollectInput, trace string) ([]CollectResult, error) {
	switch CollectionLevel(input.Level) {
	case LevelL1, LevelL2, LevelL3:
	default:
		return nil, apiError(400, "VALIDATION_ERROR", "level must be L1, L2 or L3")
	}
	if input.InjectFailure != "" && !validFailureClasses[input.InjectFailure] {
		return nil, apiError(400, "VALIDATION_ERROR", "inject_failure must be an FR-3.2 failure class")
	}
	if len(input.MachineIDs) == 0 || len(input.MachineIDs) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "machine_ids must contain 1-500 entries")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	results := []CollectResult{}
	now := time.Now().UTC()
	for _, machineID := range input.MachineIDs {
		result := CollectResult{MachineID: machineID, Level: input.Level, CollectedAt: now}
		var serial string
		e := tx.QueryRowContext(ctx, `SELECT m.serial_number FROM machines m
			JOIN project_assets pa ON pa.machine_id=m.id
			WHERE m.id=$1 AND pa.project_id=$2`, machineID, project.ID).Scan(&serial)
		if e != nil {
			if errors.Is(e, sql.ErrNoRows) {
				// Machine not bound to this project: record nothing, report
				// the miss without leaking whether it exists elsewhere.
				result.Status, result.FailureClass = "failed", "partial"
				results = append(results, result)
				continue
			}
			return nil, e
		}
		result.Serial = serial
		if input.InjectFailure != "" {
			result.Status, result.FailureClass = "failed", input.InjectFailure
			if _, e = tx.ExecContext(ctx, `INSERT INTO collection_records(id,machine_id,project_id,level,status,failure_class,raw,structured,collection_revision,freshness_max_age_seconds,collected_by,collected_at)
				VALUES($1,$2,$3,$4,'failed',$5,'{}','{}',0,86400,$6,$7)`,
				RandomID(), machineID, project.ID, input.Level, input.InjectFailure, a.User.ID, now); e != nil {
				return nil, e
			}
			results = append(results, result)
			continue
		}
		raw, structured := mockRedfishSnapshot(CollectionLevel(input.Level), serial)
		var revision int
		if e = tx.QueryRowContext(ctx, `SELECT collection_revision FROM collection_records
			WHERE machine_id=$1 AND level=$2 AND status='success'
			ORDER BY collected_at DESC LIMIT 1`, machineID, input.Level).Scan(&revision); e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		revision++
		recordID := RandomID()
		if _, e = tx.ExecContext(ctx, `INSERT INTO collection_records(id,machine_id,project_id,level,status,failure_class,raw,structured,collection_revision,freshness_max_age_seconds,collected_by,collected_at)
			VALUES($1,$2,$3,$4,'success','',$5,$6,$7,86400,$8,$9)`,
			recordID, machineID, project.ID, input.Level, modelSpecJSON(raw), modelSpecJSON(structured), revision, a.User.ID, now); e != nil {
			return nil, e
		}
		result.Status, result.Revision, result.RecordID, result.Freshness = "success", revision, recordID, "fresh"
		results = append(results, result)
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "collection.run", project.ID, trace, map[string]any{
		"level": input.Level, "machines": len(input.MachineIDs), "injected_failure": input.InjectFailure,
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return results, nil
}

// freshness derives the FR-3.5 bucket from collected_at versus the budget:
// fresh < 50%, aging < 100%, stale beyond; unknown only appears when no
// successful record exists at all.
func freshness(collectedAt time.Time, maxAge time.Time, budgetSeconds int) string {
	age := maxAge.Sub(collectedAt).Seconds()
	switch {
	case age <= float64(budgetSeconds)*0.5:
		return "fresh"
	case age <= float64(budgetSeconds):
		return "aging"
	default:
		return "stale"
	}
}

// ListCollectionRecords returns the newest record per machine and level
// (both statuses) plus the freshness bucket, or "unknown" when a machine
// bound to the project has no successful record at that level.
func (s *Store) ListCollectionRecords(ctx context.Context, a *AuthSession, project *ProjectSummary, level string, p Pagination) (Page[CollectionRecord], error) {
	out := NewPage[CollectionRecord](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	switch CollectionLevel(level) {
	case LevelL1, LevelL2, LevelL3:
	default:
		return out, apiError(400, "VALIDATION_ERROR", "level must be L1, L2 or L3")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM project_assets WHERE project_id=$1`, project.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	// Newest record per machine at this level (success or failure), joined
	// onto the project's assets so untouched machines surface as unknown.
	rows, e := tx.QueryContext(ctx, `SELECT pa.machine_id, m.serial_number, pa.hostname, cr.status, cr.failure_class,
			COALESCE(cr.structured,'{}'::jsonb), COALESCE(cr.collection_revision,0), COALESCE(cr.freshness_max_age_seconds,86400), cr.collected_at
		FROM project_assets pa
		JOIN machines m ON m.id=pa.machine_id
		LEFT JOIN LATERAL (
			SELECT cr.status, cr.failure_class, cr.structured, cr.collection_revision, cr.freshness_max_age_seconds, cr.collected_at
			FROM collection_records cr
			WHERE cr.machine_id=pa.machine_id AND cr.level=$2
			ORDER BY cr.collected_at DESC LIMIT 1
		) cr ON true
		WHERE pa.project_id=$1
		ORDER BY pa.hostname LIMIT $3 OFFSET $4`, project.ID, level, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	now := time.Now().UTC()
	for rows.Next() {
		var item CollectionRecord
		var status, failureClass, structured sql.NullString
		var revision, budget int
		var collectedAt sql.NullTime
		if e = rows.Scan(&item.MachineID, &item.Serial, &item.Hostname, &status, &failureClass, &structured, &revision, &budget, &collectedAt); e != nil {
			return out, e
		}
		at := time.Time{}
		if collectedAt.Valid {
			at = collectedAt.Time
		}
		item.Status, item.FailureClass, item.Structured = status.String, failureClass.String, structured.String
		item.Level = level
		if item.Status == "" {
			item.Status, item.Freshness = "never_collected", "unknown"
		} else if item.Status == "success" {
			item.Freshness = freshness(at, now, budget)
		} else {
			item.Freshness = "unknown"
		}
		item.CollectionRevision = revision
		item.FreshnessMaxAgeSeconds = budget
		item.CollectedAt = at
		out.Items = append(out.Items, item)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.Items == nil {
		out.Items = []CollectionRecord{}
	}
	return out, nil
}
