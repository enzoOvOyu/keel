package platform

import (
	"regexp"
	"sort"
	"time"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

const mappingRuleVersion = "keel.mapping/v1"

var mappingName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,62}$`)

type MappingPortPlan struct {
	PhysicalPortID  string `json:"physical_port_id"`
	ComponentID     string `json:"component_id"`
	ComponentSerial string `json:"component_serial"`
	PortIndex       string `json:"port_index"`
	Name            string `json:"name"`
	Role            string `json:"role"`
	Rail            string `json:"rail"`
}
type MappingGPU struct {
	ComponentID string `json:"component_id"`
	UUID        string `json:"uuid"`
	BDF         string `json:"bdf"`
	ModelKey    string `json:"model_key"`
	Affinity    string `json:"affinity"`
}
type MappingBinding struct {
	Plan             MappingPortPlan     `json:"plan"`
	LogicalName      string              `json:"logical_name"`
	Observed         wiring.ExpectedPort `json:"observed"`
	SwitchPortID     string              `json:"switch_port_id"`
	ConfirmationID   string              `json:"confirmation_id"`
	GPUs             []MappingGPU        `json:"gpus"`
	GPUApplicability string              `json:"gpu_applicability"`
}
type MappingConflict struct {
	Code           string `json:"code"`
	PhysicalPortID string `json:"physical_port_id"`
	ComponentID    string `json:"component_id"`
}
type MappingCoverage struct {
	RequiredPorts int `json:"required_ports"`
	MappedPorts   int `json:"mapped_ports"`
	RequiredNICs  int `json:"required_nics"`
	MappedNICs    int `json:"mapped_nics"`
	RequiredGPUs  int `json:"required_gpus"`
	MappedGPUs    int `json:"mapped_gpus"`
}
type MappingExcludedPort struct {
	Plan   MappingPortPlan `json:"plan"`
	Reason string          `json:"reason"`
}
type MappingCandidate struct {
	ExcludedPorts    []MappingExcludedPort `json:"excluded_ports,omitempty"`
	RuleVersion      string                `json:"rule_version"`
	ProjectID        string                `json:"project_id"`
	AssetID          string                `json:"asset_id"`
	MachineID        string                `json:"machine_id"`
	AssetVersion     int64                 `json:"asset_version"`
	ModelVersionID   string                `json:"model_version_id"`
	ModelContentHash string                `json:"model_content_hash"`
	BOMPackageID     string                `json:"bom_package_id"`
	TopologyID       string                `json:"topology_id"`
	G1ID             string                `json:"g1_id"`
	RolePlanVersion  string                `json:"role_plan_version"`
	ScopeHash        string                `json:"scope_hash"`
	BaseScopeHash    string                `json:"base_scope_hash,omitempty"`
	CorrectionID     string                `json:"correction_id,omitempty"`
	Ready            bool                  `json:"ready"`
	Verdict          string                `json:"verdict"`
	Conflicts        []MappingConflict     `json:"conflicts"`
	Coverage         MappingCoverage       `json:"coverage"`
	Bindings         []MappingBinding      `json:"bindings"`
	ValidUntil       *time.Time            `json:"valid_until"`
}

func (c *MappingCandidate) block(code, port, component string) {
	c.Ready = false
	c.Verdict = "BLOCKED"
	c.Conflicts = append(c.Conflicts, MappingConflict{Code: code, PhysicalPortID: port, ComponentID: component})
}
func (c *MappingCandidate) seal() {
	sort.Slice(c.ExcludedPorts, func(i, j int) bool {
		return c.ExcludedPorts[i].Plan.PhysicalPortID < c.ExcludedPorts[j].Plan.PhysicalPortID
	})
	sort.Slice(c.Bindings, func(i, j int) bool { return c.Bindings[i].Plan.PhysicalPortID < c.Bindings[j].Plan.PhysicalPortID })
	sort.Slice(c.Conflicts, func(i, j int) bool {
		a, b := c.Conflicts[i], c.Conflicts[j]
		if a.PhysicalPortID != b.PhysicalPortID {
			return a.PhysicalPortID < b.PhysicalPortID
		}
		if a.ComponentID != b.ComponentID {
			return a.ComponentID < b.ComponentID
		}
		return a.Code < b.Code
	})
	c.ScopeHash = ""
	base := c.BaseScopeHash
	c.BaseScopeHash = "" // Derived provenance is excluded from its own hash.
	c.ScopeHash = digest(string(networkJSON(c)))
	c.BaseScopeHash = base
}

// G2 supplies observed endpoints; the BOM supplies the complete enabled port
// denominator and names. GPU associations come ONLY from independent model
// relations, never current BDF ordering or proximity to a NIC.
func buildMappingBindings(out *MappingCandidate, plans []MappingPortPlan, components []PrecollectComponent, relations *TopologyRelationTable, confirmed map[string]MappingBinding) {
	byGPU := map[string]bool{}
	for _, component := range components {
		if component.Category == "gpu" {
			byGPU[component.ID] = false
		}
	}
	nics, mappedNICs := map[string]bool{}, map[string]bool{}
	seenPorts, names, interfaces, macs, peers := map[string]bool{}, map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{}
	for _, plan := range plans {
		if plan.Role == "unused" {
			out.ExcludedPorts = append(out.ExcludedPorts, MappingExcludedPort{Plan: plan, Reason: "not_applicable_bom_unused"})
			continue
		}
		out.Coverage.RequiredPorts++
		nics[plan.ComponentID] = true
		if seenPorts[plan.PhysicalPortID] {
			out.block("MAPPING_DUPLICATE_PHYSICAL_PORT", plan.PhysicalPortID, plan.ComponentID)
			continue
		}
		seenPorts[plan.PhysicalPortID] = true
		if plan.Role != "compute" && plan.Role != "storage" && plan.Role != "management" && plan.Role != "oob" {
			out.block("MAPPING_ROLE_POLICY_REQUIRED", plan.PhysicalPortID, plan.ComponentID)
			continue
		}
		binding, ok := confirmed[plan.PhysicalPortID]
		if !ok || binding.Observed.PhysicalPortID != plan.PhysicalPortID || binding.Observed.ComponentID != plan.ComponentID || binding.Observed.PortIndex != plan.PortIndex || binding.Observed.Rail != plan.Rail || binding.SwitchPortID == "" || binding.ConfirmationID == "" {
			out.block("MAPPING_CURRENT_G2_PORT_REQUIRED", plan.PhysicalPortID, plan.ComponentID)
			continue
		}
		binding.Plan = plan
		binding.LogicalName = plan.Name
		if binding.LogicalName == "" {
			label := plan.Rail
			if label == "" {
				label = plan.Role
			}
			binding.LogicalName = "nic-" + label + "-p" + plan.PortIndex
		}
		if !mappingName.MatchString(binding.LogicalName) {
			out.block("MAPPING_BOM_NAME_INVALID", plan.PhysicalPortID, plan.ComponentID)
		}
		unique := func(index map[string]string, value, code string) {
			if value == "" {
				out.block(code, plan.PhysicalPortID, plan.ComponentID)
				return
			}
			if original, exists := index[value]; exists && original != plan.PhysicalPortID {
				out.block(code, plan.PhysicalPortID, plan.ComponentID)
			}
			index[value] = plan.PhysicalPortID
		}
		unique(names, binding.LogicalName, "MAPPING_LOGICAL_NAME_CONFLICT")
		unique(interfaces, binding.Observed.Interface, "MAPPING_OS_INTERFACE_CONFLICT")
		unique(macs, binding.Observed.PermanentMAC, "MAPPING_PERMANENT_MAC_CONFLICT")
		unique(peers, binding.SwitchPortID, "MAPPING_SWITCH_PORT_CONFLICT")
		binding.GPUs = []MappingGPU{}
		seenGPU := map[string]bool{}
		if relations != nil {
			for _, row := range relations.Rows {
				if row.PhysicalPortID != plan.PhysicalPortID || row.NICComponentID != plan.ComponentID {
					continue
				}
				if row.Verdict != "PASS" || row.NICNode == nil || row.GPUNode == nil || row.GPUComponentID == "" || row.GPUUUID == "" || row.NICNode.BDF != binding.Observed.BDF || row.Interface != binding.Observed.Interface || row.PermanentMAC != binding.Observed.PermanentMAC || row.ExpectedAffinity != row.ObservedAffinity || row.ExpectedAffinity != row.ComputedAffinity {
					out.block("MAPPING_INDEPENDENT_GPU_NIC_RELATION_REQUIRED", plan.PhysicalPortID, plan.ComponentID)
					continue
				}
				if _, exists := byGPU[row.GPUComponentID]; !exists {
					out.block("MAPPING_GPU_OUTSIDE_BOM", plan.PhysicalPortID, row.GPUComponentID)
					continue
				}
				if seenGPU[row.GPUComponentID] {
					out.block("MAPPING_DUPLICATE_GPU_RELATION", plan.PhysicalPortID, row.GPUComponentID)
					continue
				}
				seenGPU[row.GPUComponentID] = true
				byGPU[row.GPUComponentID] = true
				binding.GPUs = append(binding.GPUs, MappingGPU{ComponentID: row.GPUComponentID, UUID: row.GPUUUID, BDF: row.GPUNode.BDF, ModelKey: row.GPUKey, Affinity: row.ExpectedAffinity})
			}
		}
		binding.GPUApplicability = "not_applicable_non_compute_role"
		if len(binding.GPUs) > 0 {
			binding.GPUApplicability = "applicable"
		}
		if plan.Role == "compute" && len(binding.GPUs) == 0 {
			out.block("MAPPING_COMPUTE_GPU_COVERAGE_REQUIRED", plan.PhysicalPortID, plan.ComponentID)
		}
		sort.Slice(binding.GPUs, func(i, j int) bool { return binding.GPUs[i].ComponentID < binding.GPUs[j].ComponentID })
		out.Bindings = append(out.Bindings, binding)
		out.Coverage.MappedPorts++
		mappedNICs[plan.ComponentID] = true
	}
	out.Coverage.RequiredNICs = len(nics)
	out.Coverage.MappedNICs = len(mappedNICs)
	out.Coverage.RequiredGPUs = len(byGPU)
	for id, covered := range byGPU {
		if covered {
			out.Coverage.MappedGPUs++
		} else {
			out.block("MAPPING_GPU_COVERAGE_REQUIRED", "", id)
		}
	}
	if out.Coverage.RequiredPorts == 0 || out.Coverage.MappedPorts != out.Coverage.RequiredPorts || out.Coverage.RequiredNICs != out.Coverage.MappedNICs {
		out.block("MAPPING_ENABLED_SCOPE_INCOMPLETE", "", "")
	}
}
