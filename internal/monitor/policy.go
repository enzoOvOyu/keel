// Package monitor owns the server telemetry contract. Display consumers never
// ingest Agent frames directly and telemetry never substitutes for gate proof.
package monitor

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
)

var ErrInvalid = errors.New("invalid monitoring contract")

const Version = "keel.monitor/v1"
const MaxFrameBytes = 8192
const MaxSamples = 64
const GlobalBytesPerSecond = 125000000 // 1 Gbit/s, including protocol reserve.
const PayloadBytesPerSecond = 100000000

type Policy struct {
	Stage                string         `json:"stage"`
	Intervals            map[string]int `json:"intervals"`
	FreshnessMultiplier  int            `json:"freshness_multiplier"`
	SteadyBytesPerSecond int            `json:"steady_bytes_per_second"`
	WindowBytesPerSecond int            `json:"window_bytes_per_second"`
	AgentlessSeconds     int            `json:"agentless_seconds"`
	RawRetentionDays     int            `json:"raw_retention_days"`
	HourlyRetentionDays  int            `json:"hourly_retention_days"`
}

func DefaultPolicy(stage string) Policy {
	var intervals []int
	switch stage {
	case "M1":
		intervals = []int{300, 300, 600, 0, 0, 0}
	case "M2":
		intervals = []int{60, 60, 300, 300, 0, 300}
	case "M3":
		intervals = []int{60, 60, 300, 300, 120, 300}
	case "M4":
		intervals = []int{30, 30, 120, 60, 60, 120}
	case "maintained":
		intervals = []int{0, 0, 0, 0, 0, 0}
	default:
		return Policy{Stage: stage}
	}
	p := Policy{Stage: stage, Intervals: map[string]int{}, FreshnessMultiplier: 3, SteadyBytesPerSecond: 10000, WindowBytesPerSecond: 200000, AgentlessSeconds: 600, RawRetentionDays: 7, HourlyRetentionDays: 90}
	for i, k := range []string{"D1", "D2", "D3", "D4", "D5", "D6"} {
		p.Intervals[k] = intervals[i]
	}
	return p
}
func (p Policy) Validate() error {
	switch p.Stage {
	case "M1", "M2", "M3", "M4", "maintained":
	default:
		return ErrInvalid
	}
	if len(p.Intervals) != 6 || p.FreshnessMultiplier != 3 || p.SteadyBytesPerSecond < 1024 || p.SteadyBytesPerSecond > 50000 || p.WindowBytesPerSecond < p.SteadyBytesPerSecond || p.WindowBytesPerSecond > 200000 || p.AgentlessSeconds < 300 || p.AgentlessSeconds > 900 || p.RawRetentionDays < 7 || p.RawRetentionDays > 365 || p.HourlyRetentionDays < 90 || p.HourlyRetentionDays > 3650 {
		return ErrInvalid
	}
	for _, k := range []string{"D1", "D2", "D3", "D4", "D5", "D6"} {
		n, ok := p.Intervals[k]
		if !ok || n != 0 && (n < 10 || n > 3600) || p.Stage == "maintained" && n != 0 {
			return ErrInvalid
		}
	}
	return nil
}

type Assignment struct {
	BasePolicyID          string     `json:"base_policy_id"`
	ValidUntil            time.Time  `json:"valid_until"`
	Version               string     `json:"version"`
	PolicyID              string     `json:"policy_id"`
	Policy                Policy     `json:"policy"`
	ExecutionID           string     `json:"execution_id"`
	WindowUntil           *time.Time `json:"window_until"`
	PayloadBytesPerSecond int        `json:"payload_bytes_per_second"`
	BurstBytes            int        `json:"burst_bytes"`
	InitialJitterMillis   int        `json:"initial_jitter_millis"`
}

// Allocation is a hard payload reservation over all admitted assets, not an
// assumption that every device may simultaneously send at its local maximum.
func Allocate(devices int) (int, error) {
	if devices < 1 {
		return 0, ErrInvalid
	}
	share := PayloadBytesPerSecond / devices
	if share > 10000 {
		share = 10000
	}
	if share < 1024 {
		return 0, ErrInvalid
	}
	return share, nil
}
func Jitter(machine string, interval time.Duration) time.Duration {
	if interval <= 0 {
		return 0
	}
	hash := sha256.Sum256([]byte(machine))
	return time.Duration(binary.BigEndian.Uint64(hash[:8]) % uint64(interval))
}
func Backoff(attempt int, machine string) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 6 {
		attempt = 6
	}
	base := time.Second * time.Duration(1<<attempt)
	return base + Jitter(machine, base/2)
}
