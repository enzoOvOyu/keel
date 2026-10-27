package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func fixtureDocument(t *testing.T, name string) ([]byte, Expectation) {
	t.Helper()
	raw, e := os.ReadFile("../../sim/fixtures/lldp/" + name + ".json")
	if e != nil {
		t.Fatal(e)
	}
	expectedRaw, e := os.ReadFile("../../sim/fixtures/lldp/expectation.json")
	if e != nil {
		t.Fatal(e)
	}
	var expected Expectation
	if json.Unmarshal(expectedRaw, &expected) != nil || expected.Validate() != nil {
		t.Fatal("invalid independent fixture expectation")
	}
	return raw, expected
}
func TestIndependentLLDPFixturesRetainSourceAndRuleDifferences(t *testing.T) {
	cases := []struct{ name, step2, step3 string }{{"dual-ended", "PASS", "PASS"}, {"link-down", "FAIL", "BLOCKED"}, {"readable-empty", "FAIL", "BLOCKED"}, {"unreadable", "BLOCKED", "BLOCKED"}, {"wrong-peer", "PASS", "FAIL"}}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			raw, expected := fixtureDocument(t, test.name)
			driver, e := NewFixtureDriver(raw)
			if e != nil {
				t.Fatal(e)
			}
			binding := recollectBinding(strings.Repeat("f", 32))
			ledger := newRecollectLedger()
			callback := func(ctx context.Context, scope ReadbackScope) (*Readback, error) {
				client, e := NewFixtureCollector(raw, binding, scope)
				if e != nil {
					return nil, e
				}
				return NewRecollector(binding, client, ledger)(ctx, scope)
			}
			archive, e := CaptureWithReadback(context.Background(), expected, binding.CommandID, driver, callback)
			if e != nil || archive == nil {
				t.Fatal(e)
			}
			if test.name == "dual-ended" {
				originalHost := *archive
				originalHost.Readback = nil
				partial, e := projectArchive(&originalHost)
				if e != nil {
					t.Fatal(e)
				}
				recovered, e := AttachOriginalReadback(partial, binding, archive.Readback.Exchanges)
				if e != nil || recovered.Readback == nil || len(recovered.Capture.Missing) != 0 {
					t.Fatal("complete original responses did not recover coordinated evidence", e)
				}
			}
			stored, _ := json.Marshal(archive)
			_, historical, e := EvaluateArchive(stored, expected, binding, "sim")
			if e != nil || historical.Step2 == "PASS" || historical.Step3 != "BLOCKED" || archive.Capture.Origin != "sim" || archive.Readback.Origin != "protocol_fixture" {
				t.Fatal("fixture acquired real evidence", e, historical)
			}
			// Synthetic rule input only, never stored, published, or claimed as hardware.
			host := fixtureClone(archive.Capture)
			host.Origin = "real"
			switches := map[string]SwitchEvidence{}
			for i := range archive.Readback.Devices {
				sample := fixtureClone(archive.Readback.Devices[i])
				switches[sample.Serial] = SwitchEvidence{Origin: "device", Snapshot: &sample}
			}
			result := Evaluate(expected, &host, switches, archive.Capture.AdvertisingStoppedAt)
			if result.Step2 != test.step2 || result.Step3 != test.step3 {
				t.Fatalf("%+v", result)
			}
		})
	}
}
func TestFixtureCannotSynthesizeObservationsFromChangedBOMOrElevateOrigin(t *testing.T) {
	raw, expected := fixtureDocument(t, "dual-ended")
	driver, e := NewFixtureDriver(raw)
	if e != nil {
		t.Fatal(e)
	}
	expected.Ports[0].PermanentMAC = "02:00:00:00:ff:ff"
	if _, e = CaptureWithDriver(context.Background(), expected, strings.Repeat("a", 32), driver); !errors.Is(e, ErrCaptureRejected) {
		t.Fatal("fixture rendered the observed MAC from changed expected values")
	}
	binding, scope := recollectScope()
	binding.Origin = "device"
	if _, e = NewFixtureCollector(raw, binding, scope); e == nil {
		t.Fatal("fixture collector claimed device source")
	}
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	doc["origin"] = "real"
	raw, _ = json.Marshal(doc)
	if _, e = NewFixtureDriver(raw); e == nil {
		t.Fatal("fixture accepted an origin override")
	}
}
func TestFixtureUnknownsPreserveSessionAndOriginalCollectorIdentity(t *testing.T) {
	for _, name := range []string{"stop-unknown", "lost-reply", "collector-running"} {
		t.Run(name, func(t *testing.T) {
			raw, expected := fixtureDocument(t, name)
			driver, e := NewFixtureDriver(raw)
			if e != nil {
				t.Fatal(e)
			}
			binding := recollectBinding(strings.Repeat("a", 32))
			ledger := newRecollectLedger()
			var client RecollectionClient
			var frozen ReadbackScope
			callback := func(ctx context.Context, scope ReadbackScope) (*Readback, error) {
				frozen = scope
				var e error
				client, e = NewFixtureCollector(raw, binding, scope)
				if e != nil {
					return nil, e
				}
				return NewRecollector(binding, client, ledger)(ctx, scope)
			}
			archive, e := CaptureWithReadback(context.Background(), expected, binding.CommandID, driver, callback)
			if !errors.Is(e, ErrOutcomeUnknown) || archive != nil {
				t.Fatal("missing terminal proof became a stopped archive", e)
			}
			if name == "lost-reply" {
				// Finish each ORIGINAL child only by GET; the collector retains stable receipts.
				requests, _ := BuildReadbackRequests(binding, frozen)
				first, e := client.QueryRecollection(context.Background(), requests[0])
				if e != nil || first == nil || first.FinishedAt == nil || first.Validate(requests[0], *first.FinishedAt) != nil || first.State != "completed" {
					t.Fatal("lost response lacked original completion")
				}
				second, e := client.QueryRecollection(context.Background(), requests[0])
				if e != nil || second == nil || second.FinishedAt == nil || first.RequestHash != second.RequestHash || !first.FinishedAt.Equal(*second.FinishedAt) {
					t.Fatal("query fabricated a newer original receipt")
				}
			}
		})
	}
}
func TestReadbackProjectionRetainsIndependentPhysicalFactsInOriginalOnly(t *testing.T) {
	binding, scope := recollectScope()
	requests, _ := BuildReadbackRequests(binding, scope)
	response := recollectResponse(requests[0])
	response.Devices[0].Capabilities["physical_link"] = true
	response.Devices[0].Ports[0].Duplex = "full"
	response.Devices[0].Ports[0].MediaClass = "optical"
	at := response.Devices[0].CollectedAt
	projected, e := projectReadback(binding, scope, []ReadbackExchange{{Request: requests[0], Response: *response}}, at)
	if e != nil || len(projected) != 1 || projected[0].Ports[0].Duplex != "" || projected[0].Ports[0].MediaClass != "" || response.Devices[0].Ports[0].Duplex != "full" {
		t.Fatal("narrow LLDP projection lost originals or contradicted capabilities", e)
	}
}

func TestOversizeAggregateRetainsBoundedOriginalHostStop(t *testing.T) {
	raw, expected := fixtureDocument(t, "dual-ended")
	document, e := decodeFixture(raw)
	if e != nil {
		t.Fatal(e)
	}
	for i := range document.Switches {
		document.Switches[i].Ports[0].Neighbor.Sysdesc = strings.Repeat("s", 600<<10)
	}
	raw, e = json.Marshal(document)
	if e != nil {
		t.Fatal(e)
	}
	driver, e := NewFixtureDriver(raw)
	if e != nil {
		t.Fatal(e)
	}
	binding := recollectBinding(strings.Repeat("d", 32))
	ledger := newRecollectLedger()
	callback := func(ctx context.Context, scope ReadbackScope) (*Readback, error) {
		client, e := NewFixtureCollector(raw, binding, scope)
		if e != nil {
			return nil, e
		}
		return NewRecollector(binding, client, ledger)(ctx, scope)
	}
	_, captureErr := CaptureWithReadback(context.Background(), expected, binding.CommandID, driver, callback)
	var unknown *UnknownCapture
	if !errors.As(captureErr, &unknown) || unknown.HostStopped == nil || unknown.HostStopped.Readback != nil || !unknown.HostStopped.Capture.Stopped {
		t.Fatal("aggregate size limit erased the independent host stop proof", captureErr)
	}
	bounded, e := json.Marshal(unknown.HostStopped)
	if e != nil || len(bounded) > MaxCaptureBytes || len(ledger.results) != 2 {
		t.Fatal("original child results or bounded host evidence lost")
	}
	if _, e = DecodeArchive(bounded); e != nil {
		t.Fatal(e)
	}
}
func TestMissingFixtureSourceCreatesAnOriginalRejectionTombstone(t *testing.T) {
	raw, _ := fixtureDocument(t, "dual-ended")
	document, e := decodeFixture(raw)
	if e != nil {
		t.Fatal(e)
	}
	document.Switches = nil
	raw, e = json.Marshal(document)
	if e != nil {
		t.Fatal(e)
	}
	binding, scope := recollectScope()
	client, e := NewFixtureCollector(raw, binding, scope)
	if e != nil {
		t.Fatal(e)
	}
	ledger := newRecollectLedger()
	run := NewRecollector(binding, client, ledger)
	if _, e = run(context.Background(), scope); !errors.Is(e, ErrOutcomeUnknown) {
		t.Fatal("first rejected response should require original proof", e)
	}
	if _, e = run(context.Background(), scope); !errors.Is(e, ErrCaptureRejected) || ledger.observations != 1 {
		t.Fatal("missing source was treated as an empty neighbor or unproven absence", e)
	}
}
