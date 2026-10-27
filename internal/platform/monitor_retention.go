package platform

import "context"

// Incremental hourly materialization keeps source/method/origin separate. Each
// affected bucket is recomputed from distinct observation timestamps, so retries
// and repeated immutable full snapshots cannot inflate sample coverage.
func (s *Store) MaintainMonitorHistory(ctx context.Context) error {
	if !s.MonitorEnabled {
		return nil
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var locked bool
	if e = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('monitor-history',0))`).Scan(&locked); e != nil || !locked {
		return e
	}
	_, e = tx.ExecContext(ctx, `WITH changed AS(
 SELECT sample.project_id,sample.machine_id,sample.source,sample.method,sample.origin,sample.metric_key,sample.entity_key,date_trunc('hour',sample.observed_at) AS bucket_at
 FROM monitor_metric_samples sample LEFT JOIN monitor_hourly_samples old ON old.project_id=sample.project_id AND old.machine_id=sample.machine_id AND old.source=sample.source AND old.method=sample.method AND old.origin=sample.origin AND old.metric_key=sample.metric_key AND old.entity_key=sample.entity_key AND old.bucket_at=date_trunc('hour',sample.observed_at)
 WHERE sample.observed_at<date_trunc('hour',clock_timestamp()) AND sample.sequence>coalesce(old.last_sequence,0)
 GROUP BY 1,2,3,4,5,6,7,8 ORDER BY 8 LIMIT 100
 ), points AS(
 SELECT DISTINCT ON(s.project_id,s.machine_id,s.source,s.method,s.origin,s.metric_key,s.entity_key,s.observed_at) s.*,changed.bucket_at
 FROM changed JOIN monitor_metric_samples s ON s.project_id=changed.project_id AND s.machine_id=changed.machine_id AND s.source=changed.source AND s.method=changed.method AND s.origin=changed.origin AND s.metric_key=changed.metric_key AND s.entity_key=changed.entity_key AND s.observed_at>=changed.bucket_at AND s.observed_at<changed.bucket_at+interval '1 hour'
 ORDER BY s.project_id,s.machine_id,s.source,s.method,s.origin,s.metric_key,s.entity_key,s.observed_at,s.sequence DESC
 ) INSERT INTO monitor_hourly_samples(project_id,machine_id,source,method,origin,metric_key,entity_key,bucket_at,unit,samples,good_samples,expected_samples,minimum,maximum,total,last_sequence)
 SELECT project_id,machine_id,source,method,origin,metric_key,entity_key,bucket_at,min(unit),count(*),count(*)FILTER(WHERE sample->>'quality'='good'),CASE WHEN bool_and(snapshot_only) THEN 0 ELSE ceil(3600.0/min(interval_seconds) FILTER(WHERE NOT snapshot_only)) END,min((sample->>'value')::double precision)FILTER(WHERE sample->>'quality'='good'),max((sample->>'value')::double precision)FILTER(WHERE sample->>'quality'='good'),sum((sample->>'value')::double precision)FILTER(WHERE sample->>'quality'='good'),max(sequence) FROM points GROUP BY 1,2,3,4,5,6,7,8
 ON CONFLICT(project_id,machine_id,source,method,origin,metric_key,entity_key,bucket_at)DO UPDATE SET samples=EXCLUDED.samples,good_samples=EXCLUDED.good_samples,expected_samples=EXCLUDED.expected_samples,minimum=EXCLUDED.minimum,maximum=EXCLUDED.maximum,total=EXCLUDED.total,last_sequence=EXCLUDED.last_sequence`)
	if e != nil {
		return e
	}
	// Keep raw samples for >=7d and retain every original referenced alert or
	// current latest sample. Materialization must precede any eligible deletion.
	_, e = tx.ExecContext(ctx, `DELETE FROM monitor_metric_samples WHERE sequence IN(
 SELECT s.sequence FROM monitor_metric_samples s JOIN monitor_hourly_samples h ON h.project_id=s.project_id AND h.machine_id=s.machine_id AND h.source=s.source AND h.method=s.method AND h.origin=s.origin AND h.metric_key=s.metric_key AND h.entity_key=s.entity_key AND h.bucket_at=date_trunc('hour',s.observed_at) AND h.last_sequence>=s.sequence
 WHERE s.observed_at<clock_timestamp()-make_interval(days=>greatest(7,coalesce((SELECT max((policy->>'raw_retention_days')::int) FROM(SELECT DISTINCT ON(stage)stage,policy FROM monitor_policies WHERE project_id=s.project_id ORDER BY stage,version DESC)p),7)))
 AND NOT EXISTS(SELECT 1 FROM monitor_metric_latest l WHERE l.sample_sequence=s.sequence)
 AND NOT EXISTS(SELECT 1 FROM monitor_alert_events a WHERE a.sample_sequence=s.sequence)
 AND NOT EXISTS(SELECT 1 FROM monitor_alerts a WHERE a.sample_sequence=s.sequence)
 AND NOT EXISTS(SELECT 1 FROM portmon_issue_bindings discovery WHERE discovery.sample_sequence=s.sequence)
 ORDER BY s.sequence LIMIT 2000
 )`)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `DELETE FROM monitor_hourly_samples h WHERE bucket_at<clock_timestamp()-make_interval(days=>greatest(90,coalesce((SELECT max((policy->>'hourly_retention_days')::int) FROM(SELECT DISTINCT ON(stage)stage,policy FROM monitor_policies WHERE project_id=h.project_id ORDER BY stage,version DESC)p),90)))`)
	if e != nil {
		return e
	}
	return tx.Commit()
}

// Stale source data becomes unknown, and maintained in-band alerts are muted.
// This never closes a repair issue or declares a device powered off.
func (s *Store) TickMonitorFreshness(ctx context.Context) error {
	if !s.MonitorEnabled {
		return nil
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT alert.id,CASE WHEN asset.state='maintained' AND alert.source='host' THEN 'suppressed' ELSE 'unknown' END FROM monitor_alerts alert JOIN project_assets asset ON asset.project_id=alert.project_id AND asset.machine_id=alert.machine_id JOIN monitor_metric_samples sample ON sample.sequence=alert.sample_sequence WHERE alert.state IN('firing','suppressed') AND ((asset.state='maintained' AND alert.source='host' AND alert.state<>'suppressed') OR ((asset.state<>'maintained' OR alert.source<>'host') AND CASE WHEN sample.snapshot_only THEN coalesce(sample.source_valid_until<=clock_timestamp(),true) ELSE sample.observed_at+make_interval(secs=>sample.interval_seconds*3)<=clock_timestamp() OR sample.received_at+make_interval(secs=>sample.interval_seconds*3)<=clock_timestamp() END)) ORDER BY alert.id FOR UPDATE OF alert SKIP LOCKED LIMIT 200`)
	if e != nil {
		return e
	}
	type change struct{ id, state string }
	changes := []change{}
	for rows.Next() {
		var id, next string
		if e = rows.Scan(&id, &next); e != nil {
			rows.Close()
			return e
		}
		changes = append(changes, change{id, next})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	for _, change := range changes {
		if _, e = tx.ExecContext(ctx, `UPDATE monitor_alerts SET state=$2,version=version+1 WHERE id=$1 AND state<>$2`, change.id, change.state); e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO monitor_alert_events(id,alert_id,version,state,severity,sample_sequence,context,observed_at)SELECT $1,id,version,state,severity,sample_sequence,jsonb_build_object('reason',CASE WHEN state='suppressed' THEN 'maintained_inband_stopped' ELSE 'source_stale' END),clock_timestamp() FROM monitor_alerts WHERE id=$2 ON CONFLICT(alert_id,version)DO NOTHING`, RandomID(), change.id); e != nil {
			return e
		}
	}
	return tx.Commit()
}
