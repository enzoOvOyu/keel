package execution

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// RunClaim models a finite safe operation. Even an uncancellable simulator has a
// maximum 60s run; cancellation waits for that operation's real completion.
// Persisting a simulator receipt and applying it are separate fault boundaries.
func (s *Service) RunClaim(ctx context.Context, c *Claim) error {
	if c.Target.Mode == "provision" {
		return s.runProvisionPreparationClaim(ctx, c)
	}
	if c.Target.Mode == "agent" {
		return s.runHostClaim(ctx, c, "agent")
	}
	if c.Target.Mode == "ssh" {
		return s.runSSHClaim(ctx, c)
	}
	if c.Target.Mode == "fabric" {
		return s.runNetworkClaim(ctx, c)
	}
	if c.Target.Mode == "kea" {
		return s.runKeaClaim(ctx, c)
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	stopHeartbeat := make(chan struct{})
	var heartbeat sync.WaitGroup
	heartbeat.Add(1)
	go func() {
		defer heartbeat.Done()
		tick := time.NewTicker(c.LeaseDuration / 3)
		defer tick.Stop()
		for {
			select {
			case <-stopHeartbeat:
				return
			case <-tick.C:
				beat, cancelBeat := context.WithTimeout(context.Background(), c.LeaseDuration/2)
				requested, e := s.Heartbeat(beat, c)
				cancelBeat()
				if e != nil || requested {
					cancel()
				}
			}
		}
	}()
	started := time.Now().UTC().Truncate(time.Microsecond)
	result := "success"
	reason := "completed"
	timer := time.NewTimer(time.Duration(c.Task.Sim.DurationMS) * time.Millisecond)
	timeout := time.NewTimer(time.Duration(c.Task.TimeoutMS) * time.Millisecond)
	if c.Task.Sim.Uncancellable {
		<-timer.C
		reason = "safe_endpoint_reached"
	} else {
		select {
		case <-timer.C:
		case <-work.Done():
			result = "cancelled"
			reason = "cooperative_stop_confirmed"
		case <-timeout.C:
			result = "failed"
			reason = "timeout_stop_confirmed"
		}
	}
	timer.Stop()
	timeout.Stop()
	if result == "success" && (c.Task.Sim.Outcome == "failed" || c.Attempt.AttemptNo <= c.Task.Sim.FailFirst) {
		result = "failed"
		reason = "injected_failure"
	}
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf("%d:%d:%s", c.Target.Seed, c.Task.Sim.Seed, c.Task.ID)))
	r := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: result, StartedAt: started, FinishedAt: time.Now().UTC().Truncate(time.Microsecond), Output: map[string]any{"mode": "sim", "hardware_evidence": false, "reason": reason, "seed": c.Task.Sim.Seed, "target_seed": c.Target.Seed, "sample": hex.EncodeToString(fingerprint[:8])}}
	write, cancelWrite := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelWrite()
	err := s.RecordReceipt(write, r)
	if err == nil && c.Task.Sim.Outcome != "drop_receipt" {
		err = s.Complete(write, c)
	}
	close(stopHeartbeat)
	heartbeat.Wait()
	return err
}
