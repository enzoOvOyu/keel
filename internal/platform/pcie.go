package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// PCIe topology verification (WP-2.12, FR-4.8, mock channel). The caller
// submits the collected per-device state (BDF, upstream chain, NUMA,
// LnkCap/LnkSta) plus the nvidia-smi raw text and the expected topology
// declared by the approved model version — the expectation never comes from
// the machine's own output. The judge walks four dimensions: slot path
// ownership (each device's chain root must match the model's pcie_slot),
// NUMA layout (all GPUs must sit on their expected NUMA node group),
// NVLink connectivity (expected pairs present in the raw matrix) and link
// width (LnkSta must reach LnkCap). Diffs are T7 rows; two failed targeted
// repairs set the escalation flag instead of any automatic RMA.

type PcieDevice struct {
	Role        string `json:"role"` // gpu / nic
	Index       int    `json:"index"`
	BDF         string `json:"bdf"`
	Upstream    []string `json:"upstream"` // BDF chain to the root complex
	NUMANode    int    `json:"numa_node"`
	LnkCapWidth int    `json:"lnk_cap_width"`
	LnkStaWidth int    `json:"lnk_sta_width"`
	Slot        string `json:"slot"` // observed slot (e.g. the chain root position)
}

type PcieExpected struct {
	SlotByRole map[string]string `json:"slot_by_role"`   // "gpu:0" -> pcie_slot
	GpuNumaGroup []int          `json:"gpu_numa_group"`  // expected NUMA nodes for GPUs
	NVLinkPairs []string       `json:"nvlink_pairs"`    // "0-1","0-3",... expected NVLink pairs
}

type SubmitPcieInput struct {
	MachineID string       `json:"machine_id"`
	Devices   []PcieDevice `json:"devices"`
	Expected  PcieExpected `json:"expected"`
	NVLinkRaw string       `json:"nvlink_raw"`
}

type PcieDiff struct {
	Dimension  string    `json:"dimension"`
	Detail     string    `json:"detail"`
}

type SubmitPcieResult struct {
	MachineID  string     `json:"machine_id"`
	Revision   int       `json:"revision"`
	Diffs      []PcieDiff `json:"diffs"`
	Escalated  bool      `json:"escalated"`
	RepairCount int      `json:"repair_count"`
}

// SubmitPcieSnapshot stores a versioned snapshot and runs the FR-4.8.2
// judge, recording T7 diffs; a clean snapshot resolves prior diffs and
// resets the repair counter.
func (s *Store) SubmitPcieSnapshot(ctx context.Context, a *AuthSession, project *ProjectSummary, input SubmitPcieInput, trace string) (*SubmitPcieResult, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	// Authority first: an unauthorized caller learns nothing about the
	// validation rules.
	if e := requireProjectAdmin(ctx, tx, a, project.ID); e != nil {
		return nil, e
	}
	if input.MachineID == "" || len(input.Devices) == 0 || len(input.Devices) > 64 {
		return nil, apiError(400, "VALIDATION_ERROR", "machine_id and 1-64 devices are required")
	}
	var hostname string
	if e = tx.QueryRowContext(ctx, "SELECT hostname FROM project_assets WHERE project_id=$1 AND machine_id=$2", project.ID, input.MachineID).Scan(&hostname); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(409, "VALIDATION_ERROR", "machine not bound to this project")
		}
		return nil, e
	}
	_ = hostname
	var revision int
	if e = tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(revision),0)+1 FROM pcie_snapshots WHERE machine_id=$1", input.MachineID).Scan(&revision); e != nil {
		return nil, e
	}
	devicesJSON, e := json.Marshal(input.Devices)
	if e != nil {
		return nil, e
	}
	expectedJSON, e := json.Marshal(input.Expected)
	if e != nil {
		return nil, e
	}
	snapshotID := RandomID()
	now := time.Now().UTC()
	if _, e = tx.ExecContext(ctx, `INSERT INTO pcie_snapshots(id,project_id,machine_id,revision,devices,expected,nvlink_raw,collected_by,collected_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		snapshotID, project.ID, input.MachineID, revision, string(devicesJSON), string(expectedJSON), input.NVLinkRaw, a.User.ID, now); e != nil {
		return nil, e
	}
	result := SubmitPcieResult{MachineID: input.MachineID, Revision: revision, Diffs: []PcieDiff{}}
	// Dimension 1: slot path ownership — each device's chain root must match
	// the model-declared slot for its role.
	for _, device := range input.Devices {
		key := device.Role + ":" + itoa(device.Index)
		expectedSlot, ok := input.Expected.SlotByRole[key]
		if !ok {
			continue // no declared expectation for this device: not judged
		}
		observedRoot := ""
		if n := len(device.Upstream); n > 0 {
			observedRoot = device.Upstream[n-1]
		}
		if observedRoot != expectedSlot {
			detail, _ := json.Marshal(map[string]any{"device": key, "expected_slot": expectedSlot, "observed_root": observedRoot})
			result.Diffs = append(result.Diffs, PcieDiff{Dimension: "slot_path", Detail: string(detail)})
		}
	}
	// Dimension 2: NUMA layout — every GPU must sit on a declared node.
	for _, device := range input.Devices {
		if device.Role != "gpu" {
			continue
		}
		allowed := false
		for _, node := range input.Expected.GpuNumaGroup {
			if device.NUMANode == node {
				allowed = true
				break
			}
		}
		if !allowed {
			detail, _ := json.Marshal(map[string]any{"device": "gpu:" + itoa(device.Index), "observed_numa": device.NUMANode, "expected_group": input.Expected.GpuNumaGroup})
			result.Diffs = append(result.Diffs, PcieDiff{Dimension: "numa", Detail: string(detail)})
		}
	}
	// Dimension 3: NVLink connectivity. nvlink_raw carries one line per GPU
	// (the nvidia-smi topo -m row); an expected pair i-j holds only when both
	// lines mention an NV marker (NV18/NV9/NV4/NV2/NV1).
	lines := splitLines(input.NVLinkRaw)
	for _, pair := range input.Expected.NVLinkPairs {
		i, j, ok := parsePair(pair)
		if !ok {
			continue
		}
		if !lineHasNV(lines, i) || !lineHasNV(lines, j) {
			detail, _ := json.Marshal(map[string]any{"expected_pair": pair, "note": "pair absent from nvidia-smi topo output"})
			result.Diffs = append(result.Diffs, PcieDiff{Dimension: "nvlink", Detail: string(detail)})
		}
	}
	// Dimension 4: link width — LnkSta must reach LnkCap.
	for _, device := range input.Devices {
		if device.LnkCapWidth > 0 && device.LnkStaWidth < device.LnkCapWidth {
			detail, _ := json.Marshal(map[string]any{"device": device.Role + ":" + itoa(device.Index), "lnk_cap": device.LnkCapWidth, "lnk_sta": device.LnkStaWidth})
			result.Diffs = append(result.Diffs, PcieDiff{Dimension: "link_width", Detail: string(detail)})
		}
	}
	// Persist diffs; clean snapshots resolve prior diffs and reset repairs.
	for _, diff := range result.Diffs {
		if _, e = tx.ExecContext(ctx, `INSERT INTO pcie_diffs(id,project_id,machine_id,snapshot_id,dimension,detail,created_at)
			VALUES($1,$2,$3,$4,$5,$6,$7)`,
			RandomID(), project.ID, input.MachineID, snapshotID, diff.Dimension, diff.Detail, now); e != nil {
			return nil, e
		}
	}
	var repairCount int
	if len(result.Diffs) == 0 {
		if _, e = tx.ExecContext(ctx, `UPDATE pcie_diffs SET resolved_at=$3 WHERE machine_id=$1 AND project_id=$2 AND resolved_at IS NULL`,
			input.MachineID, project.ID, now); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE pcie_diffs SET repair_count=0, escalated=false WHERE machine_id=$1 AND project_id=$2`,
			input.MachineID, project.ID); e != nil {
			return nil, e
		}
	} else {
		// A resubmission with diffs counts as one targeted repair attempt;
		// two consecutive failed attempts escalate (flag only, no auto-RMA).
		if e = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(repair_count),0)+1 FROM pcie_diffs WHERE machine_id=$1 AND project_id=$2 AND resolved_at IS NULL`,
			input.MachineID, project.ID).Scan(&repairCount); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE pcie_diffs SET repair_count=$3 WHERE machine_id=$1 AND project_id=$2 AND resolved_at IS NULL`,
			input.MachineID, project.ID, repairCount); e != nil {
			return nil, e
		}
		if repairCount >= 2 {
			if _, e = tx.ExecContext(ctx, `UPDATE pcie_diffs SET escalated=true WHERE machine_id=$1 AND project_id=$2 AND resolved_at IS NULL`,
				input.MachineID, project.ID); e != nil {
				return nil, e
			}
			result.Escalated = true
		}
		result.RepairCount = repairCount
	}
	if e = appendAudit(ctx, tx, a.User.ID, project.ID, "pcie.snapshot", input.MachineID, trace, map[string]any{
		"revision": revision, "diffs": len(result.Diffs),
	}); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &result, nil
}

func itoa(value int) string {
	return fmtInt(value)
}

func splitLines(text string) []string {
	out := []string{}
	current := ""
	for _, ch := range text {
		if ch == '\n' {
			out = append(out, current)
			current = ""
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

func lineHasNV(lines []string, gpu int) bool {
	if gpu < 0 || gpu >= len(lines) {
		return false
	}
	line := lines[gpu]
	for _, marker := range []string{"NV18", "NV9", "NV8", "NV4", "NV2", "NV1"} {
		if containsSubstr(line, marker) {
			return true
		}
	}
	return false
}

func parsePair(pair string) (int, int, bool) {
	parts := [2]string{}
	seen := 0
	current := ""
	for _, ch := range pair {
		if ch == '-' {
			if seen >= 1 {
				return 0, 0, false
			}
			parts[seen] = current
			seen++
			current = ""
		} else if ch >= '0' && ch <= '9' {
			current += string(ch)
		} else {
			return 0, 0, false
		}
	}
	if seen != 1 || current == "" || parts[0] == "" {
		return 0, 0, false
	}
	return atoiSmall(parts[0]), atoiSmall(current), true
}

func atoiSmall(text string) int {
	value := 0
	for _, ch := range text {
		if ch < '0' || ch > '9' {
			return -1
		}
		value = value*10 + int(ch-'0')
	}
	return value
}

func containsSubstr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return len(needle) == 0
}

func fmtInt(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	if negative {
		return "-" + string(digits)
	}
	return string(digits)
}

// PcieDiffView is one topology difference row.
type PcieDiffView struct {
	ID          string     `json:"id"`
	Dimension   string     `json:"dimension"`
	Detail      string     `json:"detail"`
	CreatedAt   time.Time  `json:"created_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	RepairCount int        `json:"repair_count"`
	Escalated   bool       `json:"escalated"`
}

// ListPcieDiffs returns the machine's topology diff history, newest first.
func (s *Store) ListPcieDiffs(ctx context.Context, a *AuthSession, project *ProjectSummary, machineID string, p Pagination) (Page[PcieDiffView], error) {
	out := NewPage[PcieDiffView](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e := requireProjectMember(ctx, tx, a, project.ID); e != nil {
		return out, e
	}
	var bound bool
	if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM project_assets WHERE project_id=$1 AND machine_id=$2)", project.ID, machineID).Scan(&bound); e != nil {
		return out, e
	}
	if !bound {
		return out, apiError(409, "VALIDATION_ERROR", "machine not bound to this project")
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM pcie_diffs WHERE project_id=$1 AND machine_id=$2", project.ID, machineID).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,dimension,detail,created_at,resolved_at,repair_count,escalated
		FROM pcie_diffs WHERE project_id=$1 AND machine_id=$2 ORDER BY created_at DESC LIMIT $3 OFFSET $4`, project.ID, machineID, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	defer rows.Close()
	for rows.Next() {
		var diff PcieDiffView
		if e = rows.Scan(&diff.ID, &diff.Dimension, &diff.Detail, &diff.CreatedAt, &diff.ResolvedAt, &diff.RepairCount, &diff.Escalated); e != nil {
			return out, e
		}
		out.Items = append(out.Items, diff)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.Items == nil {
		out.Items = []PcieDiffView{}
	}
	return out, nil
}
