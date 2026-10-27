package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
)

func monitorConsumeBudgetTx(ctx context.Context, tx *sql.Tx, id string, bytes int, rate, burst int, now time.Time) error {
	if _, e := tx.ExecContext(ctx, `INSERT INTO monitor_rate_buckets(id,tokens,updated_at)VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, burst, now); e != nil {
		return e
	}
	var tokens float64
	var at time.Time
	if e := tx.QueryRowContext(ctx, `SELECT tokens,updated_at FROM monitor_rate_buckets WHERE id=$1 FOR UPDATE`, id).Scan(&tokens, &at); e != nil {
		return e
	}
	elapsed := math.Max(0, now.Sub(at).Seconds())
	tokens = math.Min(float64(burst), tokens+elapsed*float64(rate))
	if tokens < float64(bytes) {
		return apiError(429, "MONITOR_BUDGET_EXCEEDED", "Telemetry budget exhausted; retain the original frame and back off")
	}
	_, e := tx.ExecContext(ctx, `UPDATE monitor_rate_buckets SET tokens=$2,updated_at=$3 WHERE id=$1`, id, tokens-float64(bytes), now)
	return e
}

// Called only from the authenticated heartbeat transaction. A successful ACK
// means receipt, raw samples, latest projections and observations all committed.
func (s *Store) ingestAgentMonitorTx(ctx context.Context, tx *sql.Tx, credential *AgentCredential, connection string, raw []byte) (string, error) {
	now := time.Now().UTC()
	frame, e := monitor.DecodeFrame(raw, now)
	if e != nil {
		return "", ErrAgentDenied
	}
	if frame == nil {
		return "", nil
	}
	if e = lockKey(ctx, tx, "monitor-stream:"+credential.ID); e != nil {
		return "", e
	}
	hash := digest(string(raw))
	var original string
	e = tx.QueryRowContext(ctx, `SELECT frame_hash FROM monitor_ingest_receipts WHERE credential_id=$1 AND frame_id=$2`, credential.ID, frame.ID).Scan(&original)
	if e == nil {
		if original != hash {
			return "", ErrAgentDenied
		}
		return frame.ID, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	var assignmentRaw []byte
	if e = tx.QueryRowContext(ctx, `SELECT assignment FROM monitor_assignments WHERE credential_id=$1 AND project_id=$2 AND machine_id=$3`, credential.ID, credential.ProjectID, credential.MachineID).Scan(&assignmentRaw); e != nil {
		return "", ErrAgentDenied
	}
	var assignment monitor.Assignment
	if json.Unmarshal(assignmentRaw, &assignment) != nil || assignment.Policy.Validate() != nil {
		return "", ErrAgentDenied
	}
	// Queued frames retain the exact server-issued sampling intervals, including
	// an original dynamic-window assignment, across policy changes and reconnects.
	var historyRaw []byte
	if e = tx.QueryRowContext(ctx, `SELECT assignment FROM monitor_assignment_versions WHERE credential_id=$1 AND policy_id=$2`, credential.ID, frame.PolicyID).Scan(&historyRaw); e != nil {
		return "", ErrAgentDenied
	}
	var history monitor.Assignment
	if json.Unmarshal(historyRaw, &history) != nil || history.Policy.Validate() != nil {
		return "", ErrAgentDenied
	}
	framePolicy := history.Policy
	var assetState, projectState string
	if e = tx.QueryRowContext(ctx, `SELECT a.state,p.state FROM project_assets a JOIN projects p ON p.id=a.project_id WHERE a.project_id=$1 AND a.machine_id=$2 FOR SHARE OF a,p`, credential.ProjectID, credential.MachineID).Scan(&assetState, &projectState); e != nil {
		return "", e
	}
	if assetState == "maintained" || projectState == "archived" || projectState == "delivered" {
		return "", apiError(409, "MONITOR_STOPPED", "In-band collection is stopped for this asset")
	}
	if !frame.Full {
		var baseline string
		e = tx.QueryRowContext(ctx, `SELECT last_frame_id FROM monitor_streams WHERE credential_id=$1`, credential.ID).Scan(&baseline)
		if e != nil || baseline != frame.BaselineID {
			return "", apiError(409, "MONITOR_FULL_RESYNC_REQUIRED", "Delta must reference the exact committed baseline frame")
		}
	}
	samples := append([]monitor.Sample{}, frame.Samples...)
	for _, unchanged := range frame.Unchanged {
		var previous []byte
		var sourceCredential string
		e = tx.QueryRowContext(ctx, `SELECT sample.sample,coalesce(sample.credential_id,'') FROM monitor_metric_latest latest JOIN monitor_metric_samples sample ON sample.sequence=latest.sample_sequence WHERE latest.project_id=$1 AND latest.machine_id=$2 AND latest.source='host' AND latest.method='agent' AND latest.metric_key=$3 AND latest.entity_key=$4`, credential.ProjectID, credential.MachineID, unchanged.Key, unchanged.Entity).Scan(&previous, &sourceCredential)
		var sample monitor.Sample
		if e != nil || sourceCredential != credential.ID || json.Unmarshal(previous, &sample) != nil || !unchanged.ObservedAt.After(sample.ObservedAt) {
			return "", ErrAgentDenied
		}
		sample.ObservedAt = unchanged.ObservedAt
		samples = append(samples, sample)
	}
	// Always take the global bucket before a per-device bucket to avoid lock
	// inversion. Protocol overhead has a separate 25 MB/s reservation.
	if e = monitorConsumeBudgetTx(ctx, tx, "global", len(raw), monitor.PayloadBytesPerSecond, monitor.PayloadBytesPerSecond, now); e != nil {
		return "", e
	}
	if e = monitorConsumeBudgetTx(ctx, tx, "agent:"+credential.MachineID, len(raw), assignment.PayloadBytesPerSecond, assignment.BurstBytes, now); e != nil {
		return "", e
	}
	for _, sample := range samples {
		definition := monitor.Metrics[sample.Key]
		interval := framePolicy.Intervals[definition.DType]
		if definition.DType == "B7" || interval == 0 {
			return "", ErrAgentDenied
		}
		if e = s.recordMonitorSampleTx(ctx, tx, credential.ProjectID, credential.MachineID, credential.ID, frame.ID, frame.PolicyID, "host", "agent", "real", interval, sample, now); e != nil {
			return "", e
		}
	}
	if e = s.monitorEvaluateFrameTx(ctx, tx, credential.ProjectID, credential.MachineID, credential.ID, frame.ID, now); e != nil {
		return "", e
	}
	if frame.GapStart != nil {
		if _, e = tx.ExecContext(ctx, `INSERT INTO monitor_gaps(project_id,machine_id,credential_id,frame_id,source,method,started_at,ended_at,reason)VALUES($1,$2,$3,$4,'host','agent',$5,$6,'local_queue_exhausted')`, credential.ProjectID, credential.MachineID, credential.ID, frame.ID, *frame.GapStart, *frame.GapEnd); e != nil {
			return "", e
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO monitor_ingest_receipts(credential_id,frame_id,project_id,machine_id,frame_hash,sample_count,received_at)VALUES($1,$2,$3,$4,$5,$6,$7)`, credential.ID, frame.ID, credential.ProjectID, credential.MachineID, hash, len(samples), now); e != nil {
		return "", e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO monitor_streams(credential_id,connection_id,last_frame_id,updated_at)VALUES($1,$2,$3,$4)ON CONFLICT(credential_id)DO UPDATE SET connection_id=EXCLUDED.connection_id,last_frame_id=EXCLUDED.last_frame_id,updated_at=EXCLUDED.updated_at`, credential.ID, connection, frame.ID, now); e != nil {
		return "", e
	}
	return frame.ID, nil
}

// All accepted methods converge here. Port binding uses the immutable physical
// tuple, never an OS-name-only match or a client-supplied gate verdict.
func (s *Store) recordMonitorSampleTx(ctx context.Context, tx *sql.Tx, project, machine, credential, frame, policy, source, method, origin string, interval int, sample monitor.Sample, now time.Time) error {
	if sample.Validate(now) != nil {
		return apiError(400, "MONITOR_INVALID_SAMPLE", "Invalid observation")
	}
	definition := monitor.Metrics[sample.Key]
	var port, revision sql.NullString
	if source == "host" && sample.PermanentMAC != "" && sample.BDF != "" && sample.Interface != "" {
		e := tx.QueryRowContext(ctx, `SELECT physical_port_id,mapping_revision_id FROM mapping_published_ports WHERE project_id=$1 AND machine_id=$2 AND binding#>>'{observed,permanent_mac}'=$3 AND binding#>>'{observed,bdf}'=$4 AND binding#>>'{observed,interface}'=$5`, project, machine, sample.PermanentMAC, sample.BDF, sample.Interface).Scan(&port, &revision)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
	}
	var sequence int64
	if e := tx.QueryRowContext(ctx, `INSERT INTO monitor_metric_samples(project_id,machine_id,credential_id,frame_id,metric_key,entity_key,dtype,source,method,origin,unit,sample,observed_at,received_at,policy_id,interval_seconds,physical_port_id,mapping_revision_id)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)RETURNING sequence`, project, machine, nullBOMString(credential), frame, sample.Key, sample.Entity, definition.DType, source, method, origin, definition.Unit, networkJSON(sample), sample.ObservedAt, now, policy, interval, port, revision).Scan(&sequence); e != nil {
		return e
	}
	_, e := tx.ExecContext(ctx, `INSERT INTO monitor_metric_latest(project_id,machine_id,source,method,metric_key,entity_key,sample_sequence)VALUES($1,$2,$3,$4,$5,$6,$7)ON CONFLICT(project_id,machine_id,source,method,metric_key,entity_key)DO UPDATE SET sample_sequence=EXCLUDED.sample_sequence WHERE (SELECT observed_at FROM monitor_metric_samples WHERE sequence=monitor_metric_latest.sample_sequence)<$8`, project, machine, source, method, sample.Key, sample.Entity, sequence, sample.ObservedAt)
	return e
}
