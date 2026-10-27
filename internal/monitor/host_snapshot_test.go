package monitor

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestHostSnapshotRejectsScopeBlendingAndRetainsUnknown(t *testing.T) {
	now := time.Now().UTC()
	snapshot := HostSnapshot{Version: HostSnapshotVersion, NetworkOnly: true, StartedAt: now.Add(-time.Second), CollectedAt: now, Samples: []Sample{{Key: "link.state", Entity: "nic0", Quality: "unknown", ObservedAt: now}}}
	check := func() error { raw, _ := json.Marshal(snapshot); _, err := DecodeHostSnapshot(raw, now); return err }
	raw, _ := json.Marshal(snapshot)
	raw = bytes.Replace(raw, []byte(`"network_only":true`), []byte(`"network_only":false,"network_only":true`), 1)
	if _, err := DecodeHostSnapshot(raw, now); err == nil {
		t.Fatal("duplicate snapshot metadata accepted")
	}
	if check() != nil {
		t.Fatal("explicit unknown source rejected")
	}
	snapshot.Samples[0].Key = "host.load1"
	if check() == nil {
		t.Fatal("polling scope acquired D3")
	}
	snapshot.NetworkOnly = false
	snapshot.Samples[0].Key = "bmc.health"
	if check() == nil {
		t.Fatal("SSH acquired independent BMC source")
	}
	snapshot.Samples[0].Key = "link.state"
	snapshot.Samples[0].ObservedAt = now.Add(-time.Hour)
	if check() == nil {
		t.Fatal("old metric attributed to current snapshot")
	}
	snapshot.Samples[0].ObservedAt = now
	snapshot.Samples = append(snapshot.Samples, snapshot.Samples[0])
	if check() == nil {
		t.Fatal("duplicate identity accepted")
	}
}
