package platform

import (
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

func TestMonitorSwitchSamplesPreserveUnknownAndIndependentPeer(t *testing.T) {
	at := time.Now().UTC().Add(-time.Hour)
	missing := monitorSwitchSamples(fabricmon.Port{Name: "Ethernet1"}, "physical-port", at)
	if len(missing) != 5 {
		t.Fatal("missing source fields changed the metric denominator")
	}
	for _, sample := range missing {
		if sample.Quality != "unknown" || sample.Value != nil || sample.State != "" || sample.Interface != "" || sample.PermanentMAC != "" || sample.BDF != "" || sample.Entity != "physical-port" || !sample.ObservedAt.Equal(at) || sample.Validate(time.Now()) != nil {
			t.Fatal("unknown source acquired identity or healthy values", sample)
		}
	}
	down, speed, rx := false, int64(400000), -14.25
	observed := monitorSwitchSamples(fabricmon.Port{Name: "Ethernet1", LinkUp: &down, SpeedMbps: &speed, Optics: &fabricmon.Optics{RxDBM: &rx}}, "physical-port", at)
	if observed[0].State != "down" || observed[0].Quality != "good" || observed[1].Value == nil || *observed[1].Value != 400000 || observed[2].Quality != "unknown" || observed[3].Value == nil || *observed[3].Value != rx || observed[4].Quality != "unknown" {
		t.Fatal("peer values replaced, lost, or filled", observed)
	}
	for _, sample := range observed {
		if sample.Validate(time.Now()) != nil {
			t.Fatal("invalid projected source", sample)
		}
	}
}
