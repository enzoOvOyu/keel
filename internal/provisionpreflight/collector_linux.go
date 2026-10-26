//go:build linux

package provisionpreflight

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

var hexIdentity = regexp.MustCompile(`^0x[0-9a-f]{4}$`)

type hostSource interface {
	read(string, int64) (string, error)
	names(string, int) ([]string, error)
	exists(string) (bool, error)
	link(string) (string, error)
	health(context.Context, string) Disk
}
type Collector struct {
	source hostSource
	arch   string
}

func NewCollector() *Collector { return &Collector{source: linuxSource{}, arch: runtime.GOARCH} }

// Collect only reads sysfs/DMI and invokes smartctl's read-only health command.
// It does not enable SMART, start tests, change firmware, mount or erase disks.
// The approved initrd must include the necessary drivers and smartctl binary.
func (c *Collector) Collect(parent context.Context) Observation {
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	out := Observation{Source: Source, StartedAt: time.Now().UTC(), Arch: c.arch, PCI: []PCI{}, Ports: []Port{}, Disks: []Disk{}, Missing: []string{}}
	missing := func(reason string) {
		if len(out.Missing) < 128 {
			out.Missing = append(out.Missing, reason)
		}
	}
	source := c.source
	serial, err := source.read("/sys/class/dmi/id/product_serial", 256)
	if err != nil || serial == "" {
		serial, err = source.read("/sys/firmware/devicetree/base/serial-number", 256)
	}
	if err == nil {
		out.Serial = strings.TrimSpace(strings.TrimRight(serial, "\x00"))
	}
	if out.Serial == "" {
		missing("system_serial_unavailable")
	}
	if out.Arch != "amd64" && out.Arch != "arm64" {
		out.Arch = ""
		missing("architecture_unsupported")
	}
	names, err := source.names("/sys/bus/pci/devices", 4096)
	if err != nil {
		missing("pci_inventory_unavailable")
	}
	pci := map[string]PCI{}
	for _, name := range names {
		if ctx.Err() != nil {
			missing("collection_deadline")
			break
		}
		if !pciAddress.MatchString(name) {
			continue
		}
		base := "/sys/bus/pci/devices/" + name
		class, e := source.read(base+"/class", 64)
		if e != nil || !classCode.MatchString(class) {
			missing("pci_class_unavailable:" + name)
			continue
		}
		vendor, e1 := source.read(base+"/vendor", 64)
		device, e2 := source.read(base+"/device", 64)
		if e1 != nil || e2 != nil || !hexIdentity.MatchString(vendor) || !hexIdentity.MatchString(device) {
			missing("pci_identity_unavailable:" + name)
			continue
		}
		vf, e := source.exists(base + "/physfn")
		if e != nil {
			missing("pci_function_identity_unavailable:" + name)
			continue
		}
		slot, _ := source.read(base+"/physical_slot", 64)
		if strings.ContainsAny(slot, "\r\n\x00") {
			slot = ""
		}
		record := PCI{Address: name, Class: class, Identity: vendor[2:] + ":" + device[2:], PhysicalSlot: slot, VirtualFunction: vf}
		if !vf && strings.HasPrefix(class, "0x02") {
			bound, driverErr := source.exists(base + "/driver")
			if driverErr != nil || !bound {
				missing("physical_nic_driver_unavailable:" + name)
			}
		}
		out.PCI = append(out.PCI, record)
		pci[name] = record
	}
	interfaces, err := source.names("/sys/class/net", 2048)
	if err != nil {
		missing("network_inventory_unavailable")
	}
	for _, name := range interfaces {
		if !netName.MatchString(name) {
			continue
		}
		path, e := source.link("/sys/class/net/" + name + "/device")
		if e != nil {
			continue
		} // Loopback/bonds/VLANs do not establish physical NIC identity.
		address := filepath.Base(path)
		device, ok := pci[address]
		if !ok || device.VirtualFunction || !strings.HasPrefix(device.Class, "0x02") {
			continue
		}
		raw, e := source.read("/sys/class/net/"+name+"/address", 64)
		mac, parseErr := net.ParseMAC(raw)
		if e != nil || parseErr != nil || len(mac) != 6 || mac[0]&1 != 0 || mac.String() == "00:00:00:00:00:00" {
			missing("physical_mac_unavailable:" + name)
			continue
		}
		out.Ports = append(out.Ports, Port{Name: name, MAC: mac.String(), PCI: address})
	}
	devices, err := source.names("/sys/block", 128)
	if err != nil {
		missing("disk_inventory_unavailable")
	}
	for _, name := range devices {
		if !diskName.MatchString(name) {
			if !strings.HasPrefix(name, "loop") && !strings.HasPrefix(name, "ram") && !strings.HasPrefix(name, "zram") && !strings.HasPrefix(name, "dm-") && !strings.HasPrefix(name, "md") {
				missing("unsupported_disk_health_source:" + name)
			}
			continue
		}
		if ctx.Err() != nil {
			missing("collection_deadline")
			break
		}
		out.Disks = append(out.Disks, source.health(ctx, name))
	}
	if ctx.Err() != nil {
		missing("collection_deadline")
	}
	sort.Slice(out.PCI, func(i, j int) bool { return out.PCI[i].Address < out.PCI[j].Address })
	sort.Slice(out.Ports, func(i, j int) bool { return out.Ports[i].Name < out.Ports[j].Name })
	sort.Slice(out.Disks, func(i, j int) bool { return out.Disks[i].Device < out.Disks[j].Device })
	out.FinishedAt = time.Now().UTC()
	return out
}

type linuxSource struct{}

func (linuxSource) read(path string, limit int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return "", ErrInvalid
	}
	return strings.TrimSpace(string(raw)), nil
}
func (linuxSource) names(path string, limit int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(limit + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(names) > limit {
		return nil, fmt.Errorf("hardware directory limit exceeded")
	}
	sort.Strings(names)
	return names, nil
}
func (linuxSource) exists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
func (linuxSource) link(path string) (string, error) { return filepath.EvalSymlinks(path) }
