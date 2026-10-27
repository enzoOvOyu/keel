//go:build linux

package pcitopology

import (
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CollectSysfs reads a fixed root only. It runs no subprocess and exposes no
// caller-controlled filesystem path. Unsupported attributes remain nil.
func CollectSysfs(ctx context.Context) ([]Node, []string) {
	return collectSysfs(ctx, "/sys/bus/pci/devices")
}
func collectSysfs(ctx context.Context, root string) ([]Node, []string) {
	out := []Node{}
	missing := []string{}
	entries, e := os.ReadDir(root)
	if e != nil || len(entries) > MaxNodes {
		return out, []string{"pci_sysfs_unavailable_or_limit"}
	}
	sockets := map[int]*int{}
	for _, entry := range entries {
		if ctx.Err() != nil {
			missing = append(missing, "pci_sysfs_deadline")
			break
		}
		bdf := CanonicalBDF(entry.Name())
		if bdf == "" {
			continue
		}
		path, e := filepath.EvalSymlinks(filepath.Join(root, bdf))
		if e != nil {
			missing = append(missing, "pci_path_unavailable:"+bdf)
			continue
		}
		n := Node{BDF: bdf, Upstream: []string{}, Class: attribute(path, "class"), PhysicalSlot: attribute(path, "physical_slot")}
		parts := strings.Split(filepath.ToSlash(path), "/")
		for _, p := range parts {
			if rootPattern.MatchString(p) {
				n.Root = p
			}
			if p != "" && CanonicalBDF(p) == p && p != bdf {
				n.Upstream = append(n.Upstream, p)
			}
		}
		if n.Root == "" || len(n.Upstream) > 32 {
			missing = append(missing, "pci_root_unavailable:"+bdf)
			continue
		}
		if v, e := strconv.Atoi(attribute(path, "numa_node")); e == nil && v >= 0 && v <= 4095 {
			n.NUMA = &v
			if socket, ok := sockets[v]; ok {
				n.Socket = socket
			} else {
				n.Socket = nodeSocket(ctx, v)
				sockets[v] = n.Socket
			}
		}
		n.MaxSpeedGT = speedAttribute(path, "max_link_speed")
		n.SpeedGT = speedAttribute(path, "current_link_speed")
		n.MaxWidth = widthAttribute(path, "max_link_width")
		n.Width = widthAttribute(path, "current_link_width")
		n.PCIeType = pcieType(path)
		// Linux exposes a physical slot on a hotplug upstream bridge on some
		// systems. Carry only a directly reported slot from the nearest ancestor.
		if n.PhysicalSlot == "" {
			for parent := filepath.Dir(path); parent != filepath.Dir(parent) && CanonicalBDF(filepath.Base(parent)) != ""; parent = filepath.Dir(parent) {
				if slot := attribute(parent, "physical_slot"); slot != "" {
					n.PhysicalSlot = slot
					break
				}
			}
		}
		if len(n.PhysicalSlot) > 64 {
			n.PhysicalSlot = ""
		}
		out = append(out, n)
		if len(missing) > 255 {
			missing = append(missing[:255], "pci_observation_limit")
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].BDF < out[j].BDF })
	return out, missing
}

// Socket membership must be observed, not inferred from NUMA numbering (SNC
// can expose several NUMA nodes in one socket). Every CPU in the node agrees.
func nodeSocket(ctx context.Context, node int) *int {
	text := attribute("/sys/devices/system/node/node"+strconv.Itoa(node), "cpulist")
	if text == "" {
		return nil
	}
	cpus := []int{}
	for _, part := range strings.Split(text, ",") {
		lo, hi, rangeOK := strings.Cut(part, "-")
		first, e := strconv.Atoi(lo)
		if e != nil || first < 0 || first > 65535 {
			return nil
		}
		last := first
		if rangeOK {
			last, e = strconv.Atoi(hi)
			if e != nil || last < first || last > 65535 {
				return nil
			}
		}
		if len(cpus)+last-first+1 > 4096 {
			return nil
		}
		for cpu := first; cpu <= last; cpu++ {
			cpus = append(cpus, cpu)
		}
	}
	socket := -1
	for _, cpu := range cpus {
		if ctx.Err() != nil {
			return nil
		}
		value, e := strconv.Atoi(attribute("/sys/devices/system/cpu/cpu"+strconv.Itoa(cpu)+"/topology", "physical_package_id"))
		if e != nil || value < 0 || value > 4095 {
			return nil
		}
		if socket >= 0 && value != socket {
			return nil
		}
		socket = value
	}
	if socket < 0 {
		return nil
	}
	return &socket
}
func attribute(path, name string) string {
	f, e := os.Open(filepath.Join(path, name))
	if e != nil {
		return ""
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 257))
	if e != nil || len(b) > 256 {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func speedAttribute(path, name string) *float64 {
	f := strings.Fields(attribute(path, name))
	if len(f) < 2 || f[1] != "GT/s" {
		return nil
	}
	n, e := strconv.ParseFloat(f[0], 64)
	if e != nil || !validSpeed(n) {
		return nil
	}
	return &n
}
func widthAttribute(path, name string) *int {
	n, e := strconv.Atoi(attribute(path, name))
	if e != nil || !validWidth(n) {
		return nil
	}
	return &n
}
func pcieType(path string) string {
	f, e := os.Open(filepath.Join(path, "config"))
	if e != nil {
		return "unknown"
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 256))
	if e != nil || len(b) < 64 {
		return "unknown"
	}
	if binary.LittleEndian.Uint16(b[6:8])&0x10 == 0 {
		return "unknown"
	}
	pos := int(b[0x34] & 0xfc)
	seen := map[int]bool{}
	for i := 0; i < 48 && pos != 0; i++ {
		if pos < 0x40 || pos+4 > len(b) || seen[pos] {
			return "unknown"
		}
		seen[pos] = true
		if b[pos] == 0x10 {
			switch (binary.LittleEndian.Uint16(b[pos+2:pos+4]) >> 4) & 0xf {
			case 0:
				return "endpoint"
			case 1:
				return "legacy_endpoint"
			case 4:
				return "root_port"
			case 5:
				return "switch_upstream"
			case 6:
				return "switch_downstream"
			case 7:
				return "pcie_pci_bridge"
			case 8:
				return "pci_pcie_bridge"
			case 9:
				return "root_endpoint"
			default:
				return "unknown"
			}
		}
		pos = int(b[pos+1] & 0xfc)
	}
	return "unknown"
}
