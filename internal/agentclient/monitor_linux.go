//go:build linux

package agentclient

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/hostmetrics"
	"github.com/enzoOvOyu/keel/internal/monitor"
)

// Sampling lives outside the reconnecting transport. Critical changes are
// fsynced before sending; overflow stops sampling and produces an explicit gap.
func RunMonitorCollector(ctx context.Context, c Config) error {
	if !c.MonitorEnabled {
		return nil
	}
	if _, e := monitorQueueDirectory(c); e != nil {
		return e
	}
	cpu := &hostmetrics.CPUCounter{}
	previous := map[string]monitor.Sample{}
	last := map[string]time.Time{}
	var baseline, policyID string
	var jittered bool
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			assignment, e := monitorLoadAssignment(c)
			if e != nil {
				return e
			}
			if assignment == nil || !now.Before(assignment.ValidUntil) || assignment.Policy.Stage == "maintained" {
				continue
			}
			if !jittered {
				jittered = true
				timer := time.NewTimer(time.Duration(assignment.InitialJitterMillis) * time.Millisecond)
				select {
				case <-ctx.Done():
					timer.Stop()
					return nil
				case <-timer.C:
				}
				now = time.Now()
			}
			if policyID != assignment.PolicyID {
				policyID = assignment.PolicyID
				last = map[string]time.Time{}
				previous = map[string]monitor.Sample{}
				baseline = ""
			}
		collect:
			for _, dtype := range []string{"D1", "D2", "D3", "D4", "D5", "D6"} {
				if !time.Now().Before(assignment.ValidUntil) {
					break
				}
				interval := assignment.Policy.Intervals[dtype]
				if interval == 0 || now.Sub(last[dtype]) < time.Duration(interval)*time.Second {
					continue
				}
				deadline := time.Now().Add(15 * time.Second)
				if assignment.ValidUntil.Before(deadline) {
					deadline = assignment.ValidUntil
				}
				work, cancel := context.WithDeadline(ctx, deadline)
				samples := hostmetrics.CollectGroup(work, dtype, time.Now().UTC(), cpu)
				if dtype == "D6" {
					samples = []monitor.Sample{{Key: "service.state", Entity: "keel-agent", State: "running", Quality: "good", ObservedAt: time.Now().UTC()}}
				}
				cancel()
				for offset := 0; offset < len(samples); offset += 12 {
					end := offset + 12
					if end > len(samples) {
						end = len(samples)
					}
					id := make([]byte, 16)
					if _, e = rand.Read(id); e != nil {
						return e
					}
					frame := monitor.Frame{Version: monitor.Version, ID: hex.EncodeToString(id), PolicyID: policyID, Full: baseline == "", BaselineID: baseline, Samples: []monitor.Sample{}, Unchanged: []monitor.Unchanged{}}
					for _, sample := range samples[offset:end] {
						old, ok := previous[sample.Identity()]
						old.ObservedAt = sample.ObservedAt
						if ok && string(mustMonitorJSON(old)) == string(mustMonitorJSON(sample)) && !frame.Full {
							frame.Unchanged = append(frame.Unchanged, monitor.Unchanged{Key: sample.Key, Entity: sample.Entity, ObservedAt: sample.ObservedAt})
						} else {
							frame.Samples = append(frame.Samples, sample)
						}
					}
					if e = monitorQueueAppend(c, frame); e != nil {
						if errors.Is(e, errMonitorQueueFull) {
							break collect
						}
						return e
					}
					baseline = frame.ID
					for _, sample := range samples[offset:end] {
						previous[sample.Identity()] = sample
					}
				}
				last[dtype] = now
			}
		}
	}
}
func mustMonitorJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
