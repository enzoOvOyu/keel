package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
)

func (s *Store) monitorEvaluateFrameTx(ctx context.Context, tx *sql.Tx, project, machine, credential, frame string, now time.Time) error {
	rows, e := tx.QueryContext(ctx, `SELECT `+monitorObservationFields+` FROM monitor_metric_samples sample JOIN monitor_metric_latest latest ON latest.sample_sequence=sample.sequence WHERE sample.project_id=$1 AND sample.machine_id=$2 AND sample.credential_id IS NOT DISTINCT FROM nullif($3,'')::varchar AND sample.frame_id=$4 ORDER BY sample.sequence`, project, machine, credential, frame)
	if e != nil {
		return e
	}
	observations := []MonitorObservation{}
	for rows.Next() {
		item, e := scanMonitorObservation(rows, now)
		if e != nil {
			rows.Close()
			return e
		}
		observations = append(observations, *item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	return s.monitorEvaluateObservationsTx(ctx, tx, project, machine, observations, now)
}
func (s *Store) monitorEvaluateObservationsTx(ctx context.Context, tx *sql.Tx, project, machine string, observations []MonitorObservation, now time.Time) error {
	limits, e := monitorLimitsTx(ctx, tx, project)
	if e != nil {
		return e
	}
	var current *MappingRevision
	e = tx.QueryRowContext(ctx, `SELECT id FROM project_assets WHERE project_id=$1 AND machine_id=$2`, project, machine).Scan(new(string))
	if e != nil {
		return e
	}
	candidate, e := scanMappingRevision(tx.QueryRowContext(ctx, `SELECT `+mappingRevisionFields+` FROM mapping_revisions r JOIN mapping_assessments a ON a.id=r.assessment_id WHERE r.project_id=$1 AND r.machine_id=$2 ORDER BY r.version DESC LIMIT 1`, project, machine))
	if e != nil && !errors.Is(e, ErrNotFound) {
		return e
	}
	if e == nil {
		if e = s.mappingValidityTx(ctx, tx, candidate); e != nil {
			return e
		}
		if candidate.EffectiveVerdict == "PASS" {
			current = candidate
		}
	}
	enabled := map[string]bool{}
	if current != nil {
		for _, binding := range current.Candidate.Bindings {
			enabled[binding.Plan.PhysicalPortID] = binding.Plan.Role != "unused"
		}
	}
	for _, observation := range observations {
		if observation.Origin != "real" || observation.Freshness != "fresh" {
			continue
		}
		sample := observation.Sample
		expectedUp := current != nil && observation.MappingRevisionID == current.ID && enabled[observation.PhysicalPortID]
		var previous *monitor.Sample
		var raw []byte
		e = tx.QueryRowContext(ctx, `SELECT sample FROM monitor_metric_samples WHERE project_id=$1 AND machine_id=$2 AND source=$3 AND method=$4 AND origin=$5 AND metric_key=$6 AND entity_key=$7 AND observed_at<$8 ORDER BY observed_at DESC,sequence DESC LIMIT 1`, project, machine, observation.Source, observation.Method, observation.Origin, sample.Key, sample.Entity, sample.ObservedAt).Scan(&raw)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil {
			var prior monitor.Sample
			if e = json.Unmarshal(raw, &prior); e != nil {
				return e
			}
			previous = &prior
		}
		transitions := 0
		if expectedUp && sample.Key == "link.state" {
			e = tx.QueryRowContext(ctx, `WITH history AS(SELECT observed_at,sample->>'state' AS state,sample->>'quality' AS quality,lag(sample->>'state')OVER(ORDER BY observed_at,sequence) AS previous_state FROM monitor_metric_samples WHERE project_id=$1 AND machine_id=$2 AND physical_port_id=$3 AND mapping_revision_id=$4 AND source=$5 AND method=$6 AND origin='real' AND metric_key='link.state' AND observed_at>=$7 AND observed_at<=$8) SELECT count(*) FROM history WHERE quality='good' AND state='down' AND previous_state='up' AND observed_at>=$9`, project, machine, observation.PhysicalPortID, current.ID, observation.Source, observation.Method, sample.ObservedAt.Add(-10*time.Minute-time.Duration(observation.StaleAfterSeconds)*time.Second), sample.ObservedAt, sample.ObservedAt.Add(-10*time.Minute)).Scan(&transitions)
			if e != nil {
				return e
			}
		}
		for _, finding := range monitor.Evaluate(sample, previous, expectedUp, transitions, limits) {
			if observation.SnapshotOnly && finding.RuleID == "link-flapping" {
				continue
			}
			// No valid current physical mapping means no inferred port incident.
			if (strings.HasPrefix(finding.RuleID, "link-") || strings.HasPrefix(finding.RuleID, "optics-") || finding.RuleID == "pfc-pause-growth") && !expectedUp {
				continue
			}
			if e = recordMonitorFindingTx(ctx, tx, project, machine, observation, finding, "monitor/v1:"+digest(string(networkJSON(limits))), now); e != nil {
				return e
			}
		}
	}
	return nil
}
func recordMonitorFindingTx(ctx context.Context, tx *sql.Tx, project, machine string, sample MonitorObservation, finding monitor.Finding, ruleVersion string, now time.Time) error {
	var sampleSequence any
	if sample.Sequence > 0 {
		sampleSequence = sample.Sequence
	}
	scopeKey := sample.PhysicalPortID
	if scopeKey == "" {
		scopeKey = sample.Sample.Entity
	}
	if e := lockKey(ctx, tx, "monitor-alert:"+project+":"+machine+":"+sample.Source+":"+sample.Method+":"+sample.Origin+":"+scopeKey+":"+finding.RuleID); e != nil {
		return e
	}
	var id, oldState, issue string
	var oldSeverity string
	var version int64
	e := tx.QueryRowContext(ctx, `SELECT id,state,severity,coalesce(issue_id,''),version FROM monitor_alerts WHERE project_id=$1 AND machine_id=$2 AND source=$3 AND method=$4 AND origin=$5 AND scope_key=$6 AND rule_id=$7 FOR UPDATE`, project, machine, sample.Source, sample.Method, sample.Origin, scopeKey, finding.RuleID).Scan(&id, &oldState, &oldSeverity, &issue, &version)
	fresh := errors.Is(e, sql.ErrNoRows)
	if e != nil && !fresh {
		return e
	}
	if fresh && finding.State != "firing" {
		return nil
	}
	var owner string
	if e = tx.QueryRowContext(ctx, `SELECT created_by FROM projects WHERE id=$1`, project).Scan(&owner); e != nil {
		return e
	}
	state := finding.State
	if issue != "" {
		var issueState string
		if e = tx.QueryRowContext(ctx, `SELECT state FROM issues WHERE id=$1`, issue).Scan(&issueState); e != nil {
			return e
		}
		if issueState == "in_repair" && state == "firing" {
			state = "suppressed"
		}
		if issueState == "closed" && finding.State == "firing" {
			issue = ""
		}
	}
	createdIssue := false
	if finding.State == "firing" && finding.Severity == "critical" && issue == "" && finding.Category != "" {
		input := CreateIssueInput{Title: finding.RuleID + " · " + sample.Sample.Entity, Category: finding.Category, Source: "portmon", Phase: "delivery", Anchors: []IssueAnchor{{Kind: "machine", ID: machine}}, Evidence: IssueEvidence{ObservedAt: sample.Sample.ObservedAt, SourceVersion: ruleVersion, Expected: "enabled role port healthy", Actual: string(networkJSON(finding.Context))}}
		if sample.PhysicalPortID != "" {
			input.Anchors = append(input.Anchors, IssueAnchor{Kind: "port", ID: sample.PhysicalPortID})
		}
		created, e := insertIssueTx(ctx, tx, &AuthSession{User: UserSummary{ID: owner}}, project, input, "monitor_rule", "", map[string]any{"sample_sequence": sample.Sequence, "source": sample.Source, "method": sample.Method, "mapping_revision_id": sample.MappingRevisionID})
		if e != nil {
			return e
		}
		issue = created.ID
		createdIssue = true
	}
	if fresh {
		id = RandomID()
		version = 1
		_, e = tx.ExecContext(ctx, `INSERT INTO monitor_alerts(id,project_id,machine_id,physical_port_id,mapping_revision_id,source,method,origin,entity_key,dtype,rule_id,rule_version,severity,state,first_at,last_at,count,version,sample_sequence,issue_id,context,scope_key)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$15,1,1,$16,$17,$18,$19)`, id, project, machine, nullBOMString(sample.PhysicalPortID), nullBOMString(sample.MappingRevisionID), sample.Source, sample.Method, sample.Origin, sample.Sample.Entity, sample.DType, finding.RuleID, ruleVersion, finding.Severity, state, sample.Sample.ObservedAt, sampleSequence, nullBOMString(issue), networkJSON(finding.Context), scopeKey)
	} else {
		version++
		_, e = tx.ExecContext(ctx, `UPDATE monitor_alerts SET physical_port_id=$2,mapping_revision_id=$3,rule_version=$4,severity=$5,state=$6::varchar,last_at=$7,count=count+CASE WHEN $6::varchar IN('firing','suppressed')THEN 1 ELSE 0 END,version=$8,sample_sequence=$9,issue_id=$10,context=$11 WHERE id=$1`, id, nullBOMString(sample.PhysicalPortID), nullBOMString(sample.MappingRevisionID), ruleVersion, finding.Severity, state, sample.Sample.ObservedAt, version, sampleSequence, nullBOMString(issue), networkJSON(finding.Context))
	}
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO monitor_alert_events(id,alert_id,version,state,severity,sample_sequence,context,observed_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, RandomID(), id, version, state, finding.Severity, sampleSequence, networkJSON(finding.Context), sample.Sample.ObservedAt); e != nil {
		return e
	}
	if createdIssue && sample.PhysicalPortID != "" {
		binding, err := tx.ExecContext(ctx, `INSERT INTO portmon_issue_bindings(issue_id,project_id,asset_id,machine_id,asset_version,physical_port_id,mapping_revision_id,alert_id,sample_sequence,rule_id,rule_version) SELECT $1::varchar,$2::varchar,asset.id,$3::varchar,asset.version,$4::varchar,$5::varchar,$6::varchar,$7::bigint,$8::varchar,$9::text FROM project_assets asset WHERE asset.project_id=$2 AND asset.machine_id=$3`, issue, project, machine, sample.PhysicalPortID, sample.MappingRevisionID, id, sample.Sequence, finding.RuleID, ruleVersion)
		if err != nil {
			return err
		}
		count, err := binding.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return apiError(409, "PORTMON_DISCOVERY_SCOPE_CHANGED", "Original asset scope changed during port discovery")
		}
	}
	if state == "firing" && (fresh || oldState != "firing" || oldSeverity != finding.Severity) {
		_, e = tx.ExecContext(ctx, `INSERT INTO monitor_notification_outbox(id,project_id,alert_id,aggregate_version,event_type,severity,source,issue_id,created_at)VALUES($1,$2,$3,$4,'monitor.alert',$5,$6,$7,$8)`, RandomID(), project, id, version, finding.Severity, sample.Source, nullBOMString(issue), now)
	}
	return e
}

func monitorLimitsTx(ctx context.Context, tx *sql.Tx, project string) (map[string]monitor.Limit, error) {
	thresholds, e := fabricDDMLimitsTx(ctx, tx, project)
	if e != nil {
		return nil, e
	}
	limits := map[string]monitor.Limit{}
	for key, value := range thresholds {
		limits[key] = monitor.Limit{WarnMin: value.WarnMin, WarnMax: value.WarnMax, CriticalMin: value.CriticalMin, CriticalMax: value.CriticalMax}
	}
	// PFC growth has no invented default threshold. It remains unknown until
	// the shared catalog supplies an explicit approved project/global value.
	values, e := catalogDefaultsTx(ctx, tx, project, "mon.pfc_pause_delta.critical_max")
	if e != nil {
		return nil, e
	}
	if raw := values["mon.pfc_pause_delta.critical_max"]; raw != "" {
		var value float64
		if json.Unmarshal([]byte(raw), &value) != nil {
			return nil, apiError(409, "MONITOR_THRESHOLD_INVALID", "PFC threshold must be numeric")
		}
		limits["pfc_pause_delta"] = monitor.Limit{CriticalMax: &value}
	}
	return limits, nil
}
