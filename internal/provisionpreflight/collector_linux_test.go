//go:build linux

package provisionpreflight

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

type fixtureHost struct {
	files       map[string]string
	directories map[string][]string
	links       map[string]string
	existing    map[string]bool
	probes      []string
}

func (f *fixtureHost) read(path string, limit int64) (string, error) {
	v, ok := f.files[path]
	if !ok || int64(len(v)) > limit {
		return "", os.ErrNotExist
	}
	return v, nil
}
func (f *fixtureHost) names(path string, limit int) ([]string, error) {
	v, ok := f.directories[path]
	if !ok || len(v) > limit {
		return nil, os.ErrNotExist
	}
	return v, nil
}
func (f *fixtureHost) link(path string) (string, error) {
	v, ok := f.links[path]
	if !ok {
		return "", os.ErrNotExist
	}
	return v, nil
}
func (f *fixtureHost) exists(path string) (bool, error) { return f.existing[path], nil }
func (f *fixtureHost) health(_ context.Context, name string) Disk {
	f.probes = append(f.probes, name)
	yes := true
	return Disk{Device: "/dev/" + name, Healthy: &yes, Reason: "fixture-only"}
}
func collectorFixture() *fixtureHost {
	f := &fixtureHost{files: map[string]string{"/sys/class/dmi/id/product_serial": "fixture-server"}, directories: map[string][]string{"/sys/bus/pci/devices": {}, "/sys/class/net": {"eth0", "eth1", "vf0", "lo", "bond0"}, "/sys/block": {"loop0", "ram0", "sda", "nvme0n1", "dm-0"}}, links: map[string]string{}, existing: map[string]bool{}}
	for _, v := range []struct {
		address, class, identity, slot string
		vf                             bool
	}{{"0000:01:00.0", "0x020000", "15b3:1021", "slot-1", false}, {"0000:01:00.1", "0x020000", "15b3:1021", "slot-1", false}, {"0000:01:00.2", "0x020000", "15b3:1021", "", true}, {"0000:02:00.0", "0x030200", "10de:2901", "slot-2", false}, {"0000:03:00.0", "0x030000", "1a03:2000", "", false}} {
		base := "/sys/bus/pci/devices/" + v.address
		parts := strings.Split(v.identity, ":")
		f.directories["/sys/bus/pci/devices"] = append(f.directories["/sys/bus/pci/devices"], v.address)
		f.files[base+"/class"] = v.class
		f.files[base+"/vendor"] = "0x" + parts[0]
		f.files[base+"/device"] = "0x" + parts[1]
		f.files[base+"/physical_slot"] = v.slot
		f.existing[base+"/physfn"] = v.vf
		f.existing[base+"/driver"] = true
	}
	for i, name := range []string{"eth0", "eth1", "vf0"} {
		address := []string{"0000:01:00.0", "0000:01:00.1", "0000:01:00.2"}[i]
		f.links["/sys/class/net/"+name+"/device"] = "/sys/devices/pci0000:00/" + address
		f.files["/sys/class/net/"+name+"/address"] = []string{"02:00:00:00:00:01", "02:00:00:00:00:02", "02:00:00:00:00:03"}[i]
	}
	return f
}
func TestCollectorReadsPhysicalFactsOnly(t *testing.T) {
	f := collectorFixture()
	c := &Collector{source: f, arch: "amd64"}
	o := c.Collect(context.Background())
	s, _, _ := fixture()
	s.ExpiresAt = time.Now().Add(time.Minute)
	r, err := Evaluate(s, o, time.Now())
	if err != nil || r.Status != "PASS" {
		t.Fatalf("%+v %v", r, err)
	}
	if len(o.Ports) != 2 || len(o.Disks) != 2 || len(f.probes) != 2 {
		t.Fatalf("virtual interfaces or disks included: %+v probes=%v", o, f.probes)
	}
}
func TestCollectorMissingDriverAndUnsupportedDiskBlock(t *testing.T) {
	f := collectorFixture()
	f.existing["/sys/bus/pci/devices/0000:01:00.0/driver"] = false
	f.directories["/sys/block"] = []string{"sda", "mmcblk0"}
	o := (&Collector{source: f, arch: "amd64"}).Collect(context.Background())
	s, _, _ := fixture()
	s.ExpiresAt = time.Now().Add(time.Minute)
	r, err := Evaluate(s, o, time.Now())
	if err != nil || r.Status != "BLOCKED" {
		t.Fatalf("%+v %v", r, err)
	}
	if len(f.probes) != 1 || f.probes[0] != "sda" {
		t.Fatalf("unapproved probe: %v", f.probes)
	}
}
func TestCollectorARMSerialAndCanceledCollection(t *testing.T) {
	f := collectorFixture()
	delete(f.files, "/sys/class/dmi/id/product_serial")
	f.files["/sys/firmware/devicetree/base/serial-number"] = "arm-server\x00"
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := (&Collector{source: f, arch: "arm64"}).Collect(ctx)
	if o.Serial != "arm-server" || o.Arch != "arm64" || len(o.Missing) == 0 || len(f.probes) != 0 {
		t.Fatalf("%+v probes=%v", o, f.probes)
	}
}
