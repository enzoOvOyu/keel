package monitor

import (
	"bytes"
	"encoding/json"
	"io"
	"slices"
	"time"
)

const HostSnapshotVersion = "keel.monitor-snapshot/v1"
const MaxHostSnapshotBytes = 512 << 10
const MaxHostSnapshotSamples = 2048

type HostSnapshot struct {
	Version     string    `json:"version"`
	NetworkOnly bool      `json:"network_only"`
	StartedAt   time.Time `json:"started_at"`
	CollectedAt time.Time `json:"collected_at"`
	Truncated   bool      `json:"truncated"`
	Samples     []Sample  `json:"samples"`
}

// A snapshot has no heartbeat baseline or continuous-coverage promise. D6 is
// unknown unless an actual independent process observation exists.
func DecodeHostSnapshot(raw []byte, now time.Time) (*HostSnapshot, error) {
	if len(raw) == 0 || len(raw) > MaxHostSnapshotBytes || !hostSnapshotUniqueKeys(raw) {
		return nil, ErrInvalid
	}
	var out HostSnapshot
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&out) != nil || decoder.Decode(new(any)) != io.EOF || out.Version != HostSnapshotVersion || out.StartedAt.IsZero() || out.CollectedAt.Before(out.StartedAt) || out.CollectedAt.After(now.Add(5*time.Second)) || out.StartedAt.Before(now.Add(-7*24*time.Hour)) || out.CollectedAt.Sub(out.StartedAt) > 30*time.Second || out.Samples == nil || len(out.Samples) > MaxHostSnapshotSamples {
		return nil, ErrInvalid
	}
	seen := map[string]bool{}
	for _, sample := range out.Samples {
		definition, ok := Metrics[sample.Key]
		if !ok || sample.Validate(now) != nil || sample.ObservedAt.Before(out.StartedAt) || sample.ObservedAt.After(out.CollectedAt) || seen[sample.Identity()] || out.NetworkOnly && !slices.Contains([]string{"D1", "D2"}, definition.DType) || definition.DType == "B7" {
			return nil, ErrInvalid
		}
		seen[sample.Identity()] = true
	}
	return &out, nil
}

// Duplicate metadata/identity keys are ambiguous even if encoding/json would
// silently choose their last value. Limit nesting before decoding the schema.
func hostSnapshotUniqueKeys(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var visit func(int) bool
	visit = func(depth int) bool {
		if depth > 32 {
			return false
		}
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return true
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for decoder.More() {
				key, err := decoder.Token()
				if err != nil {
					return false
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return false
				}
				seen[name] = true
				if !visit(depth + 1) {
					return false
				}
			}
		case '[':
			for decoder.More() {
				if !visit(depth + 1) {
					return false
				}
			}
		default:
			return false
		}
		end, err := decoder.Token()
		return err == nil && (delim == '{' && end == json.Delim('}') || delim == '[' && end == json.Delim(']'))
	}
	if !visit(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
