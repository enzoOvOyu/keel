package agentinventory

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAgentCacheFrameBoundsAndOwnershipOnlyContract(t *testing.T) {
	now := time.Now().UTC()
	f := CacheFrame{Version: CacheVersion, State: "ready", CapturedAt: now, ManifestSHA256: strings.Repeat("a", 64), Entries: []CacheItem{}}
	for i := 1; i <= MaxCacheEntries; i++ {
		f.Entries = append(f.Entries, CacheItem{SHA256: fmt.Sprintf("%064x", i), Size: 8192})
	}
	raw, err := json.Marshal(f)
	if err != nil || len(raw) > MaxCacheFrameBytes {
		t.Fatal("full supported index exceeds frame", len(raw), err)
	}
	if got, err := DecodeCache(raw, now); err != nil || len(got.Entries) != MaxCacheEntries {
		t.Fatal("complete index rejected", err)
	}
	for _, change := range []func(*CacheFrame){
		func(f *CacheFrame) {
			f.Entries = append(f.Entries, CacheItem{SHA256: strings.Repeat("f", 64), Size: 1})
		},
		func(f *CacheFrame) { f.Entries[0].SHA256 = f.Entries[1].SHA256 },
		func(f *CacheFrame) { f.Entries[0].Size = 8193 },
		func(f *CacheFrame) { f.Entries[0].Size = 0 },
		func(f *CacheFrame) { f.ManifestSHA256 = "unbound" },
		func(f *CacheFrame) { f.Entries = nil },
		func(f *CacheFrame) { f.CapturedAt = now.Add(-31 * time.Second) },
		func(f *CacheFrame) { f.CapturedAt = now.Add(6 * time.Second) },
		func(f *CacheFrame) { f.State = "unavailable"; f.FailureCode = "cache_index_busy" },
	} {
		bad := f
		bad.Entries = append([]CacheItem{}, f.Entries...)
		change(&bad)
		if bad.Validate(now) == nil {
			t.Fatal("invalid cache observation admitted", bad)
		}
	}
	failed := CacheFrame{Version: CacheVersion, State: "unavailable", CapturedAt: now, Entries: []CacheItem{}, FailureCode: "cache_index_busy"}
	if failed.Validate(now) != nil {
		t.Fatal("explicit failure rejected")
	}
	if _, err := DecodeCache(append(raw, []byte(` {}`)...), now); err == nil {
		t.Fatal("trailing document accepted")
	}
	if _, err := DecodeCache([]byte(strings.Replace(string(raw), `"version":`, `"seed_media_id":"old-project-reference","version":`, 1)), now); err == nil {
		t.Fatal("private source reference accepted")
	}
	if _, err := DecodeCache(make([]byte, MaxCacheFrameBytes+1), now); err == nil {
		t.Fatal("oversize observation accepted")
	}
	if got, err := DecodeCache(nil, now); err != nil || got != nil {
		t.Fatal("legacy heartbeat rejected")
	}
}
