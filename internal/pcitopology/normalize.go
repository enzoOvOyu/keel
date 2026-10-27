package pcitopology

import (
	"encoding/csv"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var gpuLabel = regexp.MustCompile(`^GPU[0-9]{1,3}$`)
var nicLabel = regexp.MustCompile(`^NIC[0-9]{1,3}$`)
var uuidPattern = regexp.MustCompile(`^GPU-[a-fA-F0-9-]{8,80}$`)
var nvToken = regexp.MustCompile(`^NV[1-9][0-9]{0,2}$`)

// Normalize adds vendor observations to fixed sysfs reads. NIC labels are
// resolved solely through NVIDIA's NIC Legend interface names, never BDF order.
// Unknown vendor formats are retained in the parent raw bundle and block proof.
func Normalize(nodes []Node, interfaces map[string]string, identity, matrix, nvlink string, missing []string) *Snapshot {
	s := &Snapshot{Version: Version, Nodes: nodes, Devices: []Device{}, Affinity: []Affinity{}, NVLinks: []NVLink{}, Missing: append([]string{}, missing...)}
	addMissing := func(code string) {
		if len(s.Missing) < 256 {
			s.Missing = append(s.Missing, code)
		}
	}
	nodeMap := map[string]Node{}
	for _, n := range nodes {
		nodeMap[n.BDF] = n
	}
	labels := map[string]Device{}
	reader := csv.NewReader(strings.NewReader(identity))
	reader.FieldsPerRecord = -1
	for {
		row, e := reader.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			addMissing("gpu_identity_format_unsupported")
			break
		}
		if len(row) != 3 && len(row) != 4 {
			addMissing("gpu_identity_format_unsupported")
			continue
		}
		index, e := strconv.Atoi(strings.TrimSpace(row[0]))
		bdf := CanonicalBDF(row[2])
		uuid := strings.TrimSpace(row[1])
		label := "GPU" + strconv.Itoa(index)
		if e != nil || index < 0 || index > 255 || !uuidPattern.MatchString(uuid) || bdf == "" || nodeMap[bdf].BDF == "" || labels[label].Label != "" {
			addMissing("gpu_identity_unresolved")
			continue
		}
		d := Device{Label: label, Kind: "gpu", BDF: bdf, Identity: uuid}
		if len(row) == 4 {
			serial := strings.TrimSpace(row[3])
			if serial != "N/A" && serial != "[N/A]" && serial != "Not Supported" && len(serial) <= 120 {
				d.Serial = serial
			}
		}
		labels[label] = d
	}
	if strings.TrimSpace(identity) == "" {
		addMissing("gpu_identity_unavailable")
	}
	lines := strings.Split(matrix, "\n")
	for _, line := range lines {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if !ok || !nicLabel.MatchString(key) {
			continue
		}
		bdf := interfaces[value]
		if bdf == "" || nodeMap[bdf].BDF == "" || labels[key].Label != "" {
			addMissing("nic_legend_unresolved:" + key)
			continue
		}
		labels[key] = Device{Label: key, Kind: "nic", BDF: bdf, Identity: value}
	}
	columns := []string{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(columns) == 0 {
			if !gpuLabel.MatchString(fields[0]) {
				continue
			}
			// Header has no row label, and ends with CPU/NUMA affinity headings.
			for _, f := range fields {
				if gpuLabel.MatchString(f) || nicLabel.MatchString(f) {
					columns = append(columns, f)
				} else {
					break
				}
			}
			continue
		}
		if !gpuLabel.MatchString(fields[0]) && !nicLabel.MatchString(fields[0]) {
			continue
		}
		from := labels[fields[0]]
		if from.Label == "" || len(fields) < len(columns)+1 {
			addMissing("affinity_row_unresolved:" + fields[0])
			continue
		}
		for i, toLabel := range columns {
			to := labels[toLabel]
			token := fields[i+1]
			if to.Label == "" {
				addMissing("affinity_identity_unresolved:" + toLabel)
				continue
			}
			if !validAffinity(token) {
				addMissing("affinity_token_unsupported:" + from.Label + ":" + toLabel)
				continue
			}
			computed := PCIeAffinity(nodeMap[from.BDF], nodeMap[to.BDF], nodeMap)
			s.Affinity = append(s.Affinity, Affinity{From: from.Label, To: toLabel, Observed: token, Computed: computed})
		}
	}
	if len(columns) == 0 {
		addMissing("gpu_affinity_unavailable")
	}
	current := ""
	for _, line := range strings.Split(nvlink, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "GPU ") {
			head, _, _ := strings.Cut(strings.TrimPrefix(line, "GPU "), ":")
			current = "GPU" + strings.TrimSpace(head)
			if labels[current].Kind != "gpu" {
				current = ""
				addMissing("nvlink_gpu_unresolved")
			}
			continue
		}
		if current == "" || !strings.HasPrefix(line, "Link ") {
			continue
		}
		idx, text, ok := strings.Cut(strings.TrimPrefix(line, "Link "), ":")
		n, e := strconv.Atoi(strings.TrimSpace(idx))
		if !ok || e != nil || n < 0 || n >= 128 {
			addMissing("nvlink_index_unresolved")
			continue
		}
		state := "unknown"
		text = strings.TrimSpace(text)
		if text == "<inactive>" || strings.EqualFold(text, "inactive") {
			state = "inactive"
		} else {
			parts := strings.Fields(text)
			if len(parts) == 2 && parts[1] == "GB/s" {
				if rate, e := strconv.ParseFloat(parts[0], 64); e == nil && rate > 0 && rate < 10000 {
					state = "active"
				}
			}
		}
		s.NVLinks = append(s.NVLinks, NVLink{GPU: current, Index: n, State: state})
	}
	// Include every independently observed physical NIC, even if NVIDIA omits
	// it. Missing Legend coverage blocks affinity rather than hiding a device.
	seen := map[string]bool{}
	for _, d := range labels {
		s.Devices = append(s.Devices, d)
		seen[d.BDF] = true
	}
	names := make([]string, 0, len(interfaces))
	for name := range interfaces {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		bdf := interfaces[name]
		if !seen[bdf] && nodeMap[bdf].BDF != "" {
			s.Devices = append(s.Devices, Device{Label: "IF:" + name, Kind: "nic", BDF: bdf, Identity: name})
			seen[bdf] = true
			addMissing("nic_affinity_unavailable:" + name)
		}
	}
	sort.Slice(s.Devices, func(i, j int) bool { return s.Devices[i].Label < s.Devices[j].Label })
	sort.Strings(s.Missing)
	return s
}
func validAffinity(v string) bool {
	switch v {
	case "X", "PIX", "PXB", "PHB", "NODE", "SYS":
		return true
	}
	if !nvToken.MatchString(v) {
		return false
	}
	n, _ := strconv.Atoi(strings.TrimPrefix(v, "NV"))
	return n <= 128
}
