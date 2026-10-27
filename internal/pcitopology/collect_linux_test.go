//go:build linux

package pcitopology

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestSysfsPathAndReadOnlyConfigurationCapability(t *testing.T) {
	root := t.TempDir()
	bus := filepath.Join(root, "bus")
	if e := os.MkdirAll(bus, 0700); e != nil {
		t.Fatal(e)
	}
	base := filepath.Join(root, "devices", "pci0000:00", "0000:00:01.0")
	end := filepath.Join(base, "0000:01:00.0")
	for _, dir := range []string{base, end} {
		if e := os.MkdirAll(dir, 0700); e != nil {
			t.Fatal(e)
		}
	}
	for name, dir := range map[string]string{"0000:00:01.0": base, "0000:01:00.0": end} {
		if e := os.Symlink(dir, filepath.Join(bus, name)); e != nil {
			t.Fatal(e)
		}
	}
	for name, text := range map[string]string{"numa_node": "0", "physical_slot": "PCI-E1", "max_link_speed": "32.0 GT/s PCIe", "current_link_speed": "16.0 GT/s PCIe", "max_link_width": "16", "current_link_width": "8", "class": "0x030200"} {
		if e := os.WriteFile(filepath.Join(end, name), []byte(text), 0600); e != nil {
			t.Fatal(e)
		}
	}
	config := make([]byte, 256)
	binary.LittleEndian.PutUint16(config[6:8], 0x10)
	config[0x34] = 0x40
	config[0x40] = 0x10
	binary.LittleEndian.PutUint16(config[0x42:0x44], 4<<4)
	if e := os.WriteFile(filepath.Join(base, "config"), config, 0600); e != nil {
		t.Fatal(e)
	}
	nodes, missing := collectSysfs(context.Background(), bus)
	if len(missing) != 0 || len(nodes) != 2 {
		t.Fatalf("%+v %+v", nodes, missing)
	}
	if nodes[0].PCIeType != "root_port" || len(nodes[1].Upstream) != 1 || nodes[1].Upstream[0] != nodes[0].BDF || nodes[1].NUMA == nil || *nodes[1].Width != 8 || *nodes[1].SpeedGT != 16 {
		t.Fatalf("%+v", nodes)
	}
	// A cyclic capability list is unknown, never a synthesized endpoint.
	config[0x40] = 1
	config[0x41] = 0x40
	if e := os.WriteFile(filepath.Join(base, "config"), config, 0600); e != nil {
		t.Fatal(e)
	}
	if got := pcieType(base); got != "unknown" {
		t.Fatal(got)
	}
}
