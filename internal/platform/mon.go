package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Monitoring data plane (WP-2.10, FR-13, mock channel). The stage-based
// policy tiers mirror the FR-13.2 table exactly; ingest accepts
// changed-items-only samples (the agent contract: unchanged data is not
// re-sent, so bandwidth scales with changes, not with fleet size); the
// flapping rule (>=3 link-down samples within 10 minutes on one port)
// opens an alert that stays until a sustained-up observation closes it;
// maintenance stage answers a fixed policy with stopped intervals and
// rejects ingest (dynamic monitoring ends after cleanup per FR-13.2).

type MonPolicy struct {
	ID                    string    `json:"id"`
	Stage                 string    `json:"stage"`
	D1D2IntervalSeconds   int       `json:"d1_d2_interval_seconds"`
	D3IntervalSeconds     int       `json:"d3_interval_seconds"`
	D4IntervalSeconds     int       `json:"d4_interval_seconds"`
	D5IntervalSeconds     int       `json:"d5_interval_seconds"`
	BurstMultiplier       int       `json:"burst_multiplier"`
	SteadyBudgetKBps      int       `json:"steady_budget_kbps"`
	BurstBudgetKBps       int       `json:"burst_budget_kbps"`
	CreatedAt             time.Time `json:"created_at"`
}

// StageTier returns the FR-13.2 intervals for a stage; D4/D5 = 0 means off.
func StageTier(stage string) (d1d2, d3, d4, d5 int) {
	switch stage {
	case "M1":
		return 300, 600, 0, 0
	case "M2":
		return 60, 300, 300, 0
	case "M3":
		return 60, 300, 300, 120
	case "M4":
		return 30, 120, 60, 60
	case "maintenance":
		return 0, 0, 0, 0
	}
	return 0, 0, 0, 0
}

type SetPolicyInput struct {
	Stage           string `json:"stage"`
	BurstMultiplier int    `json:"burst_multiplier"`
}

// SetMonPolicy creates or updates the project's policy for a stage using
// the FR-13.2 tier table (intervals are spec-fixed; only the burst
// multiplier and budgets are project-tunable).
func (s *Store) SetMonPolicy(ctx context.Context, a *AuthSession, project *ProjectSummary, input SetPolicyInput, trace string) (*MonPolicy, error) {
	switch input.Stage {
	case "M1", "M2", "M3", "M4", "maintenance":
	default:
		return nil, apiError(400, "VALIDATION_ERROR", "stage must be M1, M2, M3, M4 or maintenance")
	}
	if input.BurstMultiplier == 0 {
		input.BurstMultiplier = 1
	}
	if input.BurstMultiplier < 1 || input.BurstMultiplier > 4 {
		return nil, apiError(400, "VALIDATION_ERROR", "burst multiplier must be 1-4")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	d1d2, d3, d4, d5 := StageTier(input.Stage)
	now := time.Now().UTC()
	policy := MonPolicy{Stage: input.Stage, D1D2IntervalSeconds: d1d2, D3IntervalSeconds: d3, D4IntervalSeconds: d4, D5IntervalSeconds: d5, BurstMultiplier: input.BurstMultiplier, SteadyBudgetKBps: 50, BurstBudgetKBps: 200, CreatedAt: now}
	if _, e = tx.ExecContext(ctx, `INSERT INTO mon_policies(id,project_id,stage,d1_d2_interval_seconds,d3_interval_seconds,d4_interval_seconds,d5_interval_seconds,burst_multiplier,steady_budget_kbps,burst_budget_kbps,created_by,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT(project_id,stage) DO UPDATE SET d1_d2_interval_seconds=$4,d3_interval_seconds=$5,d4_interval_seconds=$6,d5_interval_seconds=$7,burst_multiplier=$8,steady_budget_kbps=$9,burst_budget_kbps=$10,created_by=$11,created_at=$12`,
		RandomID(), project.ID, policy.Stage, policy.D1D2IntervalSeconds, policy.D3IntervalSeconds, policy.D4IntervalSeconds, policy.D5IntervalSeconds, policy.BurstMultiplier, policy.SteadyBudgetKBps, policy.BurstBudgetKBps, a.User.ID, now); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "mon.policy.set", project.ID, trace, map[string]any{"stage": policy.Stage, "burst": policy.BurstMultiplier}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	policy.ID = "current"
	return &policy, nil
}

type IngestInput struct {
	MachineID string         `json:"machine_id"`
	Stage     string         `json:"stage"`
	Changed   []MonChange    `json:"changed"`
}

type MonChange struct {
	Port   string         `json:"port"`
	Metric string         `json:"metric"`
	Value  map[string]any `json:"value"`
}

type IngestResult struct {
	MachineID   string `json:"machine_id"`
	Accepted    int    `json:"accepted"`
	AlertOpened int    `json:"alert_opened"`
}

var flappingWindow = 10 * time.Minute
var flappingThreshold = 3

// IngestMonSamples accepts changed-items-only samples. Maintenance stage
// refuses ingest (dynamic monitoring ends after cleanup). Link-down samples
// feed the flapping rule: >=3 downs within the 10-minute window on one
// machine+port opens a mon_alert (still-open alerts are not duplicated).
func (s *Store) IngestMonSamples(ctx context.Context, a *AuthSession, project *ProjectSummary, input IngestInput, trace string) (*IngestResult, error) {
	if input.MachineID == "" || len(input.Changed) > 500 {
		return nil, apiError(400, "VALIDATION_ERROR", "machine_id and 1-500 changed items are required")
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
	// Maintenance ends dynamic monitoring.
	if input.Stage == "maintenance" {
		return nil, apiError(409, "VALIDATION_ERROR", "maintenance stage stops dynamic monitoring; ingest refused")
	}
	now := time.Now().UTC()
	result := IngestResult{MachineID: input.MachineID}
	for _, change := range input.Changed {
		value, e := json.Marshal(change.Value)
		if e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO mon_samples(id,project_id,machine_id,port,metric,value,source,ingested_at)
			VALUES($1,$2,$3,$4,$5,$6,'agent',$7)`,
			RandomID(), project.ID, input.MachineID, change.Port, change.Metric, string(value), now); e != nil {
			return nil, e
		}
		result.Accepted++
		// Flapping rule: >=3 link-down samples in the window opens an alert.
		if change.Metric == "link" && change.Value["state"] == "down" {
			var downs int
			if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM mon_samples
				WHERE project_id=$1 AND machine_id=$2 AND port=$3 AND metric='link' AND value->>'state'='down' AND ingested_at > $4`,
				project.ID, input.MachineID, change.Port, now.Add(-flappingWindow)).Scan(&downs); e != nil {
				return nil, e
			}
			if downs >= flappingThreshold {
				var openID string
				e = tx.QueryRowContext(ctx, `SELECT id FROM mon_alerts WHERE project_id=$1 AND machine_id=$2 AND port=$3 AND rule='flapping' AND closed_at IS NULL`,
					project.ID, input.MachineID, change.Port).Scan(&openID)
				if errors.Is(e, sql.ErrNoRows) {
					detail, _ := json.Marshal(map[string]any{"downs_in_window": downs, "window_minutes": 10})
					if _, e = tx.ExecContext(ctx, `INSERT INTO mon_alerts(id,project_id,machine_id,port,rule,detail,opened_at)
						VALUES($1,$2,$3,$4,'flapping',$5,$6)`,
						RandomID(), project.ID, input.MachineID, change.Port, string(detail), now); e != nil {
						return nil, e
					}
					result.AlertOpened++
				} else if e != nil {
					return nil, e
				}
			}
		}
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "mon.ingest", input.MachineID, trace, map[string]any{"changed": len(input.Changed)}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &result, nil
}

type MonAlert struct {
	ID        string     `json:"id"`
	MachineID string     `json:"machine_id"`
	Hostname  string     `json:"hostname"`
	Port      string     `json:"port"`
	Rule      string     `json:"rule"`
	Detail    string     `json:"detail"`
	OpenedAt  time.Time  `json:"opened_at"`
	ClosedAt  *time.Time  `json:"closed_at,omitempty"`
}

// ListMonAlerts returns the project's alerts, newest first.
func (s *Store) ListMonAlerts(ctx context.Context, a *AuthSession, project *ProjectSummary, p Pagination) (Page[MonAlert], error) {
	out := NewPage[MonAlert](p)
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
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM mon_alerts WHERE project_id=$1", project.ID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT a.id,a.machine_id,pa.hostname,a.port,a.rule,a.detail,a.opened_at,a.closed_at
		FROM mon_alerts a JOIN project_assets pa ON pa.machine_id=a.machine_id AND pa.project_id=a.project_id
		WHERE a.project_id=$1 ORDER BY a.opened_at DESC LIMIT $2 OFFSET $3`, project.ID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var alert MonAlert
		if e = rows.Scan(&alert.ID, &alert.MachineID, &alert.Hostname, &alert.Port, &alert.Rule, &alert.Detail, &alert.OpenedAt, &alert.ClosedAt); e != nil {
			return out, e
		}
		out.Items = append(out.Items, alert)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.Items == nil {
		out.Items = []MonAlert{}
	}
	return out, nil
}
