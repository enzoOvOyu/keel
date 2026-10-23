package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"sort"
	"time"
)

type networkRuleHistoricalSample struct {
	Hash     string
	Snapshot fabricmon.Snapshot
}

// The minimum rule window is 60s. Replay one actual observation boundary per
// minute, plus every configuration snapshot boundary. No archived judgement is
// changed. Oversized previews fail explicitly rather than silently truncate.
func networkRuleReplayTx(ctx context.Context, tx *sql.Tx, project string, beforeRules, afterRules []fabricmon.LosslessRule) ([]fabricmon.LosslessFinding, []fabricmon.LosslessFinding, int, int, []string, error) {
	pkg, devices, _, e := fabricInventoryTx(ctx, tx, project)
	if e != nil {
		return nil, nil, 0, 0, nil, e
	}
	variables, e := loadLosslessIntentTx(ctx, tx, project)
	if e != nil {
		return nil, nil, 0, 0, nil, e
	}
	collection, e := latestFabricCollectionTx(ctx, tx, project)
	if e != nil {
		return nil, nil, 0, 0, nil, e
	}
	source := ""
	if collection != nil {
		source = collection.EvidenceSource
	}
	catalog, e := fabricmon.DefaultLosslessCatalog()
	if e != nil {
		return nil, nil, 0, 0, nil, e
	}
	registry, e := catalog.Registry()
	if e != nil {
		return nil, nil, 0, 0, nil, e
	}
	now := time.Now().UTC()
	since := now.Add(-48 * time.Hour)
	before, after := []fabricmon.LosslessFinding{}, []fabricmon.LosslessFinding{}
	warnings := []string{"replay_uses_current_approved_intent", "replay_resolution_60_seconds"}
	samplesCount, evaluationCount := 0, 0
	for _, device := range devices {
		rows, e := tx.QueryContext(ctx, `SELECT s.content_hash,s.sample FROM fabric_switch_samples s JOIN fabric_collection_runs r ON r.id=s.run_id WHERE r.project_id=$1 AND r.package_id=$2 AND s.switch_id=$3 AND r.collected_at>now()-interval '48 hours' AND r.report->>'evidence_source'=$4 ORDER BY r.collected_at,s.content_hash LIMIT 2882`, project, pkg, device.ID, source)
		if e != nil {
			return nil, nil, 0, 0, nil, e
		}
		samples := []networkRuleHistoricalSample{}
		points := []fabricmon.MetricPoint{}
		seen := map[string]string{}
		conflict := false
		minutes := map[int64]time.Time{}
		for rows.Next() {
			var sample networkRuleHistoricalSample
			var raw []byte
			if e = rows.Scan(&sample.Hash, &raw); e != nil {
				rows.Close()
				return nil, nil, 0, 0, nil, e
			}
			if e = json.Unmarshal(raw, &sample.Snapshot); e != nil {
				rows.Close()
				return nil, nil, 0, 0, nil, e
			}
			samples = append(samples, sample)
			for _, p := range sample.Snapshot.MetricPoints {
				if p.At.Before(since) || p.At.After(now) {
					continue
				}
				if p.Provider == "" {
					p.Provider = sample.Snapshot.Provider
				}
				identity := fabricMetricIdentity(p)
				encoded := string(networkJSON(p))
				if old, ok := seen[identity]; ok {
					conflict = conflict || old != encoded
					continue
				}
				seen[identity] = encoded
				points = append(points, p)
				minute := p.At.Unix() / 60
				if old, ok := minutes[minute]; !ok || p.At.After(old) {
					minutes[minute] = p.At
				}
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, nil, 0, 0, nil, e
		}
		if len(samples) > 2881 || len(points) > 100000 {
			return nil, nil, 0, 0, nil, apiError(413, "NETWORK_RULE_REPLAY_LIMIT", "Preview exceeds bounded history capacity; narrow the retained test dataset or use the planned asynchronous replay worker")
		}
		samplesCount += len(samples)
		if conflict {
			points = nil
			warnings = append(warnings, "metric_history_conflict:"+device.ID)
		}
		evaluation, e := fabricmon.NewMetricEvaluation(registry, points, nil, now)
		if e != nil {
			return nil, nil, 0, 0, nil, e
		}
		units, intents := deviceLosslessUnits(device, variables[device.Serial])
		checkpoints := []time.Time{}
		for _, at := range minutes {
			checkpoints = append(checkpoints, at)
		}
		for _, sample := range samples {
			checkpoints = append(checkpoints, sample.Snapshot.CollectedAt)
		}
		if len(checkpoints) == 0 {
			checkpoints = append(checkpoints, now)
			warnings = append(warnings, "no_historical_samples:"+device.ID)
		}
		sort.Slice(checkpoints, func(i, j int) bool { return checkpoints[i].Before(checkpoints[j]) })
		unique := []time.Time{}
		for _, at := range checkpoints {
			if len(unique) == 0 || !unique[len(unique)-1].Equal(at) {
				unique = append(unique, at)
			}
		}
		selected := -1
		for _, at := range unique {
			evaluationCount += len(units)
			if evaluationCount > 10000 {
				return nil, nil, 0, 0, nil, apiError(413, "NETWORK_RULE_REPLAY_LIMIT", "Preview exceeds 10000 port-priority checkpoints; asynchronous large-fabric replay is not yet available")
			}
			for selected+1 < len(samples) && !samples[selected+1].Snapshot.CollectedAt.After(at) {
				selected++
			}
			applied := map[string]map[string]any{}
			if selected >= 0 && at.Sub(samples[selected].Snapshot.CollectedAt) <= 5*time.Minute {
				applied = samples[selected].Snapshot.AppliedLossless
			}
			child := *evaluation
			child.Now = at
			for _, unit := range units {
				child.Facts = map[string]any{"intended": intents[unit.Port], "applied": applied[unit.Port]}
				old, new := fabricmon.EvaluateLosslessUnit(&child, unit, beforeRules), fabricmon.EvaluateLosslessUnit(&child, unit, afterRules)
				for i := range old {
					stamp := at
					old[i].EvaluatedAt = &stamp
				}
				for i := range new {
					stamp := at
					new[i].EvaluatedAt = &stamp
				}
				before = append(before, old...)
				after = append(after, new...)
			}
		}
	}
	return before, after, samplesCount, evaluationCount, warnings, nil
}
