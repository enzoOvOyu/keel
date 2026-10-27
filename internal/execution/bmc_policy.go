package execution

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/schedule"
)

type BMCPolicyInput struct {
	Level             string `json:"level"`
	TriggerKind       string `json:"trigger_kind"`
	CronExpression    string `json:"cron_expression"`
	EventType         string `json:"event_type"`
	Enabled           bool   `json:"enabled"`
	MaxRetries        int    `json:"max_retries"`
	RetryDelaySeconds int    `json:"retry_delay_seconds"`
	ExpectedVersion   int64  `json:"expected_version"`
	Reason            string `json:"reason"`
}
type BMCPolicy struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	AssetID           string     `json:"asset_id"`
	Level             string     `json:"level"`
	TriggerKind       string     `json:"trigger_kind"`
	CronExpression    string     `json:"cron_expression"`
	EventType         string     `json:"event_type"`
	Enabled           bool       `json:"enabled"`
	StateVersion      int64      `json:"state_version"`
	MaxRetries        int        `json:"max_retries"`
	RetryDelaySeconds int        `json:"retry_delay_seconds"`
	NextDue           *time.Time `json:"next_due"`
	LastEventSequence int64      `json:"last_event_sequence"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}
type BMCExecutionTrigger struct {
	PolicyID      string `json:"policy_id"`
	PolicyVersion int64  `json:"policy_version"`
	RunID         string `json:"run_id"`
	TriggerKey    string `json:"trigger_key"`
	SourceRef     string `json:"source_ref"`
	ParentRunID   string `json:"parent_run_id,omitempty"`
	AttemptNo     int    `json:"attempt_no"`
}
type BMCPolicyRun struct {
	ID             string    `json:"id"`
	PolicyID       string    `json:"policy_id"`
	ProjectID      string    `json:"project_id"`
	PolicyVersion  int64     `json:"policy_version"`
	PolicySnapshot BMCPolicy `json:"policy_snapshot"`
	TriggerKey     string    `json:"trigger_key"`
	TriggerKind    string    `json:"trigger_kind"`
	SourceRef      string    `json:"source_ref"`
	ParentRunID    *string   `json:"parent_run_id"`
	AttemptNo      int       `json:"attempt_no"`
	ExecutionID    *string   `json:"execution_id"`
	State          string    `json:"state"`
	Code           string    `json:"code"`
	ScheduledAt    time.Time `json:"scheduled_at"`
	CreatedAt      time.Time `json:"created_at"`
}

const bmcPolicyFields = `id,project_id,asset_id,level,trigger_kind,cron_expression,event_type,enabled,state_version,max_retries,retry_delay_seconds,next_due,last_event_sequence,created_by,created_at,updated_at`

func scanBMCPolicy(row interface{ Scan(...any) error }) (*BMCPolicy, error) {
	var p BMCPolicy
	e := row.Scan(&p.ID, &p.ProjectID, &p.AssetID, &p.Level, &p.TriggerKind, &p.CronExpression, &p.EventType, &p.Enabled, &p.StateVersion, &p.MaxRetries, &p.RetryDelaySeconds, &p.NextDue, &p.LastEventSequence, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	return &p, e
}
func validateBMCPolicy(in BMCPolicyInput, now time.Time) (*time.Time, error) {
	if (in.Level != "L1" && in.Level != "L2" && in.Level != "L3") || in.MaxRetries < 0 || in.MaxRetries > 3 || in.RetryDelaySeconds < 30 || in.RetryDelaySeconds > 3600 || len(strings.TrimSpace(in.Reason)) < 3 || len(in.Reason) > 1000 {
		return nil, invalid("Invalid BMC policy level, retry bounds or reason")
	}
	switch in.TriggerKind {
	case "cron":
		if in.EventType != "" {
			return nil, invalid("Cron policy cannot select an event")
		}
		c, e := schedule.Parse(in.CronExpression)
		if e != nil {
			return nil, invalid("A bounded five-field UTC cron expression is required")
		}
		n, e := c.Next(now)
		if e != nil {
			return nil, invalid("Cron expression has no occurrence within five years")
		}
		if in.Enabled {
			return &n, nil
		}
		return nil, nil
	case "event":
		if in.CronExpression != "" {
			return nil, invalid("Event policy cannot contain a cron expression")
		}
		switch in.EventType {
		case "bom.committed", "credential.batch.rotated", "asset.provisioned":
			return nil, nil
		}
	}
	return nil, invalid("Unsupported BMC inventory trigger")
}
func bmcPolicyAssetScope(ctx context.Context, tx *sql.Tx, project, asset string) error {
	var id string
	e := tx.QueryRowContext(ctx, `SELECT id FROM project_assets WHERE project_id=$1 AND id=$2 FOR SHARE`, project, asset).Scan(&id)
	if errors.Is(e, sql.ErrNoRows) {
		return platform.ErrNotFound
	}
	return e
}
func (s *Service) SaveBMCPolicy(ctx context.Context, a *Actor, project, asset, id string, in BMCPolicyInput, key, trace string) (*BMCPolicy, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	next, e := validateBMCPolicy(in, now)
	if e != nil {
		return nil, e
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = advisory(ctx, tx, "keel-admin-initialization"); e != nil {
		return nil, e
	}
	if e = authorize(ctx, tx, a, project, "Admin", "DE", "DL", "SRE"); e != nil {
		return nil, e
	}
	if e = bmcPolicyAssetScope(ctx, tx, project, asset); e != nil {
		return nil, e
	}
	scope := "bmc.policy:" + project + ":" + asset + ":" + id
	idem, prior, e := idempotency(ctx, tx, a, scope, key, in)
	if e != nil {
		return nil, e
	}
	if prior != nil {
		out, e := decode[BMCPolicy](prior)
		return &out, e
	}
	var out *BMCPolicy
	if id == "" {
		if in.ExpectedVersion != 0 {
			return nil, conflict("New policy requires expected_version=0")
		}
		if e = advisory(ctx, tx, "bmc-policy-asset:"+asset); e != nil {
			return nil, e
		}
		var count int
		if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bmc_collection_policies WHERE project_id=$1 AND asset_id=$2`, project, asset).Scan(&count); e != nil {
			return nil, e
		}
		if count >= 12 {
			return nil, conflict("At most twelve inventory policies per asset")
		}
		out = &BMCPolicy{ID: platform.RandomID(), ProjectID: project, AssetID: asset, StateVersion: 1, CreatedBy: a.User.ID, CreatedAt: now}
	} else {
		out, e = scanBMCPolicy(tx.QueryRowContext(ctx, `SELECT `+bmcPolicyFields+` FROM bmc_collection_policies WHERE id=$1 AND project_id=$2 AND asset_id=$3 FOR UPDATE`, id, project, asset))
		if errors.Is(e, sql.ErrNoRows) {
			return nil, platform.ErrNotFound
		}
		if e != nil {
			return nil, e
		}
		if out.StateVersion != in.ExpectedVersion {
			return nil, conflict("Inventory policy changed; refresh its version")
		}
		if out.CreatedBy != a.User.ID && !a.User.IsAdmin {
			var role string
			if e = tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2`, project, a.User.ID).Scan(&role); e != nil {
				return nil, e
			}
			if role != "Admin" && role != "DL" && role != "SRE" {
				return nil, platform.ErrForbidden
			}
		}
		out.StateVersion++
	}
	// Edits establish a new trigger revision. No historical event backfill and no
	// stale retry settings survive the new state version.
	if e = tx.QueryRowContext(ctx, `SELECT coalesce(max(sequence),0) FROM bmc_inventory_events WHERE project_id=$1`, project).Scan(&out.LastEventSequence); e != nil {
		return nil, e
	}
	out.Level = in.Level
	out.TriggerKind = in.TriggerKind
	out.CronExpression = in.CronExpression
	out.EventType = in.EventType
	out.Enabled = in.Enabled
	out.MaxRetries = in.MaxRetries
	out.RetryDelaySeconds = in.RetryDelaySeconds
	out.NextDue = next
	out.UpdatedAt = now
	_, e = tx.ExecContext(ctx, `INSERT INTO bmc_collection_policies(`+bmcPolicyFields+`) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT(id) DO UPDATE SET level=excluded.level,trigger_kind=excluded.trigger_kind,cron_expression=excluded.cron_expression,event_type=excluded.event_type,enabled=excluded.enabled,state_version=excluded.state_version,max_retries=excluded.max_retries,retry_delay_seconds=excluded.retry_delay_seconds,next_due=excluded.next_due,last_event_sequence=excluded.last_event_sequence,updated_at=excluded.updated_at`, out.ID, project, asset, out.Level, out.TriggerKind, out.CronExpression, out.EventType, out.Enabled, out.StateVersion, out.MaxRetries, out.RetryDelaySeconds, out.NextDue, out.LastEventSequence, out.CreatedBy, out.CreatedAt, out.UpdatedAt)
	if e != nil {
		return nil, e
	}
	e = audit(ctx, tx, a.User.ID, project, "bmc.policy.save", out.ID, trace, map[string]any{"state_version": out.StateVersion, "enabled": out.Enabled, "reason": strings.TrimSpace(in.Reason)})
	if e != nil {
		return nil, e
	}
	if e = remember(ctx, tx, idem, in, out); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

// Policy history remains readable after project archival. Reads still recheck
// the real session and current membership; scheduler authority uses no session.
func (s *Service) bmcPolicyReadTx(ctx context.Context, a *Actor, project, asset string) (*sql.Tx, error) {
	if a == nil {
		return nil, platform.ErrUnauthenticated
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	fail := func(e error) (*sql.Tx, error) { tx.Rollback(); return nil, e }
	if e = platform.RecheckSessionTx(ctx, tx, a); e != nil {
		return fail(e)
	}
	if !a.User.IsAdmin {
		var role string
		e = tx.QueryRowContext(ctx, `SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE`, project, a.User.ID).Scan(&role)
		if errors.Is(e, sql.ErrNoRows) {
			e = platform.ErrNotFound
		}
		if e != nil {
			return fail(e)
		}
	}
	if e = bmcPolicyAssetScope(ctx, tx, project, asset); e != nil {
		return fail(e)
	}
	return tx, nil
}
func (s *Service) ListBMCPolicies(ctx context.Context, a *Actor, project, asset string) ([]BMCPolicy, error) {
	tx, e := s.bmcPolicyReadTx(ctx, a, project, asset)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT `+bmcPolicyFields+` FROM bmc_collection_policies WHERE project_id=$1 AND asset_id=$2 ORDER BY created_at,id`, project, asset)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []BMCPolicy{}
	for rows.Next() {
		p, e := scanBMCPolicy(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}
func (s *Service) ListBMCPolicyRuns(ctx context.Context, a *Actor, project, asset, id string, page platform.Pagination) (platform.Page[BMCPolicyRun], error) {
	out := platform.NewPage[BMCPolicyRun](page)
	if e := page.Validate(); e != nil {
		return out, e
	}
	tx, e := s.bmcPolicyReadTx(ctx, a, project, asset)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var found string
	e = tx.QueryRowContext(ctx, `SELECT id FROM bmc_collection_policies WHERE id=$1 AND project_id=$2 AND asset_id=$3`, id, project, asset).Scan(&found)
	if errors.Is(e, sql.ErrNoRows) {
		return out, platform.ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM bmc_policy_runs WHERE policy_id=$1`, id).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,policy_id,project_id,policy_version,policy_snapshot,trigger_key,trigger_kind,source_ref,parent_run_id,attempt_no,execution_id,state,code,scheduled_at,created_at FROM bmc_policy_runs WHERE policy_id=$1 ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3`, id, page.PageSize, page.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var r BMCPolicyRun
		var raw []byte
		e = rows.Scan(&r.ID, &r.PolicyID, &r.ProjectID, &r.PolicyVersion, &raw, &r.TriggerKey, &r.TriggerKind, &r.SourceRef, &r.ParentRunID, &r.AttemptNo, &r.ExecutionID, &r.State, &r.Code, &r.ScheduledAt, &r.CreatedAt)
		if e != nil {
			return out, e
		}
		if e = json.Unmarshal(raw, &r.PolicySnapshot); e != nil {
			return out, e
		}
		out.Items = append(out.Items, r)
	}
	return out, rows.Err()
}
