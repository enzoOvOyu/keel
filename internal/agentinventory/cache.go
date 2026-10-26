package agentinventory

import (
	"bytes"
	"encoding/json"
	"io"
	"regexp"
	"time"
)

const CacheVersion = "agent-cache-inventory/v1"
const MaxCacheFrameBytes = 8192
const MaxCacheEntries = 64

var cacheDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

type CacheItem struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// CacheFrame is a complete ownership-index observation, not content verification.
// Seed project/media/object references and arbitrary paths never leave the host.
type CacheFrame struct {
	Version        string      `json:"version"`
	State          string      `json:"state"`
	CapturedAt     time.Time   `json:"captured_at"`
	ManifestSHA256 string      `json:"manifest_sha256,omitempty"`
	Entries        []CacheItem `json:"entries"`
	FailureCode    string      `json:"failure_code,omitempty"`
}

func (f CacheFrame) Validate(now time.Time) error {
	if f.Version != CacheVersion || f.CapturedAt.IsZero() || f.CapturedAt.Before(now.Add(-30*time.Second)) || f.CapturedAt.After(now.Add(5*time.Second)) || f.Entries == nil || len(f.Entries) > MaxCacheEntries {
		return ErrInvalid
	}
	if f.State == "unavailable" {
		if len(f.Entries) != 0 || f.ManifestSHA256 != "" {
			return ErrInvalid
		}
		switch f.FailureCode {
		case "cache_directory_unavailable", "cache_index_unavailable", "cache_index_busy", "cache_observation_interrupted":
			return nil
		}
		return ErrInvalid
	}
	if f.State != "ready" || f.FailureCode != "" || !cacheDigest.MatchString(f.ManifestSHA256) {
		return ErrInvalid
	}
	previous := ""
	for _, item := range f.Entries {
		if !cacheDigest.MatchString(item.SHA256) || item.SHA256 <= previous || item.Size < 1 || item.Size > 8192 {
			return ErrInvalid
		}
		previous = item.SHA256
	}
	return nil
}

func DecodeCache(raw []byte, now time.Time) (*CacheFrame, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	if len(raw) > MaxCacheFrameBytes {
		return nil, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	var f CacheFrame
	if d.Decode(&f) != nil || d.Decode(new(any)) != io.EOF || f.Validate(now) != nil {
		return nil, ErrInvalid
	}
	return &f, nil
}
