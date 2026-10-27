package wiring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

// FixtureDocument contains independently recorded synthetic observations. It
// deliberately has no origin override and does not render observations from an
// Expectation. Runtime timestamps belong to a simulated session, never hardware.
type FixtureDocument struct {
	Version        string               `json:"version"`
	Before         []LocalState         `json:"before"`
	During         []LocalState         `json:"during"`
	After          []LocalState         `json:"after"`
	Probes         []RawProbe           `json:"probes"`
	Switches       []fabricmon.Snapshot `json:"switches"`
	WindowComplete bool                 `json:"window_complete"`
	HostFault      string               `json:"host_fault"`
	CollectorFault string               `json:"collector_fault"`
}

func decodeFixture(raw []byte) (*FixtureDocument, error) {
	if len(raw) < 1 || len(raw) > MaxCaptureBytes {
		return nil, ErrCaptureRejected
	}
	tokens := json.NewDecoder(bytes.NewReader(raw))
	if uniqueJSONValue(tokens, 0) != nil {
		return nil, ErrCaptureRejected
	}
	if _, e := tokens.Token(); e != io.EOF {
		return nil, ErrCaptureRejected
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var document FixtureDocument
	if decoder.Decode(&document) != nil || document.Version != "keel.lldp-fixture/v1" || len(document.Before) < 1 || len(document.Before) > MaxPorts || len(document.During) > MaxPorts || len(document.After) > MaxPorts || len(document.Probes) > 3*MaxPorts || len(document.Switches) > MaxPorts {
		return nil, ErrCaptureRejected
	}
	switch document.HostFault {
	case "", "start_unknown", "observe_failed", "stop_unknown":
	default:
		return nil, ErrCaptureRejected
	}
	switch document.CollectorFault {
	case "", "lost_reply", "running", "stopped", "rejected":
	default:
		return nil, ErrCaptureRejected
	}
	return &document, nil
}
func fixtureClone[T any](value T) T {
	raw, _ := json.Marshal(value)
	var out T
	_ = json.Unmarshal(raw, &out)
	return out
}

type fixtureDriver struct {
	mu               sync.Mutex
	document         FixtureDocument
	session          *fixtureSession
	started, stopped bool
}
type fixtureSession struct {
	driver    *fixtureDriver
	plan      Plan
	startedAt time.Time
}

var _ Driver = (*fixtureDriver)(nil)
var _ Session = (*fixtureSession)(nil)

// NewFixtureDriver performs no subprocess, filesystem write or interface change.
// Its fixed sim provenance cannot be elevated by fixture JSON or an expected BOM.
func NewFixtureDriver(raw []byte) (Driver, error) {
	document, e := decodeFixture(raw)
	if e != nil {
		return nil, e
	}
	return &fixtureDriver{document: *document}, nil
}
func (d *fixtureDriver) Origin() string { return "sim" }
func (d *fixtureDriver) Inspect(ctx context.Context, expected Expectation) ([]LocalState, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	states := d.document.Before
	if d.stopped {
		states = d.document.After
	}
	selected := map[string]bool{}
	for _, p := range expected.Ports {
		selected[p.PhysicalPortID] = true
	}
	return selectFixtureStates(states, selected), nil
}
func (d *fixtureDriver) Begin(ctx context.Context, plan Plan, before []LocalState) (Session, error) {
	if ctx.Err() != nil {
		return nil, ErrCaptureRejected
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.started {
		return nil, ErrOutcomeUnknown
	}
	if !entityID.MatchString(plan.CommandID) || !scopeDigest.MatchString(plan.Digest) || len(plan.Ports) != len(before) {
		return nil, ErrCaptureRejected
	}
	d.started = true
	d.session = &fixtureSession{driver: d, plan: fixtureClone(plan), startedAt: time.Now().UTC()}
	if d.document.HostFault == "start_unknown" {
		return d.session, ErrOutcomeUnknown
	}
	return d.session, nil
}
func (s *fixtureSession) Observe(ctx context.Context) (*Observation, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	d := s.driver
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped || d.document.HostFault == "observe_failed" {
		return nil, errors.New("simulated observation unavailable")
	}
	selected := map[string]bool{}
	for _, p := range s.plan.Ports {
		selected[p.PhysicalPortID] = true
	}
	probes := []RawProbe{}
	for _, p := range d.document.Probes {
		if selected[p.PhysicalPortID] {
			probes = append(probes, p)
		}
	}
	return &Observation{PlanDigest: s.plan.Digest, AdvertisingAt: s.startedAt, CollectedAt: time.Now().UTC(), WindowComplete: d.document.WindowComplete, States: selectFixtureStates(d.document.During, selected), Probes: probes}, nil
}
func (s *fixtureSession) Stop(ctx context.Context) (StopProof, error) {
	if ctx.Err() != nil {
		return StopProof{}, ErrOutcomeUnknown
	}
	d := s.driver
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.document.HostFault == "stop_unknown" {
		return StopProof{}, ErrOutcomeUnknown
	}
	d.stopped = true
	return StopProof{PlanDigest: s.plan.Digest, Stopped: true, Restored: true}, nil
}

type fixtureCollector struct {
	mu       sync.Mutex
	requests map[string]fabricboot.CollectorRecollectRequest
	devices  map[string]fabricmon.Snapshot
	receipts map[string]fabricboot.CollectorRecollectObservation
	fault    string
}

var _ RecollectionClient = (*fixtureCollector)(nil)

// NewFixtureCollector is scoped to one frozen command/window. It only returns
// protocol_fixture responses from independently supplied switch samples, never
// derives LLDP peers from the host BOM and never dials an endpoint.
func NewFixtureCollector(raw []byte, binding ReadbackBinding, scope ReadbackScope) (RecollectionClient, error) {
	if binding.Origin != "protocol_fixture" {
		return nil, ErrCaptureRejected
	}
	document, e := decodeFixture(raw)
	if e != nil {
		return nil, e
	}
	requests, e := BuildReadbackRequests(binding, scope)
	if e != nil {
		return nil, e
	}
	out := &fixtureCollector{requests: map[string]fabricboot.CollectorRecollectRequest{}, devices: map[string]fabricmon.Snapshot{}, receipts: map[string]fabricboot.CollectorRecollectObservation{}, fault: document.CollectorFault}
	for _, request := range requests {
		out.requests[request.CommandID] = request
	}
	for _, sample := range document.Switches {
		if _, exists := out.devices[sample.Serial]; exists || sample.Serial == "" {
			return nil, ErrCaptureRejected
		}
		sample.CollectedAt = time.Now().UTC()
		if sample.Validate(sample.Serial, time.Now().UTC()) != nil {
			return nil, ErrCaptureRejected
		}
		out.devices[sample.Serial] = sample
	}
	return out, nil
}
func (c *fixtureCollector) accepts(request fabricboot.CollectorRecollectRequest) bool {
	frozen, ok := c.requests[request.CommandID]
	return ok && request.Validate() == nil && netbaseline.ContentHash(frozen) == netbaseline.ContentHash(request)
}
func (c *fixtureCollector) RecollectPorts(ctx context.Context, request fabricboot.CollectorRecollectRequest) (*fabricboot.CollectorRecollectResponse, error) {
	if ctx.Err() != nil {
		return nil, ErrOutcomeUnknown
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.accepts(request) {
		return nil, ErrCaptureRejected
	}
	if prior, exists := c.receipts[request.CommandID]; exists {
		if prior.State != "completed" {
			return nil, ErrOutcomeUnknown
		}
		return fixtureClone(prior.Result), nil
	}
	result := &fabricboot.CollectorRecollectResponse{Version: request.Version, ProjectID: request.ProjectID, ExecutionID: request.ExecutionID, CommandID: request.CommandID, FencingToken: request.FencingToken, ScopeHash: request.ScopeHash, EvidenceSource: "protocol_fixture", Devices: []fabricmon.Snapshot{}}
	for _, target := range request.Targets {
		sample, ok := c.devices[target.Serial]
		if !ok {
			return c.reject(request)
		}
		// Project only selected port evidence; whole-switch tables are never copied.
		byName := map[string]fabricmon.Port{}
		for _, p := range sample.Ports {
			byName[p.Name] = p
		}
		projected := fabricmon.Snapshot{Version: sample.Version, Source: sample.Source, Provider: sample.Provider, Serial: sample.Serial, NOS: sample.NOS, CollectedAt: time.Now().UTC(), Capabilities: map[string]bool{"lldp": sample.Capabilities["lldp"], "interfaces": sample.Capabilities["interfaces"]}, Ports: []fabricmon.Port{}}
		for _, name := range target.Ports {
			p, exists := byName[name]
			if !exists {
				return c.reject(request)
			}
			projected.Ports = append(projected.Ports, projectWirePorts([]fabricmon.Port{p})...)
		}
		result.Devices = append(result.Devices, projected)
	}
	if result.Validate(request, time.Now().UTC()) != nil {
		return nil, ErrCaptureRejected
	}
	finished := time.Now().UTC()
	receipt := fabricboot.CollectorRecollectObservation{Version: "fabric-recollect-status/v1", ProjectID: request.ProjectID, ExecutionID: request.ExecutionID, CommandID: request.CommandID, FencingToken: request.FencingToken, ScopeHash: request.ScopeHash, RequestHash: netbaseline.ContentHash(request), EvidenceSource: "protocol_fixture", State: "completed", FinishedAt: &finished, Result: result}
	switch c.fault {
	case "running":
		receipt.State = "running"
		receipt.FinishedAt = nil
		receipt.Result = nil
	case "stopped", "rejected":
		receipt.State = c.fault
		receipt.Result = nil
	}
	c.receipts[request.CommandID] = fixtureClone(receipt)
	if c.fault != "" {
		return nil, ErrOutcomeUnknown
	}
	return fixtureClone(result), nil
}
func (c *fixtureCollector) QueryRecollection(ctx context.Context, request fabricboot.CollectorRecollectRequest) (*fabricboot.CollectorRecollectObservation, error) {
	if ctx.Err() != nil {
		return nil, ErrOutcomeUnknown
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.accepts(request) {
		return nil, ErrCaptureRejected
	}
	receipt, exists := c.receipts[request.CommandID]
	if !exists {
		// Absence cannot certify that a delayed original dispatch will never arrive.
		return &fabricboot.CollectorRecollectObservation{Version: "fabric-recollect-status/v1", ProjectID: request.ProjectID, ExecutionID: request.ExecutionID, CommandID: request.CommandID, FencingToken: request.FencingToken, ScopeHash: request.ScopeHash, RequestHash: netbaseline.ContentHash(request), EvidenceSource: "protocol_fixture", State: "unknown"}, nil
	}
	copy := fixtureClone(receipt)
	return &copy, nil
}

// A fixture with missing source data rejects the ORIGINAL command durably; an
// empty HTTP result is not used as a pretend rejection or link-down sample.
func (c *fixtureCollector) reject(request fabricboot.CollectorRecollectRequest) (*fabricboot.CollectorRecollectResponse, error) {
	at := time.Now().UTC()
	c.receipts[request.CommandID] = fabricboot.CollectorRecollectObservation{Version: "fabric-recollect-status/v1", ProjectID: request.ProjectID, ExecutionID: request.ExecutionID, CommandID: request.CommandID, FencingToken: request.FencingToken, ScopeHash: request.ScopeHash, RequestHash: netbaseline.ContentHash(request), EvidenceSource: "protocol_fixture", State: "rejected", FinishedAt: &at}
	return nil, ErrOutcomeUnknown
}

// Selection filters independently recorded observations; it never creates or
// rewrites a fixture to match an expected physical identity or peer.
func selectFixtureStates(states []LocalState, selected map[string]bool) []LocalState {
	out := []LocalState{}
	for _, state := range states {
		if selected[state.PhysicalPortID] {
			out = append(out, state)
		}
	}
	return cloneLocalStates(out)
}
