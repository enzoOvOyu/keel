package platform

import (
	"slices"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

// Only server-owned frozen facts are converted to runner plans. This function
// authorizes no work; the domain admission/worker must also verify the current
// scope, exact command/fence and exclusive machine resources.
func WiringExpectation(facts *WiringFacts, asset string) (wiring.Expectation, error) {
	if facts == nil || !facts.Ready {
		return wiring.Expectation{}, apiError(409, "WIRING_PREREQUISITES_REQUIRED", "Current immutable wiring facts required")
	}
	for _, host := range facts.Targets {
		if host.AssetID != asset {
			continue
		}
		out := wiring.Expectation{HealthPolicy: facts.HealthPolicy, Version: wiring.Version, ProjectID: facts.ProjectID, AssetID: host.AssetID, MachineID: host.MachineID, Serial: host.Serial, Hostname: host.Hostname, ScopeHash: facts.FactsHash, Ports: []wiring.ExpectedPort{}}
		for _, p := range host.Ports {
			out.Ports = append(out.Ports, wiring.ExpectedPort{PhysicalPortID: p.PhysicalPortID, ComponentID: p.ComponentID, PortIndex: p.PortIndex, Interface: p.Interface, PermanentMAC: p.PermanentMAC, BDF: p.BDF, Rail: p.Rail, CableID: p.CableID, SwitchID: p.SwitchID, SwitchSerial: p.SwitchSerial, SwitchChassisIDs: slices.Clone(p.SwitchChassisIDs), SwitchPort: p.SwitchPort})
		}
		if out.Validate() != nil {
			return out, apiError(409, "WIRING_PORT_IDENTITY_UNSUPPORTED", "Frozen identity cannot form a bounded LLDP plan")
		}
		return out, nil
	}
	return wiring.Expectation{}, ErrNotFound
}
