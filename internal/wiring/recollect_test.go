package wiring

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

var _ RecollectionClient = (*fabricboot.ProbeSuite)(nil)

type recollectLedger struct {
	requests                map[string]string
	results                 map[string]*fabricboot.CollectorRecollectResponse
	failBegin, failComplete bool
}

func newRecollectLedger() *recollectLedger {
	return &recollectLedger{requests: map[string]string{}, results: map[string]*fabricboot.CollectorRecollectResponse{}}
}
func (l *recollectLedger) Begin(_ context.Context, _ ReadbackBinding, r fabricboot.CollectorRecollectRequest) (ReadbackDecision, error) {
	if l.failBegin {
		return ReadbackDecision{}, errors.New("commit failed")
	}
	raw, _ := json.Marshal(r)
	prior, exists := l.requests[r.CommandID]
	if exists && prior != string(raw) {
		return ReadbackDecision{}, errors.New("original dispatch differs")
	}
	if exists {
		return ReadbackDecision{Result: l.results[r.CommandID]}, nil
	}
	l.requests[r.CommandID] = string(raw)
	return ReadbackDecision{Send: true}, nil
}
func (l *recollectLedger) Complete(_ context.Context, _ ReadbackBinding, r fabricboot.CollectorRecollectRequest, response fabricboot.CollectorRecollectResponse) error {
	if l.failComplete {
		return errors.New("receipt persistence failed")
	}
	l.results[r.CommandID] = &response
	return nil
}

type recollectClient struct {
	ledger             *recollectLedger
	posts, queries     int
	loseReply, running bool
}

func (c *recollectClient) RecollectPorts(_ context.Context, r fabricboot.CollectorRecollectRequest) (*fabricboot.CollectorRecollectResponse, error) {
	c.posts++
	if c.ledger.requests[r.CommandID] == "" {
		return nil, errors.New("POST without committed original identity")
	}
	if c.loseReply {
		return nil, errors.New("lost reply")
	}
	return recollectResponse(r), nil
}
func (c *recollectClient) QueryRecollection(_ context.Context, r fabricboot.CollectorRecollectRequest) (*fabricboot.CollectorRecollectObservation, error) {
	c.queries++
	result := recollectResponse(r)
	now := time.Now().UTC()
	out := &fabricboot.CollectorRecollectObservation{Version: "fabric-recollect-status/v1", ProjectID: r.ProjectID, ExecutionID: r.ExecutionID, CommandID: r.CommandID, FencingToken: r.FencingToken, ScopeHash: r.ScopeHash, RequestHash: netbaseline.ContentHash(r), EvidenceSource: r.EvidenceSource, State: "completed", FinishedAt: &now, Result: result}
	if c.running {
		out.State = "running"
		out.FinishedAt = nil
		out.Result = nil
	}
	return out, nil
}
func recollectResponse(r fabricboot.CollectorRecollectRequest) *fabricboot.CollectorRecollectResponse {
	up := true
	devices := []fabricmon.Snapshot{}
	for _, target := range r.Targets {
		ports := []fabricmon.Port{}
		for _, name := range target.Ports {
			ports = append(ports, fabricmon.Port{Name: name, LinkUp: &up, LLDPReadable: true})
		}
		devices = append(devices, fabricmon.Snapshot{Version: "fabric-snapshot/v1", Source: "switch", Provider: "synthetic-software-fixture", Serial: target.Serial, CollectedAt: time.Now().UTC(), Capabilities: map[string]bool{"lldp": true, "interfaces": true}, Ports: ports})
	}
	return &fabricboot.CollectorRecollectResponse{Version: r.Version, ProjectID: r.ProjectID, ExecutionID: r.ExecutionID, CommandID: r.CommandID, FencingToken: r.FencingToken, ScopeHash: r.ScopeHash, EvidenceSource: r.EvidenceSource, Devices: devices}
}
func recollectBinding(command string) ReadbackBinding {
	return ReadbackBinding{ProjectID: strings.Repeat("1", 32), ExecutionID: strings.Repeat("b", 32), AttemptID: strings.Repeat("c", 32), CommandID: command, FencingToken: 7, Origin: "protocol_fixture"}
}
func recollectScope() (ReadbackBinding, ReadbackScope) {
	expected, _, _, _ := wireFixture()
	command := strings.Repeat("d", 32)
	plan, _ := BuildPlan(expected, command)
	return recollectBinding(command), ReadbackScope{PlanDigest: plan.Digest, CommandID: command, ScopeHash: expected.ScopeHash, AdvertisingAt: time.Now().UTC().Add(-time.Second), Ports: expected.Ports}
}
func TestReadbackChunkIdentityAndOriginalCommandRecovery(t *testing.T) {
	binding, scope := recollectScope()
	for _, name := range []string{"swp2", "swp3", "swp4", "swp5"} {
		p := scope.Ports[0]
		p.SwitchPort = name
		scope.Ports = append(scope.Ports, p)
	}
	requests, e := BuildReadbackRequests(binding, scope)
	if e != nil || len(requests) != 3 {
		t.Fatalf("chunks=%+v err=%v", requests, e)
	}
	reversed := scope
	reversed.Ports = slices.Clone(scope.Ports)
	slices.Reverse(reversed.Ports)
	same, e := BuildReadbackRequests(binding, reversed)
	a, _ := json.Marshal(requests)
	b, _ := json.Marshal(same)
	if e != nil || string(a) != string(b) {
		t.Fatal("caller order changed child commands")
	}
	for _, r := range requests {
		if r.Validate() != nil || len(r.Targets) != 1 || len(r.Targets[0].Ports) > 2 {
			t.Fatal("broadened existing collector contract")
		}
	}
	ledger := newRecollectLedger()
	client := &recollectClient{ledger: ledger, loseReply: true}
	run := NewRecollector(binding, client, ledger)
	if _, e = run(context.Background(), scope); !errors.Is(e, ErrOutcomeUnknown) || client.posts != 1 {
		t.Fatal("ambiguous POST did not stay unknown")
	}
	client.running = true
	if _, e = run(context.Background(), scope); !errors.Is(e, ErrOutcomeUnknown) || client.posts != 1 || client.queries != 1 {
		t.Fatal("unknown child was re-POSTed")
	}
	client.running = false
	client.loseReply = false
	proof, e := run(context.Background(), scope)
	if e != nil || proof == nil || client.posts != 3 || client.queries != 2 || len(proof.Exchanges) != 3 || len(proof.Devices) != 1 || len(proof.Devices[0].Ports) != 5 {
		t.Fatalf("recovery=%+v err=%v posts=%d queries=%d", proof, e, client.posts, client.queries)
	}
	if _, e = run(context.Background(), scope); e != nil || client.posts != 3 || client.queries != 2 {
		t.Fatal("persisted completion was sent again")
	}
}
func TestReadbackPersistenceAndExchangeProjection(t *testing.T) {
	binding, scope := recollectScope()
	ledger := newRecollectLedger()
	client := &recollectClient{ledger: ledger}
	run := NewRecollector(binding, client, ledger)
	ledger.failBegin = true
	if _, e := run(context.Background(), scope); !errors.Is(e, ErrOutcomeUnknown) || client.posts != 0 {
		t.Fatal("transport ran before durable admission")
	}
	ledger.failBegin = false
	ledger.failComplete = true
	if _, e := run(context.Background(), scope); !errors.Is(e, ErrOutcomeUnknown) || client.posts != 1 {
		t.Fatal("unretained completion became success")
	}
	ledger.failComplete = false
	proof, e := run(context.Background(), scope)
	if e != nil || client.posts != 1 || client.queries != 1 {
		t.Fatal("persistence failure triggered redispatch", e)
	}
	requests, _ := BuildReadbackRequests(binding, scope)
	proof.Exchanges[0].Response.Devices[0].CollectedAt = scope.AdvertisingAt.Add(-time.Millisecond)
	if _, e = projectReadback(binding, scope, proof.Exchanges, time.Now()); e == nil {
		t.Fatal("one stale chunk hid under a merged timestamp")
	}
	proof.Exchanges[0].Response = *recollectResponse(requests[0])
	proof.Exchanges[0].Request.CommandID = strings.Repeat("e", 32)
	if _, e = projectReadback(binding, scope, proof.Exchanges, time.Now()); e == nil {
		t.Fatal("unrelated child command became retained evidence")
	}
	if _, e = NewRecollector(binding, client, nil)(context.Background(), scope); !errors.Is(e, ErrCaptureRejected) {
		t.Fatal("nil ledger allowed dispatch")
	}
}
