package precollect

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"
)

type ExpectedPort struct {
	ID          string `json:"physical_port_id"`
	ComponentID string `json:"component_id"`
	MAC         string `json:"permanent_mac"`
	PortIndex   string `json:"port_index"`
}
type Expectation struct {
	Serial   string         `json:"serial"`
	Arch     string         `json:"arch"`
	GPUCount *int           `json:"gpu_count"`
	NICCount *int           `json:"nic_count"`
	Ports    []ExpectedPort `json:"ports"`
}
type Finding struct {
	Code    string `json:"code"`
	Field   string `json:"field"`
	Target  string `json:"target"`
	Verdict string `json:"verdict"`
}
type PortObservation struct {
	PhysicalPortID string `json:"physical_port_id"`
	ComponentID    string `json:"component_id"`
	PermanentMAC   string `json:"permanent_mac"`
	Interface      string `json:"interface"`
	BDF            string `json:"bdf"`
}
type Evaluation struct {
	RuleVersion         string            `json:"rule_version"`
	Verdict             string            `json:"verdict"`
	Required            int               `json:"required"`
	Present             int               `json:"present"`
	CompletenessPercent float64           `json:"completeness_percent"`
	Findings            []Finding         `json:"findings"`
	Ports               []PortObservation `json:"ports"`
}

// Field completeness, independent expectations and identity are distinct. A
// simulated complete report never produces real GateEvidence PASS.
func Evaluate(r *Report, expected Expectation, bmcObserved bool, now time.Time, ttl time.Duration) Evaluation {
	out := Evaluation{RuleVersion: RuleVersion, Verdict: "PASS", Findings: []Finding{}, Ports: []PortObservation{}}
	add := func(code, field, target, verdict string) {
		out.Findings = append(out.Findings, Finding{code, field, target, verdict})
		if verdict == "FAIL" {
			out.Verdict = "FAIL"
		} else if out.Verdict == "PASS" {
			out.Verdict = "BLOCKED"
		}
	}
	require := func(ok bool, field, target string) {
		out.Required++
		if ok {
			out.Present++
		} else {
			add("PRECOLLECT_FIELD_MISSING", field, target, "BLOCKED")
		}
	}
	if r == nil {
		add("PRECOLLECT_EVIDENCE_REQUIRED", "report", "", "BLOCKED")
		return out
	}
	if r.Origin != "real" {
		add("PRECOLLECT_SIMULATION_ONLY", "source", "", "BLOCKED")
	}
	if ttl <= 0 || r.CollectedAt.IsZero() || r.CollectedAt.After(now) || now.Sub(r.CollectedAt) >= ttl {
		add("PRECOLLECT_EVIDENCE_STALE", "collected_at", "", "BLOCKED")
	}
	require(r.Serial != "", "system.serial", "")
	if r.Serial != "" && r.Serial != expected.Serial {
		add("PRECOLLECT_SERIAL_MISMATCH", "system.serial", "", "FAIL")
	}
	require(r.Arch == "amd64" || r.Arch == "arm64", "system.arch", "")
	if expected.Arch != "amd64" && expected.Arch != "arm64" {
		add("PRECOLLECT_MODEL_ARCH_REQUIRED", "model.arch", "", "BLOCKED")
	} else if r.Arch != expected.Arch {
		add("PRECOLLECT_ARCH_MISMATCH", "system.arch", "", "FAIL")
	}
	require(strings.TrimSpace(r.OSRelease) != "", "system.os_release", "")
	require(bmcObserved, "system.bmc_address", "")
	require(r.ConfigUnchanged, "system.configuration_unchanged", "")
	require(r.PCITopology != "", "system.pci_topology", "")
	if expected.GPUCount == nil || expected.NICCount == nil {
		add("PRECOLLECT_MODEL_EXPECTATION_REQUIRED", "model", "", "BLOCKED")
	}
	if expected.GPUCount != nil && len(r.GPUs) != *expected.GPUCount {
		add("PRECOLLECT_GPU_COUNT_MISMATCH", "gpus", "", "FAIL")
	}
	if expected.GPUCount != nil && *expected.GPUCount > 0 {
		require(r.GPUTopology != "", "gpu.topology", "")
		require(r.NVLinkStatus != "", "gpu.nvlink", "")
	}
	if expected.GPUCount != nil && *expected.GPUCount >= 0 && *expected.GPUCount <= 256 {
		for i := len(r.GPUs); i < *expected.GPUCount; i++ {
			for _, field := range []string{"gpu.uuid", "gpu.bdf", "gpu.hbm_capacity", "gpu.hbm_ecc_observation", "gpu.driver"} {
				require(false, field, strconv.Itoa(i))
			}
		}
	}
	for _, g := range r.GPUs {
		require(g.Present && gpuUUID.MatchString(g.UUID), "gpu.uuid", g.BDF)
		require(g.BDF != "" && CanonicalBDF(g.BDF) == g.BDF, "gpu.bdf", g.UUID)
		require(g.MemoryMiB != nil && *g.MemoryMiB > 0, "gpu.hbm_capacity", g.UUID)
		require(g.UncorrectedECC != nil, "gpu.hbm_ecc_observation", g.UUID)
		require(g.DriverVersion != "" && g.DriverVersion != "N/A", "gpu.driver", g.UUID)
	}
	expectedMACs := map[string]ExpectedPort{}
	components := map[string]bool{}
	for _, p := range expected.Ports {
		if !ID.MatchString(p.ID) || !ID.MatchString(p.ComponentID) || p.MAC == "" || CanonicalMAC(p.MAC) != p.MAC || expectedMACs[p.MAC].ID != "" {
			add("PRECOLLECT_BOM_IDENTITY_INVALID", "bom.port", p.ID, "BLOCKED")
			continue
		}
		expectedMACs[p.MAC] = p
		components[p.ComponentID] = true
	}
	if len(expectedMACs) == 0 {
		add("PRECOLLECT_BOM_PORTS_REQUIRED", "bom.ports", "", "BLOCKED")
	}
	if expected.NICCount != nil && len(components) != *expected.NICCount {
		add("PRECOLLECT_BOM_MODEL_NIC_MISMATCH", "bom.components", "", "BLOCKED")
	}
	seen := map[string]bool{}
	physicalBDF := map[string]bool{}
	for _, n := range r.NICs {
		if !n.VirtualFunction {
			physicalBDF[n.BDF] = true
		}
	}
	for _, n := range r.NICs {
		if n.VirtualFunction {
			require(iface.MatchString(n.Name), "nic.vf_interface", n.Name)
			require(n.BDF != "" && CanonicalBDF(n.BDF) == n.BDF, "nic.vf_bdf", n.Name)
			require(n.PFBDF != "" && physicalBDF[n.PFBDF], "nic.vf_parent", n.Name)
			continue
		}
		p, matched := expectedMACs[n.PermanentMAC]
		if !matched || seen[n.PermanentMAC] {
			add("PRECOLLECT_MAC_BOM_MISMATCH", "nic.permanent_mac", n.Name, "FAIL")
		}
		seen[n.PermanentMAC] = true
		require(n.PermanentMAC != "", "nic.permanent_mac", n.Name)
		require(n.BDF != "" && CanonicalBDF(n.BDF) == n.BDF, "nic.bdf", n.Name)
		require(iface.MatchString(n.Name), "nic.interface", n.Name)
		require(n.Link == "yes" || n.Link == "no", "nic.link", n.Name)
		require(n.SpeedMbps != nil, "nic.speed", n.Name)
		require(n.Driver != "", "nic.driver", n.Name)
		require(len(n.RDMALinks) > 0, "nic.rdma_links", n.Name)
		require(len(r.RDMADevices) > 0, "nic.rdma_devices", n.Name)
		require(n.DOMState == "ready" && n.ModulePresent != nil && n.TemperatureC != nil && n.RxPowerDBm != nil && n.TxPowerDBm != nil, "nic.dom", n.Name)
		if matched {
			out.Ports = append(out.Ports, PortObservation{p.ID, p.ComponentID, n.PermanentMAC, n.Name, n.BDF})
		}
	}
	for mac, p := range expectedMACs {
		require(seen[mac], "bom.permanent_mac", p.ID)
		if !seen[mac] {
			for _, field := range []string{"nic.permanent_mac", "nic.bdf", "nic.interface", "nic.link", "nic.speed", "nic.driver", "nic.rdma_links", "nic.rdma_devices", "nic.dom"} {
				require(false, field, p.ID)
			}
		}
	}
	for _, code := range r.Missing {
		add(code, "collector", "", "BLOCKED")
	}
	if out.Required > 0 {
		out.CompletenessPercent = float64(out.Present) * 100 / float64(out.Required)
	}
	sort.Slice(out.Findings, func(i, j int) bool {
		a, _ := json.Marshal(out.Findings[i])
		b, _ := json.Marshal(out.Findings[j])
		return string(a) < string(b)
	})
	sort.Slice(out.Ports, func(i, j int) bool { return out.Ports[i].PhysicalPortID < out.Ports[j].PhysicalPortID })
	return out
}
