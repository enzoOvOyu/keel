package wiring

import (
	"strings"
	"time"
)

// EvaluateArchivePorts retains the full-command verdict and independently
// evaluates each selected physical port from the SAME validated original bytes.
// Global stop/scope/source failures apply to every port. Only known, explicitly
// port-scoped parser failures can be isolated from an unrelated port.
func EvaluateArchivePorts(raw []byte, expected Expectation, binding ReadbackBinding, hostOrigin string) (*Archive, Result, map[string]Result, error) {
	archive, result, e := EvaluateArchive(raw, expected, binding, hostOrigin)
	if e != nil {
		return nil, result, nil, e
	}
	switches := map[string]SwitchEvidence{}
	if archive.Readback != nil {
		for i := range archive.Readback.Devices {
			sample := &archive.Readback.Devices[i]
			switches[sample.Serial] = SwitchEvidence{Origin: archive.Readback.Origin, Snapshot: sample}
		}
	}
	return archive, result, evaluatePorts(expected, &archive.Capture, switches, archive.Capture.AdvertisingStoppedAt), nil
}

func evaluatePorts(expected Expectation, capture *Capture, switches map[string]SwitchEvidence, at time.Time) map[string]Result {
	selected := map[string]bool{}
	for _, p := range expected.Ports {
		selected[p.PhysicalPortID] = true
	}
	out := make(map[string]Result, len(expected.Ports))
	for _, p := range expected.Ports {
		one := expected
		one.Ports = []ExpectedPort{p}
		host := *capture
		host.Ports = []HostPort{}
		host.Missing = []string{}
		for _, observed := range capture.Ports {
			if observed.PhysicalPortID == p.PhysicalPortID {
				host.Ports = append(host.Ports, observed)
			}
		}
		for _, missing := range capture.Missing {
			prefix, id, scoped := strings.Cut(missing, ":")
			known := prefix == "lldp_local_unavailable" || prefix == "lldp_neighbors_unavailable" || prefix == "lldp_local_format_unsupported" || prefix == "lldp_neighbors_format_unsupported"
			if scoped && known && selected[id] && id != p.PhysicalPortID {
				continue
			}
			host.Missing = append(host.Missing, missing)
		}
		out[p.PhysicalPortID] = Evaluate(one, &host, switches, at)
	}
	return out
}
