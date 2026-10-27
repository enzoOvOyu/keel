package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestHistoricalArchiveCannotReplaceCommandScopeOrOrigin(t *testing.T) {
	driver := newCaptureDriver()
	binding := recollectBinding(strings.Repeat("a", 32))
	archive, e := CaptureWithDriver(context.Background(), driver.expected, binding.CommandID, driver)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(archive)
	_, result, e := EvaluateArchive(raw, driver.expected, binding, "sim")
	if e != nil || result.Step2 != "BLOCKED" || result.Step3 != "BLOCKED" {
		t.Fatal("stopped partial fixture acquired a gate verdict", e, result)
	}
	if _, _, e = EvaluateArchive(raw, driver.expected, binding, "real"); e == nil {
		t.Fatal("fixture origin could be upgraded by the consumer")
	}
	changed := driver.expected
	changed.Serial += "-different"
	if _, _, e = EvaluateArchive(raw, changed, binding, "sim"); e == nil {
		t.Fatal("capture replaced the frozen command expectation")
	}
	changed = driver.expected
	changed.ScopeHash = strings.Repeat("b", 64)
	if _, _, e = EvaluateArchive(raw, changed, binding, "sim"); e == nil {
		t.Fatal("capture rebound to a different admission scope")
	}
	other := binding
	other.CommandID = strings.Repeat("c", 32)
	if _, _, e = EvaluateArchive(raw, driver.expected, other, "sim"); e == nil {
		t.Fatal("capture rebound to another original command")
	}
}
func TestArchiveRejectsInventedMergedSwitchProjection(t *testing.T) {
	driver := newCaptureDriver()
	binding := recollectBinding(strings.Repeat("d", 32))
	ledger := newRecollectLedger()
	client := &recollectClient{ledger: ledger}
	archive, e := CaptureWithReadback(context.Background(), driver.expected, binding.CommandID, driver, NewRecollector(binding, client, ledger))
	if e != nil || archive == nil || len(archive.Capture.Missing) != 0 {
		t.Fatal("coordinated fixture not retained", e)
	}
	raw, _ := json.Marshal(archive)
	if _, _, e = EvaluateArchive(raw, driver.expected, binding, "sim"); e != nil {
		t.Fatal(e)
	}
	archive.Readback.Devices[0].Provider = "invented-projection"
	raw, _ = json.Marshal(archive)
	if _, _, e = EvaluateArchive(raw, driver.expected, binding, "sim"); e == nil {
		t.Fatal("invented projection overrode the original response")
	}
}

func TestOriginalReadbackCanRecoverWithoutChangingOrRestartingHost(t *testing.T) {
	raw, expected := fixtureDocument(t, "lost-reply")
	driver, e := NewFixtureDriver(raw)
	if e != nil {
		t.Fatal(e)
	}
	binding := recollectBinding(strings.Repeat("e", 32))
	ledger := newRecollectLedger()
	var client RecollectionClient
	var scope ReadbackScope
	callback := func(ctx context.Context, selected ReadbackScope) (*Readback, error) {
		scope = selected
		var e error
		client, e = NewFixtureCollector(raw, binding, scope)
		if e != nil {
			return nil, e
		}
		return NewRecollector(binding, client, ledger)(ctx, scope)
	}
	_, captureErr := CaptureWithReadback(context.Background(), expected, binding.CommandID, driver, callback)
	var pending *UnknownCapture
	if !errors.As(captureErr, &pending) || pending.HostStopped == nil {
		t.Fatal("lost reply did not retain independent host stop", captureErr)
	}
	before, e := HostEvidenceHash(pending.HostStopped)
	if e != nil {
		t.Fatal(e)
	}
	requests, _ := BuildReadbackRequests(binding, scope)
	observation, e := client.QueryRecollection(context.Background(), requests[0])
	if e != nil || observation == nil || observation.Result == nil || observation.Validate(requests[0], time.Now().UTC()) != nil {
		t.Fatal("original completed response missing", e)
	}
	recovered, e := AttachOriginalReadback(pending.HostStopped, binding, []ReadbackExchange{{Request: requests[0], Response: *observation.Result}})
	if e != nil || recovered == nil {
		t.Fatal(e)
	}
	after, e := HostEvidenceHash(recovered)
	if e != nil || before != after || recovered.Readback != nil || len(recovered.Capture.Missing) == 0 {
		t.Fatal("partial scope lost missing facts or changed stopped host bytes")
	}
	if _, e = driver.Begin(context.Background(), Plan{CommandID: binding.CommandID, Digest: pending.HostStopped.PlanDigest}, pending.HostStopped.Before); !errors.Is(e, ErrOutcomeUnknown) {
		t.Fatal("recovery allowed another host Begin")
	}
	altered := *observation.Result
	altered.CommandID = strings.Repeat("f", 32)
	if _, e = AttachOriginalReadback(pending.HostStopped, binding, []ReadbackExchange{{Request: requests[0], Response: altered}}); e == nil {
		t.Fatal("another command's switch response rebound to stopped host")
	}
	late := fixtureClone(*observation.Result)
	late.Devices[0].CollectedAt = pending.HostStopped.Capture.AdvertisingStoppedAt.Add(time.Second)
	if _, e = AttachOriginalReadback(pending.HostStopped, binding, []ReadbackExchange{{Request: requests[0], Response: late}}); e == nil {
		t.Fatal("post-stop read masqueraded as original live-advertisement evidence")
	}
}
