package platform

import (
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"github.com/enzoOvOyu/keel/internal/monitor"
)

// Convert an independently collected peer port, never host/Agent values. Missing
// fields produce explicit unknown samples; no source substitutes for another.
func monitorSwitchSamples(port fabricmon.Port, physical string, at time.Time) []monitor.Sample {
	samples := []monitor.Sample{}
	add := func(key string, value *float64, state string) {
		quality := "good"
		if value == nil && state == "" {
			quality = "unknown"
		}
		samples = append(samples, monitor.Sample{Key: key, Entity: physical, Value: value, State: state, Quality: quality, ObservedAt: at})
	}
	state := ""
	if port.LinkUp != nil {
		state = "down"
		if *port.LinkUp {
			state = "up"
		}
	}
	add("link.state", nil, state)
	var speed *float64
	if port.SpeedMbps != nil {
		v := float64(*port.SpeedMbps)
		speed = &v
	}
	add("link.speed", speed, "")
	var temperature, rx, tx *float64
	if port.Optics != nil {
		temperature, rx, tx = port.Optics.TemperatureC, port.Optics.RxDBM, port.Optics.TxDBM
	}
	add("dom.temperature", temperature, "")
	add("dom.rx_power", rx, "")
	add("dom.tx_power", tx, "")
	return samples
}
