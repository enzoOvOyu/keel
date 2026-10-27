package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"github.com/enzoOvOyu/keel/internal/monitor"
)

type monitorSwitchCandidate struct{ Run, Switch, Project, Machine, Asset, Port, Revision string }
type monitorSwitchOriginal struct {
	Hash, Origin, Serial string
	ValidUntil           time.Time
	Sample               fabricmon.Snapshot
}

// This consumes retained FR-0 observations only: no device connection, refresh,
// gate approval or new collection is performed by monitoring. Per-port receipts
// permit bounded incremental catch-up without duplicating raw observations.
func (s *Store) TickMonitorSources(ctx context.Context, limit int) (int, error) {
	if !s.MonitorEnabled {
		return 0, nil
	}
	if limit < 1 || limit > 200 {
		return 0, apiError(400, "VALIDATION_ERROR", "One to 200 source projections required")
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	rows, e := tx.QueryContext(ctx, `SELECT source.run_id,source.switch_id,mapping.project_id,mapping.machine_id,mapping.asset_id,mapping.physical_port_id,mapping.mapping_revision_id FROM fabric_switch_samples source JOIN fabric_collection_runs run ON run.id=source.run_id JOIN mapping_published_ports mapping ON mapping.project_id=run.project_id AND mapping.binding#>>'{observed,switch_id}'=source.switch_id JOIN projects project ON project.id=run.project_id WHERE run.collected_at>clock_timestamp()-interval '7 days' AND project.state NOT IN('archived','delivered') AND NOT EXISTS(SELECT 1 FROM monitor_switch_source_receipts receipt WHERE receipt.run_id=source.run_id AND receipt.switch_id=source.switch_id AND receipt.physical_port_id=mapping.physical_port_id) AND EXISTS(SELECT 1 FROM jsonb_array_elements(source.sample->'ports') port WHERE port->>'name'=mapping.binding#>>'{observed,switch_port}') ORDER BY run.collected_at DESC,source.run_id,source.switch_id,mapping.machine_id,mapping.physical_port_id LIMIT $1`, limit)
	if e != nil {
		return 0, e
	}
	candidates := []monitorSwitchCandidate{}
	for rows.Next() {
		var c monitorSwitchCandidate
		if e = rows.Scan(&c.Run, &c.Switch, &c.Project, &c.Machine, &c.Asset, &c.Port, &c.Revision); e != nil {
			rows.Close()
			return 0, e
		}
		candidates = append(candidates, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	now := time.Now().UTC()
	count, e := monitorDeviceCountTx(ctx, tx)
	if e != nil {
		return 0, e
	}
	share, e := monitor.Allocate(count)
	if e != nil {
		return 0, e
	}
	originals := map[string]*monitorSwitchOriginal{}
	revisions := map[string]*MappingRevision{}
	policies := map[string]*MonitorPolicyRevision{}
	frames := map[string][]string{}
	scopes := map[string]monitorSwitchCandidate{}
	processed := 0
	for _, c := range candidates {
		revision, loaded := revisions[c.Revision]
		if !loaded {
			revision, e = scanMappingRevision(tx.QueryRowContext(ctx, `SELECT `+mappingRevisionFields+` FROM mapping_revisions r JOIN mapping_assessments a ON a.id=r.assessment_id WHERE r.id=$1 AND r.project_id=$2 AND r.machine_id=$3`, c.Revision, c.Project, c.Machine))
			if e != nil {
				return 0, e
			}
			if e = s.mappingValidityTx(ctx, tx, revision); e != nil {
				return 0, e
			}
			revisions[c.Revision] = revision
		}
		if revision.EffectiveVerdict != "PASS" {
			continue
		}
		var binding *MappingBinding
		for i := range revision.Candidate.Bindings {
			b := &revision.Candidate.Bindings[i]
			if b.Plan.PhysicalPortID == c.Port && b.Observed.SwitchID == c.Switch {
				binding = b
				break
			}
		}
		if binding == nil {
			continue
		}
		original := originals[c.Run+":"+c.Switch]
		if original == nil {
			original = &monitorSwitchOriginal{}
			var raw []byte
			var source string
			e = tx.QueryRowContext(ctx, `SELECT sample.content_hash,sample.sample,run.valid_until,run.report->>'evidence_source',switch.serial_number FROM fabric_switch_samples sample JOIN fabric_collection_runs run ON run.id=sample.run_id JOIN switches switch ON switch.id=sample.switch_id WHERE sample.run_id=$1 AND sample.switch_id=$2 AND run.project_id=$3`, c.Run, c.Switch, c.Project).Scan(&original.Hash, &raw, &original.ValidUntil, &source, &original.Serial)
			if e != nil {
				return 0, e
			}
			if json.Unmarshal(raw, &original.Sample) != nil || original.Sample.Validate(original.Serial, original.Sample.CollectedAt) != nil || fabricboot.ManagementPlanDigest(original.Sample) != original.Hash {
				return 0, apiError(409, "MONITOR_SOURCE_CORRUPT", "Retained network snapshot failed its original identity/digest")
			}
			original.Origin = "sim"
			if source == "device" {
				original.Origin = "real"
			}
			if deadline := original.Sample.CollectedAt.Add(5 * time.Minute); deadline.Before(original.ValidUntil) {
				original.ValidUntil = deadline
			}
			originals[c.Run+":"+c.Switch] = original
		}
		var peer *fabricmon.Port
		for i := range original.Sample.Ports {
			if original.Sample.Ports[i].Name == binding.Observed.SwitchPort {
				peer = &original.Sample.Ports[i]
				break
			}
		}
		if peer == nil {
			continue
		}
		key := c.Project + ":" + c.Machine
		policy := policies[key]
		if policy == nil {
			var state string
			if e = tx.QueryRowContext(ctx, `SELECT state FROM project_assets WHERE id=$1 AND project_id=$2 AND machine_id=$3`, c.Asset, c.Project, c.Machine).Scan(&state); e != nil {
				return 0, e
			}
			stage := monitorStage(state)
			if stage == "maintained" {
				stage = "M2"
			} // Only in-band collection stops; this is an independent network snapshot.
			policy, e = monitorPolicyTx(ctx, tx, c.Project, stage)
			if e != nil {
				return 0, e
			}
			policies[key] = policy
		}
		samples := monitorSwitchSamples(*peer, c.Port, original.Sample.CollectedAt)
		payload := networkJSON(samples)
		if _, e = tx.ExecContext(ctx, `SAVEPOINT monitor_source_port`); e != nil {
			return 0, e
		}
		if e = monitorConsumeBudgetTx(ctx, tx, "global", len(payload), monitor.PayloadBytesPerSecond, monitor.PayloadBytesPerSecond, now); e == nil {
			e = monitorConsumeBudgetTx(ctx, tx, "agent:"+c.Machine, len(payload), min(share, policy.Policy.SteadyBytesPerSecond), monitor.MaxFrameBytes, now)
		}
		if e != nil {
			var domain *Error
			if !errors.As(e, &domain) || domain.Status != 429 {
				return 0, e
			}
			if _, e = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT monitor_source_port`); e != nil {
				return 0, e
			}
			if _, e = tx.ExecContext(ctx, `RELEASE SAVEPOINT monitor_source_port`); e != nil {
				return 0, e
			}
			continue // Original source remains pending; never acknowledge dropped data.
		}
		frame := digest("switch-source:" + c.Run + ":" + c.Switch + ":" + c.Port)
		changed, err := tx.ExecContext(ctx, `INSERT INTO monitor_switch_source_receipts(frame_id,run_id,switch_id,project_id,machine_id,physical_port_id,mapping_revision_id,source_hash,sample_count,received_at)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(run_id,switch_id,physical_port_id)DO NOTHING`, frame, c.Run, c.Switch, c.Project, c.Machine, c.Port, c.Revision, original.Hash, len(samples), now)
		if err != nil {
			return 0, err
		}
		n, err := changed.RowsAffected()
		if err != nil {
			return 0, err
		}
		if n > 0 {
			proof := monitorSampleProof{SourceRef: "fabric:" + c.Run + ":" + c.Switch, SourceHash: original.Hash, PhysicalPortID: c.Port, MappingRevisionID: c.Revision, SnapshotOnly: true, ValidUntil: &original.ValidUntil}
			for _, sample := range samples {
				interval := max(10, policy.Policy.Intervals[monitor.Metrics[sample.Key].DType])
				if e = s.recordMonitorSourceSampleTx(ctx, tx, c.Project, c.Machine, "", frame, policy.ID, "switch", "collector", original.Origin, interval, sample, now, proof); e != nil {
					return 0, e
				}
			}
			frames[key] = append(frames[key], frame)
			scopes[key] = c
			processed++
		}
		if _, e = tx.ExecContext(ctx, `RELEASE SAVEPOINT monitor_source_port`); e != nil {
			return 0, e
		}
	}
	for key, ids := range frames {
		c := scopes[key]
		rows, e = tx.QueryContext(ctx, `SELECT `+monitorObservationFields+` FROM monitor_metric_samples sample JOIN monitor_metric_latest latest ON latest.sample_sequence=sample.sequence WHERE sample.project_id=$1 AND sample.machine_id=$2 AND sample.credential_id IS NULL AND sample.frame_id=ANY($3) ORDER BY sample.sequence`, c.Project, c.Machine, ids)
		if e != nil {
			return 0, e
		}
		observations := []MonitorObservation{}
		for rows.Next() {
			item, err := scanMonitorObservation(rows, now)
			if err != nil {
				rows.Close()
				return 0, err
			}
			observations = append(observations, *item)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return 0, e
		}
		if e = s.monitorEvaluateObservationsTx(ctx, tx, c.Project, c.Machine, observations, now); e != nil {
			return 0, e
		}
	}
	return processed, tx.Commit()
}
