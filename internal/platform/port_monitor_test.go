package platform

import (
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
)

func TestPortMonitorNeverInfersHealthyFromStaleSimUnmappedOrOtherRevision(t *testing.T) {
	now := time.Now().UTC()
	binding := MappingBinding{Plan: MappingPortPlan{PhysicalPortID: "port", Role: "compute"}}
	healthy := MonitorObservation{Sequence: 1, Source: "host", Method: "agent", Origin: "real", MappingRevisionID: "revision", Freshness: "fresh", Sample: monitor.Sample{Key: "link.state", State: "up", Quality: "good", ObservedAt: now}}
	item := PortMonitorItem{Plan: binding.Plan, Binding: &binding, MappingState: "verified", Observations: []MonitorObservation{healthy}}
	requireState := func(expected string) {
		t.Helper()
		state, _ := portMonitorState(item, "revision", false)
		if state != expected {
			t.Fatal("state", state, "wanted", expected)
		}
	}
	requireState("up")
	item.Observations[0].Origin = "sim"
	requireState("unknown")
	item.Observations[0] = healthy
	item.Observations[0].Freshness = "stale"
	requireState("unknown")
	item.Observations[0] = healthy
	item.Observations[0].MappingRevisionID = "old"
	requireState("unknown")
	item.Observations[0] = healthy
	item.MappingState = "blocked"
	requireState("unknown")
	item.MappingState = "verified"
	item.Plan.Role = "unused"
	requireState("disabled")
	item.Plan.Role = "compute"
	item.Observations[0].Method = "ssh"
	requireState("degraded")
	item.Observations[0] = healthy
	item.OpenIssueIDs = []string{"open-issue"}
	requireState("degraded")
	item.OpenIssueIDs = nil
	item.Observations[0].Sample.State = "down"
	requireState("down")
	sequence := int64(1)
	item.Alerts = []MonitorAlert{{RuleID: "link-flapping", State: "firing", MappingRevisionID: "revision", SampleSequence: &sequence}}
	requireState("flapping")
	item.Observations[0].Freshness = "stale"
	requireState("unknown")
	if state, _ := portMonitorState(item, "revision", true); state != "maintained" {
		t.Fatal("maintained in-band continued")
	}
	if portMonitorNetworkState(PortMonitorSummary{}, "verified", false) != "unknown" {
		t.Fatal("empty denominator became healthy")
	}
	if portMonitorNetworkState(PortMonitorSummary{Up: 2, Unknown: 1}, "verified", false) != "unknown" {
		t.Fatal("partial coverage became healthy")
	}
	if portMonitorNetworkState(PortMonitorSummary{Up: 2, Down: 1}, "verified", false) != "abnormal" {
		t.Fatal("known down hidden by aggregation")
	}
}
