package taskrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/enzoOvOyu/keel/internal/hostmetrics"
	"github.com/enzoOvOyu/keel/internal/monitor"
	"github.com/enzoOvOyu/keel/internal/precollect"
)

func validateMonitorObservation(o Observation) error {
	if o.Output["stopped"] != true {
		return ErrRejected
	}
	if o.State != "success" {
		if o.Output["reason"] != "monitor_capture_failed" {
			return ErrRejected
		}
		return nil
	}
	sha, ok := o.Output["monitor_sha256"].(string)
	size, valid := scriptOutputInteger(o.Output["monitor_size"])
	if !ok || !scriptDigestPattern.MatchString(sha) || !valid || size < 1 || size > monitor.MaxHostSnapshotBytes || o.Output["monitor_version"] != monitor.HostSnapshotVersion || o.Output["reason"] != "monitor_observed" {
		return ErrRejected
	}
	return nil
}

func captureMonitor(ctx context.Context, dir string, networkOnly bool) (map[string]any, string, error) {
	return captureMonitorWith(ctx, dir, networkOnly, hostmetrics.CollectGroup)
}
func captureMonitorWith(ctx context.Context, dir string, networkOnly bool, collect func(context.Context, string, time.Time, *hostmetrics.CPUCounter) []monitor.Sample) (map[string]any, string, error) {
	snapshot := monitor.HostSnapshot{Version: monitor.HostSnapshotVersion, NetworkOnly: networkOnly, StartedAt: time.Now().UTC(), Samples: []monitor.Sample{}}
	cpu := &hostmetrics.CPUCounter{}
	groups := []string{"D1", "D2"}
	if !networkOnly {
		groups = append(groups, "D3", "D4", "D5", "D6")
	}
	for _, group := range groups {
		if ctx.Err() != nil {
			return map[string]any{"stopped": true}, "monitor_capture_failed", nil
		}
		samples := collect(ctx, group, time.Now().UTC(), cpu)
		remaining := monitor.MaxHostSnapshotSamples - len(snapshot.Samples)
		if len(samples) > remaining {
			samples = samples[:remaining]
			snapshot.Truncated = true
		}
		snapshot.Samples = append(snapshot.Samples, samples...)
		if snapshot.Truncated {
			break
		}
	}
	snapshot.CollectedAt = time.Now().UTC()
	raw, err := json.Marshal(snapshot)
	if err != nil || ctx.Err() != nil {
		return map[string]any{"stopped": true}, "monitor_capture_failed", nil
	}
	if _, err = monitor.DecodeHostSnapshot(raw, snapshot.CollectedAt); err != nil {
		return map[string]any{"stopped": true}, "monitor_capture_failed", nil
	}
	file, err := os.OpenFile(filepath.Join(dir, "monitor.json"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, "", ErrUnknown
	}
	_, err = file.Write(raw)
	if err == nil {
		err = file.Sync()
	}
	closed := file.Close()
	if err != nil || closed != nil {
		return nil, "", ErrUnknown
	}
	parent, err := os.Open(dir)
	if err != nil {
		return nil, "", ErrUnknown
	}
	err = parent.Sync()
	parent.Close()
	if err != nil {
		return nil, "", ErrUnknown
	}
	return map[string]any{"stopped": true, "monitor_version": monitor.HostSnapshotVersion, "monitor_sha256": precollect.Hash(raw), "monitor_size": len(raw)}, "monitor_observed", nil
}
