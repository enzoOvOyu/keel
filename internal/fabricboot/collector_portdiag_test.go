package fabricboot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
	"github.com/enzoOvOyu/keel/internal/portdiag"
)

func TestPortDiagnosticUsesOriginalRecollectionEnvelopeAndScope(t *testing.T) {
	request, response, now := recollectionFixture()
	request.Version = portdiag.Version
	request.AttemptID = strings.Repeat("f", 32)
	scope := portdiag.Scope{ProjectID: request.ProjectID, IssueID: strings.Repeat("1", 32), MachineID: strings.Repeat("2", 32), Serial: "HOST-A", PhysicalPortID: strings.Repeat("3", 32), Interface: "eth0", PermanentMAC: "02:00:00:00:00:01", BDF: "0000:01:00.0", SwitchID: request.Targets[0].SwitchID, SwitchSerial: "SW-A", SwitchPort: "swp1", Category: "T1", RuleID: "link-down", Source: "host", Method: "agent", Origin: "sim", RepairedAt: now.Add(-time.Minute), Limits: map[string]monitor.Limit{}}
	request.PortDiagnostic = &scope
	response.Version = request.Version
	response.Devices = nil
	response.PortDiagnostic = &portdiag.Capture{Version: portdiag.Version, Binding: portdiag.Binding{ExecutionID: request.ExecutionID, AttemptID: request.AttemptID, CommandID: request.CommandID, FencingToken: request.FencingToken, ScopeHash: request.ScopeHash}, Scope: scope, StartedAt: now.Add(-time.Second), FinishedAt: now, Stopped: true, LLDPReadable: true, HostPeerSerial: "SW-A", HostPeerPort: "swp1", SwitchPeerSerial: "HOST-A", SwitchPeerMAC: scope.PermanentMAC, SwitchPeerInterface: scope.Interface, RawRefs: []string{"original:fixture"}, Observations: []monitor.Sample{{Key: "link.state", Entity: "eth0", State: "up", Quality: "good", ObservedAt: now, Interface: scope.Interface, PermanentMAC: scope.PermanentMAC, BDF: scope.BDF}}}
	if e := response.Validate(request, now); e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*CollectorRecollectResponse){
		func(v *CollectorRecollectResponse) { v.CommandID = strings.Repeat("9", 32) }, func(v *CollectorRecollectResponse) { v.PortDiagnostic.Binding.FencingToken++ }, func(v *CollectorRecollectResponse) { v.PortDiagnostic.Scope.Origin = "real" }, func(v *CollectorRecollectResponse) { v.PortDiagnostic.Scope.Interface = "eth1" }, func(v *CollectorRecollectResponse) { v.PortDiagnostic = nil },
	} {
		raw, _ := json.Marshal(response)
		var changed CollectorRecollectResponse
		_ = json.Unmarshal(raw, &changed)
		change(&changed)
		if changed.Validate(request, now) == nil {
			t.Fatal("expanded, relabelled or foreign original diagnostic accepted")
		}
	}
	request.PortDiagnostic.Origin = "real"
	if request.Validate() == nil {
		t.Fatal("fixture source was promoted to real diagnostic")
	}
}
