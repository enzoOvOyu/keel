//go:build linux

package provisionpreflight

import (
	"path/filepath"
	"strings"
)

// Linux exposes physical PCI slots as /sys/bus/pci/slots/<name>/address,
// not as a generic devices/<BDF>/physical_slot attribute. Placeholder addresses
// identify only a bus and cannot prove the physical card count.
func collectSlots(source hostSource, missing func(string)) map[string]string {
	out := map[string]string{}
	names, err := source.names("/sys/bus/pci/slots", 1024)
	if err != nil {
		missing("physical_slot_inventory_unavailable")
		return out
	}
	for _, name := range names {
		if !physicalSlotName.MatchString(name) || name == "." || name == ".." {
			missing("physical_slot_name_invalid")
			continue
		}
		address, err := source.read("/sys/bus/pci/slots/"+name+"/address", 64)
		if err != nil {
			missing("physical_slot_address_unavailable:" + name)
			continue
		}
		if strings.Count(address, ":") == 1 && validPCIAddress(address+":00.0") {
			continue
		}
		if !validPCIAddress(address + ".0") {
			missing("physical_slot_address_invalid:" + name)
			continue
		}
		if previous, exists := out[address]; exists && previous != name {
			out[address] = ""
			missing("physical_slot_address_ambiguous:" + address)
		} else {
			out[address] = name
		}
	}
	return out
}
func physicalSlot(source hostSource, address string, slots map[string]string) string {
	if !validPCIAddress(address) {
		return ""
	}
	if slot, found := slots[address[:len(address)-2]]; found {
		return slot
	}
	path, err := source.link("/sys/bus/pci/devices/" + address)
	if err != nil {
		return ""
	}
	// A card may contain a PCIe bridge and multiple network controllers. The
	// nearest explicit ancestor slot groups that card; a BDF alone is no slot.
	parts := strings.Split(filepath.Clean(path), "/")
	for i := len(parts) - 2; i >= 0; i-- {
		ancestor := parts[i]
		if validPCIAddress(ancestor) {
			if slot, found := slots[ancestor[:len(ancestor)-2]]; found {
				return slot
			}
		}
	}
	return ""
}
