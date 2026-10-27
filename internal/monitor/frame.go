package monitor

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"regexp"
	"time"
)

var entityPattern = regexp.MustCompile(`^[A-Za-z0-9_.:@/-]{1,128}$`)

type MetricDefinition struct {
	DType, Unit string
	Counter     bool
}

var Metrics = map[string]MetricDefinition{
	"link.state": {"D1", "state", false}, "link.speed": {"D1", "Mbps", false},
	"dom.temperature": {"D2", "C", false}, "dom.rx_power": {"D2", "dBm", false}, "dom.tx_power": {"D2", "dBm", false},
	"host.cpu_utilization": {"D3", "percent", false}, "host.load1": {"D3", "load", false}, "host.memory_used": {"D3", "bytes", false}, "host.disk_used": {"D3", "bytes", false},
	"gpu.utilization": {"D4", "percent", false}, "gpu.temperature": {"D4", "C", false}, "gpu.power": {"D4", "W", false}, "gpu.ecc": {"D4", "count", true},
	"rdma.errors": {"D5", "count", true}, "rdma.pfc_pause": {"D5", "count", true}, "service.state": {"D6", "state", false},
	"bmc.health": {"B7", "state", false}, "bmc.power": {"B7", "state", false}, "bmc.temperature": {"B7", "C", false}, "bmc.sel_critical": {"B7", "count", true},
}

type Sample struct {
	Key          string    `json:"key"`
	Entity       string    `json:"entity"`
	Value        *float64  `json:"value"`
	State        string    `json:"state"`
	Quality      string    `json:"quality"`
	ObservedAt   time.Time `json:"observed_at"`
	Interface    string    `json:"interface,omitempty"`
	PermanentMAC string    `json:"permanent_mac,omitempty"`
	BDF          string    `json:"bdf,omitempty"`
}

func (s Sample) Identity() string { return s.Key + ":" + s.Entity }
func (s Sample) Validate(now time.Time) error {
	definition, ok := Metrics[s.Key]
	if !ok || !entityPattern.MatchString(s.Entity) || s.ObservedAt.IsZero() || s.ObservedAt.After(now.Add(5*time.Second)) || s.ObservedAt.Before(now.Add(-7*24*time.Hour)) || len(s.Interface) > 15 || len(s.PermanentMAC) > 32 || len(s.BDF) > 16 {
		return ErrInvalid
	}
	switch s.Quality {
	case "good":
		if definition.Unit == "state" {
			if s.Value != nil {
				return ErrInvalid
			}
			switch s.State {
			case "up", "down", "degraded", "on", "off", "running", "stopped", "healthy", "warning", "critical":
			default:
				return ErrInvalid
			}
		} else if s.Value == nil || s.State != "" || math.IsNaN(*s.Value) || math.IsInf(*s.Value, 0) || definition.Counter && *s.Value < 0 {
			return ErrInvalid
		}
	case "unknown", "unsupported", "gap":
		if s.Value != nil || s.State != "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type Unchanged struct {
	Key        string    `json:"key"`
	Entity     string    `json:"entity"`
	ObservedAt time.Time `json:"observed_at"`
}
type Frame struct {
	BaselineID string      `json:"baseline_id,omitempty"`
	GapStart   *time.Time  `json:"gap_start,omitempty"`
	GapEnd     *time.Time  `json:"gap_end,omitempty"`
	Version    string      `json:"version"`
	ID         string      `json:"id"`
	PolicyID   string      `json:"policy_id"`
	Full       bool        `json:"full"`
	Samples    []Sample    `json:"samples"`
	Unchanged  []Unchanged `json:"unchanged"`
}

func DecodeFrame(raw []byte, now time.Time) (*Frame, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > MaxFrameBytes {
		return nil, ErrInvalid
	}
	var f Frame
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&f) != nil || d.Decode(new(any)) != io.EOF || f.Version != Version || !entityPattern.MatchString(f.ID) || len(f.ID) > 64 || len(f.PolicyID) > 64 || len(f.Samples)+len(f.Unchanged) > MaxSamples || f.Full && len(f.Unchanged) > 0 {
		return nil, ErrInvalid
	}
	if !f.Full && !entityPattern.MatchString(f.BaselineID) || (f.GapStart == nil) != (f.GapEnd == nil) || f.GapStart != nil && (f.GapStart.IsZero() || f.GapEnd.Before(*f.GapStart) || f.GapEnd.After(now.Add(5*time.Second))) {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	for _, s := range f.Samples {
		if s.Validate(now) != nil || seen[s.Identity()] {
			return nil, ErrInvalid
		}
		seen[s.Identity()] = true
	}
	for _, u := range f.Unchanged {
		id := u.Key + ":" + u.Entity
		if _, ok := Metrics[u.Key]; !ok || !entityPattern.MatchString(u.Entity) || u.ObservedAt.IsZero() || u.ObservedAt.After(now.Add(5*time.Second)) || u.ObservedAt.Before(now.Add(-7*24*time.Hour)) || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
	}
	return &f, nil
}
