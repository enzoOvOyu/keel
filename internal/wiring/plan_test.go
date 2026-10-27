package wiring

import (
	"strings"
	"testing"
)

func TestLLDPPlanPinsOneDaemonPerPortWithoutShellOrGlobalConfig(t *testing.T) {
	expected, _, _, _ := wireFixture()
	first := expected.Ports[0]
	second := first
	second.PhysicalPortID = strings.Repeat("8", 32)
	second.Interface = "eth2"
	second.PermanentMAC = "02:00:00:00:01:02"
	second.PortIndex = "2"
	second.SwitchPort = "swp2"
	expected.Ports = append(expected.Ports, second)
	plan, e := BuildPlan(expected, strings.Repeat("9", 32))
	if e != nil {
		t.Fatal(e)
	}
	if len(plan.Ports) != 2 || plan.Ports[0].Socket == plan.Ports[1].Socket {
		t.Fatal("physical ports share a daemon identity")
	}
	for _, p := range plan.Ports {
		if p.Start.Tool != "lldpd" || p.ReadNeighbors.Tool != "lldpcli" || strings.Contains(p.Configuration, "/etc/") || !strings.Contains(p.Configuration, p.PhysicalPortID) || !strings.Contains(p.Configuration, p.PermanentMAC) {
			t.Fatalf("unbound plan: %+v", p)
		}
		if len(p.Socket) > 103 {
			t.Fatal("invalid unix socket path")
		}
	}
	reverse := expected
	reverse.Ports = []ExpectedPort{second, first}
	again, e := BuildPlan(reverse, strings.Repeat("9", 32))
	if e != nil || again.Digest != plan.Digest {
		t.Fatal("port input order changed the immutable plan")
	}
	expected.Ports[0].Interface = "eth1;bad"
	if _, e = BuildPlan(expected, strings.Repeat("9", 32)); e == nil {
		t.Fatal("metacharacters reached fixed CLI config")
	}
}
