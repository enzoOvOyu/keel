package platform

import (
	"context"
	"encoding/json"
	"time"
)

type PortMonitorEvent struct {
	ID                string          `json:"id"`
	AlertID           string          `json:"alert_id"`
	Version           int64           `json:"version"`
	State             string          `json:"state"`
	Severity          string          `json:"severity"`
	Source            string          `json:"source"`
	Method            string          `json:"method"`
	Origin            string          `json:"origin"`
	RuleID            string          `json:"rule_id"`
	MappingRevisionID string          `json:"mapping_revision_id"`
	SampleSequence    *int64          `json:"sample_sequence"`
	Context           json.RawMessage `json:"context"`
	ObservedAt        time.Time       `json:"observed_at"`
}
type PortMonitorEvents struct {
	Items    []PortMonitorEvent `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}

func (s *Store) PortMonitorEvents(ctx context.Context, a *AuthSession, project, asset, port string, page Pagination) (*PortMonitorEvents, error) {
	if e := page.Validate(); e != nil {
		return nil, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	var owned bool
	if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM physical_ports port JOIN project_assets asset ON asset.machine_id=port.machine_id WHERE asset.project_id=$1 AND asset.id=$2 AND port.id=$3)`, project, asset, port).Scan(&owned); e != nil {
		return nil, e
	}
	if !owned {
		return nil, ErrNotFound
	}
	out := &PortMonitorEvents{Items: []PortMonitorEvent{}, Page: page.Page, PageSize: page.PageSize}
	from := ` FROM monitor_alert_events event JOIN monitor_alerts alert ON alert.id=event.alert_id LEFT JOIN monitor_metric_samples sample ON sample.sequence=event.sample_sequence WHERE alert.project_id=$1 AND alert.physical_port_id=$2`
	if e = tx.QueryRowContext(ctx, `SELECT count(*)`+from, project, port).Scan(&out.Total); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT event.id,event.alert_id,event.version,event.state,event.severity,alert.source,alert.method,alert.origin,alert.rule_id,coalesce(sample.mapping_revision_id,''),event.sample_sequence,event.context,event.observed_at`+from+` ORDER BY event.observed_at DESC,event.id DESC LIMIT $3 OFFSET $4`, project, port, page.PageSize, page.Offset())
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var item PortMonitorEvent
		if e = rows.Scan(&item.ID, &item.AlertID, &item.Version, &item.State, &item.Severity, &item.Source, &item.Method, &item.Origin, &item.RuleID, &item.MappingRevisionID, &item.SampleSequence, &item.Context, &item.ObservedAt); e != nil {
			rows.Close()
			return nil, e
		}
		out.Items = append(out.Items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
