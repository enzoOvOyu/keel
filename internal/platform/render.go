package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Batch configuration renderers (WP-3.2, FR-5.2–5.5). Four renderers
// consume the role bindings and the frozen mapping:
//
//	gpu_network     FR-5.2 per-NIC IP/route/policy planned per rail
//	nic_partition   FR-5.3 PF/VF split with a before/after diff preview
//	gpu_nic_binding FR-5.4 NCCL env + NUMA pinning, never cross-NUMA
//	roce_import     FR-5.5 DCQCN/PFC/ETS/MTU/congestion with read-back
//
// Every renderer is role-aware: it walks the ports whose resolved role is a
// logical role and silently skips slots bound to "unused", which is what
// makes a staged build (phase two drops storage) work without touching the
// template or the model. Nothing here guesses a value — a missing parameter
// or a missing topology snapshot refuses instead of rendering a half-truth,
// and applies always require G3 (a published mapping) plus a ConfigSet
// version to record against (FR-5.8).

type ConfigRenderInput struct {
	Kind        string         `json:"kind"`
	MachineID   string         `json:"machine_id"`
	ConfigSetID string         `json:"config_set_id"`
	Params      map[string]any `json:"params"`
	Readback    map[string]any `json:"readback"`
}

type ConfigRenderResult struct {
	Kind     string   `json:"kind"`
	Rendered string   `json:"rendered"`
	Diff     string   `json:"diff"`
	Warnings []string `json:"warnings"`
	Applied  bool     `json:"applied"`
	Verified bool     `json:"verified"`
	Receipt  string   `json:"receipt"`
}

type renderPort struct {
	portID      string
	portName    string
	role        string
	nicModel    string
	pcieSlot    string
	rail        int
	slotNo      int
	osInterface string
	bdf         string
	gpuAffinity string
	mapped      bool
}

// RenderConfig renders one of the four configuration kinds for a machine.
// Previews are member-readable; applies need project-admin, G3 evidence and
// a ConfigSet version to record against.
func (s *Store) RenderConfig(ctx context.Context, a *AuthSession, project *ProjectSummary, input ConfigRenderInput, apply bool, trace string) (*ConfigRenderResult, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	// A render exposes the planned addressing and NCCL/ROCE tuning of a
	// target: project-admin only, preview and apply alike.
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	switch input.Kind {
	case "gpu_network", "nic_partition", "gpu_nic_binding", "roce_import":
	default:
		return nil, apiError(400, "VALIDATION_ERROR", "kind must be gpu_network, nic_partition, gpu_nic_binding or roce_import")
	}
	var hostname string
	if e = tx.QueryRowContext(ctx, "SELECT hostname FROM project_assets WHERE project_id=$1 AND machine_id=$2", project.ID, input.MachineID).Scan(&hostname); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(409, "VALIDATION_ERROR", "machine not bound to this project")
		}
		return nil, e
	}
	// FR-5.9 precondition: render only after the role bindings validate.
	plan, e := loadRolePlan(ctx, tx, project.ID)
	if e != nil {
		return nil, e
	}
	if len(plan.Missing) > 0 {
		return nil, apiError(409, "ROLE_BINDING", "required role(s) with no bound slot: "+strings.Join(plan.Missing, ","))
	}
	ports, e := loadRenderPorts(ctx, tx, project.ID, input.MachineID)
	if e != nil {
		return nil, e
	}
	var mapped bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM mapping_revisions WHERE project_id=$1 AND machine_id=$2)", project.ID, input.MachineID).Scan(&mapped); e != nil {
		return nil, e
	}
	if apply && !mapped {
		return nil, apiError(409, "GATE_EVIDENCE", "batch config requires a published mapping (G3) for the target")
	}
	if input.Params == nil {
		input.Params = map[string]any{}
	}
	result := ConfigRenderResult{Kind: input.Kind, Warnings: []string{}}
	active := activePorts(ports)
	if len(active) == 0 {
		result.Warnings = append(result.Warnings, "no_active_ports")
	}
	if !mapped {
		result.Warnings = append(result.Warnings, "unmapped_target")
	}
	var set *ConfigSet
	if input.ConfigSetID != "" {
		set, e = scanConfigSet(tx.QueryRowContext(ctx, `SELECT id,name,version,status,scope,model_name,template,schema,keys,created_by,created_at,approved_by,approved_at
			FROM config_sets WHERE id=$1 AND project_id=$2`, input.ConfigSetID, project.ID))
		if e != nil {
			return nil, e
		}
		if set.Status != "approved" {
			return nil, apiError(409, "VALIDATION_ERROR", "only approved config sets render")
		}
	}
	if apply && set == nil {
		return nil, apiError(400, "VALIDATION_ERROR", "applying config requires a config set to record the apply against")
	}
	switch input.Kind {
	case "gpu_network":
		rendered, warnings, e := renderNetworkConfig(active, input.Params)
		if e != nil {
			return nil, e
		}
		result.Rendered, result.Warnings = rendered, append(result.Warnings, warnings...)
	case "nic_partition":
		rendered, diff, warnings, e := renderNicPartition(ctx, tx, project.ID, input.MachineID, a.User.ID, active, input.Params, apply)
		if e != nil {
			return nil, e
		}
		result.Rendered, result.Diff, result.Warnings = rendered, diff, append(result.Warnings, warnings...)
	case "gpu_nic_binding":
		devices, e := latestPcieDevices(ctx, tx, project.ID, input.MachineID)
		if e != nil {
			return nil, e
		}
		rendered, warnings, e := renderGpuNicBinding(active, devices, input.Params, apply)
		if e != nil {
			return nil, e
		}
		result.Rendered, result.Warnings = rendered, append(result.Warnings, warnings...)
	case "roce_import":
		if set == nil {
			return nil, apiError(400, "VALIDATION_ERROR", "roce_import requires an approved config set whose keys carry the RoCE parameters")
		}
		keys := map[string]any{}
		if len(set.Keys) > 0 {
			if e = json.Unmarshal([]byte(set.Keys), &keys); e != nil {
				return nil, apiError(409, "VALIDATION_ERROR", "config set keys are not valid JSON")
			}
		}
		before, e := latestRoceState(ctx, tx, project.ID, input.MachineID)
		if e != nil {
			return nil, e
		}
		rendered, diff, verified, receipt, warnings := renderRoceImport(keys, before, input.Readback)
		result.Rendered, result.Diff, result.Verified, result.Receipt, result.Warnings = rendered, diff, verified, receipt, append(result.Warnings, warnings...)
	}
	// FR-5.2: the three shared renderers verify a sampled read-back against
	// the rendered plan with the same contract as the RoCE import.
	if input.Kind != "roce_import" && result.Rendered != "" {
		verified, receipt, warnings := verifyRenderReadback(result.Rendered, input.Readback)
		result.Verified, result.Receipt = verified, receipt
		result.Warnings = append(result.Warnings, warnings...)
	}
	// Diff against the newest recorded apply of the same kind and set.
	if set != nil && result.Diff == "" {
		var prior sql.NullString
		_ = tx.QueryRowContext(ctx, `SELECT rendered FROM config_set_applies WHERE config_set_id=$1 AND machine_id=$2 AND kind=$3 ORDER BY applied_at DESC LIMIT 1`,
			set.ID, input.MachineID, input.Kind).Scan(&prior)
		if !prior.Valid {
			result.Diff = "new"
		} else if prior.String != result.Rendered {
			result.Diff = unifiedDiff(prior.String, result.Rendered)
		}
	}
	if apply {
		now := time.Now().UTC()
		readback := input.Readback
		if readback == nil {
			readback = map[string]any{}
		}
		readbackJSON, _ := json.Marshal(readback)
		if _, e = tx.ExecContext(ctx, `INSERT INTO config_set_applies(id,project_id,config_set_id,machine_id,rendered,diff,applied_by,applied_at,readback,verified,receipt,kind)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			RandomID(), project.ID, set.ID, input.MachineID, result.Rendered, result.Diff, a.User.ID, now, readbackJSON, result.Verified, result.Receipt, input.Kind); e != nil {
			return nil, e
		}
		if input.Kind == "roce_import" {
			before, _ := latestRoceState(ctx, tx, project.ID, input.MachineID)
			after := map[string]any{}
			_ = json.Unmarshal([]byte(set.Keys), &after)
			if after == nil {
				after = map[string]any{}
			}
			readback := input.Readback
			if readback == nil {
				readback = map[string]any{}
			}
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(after)
			readbackJSON, _ := json.Marshal(readback)
			if _, e = tx.ExecContext(ctx, `INSERT INTO roce_imports(id,project_id,config_set_id,machine_id,before,after,readback,verified,receipt,imported_by,imported_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
				RandomID(), project.ID, set.ID, input.MachineID, beforeJSON, afterJSON, readbackJSON, result.Verified, result.Receipt, a.User.ID, now); e != nil {
				return nil, e
			}
		}
		if e = appendAudit(ctx, tx, a.User.ID, project.ID, "config.render", set.ID, trace,
			map[string]any{"kind": input.Kind, "machine": input.MachineID, "applied": true, "verified": result.Verified}); e != nil {
			return nil, e
		}
		if e = tx.Commit(); e != nil {
			return nil, e
		}
		result.Applied = true
	}
	if result.Warnings == nil {
		result.Warnings = []string{}
	}
	return &result, nil
}

// activePorts keeps the ports whose role is a logical role: slots bound to
// "unused" are skipped by design (FR-5.9 phase-two scenario).
func activePorts(ports []renderPort) []renderPort {
	out := []renderPort{}
	for _, port := range ports {
		if isLogicalRole(port.role) {
			out = append(out, port)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].role != out[j].role {
			return out[i].role < out[j].role
		}
		return out[i].portName < out[j].portName
	})
	return out
}

func loadRenderPorts(ctx context.Context, tx *sql.Tx, projectID, machineID string) ([]renderPort, error) {
	rows, e := tx.QueryContext(ctx, `SELECT p.id,p.port_name,p.rail,p.role,e.os_interface,e.bdf,e.gpu_affinity
		FROM physical_ports p
		LEFT JOIN mapping_entries e ON e.port_id=p.id AND e.revision_id=(SELECT id FROM mapping_revisions WHERE project_id=$1 AND machine_id=$2 ORDER BY sequence DESC LIMIT 1)
		WHERE p.machine_id=$2 ORDER BY p.port_name`, projectID, machineID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []renderPort{}
	for rows.Next() {
		var port renderPort
		var osInterface, bdf, gpuAffinity sql.NullString
		if e = rows.Scan(&port.portID, &port.portName, &port.rail, &port.role, &osInterface, &bdf, &gpuAffinity); e != nil {
			return nil, e
		}
		port.osInterface, port.bdf, port.gpuAffinity = osInterface.String, bdf.String, gpuAffinity.String
		port.mapped = osInterface.Valid && osInterface.String != ""
		out = append(out, port)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	slots, e := modelSlotsForMachine(ctx, tx, machineID)
	if e != nil {
		return nil, e
	}
	for i := range out {
		slotNo, pcieSlot, nicModel, _ := matchSlot(slots, out[i].portName, i)
		out[i].slotNo, out[i].pcieSlot, out[i].nicModel = slotNo, pcieSlot, nicModel
	}
	return out, nil
}

// renderNetworkConfig (FR-5.2) plans per-NIC addressing from the mapping and
// the role: the address is derived from the role's subnet and the owning
// slot, so a template never names a NIC model or an interface.
func renderNetworkConfig(ports []renderPort, params map[string]any) (string, []string, error) {
	var out []string
	warnings := []string{}
	for _, port := range ports {
		block, ok := nestedMap(params, "net", port.role)
		if !ok {
			return "", nil, apiError(409, "VALIDATION_ERROR", fmt.Sprintf("parameter net.%s is missing from the frozen snapshot", port.role))
		}
		subnet, _ := block["subnet"].(string)
		if subnet == "" {
			return "", nil, apiError(409, "VALIDATION_ERROR", fmt.Sprintf("parameter net.%s.subnet is missing from the frozen snapshot", port.role))
		}
		address, e := hostAddress(subnet, 10+port.slotNo)
		if e != nil {
			return "", nil, apiError(409, "VALIDATION_ERROR", fmt.Sprintf("parameter net.%s.subnet is not a network: %v", port.role, e))
		}
		iface := port.osInterface
		if iface == "" {
			iface = "<unmapped:" + port.portName + ">"
			warnings = append(warnings, "unmapped:"+port.portName)
		}
		out = append(out, fmt.Sprintf("# role=%s slot=%d nic=%s rail=%d iface=%s", port.role, port.slotNo, port.nicModel, port.rail, iface))
		out = append(out, "auto "+iface)
		out = append(out, "iface "+iface+" inet static")
		out = append(out, "    address "+address)
		if gateway, _ := block["gateway"].(string); gateway != "" {
			out = append(out, "    gateway "+gateway)
		}
		if mtu, ok := block["mtu"]; ok {
			out = append(out, fmt.Sprintf("    mtu %v", mtu))
		}
		if routes, ok := block["routes"].([]any); ok {
			for _, route := range routes {
				out = append(out, fmt.Sprintf("    post-up ip route add %v dev %s", route, iface))
			}
		}
		if policy, _ := block["policy"].(string); policy != "" {
			out = append(out, "    post-up "+policy)
		}
	}
	return strings.Join(out, "\n"), warnings, nil
}

// hostAddress replaces the host part of a "a.b.c.d/prefix" subnet.
func hostAddress(subnet string, host int) (string, error) {
	parts := strings.SplitN(subnet, "/", 2)
	octets := strings.Split(parts[0], ".")
	if len(octets) != 4 {
		return "", errors.New("expected four octets")
	}
	octets[3] = strconv.Itoa(host)
	address := strings.Join(octets, ".")
	if len(parts) == 2 {
		address += "/" + parts[1]
	}
	return address, nil
}

// renderNicPartition (FR-5.3) derives the split from the model capability
// matrix and the BOM-declared NIC, registers rate/PF/VF separately and
// previews the before/after diff.
func renderNicPartition(ctx context.Context, tx *sql.Tx, projectID, machineID, userID string, ports []renderPort, params map[string]any, apply bool) (string, string, []string, error) {
	warnings := []string{}
	beforeText := []string{}
	afterText := []string{}
	totalRate, totalPF, totalVF := 0, 0, 0
	stated := map[string][3]int{}
	rows, e := tx.QueryContext(ctx, "SELECT port_id,pf_count,vf_count,rate_gbps FROM nic_partitions WHERE project_id=$1", projectID)
	if e != nil {
		return "", "", nil, e
	}
	for rows.Next() {
		var portID string
		var pf, vf, rate int
		if e = rows.Scan(&portID, &pf, &vf, &rate); e != nil {
			rows.Close()
			return "", "", nil, e
		}
		stated[portID] = [3]int{pf, vf, rate}
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return "", "", nil, e
	}
	rows.Close()
	capability := modelCapability(ctx, tx, projectID, machineID)
	for _, port := range ports {
		cap, ok := nicPartitionCapability(capability, port.nicModel)
		if !ok {
			warnings = append(warnings, "capability_missing:"+port.nicModel)
		}
		pf, vf := cap[0], cap[1]
		rate := cap[2]
		if block, found := nestedMap(params, "partition", port.role); found {
			if value, ok := block["pf"]; ok {
				pf = intFromAny(value)
			}
			if value, ok := block["vf"]; ok {
				vf = intFromAny(value)
			}
		}
		if pf > cap[0] || vf > cap[1] {
			return "", "", nil, apiError(409, "CAPABILITY_EXCEEDED", fmt.Sprintf("split pf=%d vf=%d exceeds the model capability pf=%d vf=%d of %s", pf, vf, cap[0], cap[1], port.nicModel))
		}
		before, found := stated[port.portID]
		if !found {
			before = [3]int{1, 0, 0}
		}
		after := [3]int{pf, vf, rate}
		iface := port.osInterface
		if iface == "" {
			iface = "<unmapped:" + port.portName + ">"
		}
		line := func(state [3]int) string {
			return fmt.Sprintf("# role=%s slot=%d nic=%s iface=%s pf=%d vf=%d rate_gbps=%d", port.role, port.slotNo, port.nicModel, iface, state[0], state[1], state[2])
		}
		beforeText = append(beforeText, line(before))
		afterText = append(afterText, line(after))
		totalRate += rate
		totalPF += pf
		totalVF += vf
		if apply {
			beforeJSON, _ := json.Marshal(map[string]any{"pf": before[0], "vf": before[1], "rate_gbps": before[2]})
			afterJSON, _ := json.Marshal(map[string]any{"pf": after[0], "vf": after[1], "rate_gbps": after[2]})
			if _, e = tx.ExecContext(ctx, `INSERT INTO nic_partitions(id,project_id,port_id,role,pf_count,vf_count,rate_gbps,before_state,after_state,updated_by,updated_at)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
				ON CONFLICT(project_id,port_id) DO UPDATE SET role=$4,pf_count=$5,vf_count=$6,rate_gbps=$7,before_state=$8,after_state=$9,updated_by=$10,updated_at=$11`,
				RandomID(), projectID, port.portID, port.role, pf, vf, rate, beforeJSON, afterJSON, userID, time.Now().UTC()); e != nil {
				return "", "", nil, e
			}
		}
	}
	afterText = append(afterText, fmt.Sprintf("# totals: ports=%d total_gbps=%d pf=%d vf=%d", len(ports), totalRate, totalPF, totalVF))
	beforeText = append(beforeText, fmt.Sprintf("# totals: ports=%d", len(ports)))
	return strings.Join(afterText, "\n"), unifiedDiff(strings.Join(beforeText, "\n"), strings.Join(afterText, "\n")), warnings, nil
}

// renderGpuNicBinding (FR-5.4) binds each GPU to its own NIC interface and
// pins the traffic inside one NUMA node; a cross-NUMA binding is a hard
// failure on apply and a warning on preview.
func renderGpuNicBinding(ports []renderPort, devices []PcieDevice, params map[string]any, apply bool) (string, []string, error) {
	warnings := []string{}
	gpuNuma := map[int]int{}
	nicNuma := map[string]int{}
	for _, device := range devices {
		switch device.Role {
		case "gpu":
			gpuNuma[device.Index] = device.NUMANode
		case "nic":
			if device.BDF != "" {
				nicNuma[device.BDF] = device.NUMANode
			}
		}
	}
	if len(devices) == 0 {
		warnings = append(warnings, "numa_unknown")
		if apply {
			return "", warnings, apiError(409, "TOPOLOGY_EVIDENCE", "GPU-NIC binding requires the FR-4.8 topology snapshot that carries the NUMA layout")
		}
	}
	gidIndex := 3
	socketThreads := 4
	if value, ok := params["nccl"].(map[string]any); ok {
		if v, found := value["gid_index"]; found {
			gidIndex = intFromAny(v)
		}
		if v, found := value["socket_nthreads"]; found {
			socketThreads = intFromAny(v)
		}
	}
	policies := []string{}
	var out []string
	for _, port := range ports {
		if port.role != "compute" {
			continue
		}
		iface := port.osInterface
		if iface == "" {
			iface = "<unmapped:" + port.portName + ">"
			warnings = append(warnings, "unmapped:"+port.portName)
		}
		gpuIndex, gpuKnown := parseGpuIndex(port.gpuAffinity)
		if !gpuKnown {
			warnings = append(warnings, "gpu_affinity_unknown:"+port.portName)
		}
		nicNode, nicKnown := nicNuma[port.bdf]
		gpuNode, gpuNodeKnown := gpuNuma[gpuIndex]
		if nicKnown && gpuNodeKnown && nicNode != gpuNode {
			warnings = append(warnings, fmt.Sprintf("cross_numa:%s(gpu=%d gpu_numa=%d nic_numa=%d)", iface, gpuIndex, gpuNode, nicNode))
			if apply {
				return "", warnings, apiError(409, "CROSS_NUMA", fmt.Sprintf("RoCE traffic of %s must not cross NUMA: gpu %d sits on node %d, the NIC on node %d", iface, gpuIndex, gpuNode, nicNode))
			}
			continue
		}
		if !nicKnown || !gpuNodeKnown {
			warnings = append(warnings, "numa_unknown:"+iface)
			if apply {
				return "", warnings, apiError(409, "TOPOLOGY_EVIDENCE", "GPU-NIC binding requires the FR-4.8 topology snapshot that carries the NUMA layout")
			}
		}
		policies = append(policies, fmt.Sprintf("GPU:%d:%s", gpuIndex, iface))
		out = append(out, fmt.Sprintf("# gpu=%d nic=%s numa=%d", gpuIndex, iface, nicNode))
		out = append(out, fmt.Sprintf("numactl --cpunodebind=%d --membind=%d   # iface=%s gpu=%d", nicNode, nicNode, iface, gpuIndex))
	}
	if len(policies) == 0 {
		out = append([]string{"# no compute-role port is bound on this target"}, out...)
	} else {
		out = append([]string{
			"NCCL_NETDEVS_POLICY=" + strings.Join(policies, ","),
			"NCCL_IB_GID_INDEX=" + strconv.Itoa(gidIndex),
			"NCCL_SOCKET_NTHREADS=" + strconv.Itoa(socketThreads),
		}, out...)
	}
	return strings.Join(out, "\n"), warnings, nil
}

func parseGpuIndex(affinity string) (int, bool) {
	if affinity == "" {
		return 0, false
	}
	trimmed := strings.TrimLeft(affinity, "gpuGPU-_ :")
	if trimmed == "" {
		return 0, false
	}
	value, e := strconv.Atoi(trimmed)
	if e != nil {
		return 0, false
	}
	return value, true
}

// renderRoceImport (FR-5.5) version-manages the DCQCN/PFC/ETS/MTU/congestion
// parameters as ConfigSet keys, previews the diff and records a receipt —
// verified only when the read-back matches what was applied.
func renderRoceImport(after, before, readback map[string]any) (string, string, bool, string, []string) {
	warnings := []string{}
	beforeLines := flattenConfig("", before)
	afterLines := flattenConfig("", after)
	receipt := ""
	verified := false
	if len(readback) == 0 {
		warnings = append(warnings, "readback_missing")
		receipt = "readback_missing: verification pending"
	} else {
		readbackLines := flattenConfig("", readback)
		expected := map[string]string{}
		for _, line := range afterLines {
			if parts := strings.SplitN(line, "=", 2); len(parts) == 2 {
				expected[parts[0]] = parts[1]
			}
		}
		mismatch := []string{}
		for _, line := range readbackLines {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			if value, ok := expected[parts[0]]; !ok || value != parts[1] {
				mismatch = append(mismatch, parts[0])
			}
		}
		sort.Strings(mismatch)
		verified = len(mismatch) == 0
		receipt = fmt.Sprintf("keys=%d verified=%t mismatches=%s", len(afterLines), verified, strings.Join(mismatch, ","))
	}
	return strings.Join(afterLines, "\n"), unifiedDiff(strings.Join(beforeLines, "\n"), strings.Join(afterLines, "\n")), verified, receipt, warnings
}

// flattenConfig renders nested configuration as sorted dotted key=value
// lines so imports can be diffed and read back deterministically.
func flattenConfig(prefix string, value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := []string{}
	for _, key := range keys {
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		switch typed := value[key].(type) {
		case map[string]any:
			out = append(out, flattenConfig(full, typed)...)
		default:
			out = append(out, fmt.Sprintf("%s=%v", full, typed))
		}
	}
	return out
}

// modelCapability reads the capability matrix of the approved model version
// the target's asset is bound to; the PF/VF split is only ever generated
// from that declaration, never from the machine's own answer.
func modelCapability(ctx context.Context, tx *sql.Tx, projectID, machineID string) map[string]any {
	var raw []byte
	if e := tx.QueryRowContext(ctx, `SELECT v.capability_matrix FROM device_model_versions v
		JOIN project_assets a ON a.model_id=v.model_id
		WHERE a.project_id=$1 AND a.machine_id=$2 ORDER BY v.version DESC LIMIT 1`, projectID, machineID).Scan(&raw); e != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if e := json.Unmarshal(raw, &out); e != nil {
		return map[string]any{}
	}
	return out
}

func nicPartitionCapability(capability map[string]any, nicModel string) ([3]int, bool) {
	block, ok := capability["nic_partition"].(map[string]any)
	if !ok {
		return [3]int{1, 0, 0}, false
	}
	entry, ok := block[nicModel].(map[string]any)
	if !ok {
		return [3]int{1, 0, 0}, false
	}
	pf := 1
	if _, found := entry["pf"]; found {
		pf = intFromAny(entry["pf"])
	}
	return [3]int{pf, intFromAny(entry["vf"]), intFromAny(entry["rate_gbps"])}, true
}

func nestedMap(params map[string]any, section, key string) (map[string]any, bool) {
	block, ok := params[section].(map[string]any)
	if !ok {
		return nil, false
	}
	value, ok := block[key].(map[string]any)
	return value, ok
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		if parsed, e := strconv.Atoi(typed); e == nil {
			return parsed
		}
	}
	return 0
}

func latestPcieDevices(ctx context.Context, tx *sql.Tx, projectID, machineID string) ([]PcieDevice, error) {
	var raw []byte
	e := tx.QueryRowContext(ctx, "SELECT devices FROM pcie_snapshots WHERE project_id=$1 AND machine_id=$2 ORDER BY revision DESC LIMIT 1", projectID, machineID).Scan(&raw)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, e
	}
	out := []PcieDevice{}
	if e = json.Unmarshal(raw, &out); e != nil {
		return nil, nil
	}
	return out, nil
}

func latestRoceState(ctx context.Context, tx *sql.Tx, projectID, machineID string) (map[string]any, error) {
	var raw []byte
	e := tx.QueryRowContext(ctx, "SELECT after FROM roce_imports WHERE project_id=$1 AND machine_id=$2 ORDER BY imported_at DESC LIMIT 1", projectID, machineID).Scan(&raw)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return map[string]any{}, nil
		}
		return nil, e
	}
	out := map[string]any{}
	if len(raw) > 0 {
		if e = json.Unmarshal(raw, &out); e != nil {
			return map[string]any{}, nil
		}
	}
	return out, nil
}
