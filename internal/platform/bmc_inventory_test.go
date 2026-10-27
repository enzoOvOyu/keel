package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
)

type bmcRawTestObjects struct {
	body         []byte
	key, version string
	tamper       bool
}

func (o *bmcRawTestObjects) Put(_ context.Context, key string, r io.Reader, _ int64) (string, error) {
	o.key = key
	o.version = "pinned-version"
	o.body, _ = io.ReadAll(r)
	return o.version, nil
}
func (o *bmcRawTestObjects) Open(_ context.Context, key, version string) (io.ReadCloser, error) {
	if key != o.key || version != o.version {
		return nil, ErrNotFound
	}
	body := o.body
	if o.tamper {
		body = []byte(`{"changed":true}`)
	}
	return io.NopCloser(strings.NewReader(string(body))), nil
}
func TestBMCRawCaptureVerifiesExactPinnedBytes(t *testing.T) {
	objects := &bmcRawTestObjects{}
	target := bmc.Target{RequestID: strings.Repeat("1", 32), ProjectID: strings.Repeat("2", 32), AssetID: strings.Repeat("3", 32), MachineID: strings.Repeat("4", 32), AssetVersion: 1, Serial: "SN-001", CredentialBatchRef: "batch", CredentialVersion: "v1"}
	body := []byte(` {"RedfishVersion":"1.0.0"} `)
	sink := bmcRawSink{objects: objects}
	ref, e := sink.StoreRaw(context.Background(), target, "/redfish/v1/", body)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(body)
	if ref.SHA256 != hex.EncodeToString(h[:]) || ref.Version != "pinned-version" || ref.Key != bmc.RawKey(target, "/redfish/v1/", ref.SHA256) {
		t.Fatal("lost immutable raw identity")
	}
	objects.tamper = true
	if _, e = sink.StoreRaw(context.Background(), target, "/redfish/v1/", body); e == nil {
		t.Fatal("accepted different retained object bytes")
	}
}
func TestBMCStaticFreshnessDoesNotInventReadiness(t *testing.T) {
	now := time.Now().UTC()
	for _, test := range []struct {
		name              string
		age               time.Duration
		current, identity bool
		state, want       string
	}{{"fresh", time.Hour, true, true, "completed", "fresh"}, {"half", 12 * time.Hour, true, true, "completed", "aging"}, {"expired", 24 * time.Hour, true, true, "completed", "stale"}, {"asset-changed", time.Minute, false, true, "completed", "stale"}, {"unconfirmed", time.Minute, true, false, "partial", "unknown"}, {"failed", time.Minute, true, true, "failed", "unknown"}, {"future", -time.Minute, true, true, "completed", "unknown"}} {
		t.Run(test.name, func(t *testing.T) {
			s := BMCCollectionSummary{State: test.state, CollectedAt: now.Add(-test.age), IdentityMatched: test.identity, AssetCurrent: test.current, FreshnessMaxAgeSeconds: 86400}
			bmcFreshness(&s, now)
			if s.Freshness != test.want {
				t.Fatalf("got %s want %s", s.Freshness, test.want)
			}
		})
	}
}
