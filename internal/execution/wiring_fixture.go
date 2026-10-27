package execution

import (
	"context"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/wiring"
	"sync"
)

// FixtureWiringAdapters retains one synthetic session per original command in
// this worker. A restart loses volatile receipts and therefore stays UNKNOWN;
// it never regenerates a collector completion or claims a real device result.
type FixtureWiringAdapters struct {
	raw      []byte
	mu       sync.Mutex
	sessions map[string]*fixtureWiringSession
}
type fixtureWiringSession struct {
	driver    wiring.Driver
	collector *fixtureWiringCollector
}

func NewFixtureWiringAdapters(raw []byte) (*FixtureWiringAdapters, error) {
	if _, e := wiring.NewFixtureDriver(raw); e != nil {
		return nil, e
	}
	return &FixtureWiringAdapters{raw: append([]byte(nil), raw...), sessions: map[string]*fixtureWiringSession{}}, nil
}
func (*FixtureWiringAdapters) HostOrigin() string   { return "sim" }
func (*FixtureWiringAdapters) SwitchOrigin() string { return "protocol_fixture" }
func (f *FixtureWiringAdapters) Open(command *platform.WiringCommand) (wiring.Driver, wiring.RecollectionClient, error) {
	if command == nil || command.HostOrigin != "sim" || command.Binding.Origin != "protocol_fixture" {
		return nil, nil, wiring.ErrCaptureRejected
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if prior := f.sessions[command.Binding.CommandID]; prior != nil {
		if string(jsonBytes(prior.collector.command)) != string(jsonBytes(command)) {
			return nil, nil, wiring.ErrCaptureRejected
		}
		return prior.driver, prior.collector, nil
	}
	// Never evict an unresolved simulated session and manufacture its receipt.
	if len(f.sessions) >= 4096 {
		return nil, nil, errors.New("fixture session capacity reached")
	}
	driver, e := wiring.NewFixtureDriver(f.raw)
	if e != nil {
		return nil, nil, e
	}
	client := &fixtureWiringCollector{raw: f.raw, command: *command}
	f.sessions[command.Binding.CommandID] = &fixtureWiringSession{driver, client}
	return driver, client, nil
}

type fixtureWiringCollector struct {
	mu      sync.Mutex
	raw     []byte
	command platform.WiringCommand
	client  wiring.RecollectionClient
}

func (c *fixtureWiringCollector) RecollectPorts(ctx context.Context, r fabricboot.CollectorRecollectRequest) (*fabricboot.CollectorRecollectResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		scope := wiring.ReadbackScope{PlanDigest: c.command.Plan.Digest, CommandID: c.command.Binding.CommandID, ScopeHash: c.command.Expectation.ScopeHash, AdvertisingAt: r.NotBefore, Ports: c.command.Expectation.Ports}
		client, e := wiring.NewFixtureCollector(c.raw, c.command.Binding, scope)
		if e != nil {
			return nil, e
		}
		c.client = client
	}
	return c.client.RecollectPorts(ctx, r)
}
func (c *fixtureWiringCollector) QueryRecollection(ctx context.Context, r fabricboot.CollectorRecollectRequest) (*fabricboot.CollectorRecollectObservation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client == nil {
		return nil, wiring.ErrOutcomeUnknown
	}
	return c.client.QueryRecollection(ctx, r)
}
