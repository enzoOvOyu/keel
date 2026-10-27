package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
)

// Incremental durable projection charges the same host/global budget as Agent
// telemetry. Raw capture admission is not an ACK that samples were ingested.
func (s *Store) TickMonitorSSHSources(ctx context.Context, limit int) (int, error) {
	if !s.MonitorEnabled {
		return 0, nil
	}
	if limit < 1 || limit > 200 {
		return 0, apiError(400, "VALIDATION_ERROR", "One to 200 original samples required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT capture.command_id,capture.project_id,capture.machine_id,capture.raw_sha256,request.policy_id,request.interval_seconds,request.payload_bytes_per_second,capture.collected_at,point.ordinality-1,point.sample FROM monitor_ssh_captures capture JOIN monitor_ssh_requests request ON request.execution_id=capture.execution_id AND request.machine_id=capture.machine_id JOIN exec_runner_receipts settled ON settled.command_id=capture.command_id AND settled.attempt_id=capture.attempt_id AND settled.fencing_token=capture.fencing_token JOIN project_assets asset ON asset.id=request.asset_id AND asset.project_id=request.project_id AND asset.machine_id=request.machine_id AND asset.version=request.asset_version JOIN projects project ON project.id=request.project_id CROSS JOIN LATERAL jsonb_array_elements(capture.snapshot->'samples') WITH ORDINALITY point(sample,ordinality) WHERE settled.result='success' AND settled.payload#>>'{output,evidence_hash}'=capture.raw_sha256 AND capture.collected_at>clock_timestamp()-interval '7 days' AND project.state NOT IN('archived','delivered') AND asset.state<>'maintained' AND NOT EXISTS(SELECT 1 FROM monitor_ssh_sample_receipts receipt WHERE receipt.command_id=capture.command_id AND receipt.sample_index=point.ordinality-1) ORDER BY capture.collected_at DESC,capture.command_id,point.ordinality LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	type candidate struct {
		command, project, machine, hash, policy string
		interval, rate, index                   int
		collected                               time.Time
		raw                                     []byte
	}
	candidates := []candidate{}
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.command, &item.project, &item.machine, &item.hash, &item.policy, &item.interval, &item.rate, &item.collected, &item.index, &item.raw); err != nil {
			rows.Close()
			return 0, err
		}
		candidates = append(candidates, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	count, err := monitorDeviceCountTx(ctx, tx)
	if err != nil {
		return 0, err
	}
	share, err := monitor.Allocate(count)
	if err != nil {
		return 0, err
	}
	now := time.Now().UTC()
	processed := 0
	frames := map[string][]string{}
	scopes := map[string]candidate{}
	for _, item := range candidates {
		var sample monitor.Sample
		if json.Unmarshal(item.raw, &sample) != nil || sample.Validate(now) != nil {
			return 0, apiError(409, "MONITOR_CAPTURE_CORRUPT", "Retained SSH snapshot no longer matches its metric schema")
		}
		if _, err = tx.ExecContext(ctx, `SAVEPOINT monitor_ssh_point`); err != nil {
			return 0, err
		}
		err = monitorConsumeBudgetTx(ctx, tx, "global", len(item.raw), monitor.PayloadBytesPerSecond, monitor.PayloadBytesPerSecond, now)
		if err == nil {
			err = monitorConsumeBudgetTx(ctx, tx, "agent:"+item.machine, len(item.raw), min(share, item.rate), monitor.MaxFrameBytes, now)
		}
		if err != nil {
			var domain *Error
			if !errors.As(err, &domain) || domain.Status != 429 {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `ROLLBACK TO SAVEPOINT monitor_ssh_point`); err != nil {
				return 0, err
			}
			if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT monitor_ssh_point`); err != nil {
				return 0, err
			}
			continue
		}
		frame := digest("ssh-source:" + item.command + ":" + string(networkJSON(item.index)))
		result, err := tx.ExecContext(ctx, `INSERT INTO monitor_ssh_sample_receipts(frame_id,command_id,sample_index,received_at)VALUES($1,$2,$3,$4)ON CONFLICT(command_id,sample_index)DO NOTHING`, frame, item.command, item.index, now)
		if err != nil {
			return 0, err
		}
		changed, err := result.RowsAffected()
		if err != nil {
			return 0, err
		}
		if changed > 0 {
			until := item.collected.Add(time.Duration(item.interval) * time.Second)
			proof := monitorSampleProof{SourceRef: "ssh:" + item.command, SourceHash: item.hash, SnapshotOnly: true, ValidUntil: &until}
			if err = s.recordMonitorSourceSampleTx(ctx, tx, item.project, item.machine, "", frame, item.policy, "host", "ssh", "real", item.interval, sample, now, proof); err != nil {
				return 0, err
			}
			key := item.project + ":" + item.machine
			frames[key] = append(frames[key], frame)
			scopes[key] = item
			processed++
		}
		if _, err = tx.ExecContext(ctx, `RELEASE SAVEPOINT monitor_ssh_point`); err != nil {
			return 0, err
		}
	}
	for key, ids := range frames {
		item := scopes[key]
		if err = s.monitorEvaluateSourceFramesTx(ctx, tx, item.project, item.machine, ids, now); err != nil {
			return 0, err
		}
	}
	return processed, tx.Commit()
}
