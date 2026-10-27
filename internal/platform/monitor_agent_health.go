package platform

import (
	"context"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
)

// Liveness comes from committed authenticated heartbeats, separately from host
// metrics, BMC reachability or power. A worker tick is not a new metric sample.
func (s *Store) TickMonitorAgentHealth(ctx context.Context) error {
	if !s.MonitorEnabled {
		return nil
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var locked bool
	if e = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(hashtextextended('monitor-agent-health',0))`).Scan(&locked); e != nil || !locked {
		return e
	}
	rows, e := tx.QueryContext(ctx, `WITH health AS(
 SELECT asset.project_id,asset.machine_id,asset.state,project.state AS project_state,last.last_seen,alert.state AS alert_state,
 CASE WHEN asset.state='maintained' OR project.state IN('archived','delivered') THEN 'suppressed' WHEN last.last_seen<=clock_timestamp()-interval '30 seconds' THEN 'firing' ELSE 'resolved' END AS wanted
 FROM project_assets asset JOIN projects project ON project.id=asset.project_id
 JOIN LATERAL(SELECT max(connection.last_seen_at) AS last_seen FROM agent_credentials identity JOIN agent_connections connection ON connection.credential_id=identity.id WHERE identity.project_id=asset.project_id AND identity.machine_id=asset.machine_id AND identity.revoked_at IS NULL AND identity.asset_version=asset.version)last ON last.last_seen IS NOT NULL
 LEFT JOIN monitor_alerts alert ON alert.project_id=asset.project_id AND alert.machine_id=asset.machine_id AND alert.source='host' AND alert.method='agent' AND alert.origin='real' AND alert.scope_key='agent' AND alert.rule_id='agent-lost'
 )SELECT project_id,machine_id,last_seen,wanted FROM health WHERE wanted IS DISTINCT FROM alert_state AND (alert_state IS NOT NULL OR wanted='firing') ORDER BY project_id,machine_id LIMIT 200`)
	if e != nil {
		return e
	}
	type transition struct {
		project, machine, state string
		last                    time.Time
	}
	changes := []transition{}
	for rows.Next() {
		var change transition
		if e = rows.Scan(&change.project, &change.machine, &change.last, &change.state); e != nil {
			rows.Close()
			return e
		}
		changes = append(changes, change)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return e
	}
	now := time.Now().UTC()
	for _, change := range changes {
		observation := MonitorObservation{DType: "D6", Source: "host", Method: "agent", Origin: "real", Sample: monitor.Sample{Key: "service.state", Entity: "agent", Quality: "unknown", ObservedAt: now}}
		finding := monitor.Finding{RuleID: "agent-lost", Severity: "warning", State: change.state, Context: map[string]any{"last_heartbeat": change.last, "heartbeat_seconds": 10, "missed_cycles": 3, "reason": "authenticated_heartbeat_timeout", "power_state": "unknown"}}
		if change.state == "suppressed" {
			finding.Context["reason"] = "inband_stopped"
		}
		if e = recordMonitorFindingTx(ctx, tx, change.project, change.machine, observation, finding, "monitor-agent-heartbeat/v1", now); e != nil {
			return e
		}
	}
	return tx.Commit()
}
