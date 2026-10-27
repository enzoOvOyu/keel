package wiring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"io"
	"reflect"
	"slices"
	"sort"
	"time"
)

var ErrOutcomeUnknown = errors.New("LLDP outcome requires reconciliation")
var ErrCaptureRejected = errors.New("LLDP capture rejected before side effects")

// UnknownCapture may carry positive HOST cleanup evidence while a collector's
// original command remains unresolved. This is not a terminal task receipt and
// must never release the shared host/switch resource set.
type UnknownCapture struct{ HostStopped *Archive }

func (e *UnknownCapture) Error() string { return ErrOutcomeUnknown.Error() }
func (e *UnknownCapture) Unwrap() error { return ErrOutcomeUnknown }

type LocalState struct {
	PhysicalPortID  string   `json:"physical_port_id"`
	Interface       string   `json:"interface"`
	BDF             string   `json:"bdf"`
	PermanentMAC    string   `json:"permanent_mac"`
	CurrentMAC      string   `json:"current_mac"`
	AdminUp         bool     `json:"admin_up"`
	MTU             int      `json:"mtu"`
	Addresses       []string `json:"addresses"`
	Master          string   `json:"master"`
	VirtualFunction bool     `json:"virtual_function"`
	ExistingLLDP    bool     `json:"existing_lldp"`
	LinkUp          *bool    `json:"link_up"`
}
type RawProbe struct {
	PhysicalPortID string `json:"physical_port_id"`
	Kind           string `json:"kind"`
	State          string `json:"state"`
	Stdout         string `json:"stdout"`
	Stderr         string `json:"stderr"`
}
type StopProof struct {
	PlanDigest string `json:"plan_digest"`
	Stopped    bool   `json:"stopped"`
	Restored   bool   `json:"restored"`
}
type Observation struct {
	PlanDigest     string       `json:"plan_digest"`
	AdvertisingAt  time.Time    `json:"advertising_at"`
	CollectedAt    time.Time    `json:"collected_at"`
	WindowComplete bool         `json:"window_complete"`
	States         []LocalState `json:"states"`
	Probes         []RawProbe   `json:"probes"`
}

// Driver is a device adapter, not a scheduler. The shared Execution owns the
// command/attempt/fence, authorization deadline, exclusive leases and recovery.
// Begin must never retry an unknown process start or alter global customer
// services. Any side-effect ambiguity returns ErrOutcomeUnknown.
// Origin is adapter-owned; a simulated driver cannot assert real hardware.
type Driver interface {
	Origin() string
	Inspect(context.Context, Expectation) ([]LocalState, error)
	Begin(context.Context, Plan, []LocalState) (Session, error)
}
type Session interface {
	Observe(context.Context) (*Observation, error)
	Stop(context.Context) (StopProof, error)
}
type ReadbackScope struct {
	PlanDigest    string         `json:"plan_digest"`
	CommandID     string         `json:"command_id"`
	ScopeHash     string         `json:"scope_hash"`
	AdvertisingAt time.Time      `json:"advertising_at"`
	Ports         []ExpectedPort `json:"ports"`
}
type Readback struct {
	Binding    ReadbackBinding      `json:"binding"`
	Exchanges  []ReadbackExchange   `json:"exchanges"`
	PlanDigest string               `json:"plan_digest"`
	CommandID  string               `json:"command_id"`
	ScopeHash  string               `json:"scope_hash"`
	Origin     string               `json:"origin"`
	Devices    []fabricmon.Snapshot `json:"devices"`
}

// Recollect runs on the execution owner while the target session still
// advertises. Implementations use fixed authenticated FR-0.8.2 adapters and
// retain their own command/fence-bound transport receipts. No callback is
// supplied by a browser, script or target observation.
type Recollect func(context.Context, ReadbackScope) (*Readback, error)
type Archive struct {
	CommandID   string       `json:"command_id"`
	Observation *Observation `json:"observation"`
	Stop        StopProof    `json:"stop_proof"`
	Readback    *Readback    `json:"readback"`
	Version     string       `json:"version"`
	PlanDigest  string       `json:"plan_digest"`
	Expectation Expectation  `json:"expectation"`
	Before      []LocalState `json:"before"`
	After       []LocalState `json:"after"`
	Probes      []RawProbe   `json:"probes"`
	Capture     Capture      `json:"capture"`
}

// CaptureWithDriver executes one already authorized, frozen command. It never
// retries Begin. Stop uses a separate short cleanup context even if observation
// was cancelled; lack of positive stop/restore evidence stays unknown and the
// caller must retain resource quarantine. It does not mark a wire/G2 PASS.
func CaptureWithDriver(ctx context.Context, expected Expectation, commandID string, driver Driver) (*Archive, error) {
	return CaptureWithReadback(ctx, expected, commandID, driver, nil)
}
func CaptureWithReadback(parent context.Context, expected Expectation, commandID string, driver Driver, recollect Recollect) (*Archive, error) {
	ctx, cancelWork := context.WithTimeout(parent, 25*time.Second)
	defer cancelWork()
	if driver == nil || expected.Validate() != nil || !entityID.MatchString(commandID) || ctx.Err() != nil {
		return nil, ErrCaptureRejected
	}
	origin := driver.Origin()
	if origin != "real" && origin != "sim" {
		return nil, ErrCaptureRejected
	}
	plan, e := BuildPlan(expected, commandID)
	if e != nil {
		return nil, ErrCaptureRejected
	}
	before, e := driver.Inspect(ctx, expected)
	if e != nil || validateLocalState(expected, before, true) != nil {
		return nil, ErrCaptureRejected
	}
	started := time.Now().UTC()
	session, e := driver.Begin(ctx, *plan, before)
	if e != nil || session == nil {
		// A failed Begin may already have brought an interface up. A returned
		// session must still prove cleanup; a nil session provides no such proof.
		if session != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			_, _ = session.Stop(cleanup)
			cancel()
		}
		return nil, ErrOutcomeUnknown
	}
	cleaned := false
	defer func() {
		if !cleaned {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			_, _ = session.Stop(cleanup)
			cancel()
		}
	}()
	observed, observeErr := session.Observe(ctx)
	var readback *Readback
	var readbackErr error
	if observeErr == nil && observed != nil && observed.PlanDigest == plan.Digest && !observed.AdvertisingAt.Before(started) && !observed.CollectedAt.Before(observed.AdvertisingAt) && !observed.CollectedAt.After(time.Now().Add(5*time.Second)) && recollect != nil {
		readback, readbackErr = recollect(ctx, ReadbackScope{PlanDigest: plan.Digest, CommandID: commandID, ScopeHash: expected.ScopeHash, AdvertisingAt: observed.AdvertisingAt, Ports: slices.Clone(expected.Ports)})
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	stopped, stopErr := session.Stop(cleanup)
	cleaned = true
	stoppedAt := time.Now().UTC()
	var after []LocalState
	if stopErr == nil && stopped.Stopped && stopped.Restored && stopped.PlanDigest == plan.Digest {
		after, e = driver.Inspect(cleanup, expected)
	} else {
		e = ErrOutcomeUnknown
	}
	cancel()
	if e != nil || validateLocalState(expected, after, false) != nil || !localConfigurationEqual(before, after) {
		return nil, ErrOutcomeUnknown
	}

	archive := &Archive{CommandID: commandID, Observation: observed, Stop: stopped, Readback: readback, Version: Version, PlanDigest: plan.Digest, Expectation: expected, Before: before, After: after, Capture: Capture{Version: Version, Origin: origin, ProjectID: expected.ProjectID, MachineID: expected.MachineID, ScopeHash: expected.ScopeHash, Serial: expected.Serial, StartedAt: started, AdvertisingStoppedAt: stoppedAt, Stopped: true, AdminRestored: true, ConfigurationPreserved: true, Ports: []HostPort{}, Missing: []string{}}}
	if observeErr != nil {
		archive.Observation = nil
	}
	if readbackErr != nil {
		archive.Readback = nil
	}
	archive, e = projectArchive(archive)
	if e != nil {
		return nil, ErrOutcomeUnknown
	}
	raw, e := json.Marshal(archive)
	if e != nil || len(raw) > MaxCaptureBytes {
		return nil, ErrOutcomeUnknown
	}
	if errors.Is(readbackErr, ErrOutcomeUnknown) {
		return nil, &UnknownCapture{HostStopped: archive}
	}
	return archive, nil
}

// Rebuild the normalized host projection from retained raw probes. It is also
// used on reverse-read; stored derived fields cannot independently claim PASS.
func projectArchive(original *Archive) (*Archive, error) {
	copy := *original
	archive := &copy
	expected := archive.Expectation
	observed := archive.Observation
	plan, e := BuildPlan(expected, archive.CommandID)
	if e != nil || plan.Digest != archive.PlanDigest {
		return nil, ErrCaptureRejected
	}
	archive.Probes = []RawProbe{}
	archive.Capture.Ports = []HostPort{}
	archive.Capture.Missing = []string{}
	if archive.Readback == nil || !readbackMatches(*plan, expected, observed, archive.Readback, archive.Capture.AdvertisingStoppedAt) {
		archive.Capture.Missing = append(archive.Capture.Missing, "switch_readback_not_coordinated")
	}
	if observed == nil || observed.PlanDigest != plan.Digest {
		archive.Capture.Missing = append(archive.Capture.Missing, "lldp_capture_failed_after_start")
		return archive, nil
	}
	archive.Capture.AdvertisingAt = observed.AdvertisingAt
	archive.Capture.CollectedAt = observed.CollectedAt
	if observed.AdvertisingAt.Before(archive.Capture.StartedAt) || observed.CollectedAt.Before(observed.AdvertisingAt) || observed.CollectedAt.After(archive.Capture.AdvertisingStoppedAt) || len(observed.Probes) > 2*len(expected.Ports) || validateLocalState(expected, observed.States, false) != nil {
		archive.Capture.Missing = append(archive.Capture.Missing, "lldp_capture_scope_invalid")
		return archive, nil
	}

	rawSize := 0
	probeMap := map[string]RawProbe{}
	for _, p := range observed.Probes {
		rawSize += len(p.Stdout) + len(p.Stderr)
		key := p.PhysicalPortID + ":" + p.Kind
		if rawSize > MaxCaptureBytes/2 || p.Kind != "neighbors" && p.Kind != "local" || p.State != "success" && p.State != "failed" {
			return archive, ErrCaptureRejected
		}
		if _, duplicate := probeMap[key]; duplicate {
			return archive, ErrCaptureRejected
		}
		probeMap[key] = p
	}
	states := map[string]LocalState{}
	for _, p := range observed.States {
		states[p.PhysicalPortID] = p
	}
	for _, p := range expected.Ports {
		state := states[p.PhysicalPortID]
		port := HostPort{PhysicalPortID: p.PhysicalPortID, Interface: state.Interface, BDF: state.BDF, PermanentMAC: state.PermanentMAC, CurrentMAC: state.CurrentMAC, LinkUp: state.LinkUp, WindowComplete: observed.WindowComplete}
		for _, kind := range []string{"local", "neighbors"} {
			probe, ok := probeMap[p.PhysicalPortID+":"+kind]
			if !ok || probe.State != "success" {
				archive.Capture.Missing = append(archive.Capture.Missing, "lldp_"+kind+"_unavailable:"+p.PhysicalPortID)
				continue
			}
			if kind == "local" {
				values, e := ParseLocalJSON0([]byte(probe.Stdout), []string{p.Interface})
				if e != nil {
					archive.Capture.Missing = append(archive.Capture.Missing, "lldp_local_format_unsupported:"+p.PhysicalPortID)
				} else {
					identity := values[p.Interface]
					port.Advertised = &identity
				}
			} else {
				values, e := ParseNeighborsJSON0([]byte(probe.Stdout), []string{p.Interface})
				if e != nil {
					archive.Capture.Missing = append(archive.Capture.Missing, "lldp_neighbors_format_unsupported:"+p.PhysicalPortID)
				} else {
					port.Neighbors = values[p.Interface]
					port.LLDPReadable = true
				}
			}
		}
		archive.Capture.Ports = append(archive.Capture.Ports, port)
	}
	archive.Probes = observed.Probes
	// Unexpected probe ports are never discarded as a partial successful capture.
	for _, probe := range observed.Probes {
		if _, ok := states[probe.PhysicalPortID]; !ok {
			return archive, ErrCaptureRejected
		}
	}
	return archive, nil
}

// DecodeArchive validates the command plan, positive cleanup and independent
// raw-to-projection equality. An HTTP upload path deliberately does not exist;
// the execution consumer must additionally verify its original fenced receipt.
func DecodeArchive(raw []byte) (*Archive, error) {
	if len(raw) == 0 || len(raw) > MaxCaptureBytes {
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
	var archive Archive
	if decoder.Decode(&archive) != nil || archive.Version != Version || archive.Capture.Version != Version || archive.Capture.Origin != "real" && archive.Capture.Origin != "sim" || archive.Capture.ProjectID != archive.Expectation.ProjectID || archive.Capture.MachineID != archive.Expectation.MachineID || archive.Capture.ScopeHash != archive.Expectation.ScopeHash || archive.Capture.Serial != archive.Expectation.Serial || !archive.Capture.Stopped || !archive.Capture.AdminRestored || !archive.Capture.ConfigurationPreserved || !archive.Stop.Stopped || !archive.Stop.Restored || archive.Stop.PlanDigest != archive.PlanDigest || validateLocalState(archive.Expectation, archive.Before, true) != nil || validateLocalState(archive.Expectation, archive.After, false) != nil || !localConfigurationEqual(archive.Before, archive.After) {
		return nil, ErrCaptureRejected
	}
	projected, e := projectArchive(&archive)
	if e != nil {
		return nil, e
	}
	expectedProjection, _ := json.Marshal(projected.Capture)
	storedProjection, _ := json.Marshal(archive.Capture)
	expectedProbes, _ := json.Marshal(projected.Probes)
	storedProbes, _ := json.Marshal(archive.Probes)
	if !bytes.Equal(expectedProjection, storedProjection) || !bytes.Equal(expectedProbes, storedProbes) {
		return nil, ErrCaptureRejected
	}
	return projected, nil
}

func validateLocalState(expected Expectation, states []LocalState, before bool) error {
	if len(states) != len(expected.Ports) {
		return ErrCaptureRejected
	}
	index := map[string]LocalState{}
	for _, p := range states {
		if _, seen := index[p.PhysicalPortID]; seen || len(p.Addresses) > 128 {
			return ErrCaptureRejected
		}
		index[p.PhysicalPortID] = p
	}
	for _, p := range expected.Ports {
		actual, ok := index[p.PhysicalPortID]
		if !ok || actual.Interface != p.Interface || actual.BDF != p.BDF || !sameMAC(actual.PermanentMAC, p.PermanentMAC) || !sameMAC(actual.CurrentMAC, p.PermanentMAC) || actual.VirtualFunction || actual.Master != "" || actual.MTU < 64 || actual.MTU > 65535 || before && actual.ExistingLLDP {
			return ErrCaptureRejected
		}
	}
	return nil
}
func localConfigurationEqual(before, after []LocalState) bool {
	normalize := func(states []LocalState) []LocalState {
		out := slices.Clone(states)
		for i := range out {
			out[i].Addresses = slices.Clone(out[i].Addresses)
			sort.Strings(out[i].Addresses)
			out[i].LinkUp = nil
		}
		sort.Slice(out, func(i, j int) bool { return out[i].PhysicalPortID < out[j].PhysicalPortID })
		return out
	}
	return reflect.DeepEqual(normalize(before), normalize(after))
}

func readbackMatches(plan Plan, expected Expectation, observed *Observation, proof *Readback, stopped time.Time) bool {
	if proof == nil || observed == nil || proof.PlanDigest != plan.Digest || proof.CommandID != plan.CommandID || proof.ScopeHash != expected.ScopeHash || proof.Origin != "device" && proof.Origin != "protocol_fixture" {
		return false
	}
	if proof.Binding.ProjectID != expected.ProjectID || proof.Binding.CommandID != plan.CommandID || proof.Binding.Origin != proof.Origin {
		return false
	}
	projection, e := projectReadback(proof.Binding, ReadbackScope{PlanDigest: plan.Digest, CommandID: plan.CommandID, ScopeHash: expected.ScopeHash, AdvertisingAt: observed.AdvertisingAt, Ports: expected.Ports}, proof.Exchanges, stopped)
	if e != nil {
		return false
	}
	want, _ := json.Marshal(projection)
	got, _ := json.Marshal(proof.Devices)
	if !bytes.Equal(want, got) {
		return false
	}
	ports := map[string]map[string]bool{}
	for _, p := range expected.Ports {
		if ports[p.SwitchSerial] == nil {
			ports[p.SwitchSerial] = map[string]bool{}
		}
		ports[p.SwitchSerial][p.SwitchPort] = true
	}
	if len(proof.Devices) != len(ports) {
		return false
	}
	seen := map[string]bool{}
	for _, sample := range proof.Devices {
		expectedPorts, ok := ports[sample.Serial]
		if !ok || seen[sample.Serial] || sample.Validate(sample.Serial, stopped) != nil || sample.CollectedAt.Before(observed.AdvertisingAt) || sample.CollectedAt.After(stopped) || len(sample.Ports) != len(expectedPorts) {
			return false
		}
		seen[sample.Serial] = true
		for _, p := range sample.Ports {
			if !expectedPorts[p.Name] {
				return false
			}
		}
	}
	return true
}
