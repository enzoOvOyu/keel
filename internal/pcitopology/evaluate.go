package pcitopology

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

// PCIeAffinity derives only relationships justified by sysfs. Distinct host
// bridges on the same NUMA node do not establish a CPU/socket interconnect, so
// that case stays unknown pending independent host-bridge evidence (NODE).
func PCIeAffinity(a, b Node, nodes map[string]Node) string {
	if a.BDF == b.BDF {
		return "X"
	}
	if a.Root == "" || b.Root == "" || a.NUMA == nil || b.NUMA == nil {
		return "unknown"
	}
	if a.Socket != nil && b.Socket != nil && *a.Socket != *b.Socket {
		return "SYS"
	}
	if *a.NUMA != *b.NUMA {
		if a.Socket != nil && b.Socket != nil && *a.Socket == *b.Socket {
			return "NODE"
		}
		return "unknown"
	}
	if a.Root != b.Root {
		if a.Socket != nil && b.Socket != nil && *a.Socket == *b.Socket {
			return "NODE"
		}
		return "unknown"
	}
	common := 0
	for common < len(a.Upstream) && common < len(b.Upstream) && a.Upstream[common] == b.Upstream[common] {
		common++
	}
	if common == 0 {
		return "PHB"
	}
	// A PCI bridge class alone cannot distinguish a root port from a switch.
	// Count switch upstream ports along the path through the common ancestor.
	switches := map[string]bool{}
	for _, path := range [][]string{a.Upstream, b.Upstream} {
		for i, id := range path {
			if i < common-1 {
				continue
			}
			n, ok := nodes[id]
			if !ok || n.PCIeType == "" || n.PCIeType == "unknown" {
				return "unknown"
			}
			if n.PCIeType == "switch_upstream" {
				switches[id] = true
			}
		}
	}
	// A common downstream switch port implies the same parent switch whose
	// upstream port is just before it in both chains.
	if common > 0 && nodes[a.Upstream[common-1]].PCIeType == "switch_downstream" {
		for i := common - 1; i >= 0; i-- {
			id := a.Upstream[i]
			if nodes[id].PCIeType == "switch_upstream" {
				switches[id] = true
				break
			}
		}
	}
	if len(switches) == 1 {
		return "PIX"
	}
	if len(switches) > 1 {
		return "PXB"
	}
	return "PHB"
}

func Evaluate(s *Snapshot, e *Expectation, bindings []Binding, real bool) Evaluation {
	out := Evaluation{RuleVersion: RuleVersion, Verdict: "PASS", Diffs: []Diff{}}
	add := func(category, code, target, verdict string, expected, actual any) {
		out.Diffs = append(out.Diffs, Diff{category, code, target, verdict, expected, actual})
		if verdict == "FAIL" {
			out.Verdict = "FAIL"
		} else if out.Verdict == "PASS" {
			out.Verdict = "BLOCKED"
		}
	}
	if e.Validate() != nil {
		add("", "TOPOLOGY_INDEPENDENT_MODEL_REQUIRED", "model", "BLOCKED", Version, nil)
		return out
	}
	if s.Validate() != nil {
		add("", "TOPOLOGY_SNAPSHOT_INVALID", "snapshot", "BLOCKED", Version, nil)
		return out
	}
	if !real {
		add("", "TOPOLOGY_SIMULATION_ONLY", "source", "BLOCKED", "real", "sim")
	}
	for _, m := range s.Missing {
		add("", "TOPOLOGY_OBSERVATION_MISSING", m, "BLOCKED", "observed", nil)
	}
	nodes := map[string]Node{}
	byLabel := map[string]Device{}
	devices := map[string]Device{}
	labels := map[string]string{}
	for _, n := range s.Nodes {
		nodes[n.BDF] = n
	}
	for _, d := range s.Devices {
		devices[d.BDF] = d
		byLabel[d.Label] = d
	}
	bound := map[string]Binding{}
	used := map[string]bool{}
	components := map[string]bool{}
	for _, b := range bindings {
		if b.Key == "" || bound[b.Key].Key != "" || !regexpID(b.ComponentID) || components[b.ComponentID] || b.BDF == "" || CanonicalBDF(b.BDF) != b.BDF || used[b.BDF] || b.Kind != "gpu" && b.Kind != "nic" {
			add("pcie_misplaced", "TOPOLOGY_COMPONENT_BINDING_INVALID", b.Key, "BLOCKED", "unique stable component", b)
			continue
		}
		bound[b.Key] = b
		used[b.BDF] = true
		components[b.ComponentID] = true
	}
	for _, d := range e.Devices {
		b, ok := bound[d.Key]
		if !ok || b.Kind != d.Kind {
			add("pcie_misplaced", "TOPOLOGY_COMPONENT_BINDING_REQUIRED", d.Key, "BLOCKED", d.Kind, b)
			continue
		}
		device, ok := devices[b.BDF]
		if !ok || device.Kind != d.Kind {
			add("pcie_misplaced", "TOPOLOGY_COMPONENT_OBSERVATION_REQUIRED", d.Key, "BLOCKED", b, nil)
			continue
		}
		labels[d.Key] = device.Label
		n := nodes[b.BDF]
		if n.PhysicalSlot == "" {
			add("pcie_misplaced", "TOPOLOGY_SLOT_UNAVAILABLE", d.Key, "BLOCKED", d.PCIESlot, nil)
		} else if n.PhysicalSlot != d.PCIESlot {
			add("pcie_misplaced", "TOPOLOGY_SLOT_MISMATCH", d.Key, "FAIL", d.PCIESlot, n.PhysicalSlot)
		}
		if n.Root != d.Root || !equalPath(n.Upstream, d.Upstream) {
			add("pcie_misplaced", "TOPOLOGY_UPSTREAM_MISMATCH", d.Key, "FAIL", map[string]any{"root": d.Root, "upstream": d.Upstream}, map[string]any{"root": n.Root, "upstream": n.Upstream})
		}
		if n.NUMA == nil {
			add("numa_anomaly", "TOPOLOGY_NUMA_UNAVAILABLE", d.Key, "BLOCKED", d.NUMA, nil)
		} else if *n.NUMA != *d.NUMA {
			add("numa_anomaly", "TOPOLOGY_NUMA_MISMATCH", d.Key, "FAIL", d.NUMA, n.NUMA)
		}
		// Compare to the independently declared slot design. A x16 adapter in
		// an intentionally x8 slot is not automatically degraded.
		if n.SpeedGT == nil || n.Width == nil || n.MaxSpeedGT == nil || n.MaxWidth == nil {
			add("lnk_degraded", "TOPOLOGY_LINK_UNAVAILABLE", d.Key, "BLOCKED", d, nil)
		} else {
			if *n.MaxSpeedGT < d.SpeedGT || *n.MaxWidth < d.Width {
				add("lnk_degraded", "TOPOLOGY_CAPABILITY_BELOW_MODEL", d.Key, "FAIL", map[string]any{"speed_gt": d.SpeedGT, "width": d.Width}, map[string]any{"speed_gt": n.MaxSpeedGT, "width": n.MaxWidth})
			}
			if *n.SpeedGT < d.SpeedGT || *n.Width < d.Width {
				add("lnk_degraded", "TOPOLOGY_LINK_DEGRADED", d.Key, "FAIL", map[string]any{"speed_gt": d.SpeedGT, "width": d.Width}, map[string]any{"speed_gt": n.SpeedGT, "width": n.Width})
			}
			if *n.SpeedGT > *n.MaxSpeedGT || *n.Width > *n.MaxWidth {
				add("lnk_degraded", "TOPOLOGY_LINK_INCONSISTENT", d.Key, "BLOCKED", "state within capability", n)
			}
		}
	}
	if len(bound) != len(e.Devices) {
		add("pcie_misplaced", "TOPOLOGY_COMPONENT_SCOPE_MISMATCH", "bindings", "BLOCKED", len(e.Devices), len(bound))
	}
	if len(devices) != len(e.Devices) {
		add("pcie_misplaced", "TOPOLOGY_DEVICE_COUNT_MISMATCH", "devices", "FAIL", len(e.Devices), len(devices))
	}
	pairs := map[string]Affinity{}
	for _, a := range s.Affinity {
		a.Computed = PCIeAffinity(nodes[byLabel[a.From].BDF], nodes[byLabel[a.To].BDF], nodes)
		pairs[a.From+":"+a.To] = a
		if a.From != a.To && !strings.HasPrefix(a.Observed, "NV") {
			if a.Computed == "unknown" || a.Computed == "" {
				add("pcie_misplaced", "TOPOLOGY_AFFINITY_UNRESOLVED", a.From+":"+a.To, "BLOCKED", a.Observed, a.Computed)
			} else if a.Computed != a.Observed {
				add("pcie_misplaced", "TOPOLOGY_AFFINITY_MISMATCH", a.From+":"+a.To, "FAIL", a.Observed, a.Computed)
			}
		}
	}
	for _, a := range s.Devices {
		for _, b := range s.Devices {
			if a.Kind != "gpu" && b.Kind != "gpu" {
				continue
			}
			if _, ok := pairs[a.Label+":"+b.Label]; !ok {
				add("pcie_misplaced", "TOPOLOGY_AFFINITY_OBSERVATION_REQUIRED", a.Label+":"+b.Label, "BLOCKED", "nvidia-smi topo -m cell", nil)
			}
		}
	}
	active := map[string]int{}
	observed := map[string]bool{}
	for _, l := range s.NVLinks {
		observed[l.GPU] = true
		if l.State == "active" {
			active[l.GPU]++
		} else if l.State == "unknown" {
			add("nvlink_missing", "TOPOLOGY_NVLINK_STATE_UNKNOWN", l.GPU+":"+strconv.Itoa(l.Index), "BLOCKED", "active or inactive", nil)
		}
	}
	minimum := map[string]int{}
	for _, n := range e.NVLinks {
		a, b := labels[n.From], labels[n.To]
		if a == "" || b == "" {
			continue
		}
		p, ok := pairs[a+":"+b]
		if !ok {
			add("nvlink_missing", "TOPOLOGY_NVLINK_MATRIX_REQUIRED", n.From+":"+n.To, "BLOCKED", n.Links, nil)
			continue
		}
		links := 0
		if strings.HasPrefix(p.Observed, "NV") {
			links, _ = strconv.Atoi(strings.TrimPrefix(p.Observed, "NV"))
		}
		if links != n.Links {
			add("nvlink_missing", "TOPOLOGY_NVLINK_MATRIX_MISMATCH", n.From+":"+n.To, "FAIL", n.Links, links)
		}
		if n.Links > minimum[a] {
			minimum[a] = n.Links
		}
	}
	for label, count := range minimum {
		if !observed[label] {
			add("nvlink_missing", "TOPOLOGY_NVLINK_STATUS_REQUIRED", label, "BLOCKED", count, nil)
		} else if active[label] < count {
			add("nvlink_missing", "TOPOLOGY_NVLINK_INACTIVE", label, "FAIL", count, active[label])
		}
	}
	sort.Slice(out.Diffs, func(i, j int) bool {
		a, _ := json.Marshal(out.Diffs[i])
		b, _ := json.Marshal(out.Diffs[j])
		return string(a) < string(b)
	})
	return out
}
func equalPath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func regexpID(v string) bool {
	if len(v) != 32 {
		return false
	}
	for _, c := range v {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
