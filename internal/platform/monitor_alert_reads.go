package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

type MonitorAlert struct {
	ID                string          `json:"id"`
	MachineID         string          `json:"machine_id"`
	PhysicalPortID    string          `json:"physical_port_id"`
	MappingRevisionID string          `json:"mapping_revision_id"`
	Source            string          `json:"source"`
	Method            string          `json:"method"`
	Origin            string          `json:"origin"`
	Entity            string          `json:"entity"`
	DType             string          `json:"dtype"`
	RuleID            string          `json:"rule_id"`
	RuleVersion       string          `json:"rule_version"`
	Severity          string          `json:"severity"`
	State             string          `json:"state"`
	FirstAt           time.Time       `json:"first_at"`
	LastAt            time.Time       `json:"last_at"`
	Count             int64           `json:"count"`
	Version           int64           `json:"version"`
	SampleSequence    *int64          `json:"sample_sequence"`
	IssueID           string          `json:"issue_id"`
	Context           json.RawMessage `json:"context"`
}
type MonitorAlertFilter struct{ State, Severity, DType, MachineID, Source string }
type MonitorAlertPage struct {
	Items          []MonitorAlert `json:"items"`
	Total          int            `json:"total"`
	Page           int            `json:"page"`
	PageSize       int            `json:"page_size"`
	FiringCritical int            `json:"firing_critical"`
}

const monitorAlertFields = `id,machine_id,coalesce(physical_port_id,''),coalesce(mapping_revision_id,''),source,method,origin,entity_key,dtype,rule_id,rule_version,severity,state,first_at,last_at,count,version,sample_sequence,coalesce(issue_id,''),context`

func scanMonitorAlert(row scanner) (*MonitorAlert, error) {
	var out MonitorAlert
	var seq sql.NullInt64
	if e := row.Scan(&out.ID, &out.MachineID, &out.PhysicalPortID, &out.MappingRevisionID, &out.Source, &out.Method, &out.Origin, &out.Entity, &out.DType, &out.RuleID, &out.RuleVersion, &out.Severity, &out.State, &out.FirstAt, &out.LastAt, &out.Count, &out.Version, &seq, &out.IssueID, &out.Context); e != nil {
		return nil, e
	}
	if seq.Valid {
		out.SampleSequence = &seq.Int64
	}
	return &out, nil
}
func (s *Store) MonitorAlerts(ctx context.Context, a *AuthSession, project string, f MonitorAlertFilter, page Pagination) (*MonitorAlertPage, error) {
	if e := page.Validate(); e != nil {
		return nil, e
	}
	for _, v := range []string{f.State, f.Severity, f.DType, f.MachineID, f.Source} {
		if len(v) > 64 {
			return nil, apiError(400, "VALIDATION_ERROR", "Alert filter is too long")
		}
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	out := &MonitorAlertPage{Items: []MonitorAlert{}, Page: page.Page, PageSize: page.PageSize}
	if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM monitor_alerts WHERE project_id=$1 AND state='firing' AND severity='critical' AND origin='real'`, project).Scan(&out.FiringCritical); e != nil {
		return nil, e
	}
	predicate := ` FROM monitor_alerts WHERE project_id=$1 AND ($2='' OR state=$2)AND($3='' OR severity=$3)AND($4='' OR dtype=$4)AND($5='' OR machine_id=$5)AND($6='' OR source=$6)`
	args := []any{project, f.State, f.Severity, f.DType, f.MachineID, f.Source}
	if e = tx.QueryRowContext(ctx, `SELECT count(*)`+predicate, args...).Scan(&out.Total); e != nil {
		return nil, e
	}
	args = append(args, page.PageSize, page.Offset())
	rows, e := tx.QueryContext(ctx, `SELECT `+monitorAlertFields+predicate+` ORDER BY CASE severity WHEN 'critical' THEN 0 WHEN 'warning' THEN 1 ELSE 2 END,last_at DESC,id DESC LIMIT $7 OFFSET $8`, args...)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		item, e := scanMonitorAlert(rows)
		if e != nil {
			rows.Close()
			return nil, e
		}
		out.Items = append(out.Items, *item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

type MonitorSuppressInput struct {
	ExpectedVersion int64  `json:"expected_version"`
	Reason          string `json:"reason"`
}

func (s *Store) SuppressMonitorAlert(ctx context.Context, a *AuthSession, project, id string, in MonitorSuppressInput, trace string) (*MonitorAlert, error) {
	in.Reason = strings.TrimSpace(in.Reason)
	if in.ExpectedVersion < 1 || utf8.RuneCountInString(in.Reason) < 3 || utf8.RuneCountInString(in.Reason) > 1000 {
		return nil, apiError(400, "VALIDATION_ERROR", "Current alert version and reason required")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireProjectMember(ctx, tx, a, project); e != nil {
		return nil, e
	}
	role, e := issueRoleTx(ctx, tx, a, project)
	if e != nil {
		return nil, e
	}
	if role != "Admin" && role != "DL" && role != "NE" {
		return nil, ErrForbidden
	}
	var projectState string
	if e = tx.QueryRowContext(ctx, `SELECT state FROM projects WHERE id=$1 FOR SHARE`, project).Scan(&projectState); e != nil {
		return nil, e
	}
	if projectState == "archived" || projectState == "delivered" {
		return nil, apiError(409, "PROJECT_READ_ONLY", "Project is read-only")
	}
	item, e := scanMonitorAlert(tx.QueryRowContext(ctx, `SELECT `+monitorAlertFields+` FROM monitor_alerts WHERE project_id=$1 AND id=$2 FOR UPDATE`, project, id))
	if e != nil {
		return nil, e
	}
	if item.Version != in.ExpectedVersion {
		return nil, errVersion
	}
	var repair bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM issues WHERE id=$1 AND project_id=$2 AND state='in_repair')`, item.IssueID, project).Scan(&repair); e != nil {
		return nil, e
	}
	if !repair || item.State != "firing" {
		return nil, apiError(409, "MONITOR_REPAIR_REQUIRED", "Suppression requires the associated issue to be in repair")
	}
	item.State = "suppressed"
	item.Version++
	if _, e = tx.ExecContext(ctx, `UPDATE monitor_alerts SET state='suppressed',version=$2 WHERE id=$1`, id, item.Version); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO monitor_alert_events(id,alert_id,version,state,severity,sample_sequence,context,observed_at)VALUES($1,$2,$3,'suppressed',$4,$5,$6,clock_timestamp())`, RandomID(), id, item.Version, item.Severity, item.SampleSequence, networkJSON(map[string]any{"reason": in.Reason, "actor_id": a.User.ID})); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, project, "monitor.alert.suppress", id, trace, map[string]any{"reason": in.Reason, "issue_id": item.IssueID}); e != nil {
		return nil, e
	}
	return item, tx.Commit()
}
