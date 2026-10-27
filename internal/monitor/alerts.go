package monitor

import "time"

type Limit struct {
	WarnMin     *float64 `json:"warn_min"`
	WarnMax     *float64 `json:"warn_max"`
	CriticalMin *float64 `json:"critical_min"`
	CriticalMax *float64 `json:"critical_max"`
}
type Finding struct {
	RuleID   string
	State    string
	Severity string
	Category string
	Context  map[string]any
}

func Evaluate(sample Sample, previous *Sample, expectedUp bool, downTransitions int, limits map[string]Limit) []Finding {
	result := []Finding{}
	add := func(rule, severity, category string, hit, known bool) {
		state := "resolved"
		if !known {
			state = "unknown"
		} else if hit {
			state = "firing"
		}
		result = append(result, Finding{rule, state, severity, category, map[string]any{"sample": sample, "previous": previous}})
	}
	good := sample.Quality == "good"
	switch sample.Key {
	case "link.state":
		if !expectedUp {
			return result
		}
		add("link-down", "critical", "T1", sample.State == "down", good)
		add("link-flapping", "critical", "T4", downTransitions >= 3, good)
	case "dom.rx_power", "dom.tx_power", "dom.temperature":
		key := map[string]string{"dom.rx_power": "rx_dbm", "dom.tx_power": "tx_dbm", "dom.temperature": "temperature_c"}[sample.Key]
		limit, ok := limits[key]
		known := good && sample.Value != nil && ok && (limit.WarnMin != nil || limit.WarnMax != nil || limit.CriticalMin != nil || limit.CriticalMax != nil)
		severity := "warning"
		hit := false
		if known {
			value := *sample.Value
			if limit.CriticalMin != nil && value < *limit.CriticalMin || limit.CriticalMax != nil && value > *limit.CriticalMax {
				severity = "critical"
				hit = true
			} else if limit.WarnMin != nil && value < *limit.WarnMin || limit.WarnMax != nil && value > *limit.WarnMax {
				hit = true
			}
		}
		add("optics-"+key, severity, "T3", hit, known && expectedUp)
	case "gpu.ecc":
		known := good && sample.Value != nil && previous != nil && previous.Quality == "good" && previous.Value != nil && sample.ObservedAt.After(previous.ObservedAt) && sample.ObservedAt.Sub(previous.ObservedAt) <= 15*time.Minute
		add("gpu-ecc-growth", "warning", "", known && *sample.Value > *previous.Value, known)
	case "rdma.pfc_pause":
		// A catalog threshold is required; counter resets and gaps are unknown.
		threshold, ok := limits["pfc_pause_delta"]
		known := good && sample.Value != nil && previous != nil && previous.Quality == "good" && previous.Value != nil && sample.ObservedAt.After(previous.ObservedAt) && sample.ObservedAt.Sub(previous.ObservedAt) <= 10*time.Minute && *sample.Value >= *previous.Value && ok && threshold.CriticalMax != nil
		add("pfc-pause-growth", "critical", "T6", known && *sample.Value-*previous.Value > *threshold.CriticalMax, known && expectedUp)
	}
	return result
}
