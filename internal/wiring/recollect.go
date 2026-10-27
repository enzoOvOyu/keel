package wiring

import (
	"context"
	"encoding/json"
	"slices"
	"sort"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

type ReadbackBinding struct {
	ProjectID    string `json:"project_id"`
	ExecutionID  string `json:"execution_id"`
	AttemptID    string `json:"attempt_id"`
	CommandID    string `json:"command_id"`
	FencingToken int64  `json:"fencing_token"`
	Origin       string `json:"origin"`
}
type ReadbackExchange struct {
	Request  fabricboot.CollectorRecollectRequest  `json:"request"`
	Response fabricboot.CollectorRecollectResponse `json:"response"`
}
type ReadbackDecision struct {
	Send   bool
	Result *fabricboot.CollectorRecollectResponse
}

// Ledger.Begin must commit the exact child request identity before returning
// Send=true. A duplicate/unknown dispatch returns Send=false and can only query
// its original command. The execution owner supplies the durable ledger; a nil
// ledger can never cause a device request. Neither method may retain DB locks
// while the collector is called.
type ReadbackLedger interface {
	Begin(context.Context, ReadbackBinding, fabricboot.CollectorRecollectRequest) (ReadbackDecision, error)
	Complete(context.Context, ReadbackBinding, fabricboot.CollectorRecollectRequest, fabricboot.CollectorRecollectResponse) error
}
type RecollectionClient interface {
	RecollectPorts(context.Context, fabricboot.CollectorRecollectRequest) (*fabricboot.CollectorRecollectResponse, error)
	QueryRecollection(context.Context, fabricboot.CollectorRecollectRequest) (*fabricboot.CollectorRecollectObservation, error)
}

// Existing FR-0.8.2 requests deliberately cap two devices/two ports each. G2
// partitions its explicit selected ports without weakening that older contract.
// Stable derived child IDs prevent an unknown partial recollection from being
// reissued with a new key. Caller port ordering does not change the plan.
func BuildReadbackRequests(binding ReadbackBinding, scope ReadbackScope) ([]fabricboot.CollectorRecollectRequest, error) {
	if !entityID.MatchString(binding.ProjectID) || !entityID.MatchString(binding.ExecutionID) || !entityID.MatchString(binding.AttemptID) || !entityID.MatchString(binding.CommandID) || binding.FencingToken < 1 || binding.Origin != "device" && binding.Origin != "protocol_fixture" || scope.CommandID != binding.CommandID || !scopeDigest.MatchString(scope.ScopeHash) || !scopeDigest.MatchString(scope.PlanDigest) || scope.AdvertisingAt.IsZero() || len(scope.Ports) < 1 || len(scope.Ports) > MaxPorts {
		return nil, ErrCaptureRejected
	}
	byDevice := map[string]fabricboot.CollectorTarget{}
	serials := map[string]string{}
	seenPorts := map[string]bool{}
	for _, p := range scope.Ports {
		if !entityID.MatchString(p.SwitchID) || !field.MatchString(p.SwitchSerial) || !field.MatchString(p.SwitchPort) {
			return nil, ErrCaptureRejected
		}
		if serials[p.SwitchSerial] != "" && serials[p.SwitchSerial] != p.SwitchID || seenPorts[p.SwitchID+":"+p.SwitchPort] {
			return nil, ErrCaptureRejected
		}
		serials[p.SwitchSerial] = p.SwitchID
		seenPorts[p.SwitchID+":"+p.SwitchPort] = true
		target, ok := byDevice[p.SwitchID]
		if ok && target.Serial != p.SwitchSerial {
			return nil, ErrCaptureRejected
		}
		target.SwitchID = p.SwitchID
		target.Serial = p.SwitchSerial
		target.Ports = append(target.Ports, p.SwitchPort)
		byDevice[p.SwitchID] = target
	}
	ids := make([]string, 0, len(byDevice))
	for id := range byDevice {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	groups := []fabricboot.CollectorTarget{}
	for _, id := range ids {
		target := byDevice[id]
		sort.Strings(target.Ports)
		for i := 0; i < len(target.Ports); i += 2 {
			groups = append(groups, fabricboot.CollectorTarget{SwitchID: id, Serial: target.Serial, Ports: slices.Clone(target.Ports[i:min(i+2, len(target.Ports))])})
		}
	}
	out := []fabricboot.CollectorRecollectRequest{}
	// Each subrequest contains one device; independent two-port chunks of that
	// device remain individually recoverable and never alias the same command.
	for _, target := range groups {
		identity, _ := json.Marshal([]any{binding, scope.PlanDigest, scope.ScopeHash, target})
		request := fabricboot.CollectorRecollectRequest{Version: "fabric-recollect/v1", ProjectID: binding.ProjectID, ExecutionID: binding.ExecutionID, CommandID: Hash(identity)[:32], FencingToken: binding.FencingToken, ScopeHash: scope.ScopeHash, NotBefore: scope.AdvertisingAt, EvidenceSource: binding.Origin, Targets: []fabricboot.CollectorTarget{target}}
		if request.Validate() != nil {
			return nil, ErrCaptureRejected
		}
		out = append(out, request)
	}
	return out, nil
}

// NewRecollector adapts the fixed mTLS collector to the live host session. It
// performs only original-command queries after any recorded dispatch, never a
// second POST. Persistence failures after sending keep the outcome unknown.
func NewRecollector(binding ReadbackBinding, client RecollectionClient, ledger ReadbackLedger) Recollect {
	return func(ctx context.Context, scope ReadbackScope) (*Readback, error) {
		if client == nil || ledger == nil {
			return nil, ErrCaptureRejected
		}
		requests, e := BuildReadbackRequests(binding, scope)
		if e != nil {
			return nil, e
		}
		out := &Readback{Binding: binding, PlanDigest: scope.PlanDigest, CommandID: binding.CommandID, ScopeHash: scope.ScopeHash, Origin: binding.Origin, Devices: []fabricmon.Snapshot{}, Exchanges: []ReadbackExchange{}}
		for _, request := range requests {
			if ctx.Err() != nil {
				return nil, ErrOutcomeUnknown
			}
			decision, e := ledger.Begin(ctx, binding, request)
			if e != nil {
				return nil, ErrOutcomeUnknown
			}
			response := decision.Result
			if response != nil && decision.Send {
				return nil, ErrOutcomeUnknown
			}
			if response == nil {
				if decision.Send {
					response, e = client.RecollectPorts(ctx, request)
				} else {
					observation, queryErr := client.QueryRecollection(ctx, request)
					if queryErr != nil || observation == nil || observation.Validate(request, time.Now().UTC()) != nil || !observation.Terminal() {
						return nil, ErrOutcomeUnknown
					}
					if observation.State != "completed" {
						return nil, ErrCaptureRejected
					}
					response = observation.Result
				}
				if e != nil || response == nil {
					return nil, ErrOutcomeUnknown
				}
				if response.Validate(request, time.Now().UTC()) != nil {
					return nil, ErrOutcomeUnknown
				}
				if e = ledger.Complete(ctx, binding, request, *response); e != nil {
					return nil, ErrOutcomeUnknown
				}
			}
			if response.Validate(request, time.Now().UTC()) != nil {
				return nil, ErrOutcomeUnknown
			}
			out.Exchanges = append(out.Exchanges, ReadbackExchange{Request: request, Response: *response})
		}
		devices, e := projectReadback(binding, scope, out.Exchanges, time.Now().UTC())
		if e != nil {
			return nil, ErrOutcomeUnknown
		}
		out.Devices = devices
		return out, nil
	}
}

// Rebuild from the complete, command-bound exchanges, including every chunk's
// time. The earliest merged timestamp cannot conceal a later out-of-window read.
func projectReadback(binding ReadbackBinding, scope ReadbackScope, exchanges []ReadbackExchange, at time.Time) ([]fabricmon.Snapshot, error) {
	requests, e := BuildReadbackRequests(binding, scope)
	if e != nil || len(requests) != len(exchanges) {
		return nil, ErrCaptureRejected
	}
	samples := map[string]*fabricmon.Snapshot{}
	for i, exchange := range exchanges {
		want, _ := json.Marshal(requests[i])
		got, _ := json.Marshal(exchange.Request)
		if string(want) != string(got) || exchange.Response.Validate(requests[i], at) != nil {
			return nil, ErrCaptureRejected
		}
		for _, sample := range exchange.Response.Devices {
			prior := samples[sample.Serial]
			if prior == nil {
				copy := fabricmon.Snapshot{Version: sample.Version, Source: sample.Source, Provider: sample.Provider, Serial: sample.Serial, NOS: sample.NOS, CollectedAt: sample.CollectedAt, Capabilities: map[string]bool{"lldp": sample.Capabilities["lldp"], "interfaces": sample.Capabilities["interfaces"]}, Ports: slices.Clone(sample.Ports)}
				samples[sample.Serial] = &copy
			} else {
				if prior.Version != sample.Version || prior.Source != sample.Source || prior.Provider != sample.Provider || prior.NOS != sample.NOS {
					return nil, ErrCaptureRejected
				}
				prior.Capabilities["lldp"] = prior.Capabilities["lldp"] && sample.Capabilities["lldp"]
				prior.Capabilities["interfaces"] = prior.Capabilities["interfaces"] && sample.Capabilities["interfaces"]
				if sample.CollectedAt.Before(prior.CollectedAt) {
					prior.CollectedAt = sample.CollectedAt
				}
				prior.Ports = append(prior.Ports, sample.Ports...)
			}
		}
	}
	serials := make([]string, 0, len(samples))
	for serial := range samples {
		serials = append(serials, serial)
	}
	sort.Strings(serials)
	out := make([]fabricmon.Snapshot, 0, len(samples))
	for _, serial := range serials {
		sample := samples[serial]
		sort.Slice(sample.Ports, func(i, j int) bool { return sample.Ports[i].Name < sample.Ports[j].Name })
		if sample.Validate(serial, at) != nil {
			return nil, ErrCaptureRejected
		}
		out = append(out, *sample)
	}
	return out, nil
}
