package bmc

import (
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
)

// Inventory navigation is narrower than the Redfish service tree. A malicious
// Members or feature link must never carry credentials to account/action/OEM
// resources, even when it stays on the configured BMC origin.
func RedfishInventoryPath(reference string) (string, error) {
	canonical, e := CanonicalPath("/redfish/v1/", reference)
	if e != nil {
		return "", e
	}
	u, e := url.Parse(canonical)
	if e != nil {
		return "", e
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 2 && parts[0] == "redfish" && parts[1] == "v1" {
		return canonical, nil
	}
	if len(parts) < 3 {
		return "", errors.New("inventory resource root required")
	}
	switch parts[2] {
	case "Systems", "Managers", "Chassis", "UpdateService":
	default:
		return "", errors.New("resource is outside the read-only inventory graph")
	}
	for _, part := range parts[3:] {
		switch strings.ToLower(part) {
		case "actions", "oem", "accountservice", "accounts", "roles", "sessionservice", "sessions", "eventservice", "subscriptions", "taskservice", "tasks", "logservices", "entries", "certificates", "certificateservice", "settings":
			return "", errors.New("non-inventory Redfish resource is forbidden")
		}
	}
	return canonical, nil
}

var inventoryTypes = map[string][]string{
	"ServiceRoot":        {"ServiceRoot"},
	"Systems":            {"ComputerSystem", "ComputerSystemCollection"},
	"Managers":           {"Manager", "ManagerCollection"},
	"Chassis":            {"Chassis", "ChassisCollection"},
	"NetworkInterfaces":  {"NetworkInterface", "NetworkInterfaceCollection", "NetworkAdapter", "NetworkAdapterCollection", "NetworkDeviceFunction", "NetworkDeviceFunctionCollection", "NetworkPort", "NetworkPortCollection", "Port", "PortCollection"},
	"EthernetInterfaces": {"EthernetInterface", "EthernetInterfaceCollection"},
	"Processors":         {"Processor", "ProcessorCollection"},
	"Memory":             {"Memory", "MemoryCollection"},
	"Storage":            {"Storage", "StorageCollection", "StorageController", "StorageControllerCollection", "Drive", "DriveCollection", "Volume", "VolumeCollection"},
	"Power":              {"Power", "PowerSubsystem", "PowerSupply", "PowerSupplyCollection"},
	"Thermal":            {"Thermal", "ThermalSubsystem", "Fan", "FanCollection", "Sensor", "SensorCollection"},
	"Bios":               {"Bios"},
	"BootOptions":        {"BootOption", "BootOptionCollection"},
	"FirmwareInventory":  {"UpdateService", "SoftwareInventory", "SoftwareInventoryCollection"},
}

func inventoryType(feature, resourceType string) bool {
	for _, name := range inventoryTypes[feature] {
		if strings.HasPrefix(resourceType, "#"+name+".") {
			return true
		}
	}
	return false
}

// This is a bounded inventory envelope check, not a full DMTF schema validator.
// Invalid JSON objects are still retained as raw evidence and remain partial.
func inventoryDocumentCode(path, feature string, object map[string]any, resolve func(string, string) (string, error)) string {
	kind, _ := object["@odata.type"].(string)
	if !inventoryType(feature, kind) {
		return "inventory_resource_type_mismatch"
	}
	id, _ := object["@odata.id"].(string)
	if id == "" {
		return "inventory_resource_identity_missing"
	}
	canonical, e := resolve(path, id)
	if e != nil {
		return "inventory_resource_identity_mismatch"
	}
	base := func(v string) string { v, _, _ = strings.Cut(v, "?"); return strings.TrimSuffix(v, "/") }
	if base(canonical) != base(path) {
		return "inventory_resource_identity_mismatch"
	}
	if strings.Contains(strings.SplitN(strings.TrimPrefix(kind, "#"), ".", 2)[0], "Collection") {
		members, ok := object["Members"].([]any)
		if !ok {
			return "invalid_resource_members"
		}
		count, ok := object["Members@odata.count"].(json.Number)
		if !ok {
			return "invalid_resource_member_count"
		}
		n, e := count.Int64()
		if e != nil || n < 0 || n < int64(len(members)) {
			return "invalid_resource_member_count"
		}
		next, _ := object["Members@odata.nextLink"].(string)
		if value, present := object["Members@odata.nextLink"]; present {
			if _, ok := value.(string); !ok || next == "" {
				return "invalid_resource_next_link"
			}
		}
		if !strings.Contains(path, "?") && n > int64(len(members)) && next == "" {
			return "incomplete_resource_members"
		}
		for _, member := range members {
			link, ok := member.(map[string]any)
			if !ok {
				return "invalid_resource_members"
			}
			id, ok := link["@odata.id"].(string)
			if !ok || id == "" {
				return "invalid_resource_members"
			}
		}
	}
	return ""
}

// The collector validates absolute metadata IDs against its configured origin.
// A retained snapshot has no endpoint credentials/configuration, so its envelope
// check only compares the URI path to the already authenticated GET resource.
func retainedInventoryResolve(parent, reference string) (string, error) {
	u, e := url.Parse(reference)
	if e != nil {
		return "", e
	}
	if u.IsAbs() || u.Host != "" {
		if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" {
			return "", errors.New("invalid retained inventory identity")
		}
		reference = u.RequestURI()
	}
	return CanonicalPath(parent, reference)
}

// A successful first page is not complete evidence for the whole collection.
// Validate declared totals, unique members and the retained next-link chain.
type inventoryCoverageFailure struct {
	Failure
	Feature string
}

func collectionCoverageFailures(resources []Resource) []inventoryCoverageFailure {
	type collection struct {
		expected     int64
		members      map[string]bool
		pages        map[string]string
		inconsistent bool
		feature      string
	}
	groups := map[string]*collection{}
	retained := map[string]bool{}
	failures := []inventoryCoverageFailure{}
	for _, r := range resources {
		retained[r.Path] = true
	}
	for _, r := range resources {
		if strings.HasPrefix(r.Path, "/ipmi/") {
			continue
		}
		var object map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(r.Data)))
		decoder.UseNumber()
		if decoder.Decode(&object) != nil {
			continue
		}
		kind, _ := object["@odata.type"].(string)
		if !strings.Contains(strings.SplitN(strings.TrimPrefix(kind, "#"), ".", 2)[0], "Collection") {
			continue
		}
		members, ok := object["Members"].([]any)
		if !ok {
			continue
		}
		count, ok := object["Members@odata.count"].(json.Number)
		if !ok {
			continue
		}
		n, e := count.Int64()
		if e != nil || n < 0 {
			continue
		}
		base, _, _ := strings.Cut(r.Path, "?")
		group := groups[base]
		if group == nil {
			group = &collection{expected: n, members: map[string]bool{}, pages: map[string]string{}}
			groups[base] = group
		} else if group.expected != n {
			group.inconsistent = true
		}
		for feature := range inventoryTypes {
			if inventoryType(feature, kind) {
				group.feature = feature
				break
			}
		}
		for _, member := range members {
			link, ok := member.(map[string]any)
			if !ok {
				group.inconsistent = true
				continue
			}
			id, _ := link["@odata.id"].(string)
			path, e := retainedInventoryResolve(r.Path, id)
			if e != nil {
				group.inconsistent = true
				continue
			}
			if _, e = RedfishInventoryPath(path); e != nil {
				group.inconsistent = true
				continue
			}
			group.members[path] = true
		}
		next, _ := object["Members@odata.nextLink"].(string)
		if next != "" {
			path, e := retainedInventoryResolve(r.Path, next)
			if e != nil {
				group.inconsistent = true
			} else {
				next = path
			}
		}
		group.pages[r.Path] = next
	}
	// Sort so identical retained evidence produces stable diagnostics.
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, base := range keys {
		group := groups[base]
		bad := group.inconsistent || int64(len(group.members)) != group.expected
		for member := range group.members {
			if !retained[member] {
				bad = true
			}
		}
		seen := map[string]bool{}
		page := base
		for page != "" {
			if seen[page] {
				bad = true
				break
			}
			seen[page] = true
			next, ok := group.pages[page]
			if !ok {
				bad = true
				break
			}
			page = next
		}
		if len(seen) != len(group.pages) {
			bad = true
		}
		if bad && len(failures) < 256 {
			failures = append(failures, inventoryCoverageFailure{Failure: Failure{Path: base, Class: "partial_failure", Code: "incomplete_resource_collection", Retryable: true}, Feature: group.feature})
		}
	}
	return failures
}
