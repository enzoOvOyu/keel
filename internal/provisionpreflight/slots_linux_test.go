//go:build linux

package provisionpreflight

import (
	"strings"
	"testing"
)

func TestLinuxSlotsGroupFunctionsAndBridgeChildren(t *testing.T) {
	f := collectorFixture()
	missing := []string{}
	slots := collectSlots(f, func(v string) { missing = append(missing, v) })
	if len(missing) > 0 || physicalSlot(f, "0000:01:00.0", slots) != "slot-1" || physicalSlot(f, "0000:01:00.1", slots) != "slot-1" {
		t.Fatalf("wrong grouping %v %v", slots, missing)
	}
	f.links["/sys/bus/pci/devices/0000:04:00.0"] = "/sys/devices/pci0000:00/0000:00:01.0/0000:01:00.0/0000:04:00.0"
	if got := physicalSlot(f, "0000:04:00.0", slots); got != "slot-1" {
		t.Fatalf("bridge card was split: %s", got)
	}
	// A physical slot really named 0 is valid; a missing slot is not inferred 0.
	f.directories["/sys/bus/pci/slots"] = []string{"0"}
	f.files["/sys/bus/pci/slots/0/address"] = "0000:01:00"
	slots = collectSlots(f, func(string) {})
	if physicalSlot(f, "0000:01:00.0", slots) != "0" || physicalSlot(f, "0000:03:00.0", slots) != "" {
		t.Fatal("numeric zero or missing-slot semantics changed")
	}
}
func TestLinuxSlotsNeverGuessFromPlaceholderOrAmbiguousAddress(t *testing.T) {
	for _, scenario := range []string{"placeholder", "duplicate", "unsafe-name", "invalid-address", "extended-domain"} {
		t.Run(scenario, func(t *testing.T) {
			f := collectorFixture()
			switch scenario {
			case "placeholder":
				f.files["/sys/bus/pci/slots/slot-1/address"] = "0000:01"
			case "duplicate":
				f.directories["/sys/bus/pci/slots"] = append(f.directories["/sys/bus/pci/slots"], "other")
				f.files["/sys/bus/pci/slots/other/address"] = "0000:01:00"
			case "unsafe-name":
				f.directories["/sys/bus/pci/slots"] = []string{"../outside"}
			case "invalid-address":
				f.files["/sys/bus/pci/slots/slot-1/address"] = "0000:01:ff"
			case "extended-domain":
				f.directories["/sys/bus/pci/slots"] = []string{"large"}
				f.files["/sys/bus/pci/slots/large/address"] = "10000:01:00"
			}
			slots := collectSlots(f, func(string) {})
			if scenario == "extended-domain" {
				if physicalSlot(f, "10000:01:00.0", slots) != "large" {
					t.Fatal("valid extended domain not supported")
				}
			} else if physicalSlot(f, "0000:01:00.0", slots) != "" {
				t.Fatal("guessed a physical card identity")
			}
		})
	}
}
func TestPCIAddressCanonicalBounds(t *testing.T) {
	for _, address := range []string{"0000:00:00.0", "ffff:ff:1f.7", "10000:01:00.0", "7fffffff:00:00.0"} {
		if !validPCIAddress(address) {
			t.Fatal("valid Linux BDF rejected", address)
		}
	}
	for _, address := range []string{"0000:00:20.0", "0000:00:00.8", "00000:01:00.0", "80000000:01:00.0", "0000:00:00.0/../device", strings.Repeat("f", 40)} {
		if validPCIAddress(address) {
			t.Fatal("invalid BDF accepted", address)
		}
	}
}
