package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
)

type MonitorObservation struct {
	Sequence          int64          `json:"sequence"`
	DType             string         `json:"dtype"`
	Source            string         `json:"source"`
	Method            string         `json:"method"`
	Origin            string         `json:"origin"`
	Unit              string         `json:"unit"`
	Sample            monitor.Sample `json:"sample"`
	ReceivedAt        time.Time      `json:"received_at"`
	PolicyID          string         `json:"policy_id"`
	IntervalSeconds   int            `json:"interval_seconds"`
	StaleAfterSeconds int            `json:"stale_after_seconds"`
	Freshness         string         `json:"freshness"`
	PhysicalPortID    string         `json:"physical_port_id"`
	MappingRevisionID string         `json:"mapping_revision_id"`
}
type MonitorGap struct {
	StartedAt time.Time `json:"started_at"`
	EndedAt   time.Time `json:"ended_at"`
	Reason    string    `json:"reason"`
	Source    string    `json:"source"`
	Method    string    `json:"method"`
}
type MonitorSnapshot struct {
	CollectionEnabled bool                 `json:"collection_enabled"`
	ProjectID         string               `json:"project_id"`
	AssetID           string               `json:"asset_id"`
	MachineID         string               `json:"machine_id"`
	Stage             string               `json:"stage"`
	AsOf              time.Time            `json:"as_of"`
	Cursor            int64                `json:"cursor"`
	ResyncRequired    bool                 `json:"resync_required"`
	AgentState        string               `json:"agent_state"`
	LastHeartbeat     *time.Time           `json:"last_heartbeat"`
	Items             []MonitorObservation `json:"items"`
	Gaps              []MonitorGap         `json:"gaps"`
}

const monitorObservationFields = `sample.sequence,sample.dtype,sample.source,sample.method,sample.origin,sample.unit,sample.sample,sample.received_at,sample.policy_id,sample.interval_seconds,coalesce(sample.physical_port_id,''),coalesce(sample.mapping_revision_id,'')`

func scanMonitorObservation(row scanner, now time.Time) (*MonitorObservation, error) {
	var out MonitorObservation
	var raw []byte
	if e := row.Scan(&out.Sequence, &out.DType, &out.Source, &out.Method, &out.Origin, &out.Unit, &raw, &out.ReceivedAt, &out.PolicyID, &out.IntervalSeconds, &out.PhysicalPortID, &out.MappingRevisionID); e != nil {
		return nil, e
	}
	if e := json.Unmarshal(raw, &out.Sample); e != nil {
		return nil, e
	}
	out.StaleAfterSeconds = 3 * out.IntervalSeconds
	out.Freshness = "fresh"
	if !now.Before(out.Sample.ObservedAt.Add(time.Duration(out.StaleAfterSeconds)*time.Second)) || !now.Before(out.ReceivedAt.Add(time.Duration(out.StaleAfterSeconds)*time.Second)) {
		out.Freshness = "stale"
	}
	return &out, nil
}
func (s *Store) MonitorSnapshot(ctx context.Context, a *AuthSession, project, asset string, cursor int64) (*MonitorSnapshot, error) {
	if cursor < 0 {
		return nil, apiError(400, "VALIDATION_ERROR", "Cursor must be nonnegative")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	out := &MonitorSnapshot{CollectionEnabled: s.MonitorEnabled, ProjectID: project, AssetID: asset, Items: []MonitorObservation{}, Gaps: []MonitorGap{}, AsOf: time.Now().UTC(), AgentState: "unknown"}
	var state, projectState string
	if e = tx.QueryRowContext(ctx, `SELECT a.machine_id,a.state,p.state FROM project_assets a JOIN projects p ON p.id=a.project_id WHERE a.id=$1 AND a.project_id=$2`, asset, project).Scan(&out.MachineID, &state, &projectState); e != nil {
		return nil, e
	}
	out.Stage = monitorStage(state)
	var last sql.NullTime
	var live bool
	if e = tx.QueryRowContext(ctx, `SELECT max(c.last_seen_at),coalesce(bool_or(c.disconnected_at IS NULL AND c.last_seen_at>now()-interval '30 seconds'),false) FROM agent_credentials identity JOIN agent_connections c ON c.credential_id=identity.id WHERE identity.project_id=$1 AND identity.machine_id=$2 AND identity.revoked_at IS NULL AND identity.expires_at>now()`, project, out.MachineID).Scan(&last, &live); e != nil {
		return nil, e
	}
	if last.Valid {
		out.LastHeartbeat = &last.Time
		out.AgentState = "disconnected"
		if live {
			out.AgentState = "connected"
		}
	}
	stopped := state == "maintained" || projectState == "archived" || projectState == "delivered"
	if stopped {
		out.AgentState = "stopped"
	}
	if e = tx.QueryRowContext(ctx, `SELECT coalesce(max(sequence),0) FROM monitor_metric_samples WHERE project_id=$1 AND machine_id=$2`, project, out.MachineID).Scan(&out.Cursor); e != nil {
		return nil, e
	}
	// Return a bounded full latest snapshot. A cursor is only a change watermark;
	// it never implies that a missing delta is a continuous observation.
	out.ResyncRequired = cursor > out.Cursor
	rows, e := tx.QueryContext(ctx, `SELECT `+monitorObservationFields+` FROM monitor_metric_latest latest JOIN monitor_metric_samples sample ON sample.sequence=latest.sample_sequence WHERE latest.project_id=$1 AND latest.machine_id=$2 ORDER BY sample.source,sample.dtype,sample.entity_key,sample.metric_key LIMIT 2049`, project, out.MachineID)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		item, e := scanMonitorObservation(rows, out.AsOf)
		if e != nil {
			rows.Close()
			return nil, e
		}
		if stopped && item.Source == "host" {
			item.Freshness = "stopped"
		}
		out.Items = append(out.Items, *item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(out.Items) > 2048 {
		return nil, apiError(409, "MONITOR_SCOPE_LIMIT", "Monitoring snapshot exceeds the bounded metric scope")
	}
	gaps, e := tx.QueryContext(ctx, `SELECT started_at,ended_at,reason,source,method FROM monitor_gaps WHERE project_id=$1 AND machine_id=$2 ORDER BY ended_at DESC LIMIT 100`, project, out.MachineID)
	if e != nil {
		return nil, e
	}
	for gaps.Next() {
		var gap MonitorGap
		if e = gaps.Scan(&gap.StartedAt, &gap.EndedAt, &gap.Reason, &gap.Source, &gap.Method); e != nil {
			gaps.Close()
			return nil, e
		}
		out.Gaps = append(out.Gaps, gap)
	}
	e = gaps.Err()
	gaps.Close()
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

type MonitorTrendBucket struct {
	At              time.Time `json:"at"`
	Samples         int64     `json:"samples"`
	GoodSamples     int64     `json:"good_samples"`
	ExpectedSamples int64     `json:"expected_samples"`
	Coverage        float64   `json:"coverage"`
	Minimum         *float64  `json:"minimum"`
	Maximum         *float64  `json:"maximum"`
	Average         *float64  `json:"average"`
	UpSamples       int64     `json:"up_samples"`
	DownSamples     int64     `json:"down_samples"`
	UnknownSamples  int64     `json:"unknown_samples"`
}
type MonitorTrend struct {
	Key           string               `json:"key"`
	Entity        string               `json:"entity"`
	Source        string               `json:"source"`
	Method        string               `json:"method"`
	Origin        string               `json:"origin"`
	Unit          string               `json:"unit"`
	Window        string               `json:"window"`
	BucketSeconds int                  `json:"bucket_seconds"`
	From          time.Time            `json:"from"`
	To            time.Time            `json:"to"`
	Items         []MonitorTrendBucket `json:"items"`
}

func (s *Store) MonitorTrend(ctx context.Context, a *AuthSession, project, asset, key, entity, source, method, origin, window string) (*MonitorTrend, error) {
	definition, known := monitor.Metrics[key]
	if !known || len(entity) == 0 || len(entity) > 128 || (source != "host" && source != "switch" && source != "bmc") || (method != "agent" && method != "ssh" && method != "collector" && method != "redfish" && method != "ipmi") || (origin != "real" && origin != "sim") {
		return nil, apiError(400, "VALIDATION_ERROR", "Exact metric, entity, source, method and origin required")
	}
	duration, bucket := time.Hour, 60
	switch window {
	case "1h":
	case "24h":
		duration = 24 * time.Hour
		bucket = 300
	case "7d":
		duration = 7 * 24 * time.Hour
		bucket = 3600
	default:
		return nil, apiError(400, "VALIDATION_ERROR", "Window must be 1h, 24h or 7d")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = precollectReadScope(ctx, tx, a, project, asset); e != nil {
		return nil, e
	}
	var machine string
	if e = tx.QueryRowContext(ctx, `SELECT machine_id FROM project_assets WHERE id=$1 AND project_id=$2`, asset, project).Scan(&machine); e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	out := &MonitorTrend{Key: key, Entity: entity, Source: source, Method: method, Origin: origin, Unit: definition.Unit, Window: window, BucketSeconds: bucket, From: now.Add(-duration), To: now, Items: []MonitorTrendBucket{}}
	rows, e := tx.QueryContext(ctx, `WITH observed AS(SELECT DISTINCT ON(observed_at) observed_at,sample,interval_seconds FROM monitor_metric_samples WHERE project_id=$1 AND machine_id=$2 AND metric_key=$3 AND entity_key=$4 AND source=$5 AND method=$6 AND origin=$7 AND observed_at>=$8 AND observed_at<$9 ORDER BY observed_at,sequence DESC) SELECT date_bin($10::interval,observed_at,'2000-01-01'::timestamptz),count(*),count(*) FILTER(WHERE sample->>'quality'='good'),ceil($11/avg(interval_seconds)),min((sample->>'value')::double precision) FILTER(WHERE sample->>'quality'='good'),max((sample->>'value')::double precision) FILTER(WHERE sample->>'quality'='good'),avg((sample->>'value')::double precision) FILTER(WHERE sample->>'quality'='good'),count(*) FILTER(WHERE sample->>'quality'='good' AND sample->>'state' IN('up','on','running','healthy')),count(*) FILTER(WHERE sample->>'quality'='good' AND sample->>'state' IN('down','off','stopped','critical')) FROM observed GROUP BY 1 ORDER BY 1`, project, machine, key, entity, source, method, origin, out.From, out.To, fmt.Sprintf("%d seconds", bucket), bucket)
	if e != nil {
		return nil, e
	}
	byTime := map[time.Time]MonitorTrendBucket{}
	for rows.Next() {
		var b MonitorTrendBucket
		if e = rows.Scan(&b.At, &b.Samples, &b.GoodSamples, &b.ExpectedSamples, &b.Minimum, &b.Maximum, &b.Average, &b.UpSamples, &b.DownSamples); e != nil {
			rows.Close()
			return nil, e
		}
		b.UnknownSamples = b.Samples - b.UpSamples - b.DownSamples
		if b.ExpectedSamples > 0 {
			b.Coverage = math.Min(1, float64(b.GoodSamples)/float64(b.ExpectedSamples))
		}
		byTime[b.At.UTC()] = b
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	for at := out.From.Truncate(time.Duration(bucket) * time.Second); at.Before(now); at = at.Add(time.Duration(bucket) * time.Second) {
		b, ok := byTime[at]
		if !ok {
			b.At = at
			b.Coverage = 0
		}
		out.Items = append(out.Items, b)
	}
	return out, tx.Commit()
}
