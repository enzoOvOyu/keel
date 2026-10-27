package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"strings"
	"testing"
	"time"
)

type captureDriver struct {
	expected                                Expectation
	plan                                    Plan
	beginCount, stopCount                   int
	observeError, unknownStop, beginUnknown bool
	before, after                           []LocalState
}

func (d *captureDriver) Origin() string { return "sim" }
func (d *captureDriver) Inspect(context.Context, Expectation) ([]LocalState, error) {
	if d.stopCount > 0 {
		return d.after, nil
	}
	return d.before, nil
}
func (d *captureDriver) Begin(_ context.Context, p Plan, _ []LocalState) (Session, error) {
	d.beginCount++
	d.plan = p
	if d.beginUnknown {
		return nil, errors.New("lost process-start acknowledgement")
	}
	return d, nil
}
func (d *captureDriver) Stop(ctx context.Context) (StopProof, error) {
	d.stopCount++
	if ctx.Err() != nil || d.unknownStop {
		return StopProof{}, ErrOutcomeUnknown
	}
	return StopProof{PlanDigest: d.plan.Digest, Stopped: true, Restored: true}, nil
}
func (d *captureDriver) Observe(context.Context) (*Observation, error) {
	if d.observeError {
		return nil, errors.New("collector transport lost")
	}
	probes := []RawProbe{}
	for _, p := range d.expected.Ports {
		document := func(chassis, port, name, description string) string {
			raw, _ := json.Marshal(map[string]any{"lldp": []any{map[string]any{"interface": []any{map[string]any{"name": p.Interface, "via": "LLDP", "chassis": []any{map[string]any{"id": []any{map[string]string{"type": "local", "value": chassis}}, "name": []any{map[string]string{"value": name}}, "descr": []any{map[string]string{"value": description}}}}, "port": []any{map[string]any{"id": []any{map[string]string{"type": "local", "value": port}}}}}}}}})
			return string(raw)
		}
		probes = append(probes, RawProbe{PhysicalPortID: p.PhysicalPortID, Kind: "local", State: "success", Stdout: document(p.PermanentMAC, p.PhysicalPortID, d.expected.Hostname, Description(d.expected.Serial, p.Rail, p.PortIndex))}, RawProbe{PhysicalPortID: p.PhysicalPortID, Kind: "neighbors", State: "success", Stdout: document(p.SwitchChassisIDs[0], p.SwitchPort, "leaf-1", "synthetic leaf")})
	}
	now := time.Now().UTC()
	return &Observation{PlanDigest: d.plan.Digest, AdvertisingAt: now, CollectedAt: now, WindowComplete: true, States: d.before, Probes: probes}, nil
}
func newCaptureDriver() *captureDriver {
	expected, _, _, _ := wireFixture()
	p := expected.Ports[0]
	up := true
	states := []LocalState{{PhysicalPortID: p.PhysicalPortID, Interface: p.Interface, BDF: p.BDF, PermanentMAC: p.PermanentMAC, CurrentMAC: p.PermanentMAC, AdminUp: false, MTU: 1500, Addresses: []string{}, LinkUp: &up}}
	return &captureDriver{expected: expected, before: states, after: states}
}
func TestLLDPCaptureRetainsOriginalAndAlwaysRequiresCleanupProof(t *testing.T) {
	driver := newCaptureDriver()
	archive, e := CaptureWithDriver(context.Background(), driver.expected, strings.Repeat("a", 32), driver)
	if e != nil || archive == nil || driver.beginCount != 1 || driver.stopCount != 1 || len(archive.Probes) != 2 || !archive.Capture.Stopped || !archive.Capture.Ports[0].LLDPReadable || archive.Capture.Ports[0].Advertised == nil {
		t.Fatalf("archive=%+v err=%v", archive, e)
	}
	if archive.Capture.Origin != "sim" {
		t.Fatal("simulated device adapter acquired real provenance")
	}
	result := Evaluate(driver.expected, &archive.Capture, nil, time.Now().UTC())
	if result.Step2 == "PASS" {
		t.Fatal("sim capture became real gate evidence")
	}
	driver = newCaptureDriver()
	driver.observeError = true
	archive, e = CaptureWithDriver(context.Background(), driver.expected, strings.Repeat("b", 32), driver)
	if e != nil || archive == nil || len(archive.Capture.Missing) == 0 || driver.stopCount != 1 {
		t.Fatal("a stopped partial capture was lost or observation failure skipped cleanup")
	}
	driver = newCaptureDriver()
	driver.unknownStop = true
	if archive, e = CaptureWithDriver(context.Background(), driver.expected, strings.Repeat("c", 32), driver); !errors.Is(e, ErrOutcomeUnknown) || archive != nil || driver.beginCount != 1 {
		t.Fatal("missing stop proof became terminal success or was retried")
	}
	driver = newCaptureDriver()
	driver.beginUnknown = true
	if _, e = CaptureWithDriver(context.Background(), driver.expected, strings.Repeat("d", 32), driver); !errors.Is(e, ErrOutcomeUnknown) || driver.beginCount != 1 {
		t.Fatal("ambiguous start was retried")
	}
	driver = newCaptureDriver()
	driver.before[0].ExistingLLDP = true
	if _, e = CaptureWithDriver(context.Background(), driver.expected, strings.Repeat("e", 32), driver); !errors.Is(e, ErrCaptureRejected) || driver.beginCount != 0 {
		t.Fatal("existing customer LLDP daemon was disturbed")
	}
}

func TestSwitchReadbackHappensBeforeStoppingAdvertisement(t *testing.T) {
	driver := newCaptureDriver()
	callback := func(_ context.Context, scope ReadbackScope) (*Readback, error) {
		if driver.stopCount != 0 || driver.beginCount != 1 {
			t.Fatal("switch readback was outside active LLDP advertisement")
		}
		_, _, switches, _ := wireFixture()
		p := scope.Ports[0]
		snapshot := *switches[p.SwitchSerial].Snapshot
		snapshot.CollectedAt = time.Now().UTC()
		binding := recollectBinding(scope.CommandID)
		requests, e := BuildReadbackRequests(binding, scope)
		if e != nil {
			return nil, e
		}
		response := recollectResponse(requests[0])
		response.Devices = []fabricmon.Snapshot{snapshot}
		return &Readback{Binding: binding, Exchanges: []ReadbackExchange{{Request: requests[0], Response: *response}}, PlanDigest: scope.PlanDigest, CommandID: scope.CommandID, ScopeHash: scope.ScopeHash, Origin: "protocol_fixture", Devices: []fabricmon.Snapshot{snapshot}}, nil
	}
	archive, e := CaptureWithReadback(context.Background(), driver.expected, strings.Repeat("f", 32), driver, callback)
	if e != nil || archive == nil || len(archive.Capture.Missing) != 0 || driver.stopCount != 1 || archive.Readback == nil {
		t.Fatalf("bad coordinated capture: %+v %v", archive, e)
	}
	if archive.Capture.AdvertisingStoppedAt.Before(archive.Readback.Devices[0].CollectedAt) {
		t.Fatal("snapshot was collected after stop")
	}
	driver = newCaptureDriver()
	archive, e = CaptureWithReadback(context.Background(), driver.expected, strings.Repeat("e", 32), driver, func(context.Context, ReadbackScope) (*Readback, error) {
		return nil, errors.New("readback connection lost")
	})
	if e != nil || archive == nil || driver.stopCount != 1 || len(archive.Capture.Missing) == 0 {
		t.Fatal("readback failure skipped cleanup or fabricated completeness")
	}
}

func TestStoppedArchiveReverseReadRejectsInventedProjection(t *testing.T) {
	driver := newCaptureDriver()
	archive, e := CaptureWithDriver(context.Background(), driver.expected, strings.Repeat("a", 32), driver)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(archive)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = DecodeArchive(raw); e != nil {
		t.Fatal("retained partial simulated archive cannot be reverse-read", e)
	}
	archive.Capture.Ports[0].Neighbors[0].PortID = "forged-peer-port"
	altered, e := json.Marshal(archive)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = DecodeArchive(altered); e == nil {
		t.Fatal("a derived neighbor overrode original lldpcli bytes")
	}
	archive.Capture.Ports[0].Neighbors[0].PortID = driver.expected.Ports[0].SwitchPort
	archive.After[0].MTU = 9000
	altered, e = json.Marshal(archive)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = DecodeArchive(altered); e == nil {
		t.Fatal("configuration change acquired restoration proof")
	}
}

func TestCollectorUnknownKeepsQuarantineAfterHostCleanup(t *testing.T) {
	driver := newCaptureDriver()
	archive, e := CaptureWithReadback(context.Background(), driver.expected, strings.Repeat("a", 32), driver, func(context.Context, ReadbackScope) (*Readback, error) { return nil, ErrOutcomeUnknown })
	if !errors.Is(e, ErrOutcomeUnknown) || archive != nil || driver.stopCount != 1 {
		t.Fatal("host stop released a collector with no terminal proof")
	}
}
