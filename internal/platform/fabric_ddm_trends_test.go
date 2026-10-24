package platform

import (
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"strconv"
	"testing"
	"time"
)

func TestDDMTrendsBoundPreservesExtremaAndHiddenBreaks(t *testing.T) {
	const count = 5000
	now := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	interval := 10.0
	raw := make([]FabricDDMPoint, count)
	number := func(v float64) *float64 { return &v }
	for i := range raw {
		raw[i] = FabricDDMPoint{At: now.Add(time.Duration(i) * 10 * time.Second), ModuleSerial: "module", Provider: "collector", EvidenceRef: strconv.Itoa(i), State: "normal", Data: &fabricmon.Optics{RxDBM: number(-4), TxDBM: number(-3), TemperatureC: number(40), VoltageV: number(3.3), BiasMA: number(5)}}
	}
	raw[1003].Data.TemperatureC = number(99)
	raw[1004].Data.RxDBM = number(-100)
	raw[1005].Data.VoltageV = number(8)
	raw[1010].Data = nil
	raw[1040].Provider = "replacement"
	sampled := boundedDDMPoints(raw, &interval)
	if len(sampled) > 2000 || len(sampled) >= len(raw) {
		t.Fatalf("unbounded display: %d", len(sampled))
	}
	found := map[string]bool{}
	previous := -1
	continuous := false
	for _, p := range sampled {
		found[p.EvidenceRef] = true
		index, err := strconv.Atoi(p.EvidenceRef)
		if err != nil {
			t.Fatal(err)
		}
		if index <= previous {
			t.Fatal("evidence timestamps reordered")
		}
		for _, cut := range []int{1010, 1040, 1041} {
			if previous < cut && index >= cut && p.ContinuousFromPrevious {
				t.Fatalf("hidden break bridged at %d: %+v", cut, p)
			}
		}
		continuous = continuous || p.ContinuousFromPrevious
		previous = index
	}
	for _, index := range []int{0, count - 1, 1003, 1004, 1005} {
		if !found[strconv.Itoa(index)] {
			t.Fatalf("endpoint or extreme omitted at %d", index)
		}
	}
	if !continuous {
		t.Fatal("continuous recorded windows disconnected")
	}
	for _, p := range raw {
		if p.ContinuousFromPrevious {
			t.Fatal("raw evidence was mutated")
		}
	}
}

func TestDDMTrendsUnknownSamplingAndEmptyHistory(t *testing.T) {
	value := 1.0
	now := time.Now().UTC()
	raw := []FabricDDMPoint{{At: now, Provider: "c", ModuleSerial: "m", Data: &fabricmon.Optics{RxDBM: &value}}, {At: now.Add(time.Minute), Provider: "c", ModuleSerial: "m", Data: &fabricmon.Optics{RxDBM: &value}}}
	for _, p := range boundedDDMPoints(raw, nil) {
		if p.ContinuousFromPrevious {
			t.Fatal("unknown interval connected")
		}
	}
	if points := boundedDDMPoints(nil, nil); points == nil || len(points) != 0 {
		t.Fatal("empty history must serialize as an empty array")
	}
}
