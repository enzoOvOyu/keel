// Package provisionpreflight collects read-only pre-installation facts and
// evaluates them against an immutable independently approved expectation.
// Its report is not authorization: the control plane must verify the callback
// credential, request/attempt/fence and live machine lease before consuming it.
package provisionpreflight

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const Schema = "keel.provision-preflight/v1"
const Source = "linux-sysfs-smartctl/v1"

var ErrInvalid = errors.New("invalid provision preflight document")
var id = regexp.MustCompile(`^[a-f0-9]{32}$`)
var sha = regexp.MustCompile(`^[a-f0-9]{64}$`)
var pciAddress = regexp.MustCompile(`^[a-f0-9]{4,8}:[a-f0-9]{2}:[01][a-f0-9]\.[0-7]$`)
var physicalSlotName = regexp.MustCompile(`^[A-Za-z0-9_.: -]{1,64}$`)
var diskName = regexp.MustCompile(`^(sd[a-z]+|nvme[0-9]+n[0-9]+|vd[a-z]+|xvd[a-z]+)$`)
var netName = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
var classCode = regexp.MustCompile(`^0x[0-9a-f]{6}$`)
var pciIdentity = regexp.MustCompile(`^[a-f0-9]{4}:[a-f0-9]{4}$`)

type Expectation struct {
	Serial           string   `json:"serial_number"`
	Arch             string   `json:"arch"`
	BootMAC          string   `json:"boot_mac"`
	NICCount         *int     `json:"nic_count"`
	GPUCount         *int     `json:"gpu_count"`
	GPUIdentities    []string `json:"gpu_pci_ids"`
	NonGPUIdentities []string `json:"non_gpu_pci_ids"`
	// A count alone never authorizes a disk selection or destructive operation.
	MinimumDisks int `json:"minimum_disks"`
}
type Spec struct {
	Schema         string      `json:"schema"`
	ProjectID      string      `json:"project_id"`
	RequestID      string      `json:"request_id"`
	MachineID      string      `json:"machine_id"`
	AttemptID      string      `json:"attempt_id"`
	CommandID      string      `json:"command_id"`
	FencingToken   int64       `json:"fencing_token"`
	SnapshotSHA256 string      `json:"snapshot_sha256"`
	ExpiresAt      time.Time   `json:"expires_at"`
	Expected       Expectation `json:"expected"`
}

func (s Spec) Validate(now time.Time) error {
	if s.Schema != Schema || !id.MatchString(s.ProjectID) || !id.MatchString(s.RequestID) || !id.MatchString(s.MachineID) || !id.MatchString(s.AttemptID) || !id.MatchString(s.CommandID) || s.FencingToken < 1 || !sha.MatchString(s.SnapshotSHA256) || !s.ExpiresAt.After(now) {
		return ErrInvalid
	}
	e := s.Expected
	mac, err := net.ParseMAC(e.BootMAC)
	if len(e.Serial) == 0 || len(e.Serial) > 160 || strings.TrimSpace(e.Serial) != e.Serial || strings.ContainsAny(e.Serial, "\r\n\x00") || (e.Arch != "amd64" && e.Arch != "arm64") || err != nil || len(mac) != 6 || mac[0]&1 != 0 || e.BootMAC != mac.String() || e.BootMAC == "00:00:00:00:00:00" || e.MinimumDisks < 1 || e.MinimumDisks > 128 {
		return ErrInvalid
	}
	if (e.NICCount != nil && (*e.NICCount < 1 || *e.NICCount > 256)) || (e.GPUCount != nil && (*e.GPUCount < 0 || *e.GPUCount > 256)) || len(e.GPUIdentities) > 256 || len(e.NonGPUIdentities) > 256 {
		return ErrInvalid
	}
	seen := map[string]bool{}
	for _, identity := range append(append([]string{}, e.GPUIdentities...), e.NonGPUIdentities...) {
		if !pciIdentity.MatchString(identity) || seen[identity] {
			return ErrInvalid
		}
		seen[identity] = true
	}
	return nil
}

type PCI struct {
	Address         string `json:"address"`
	Class           string `json:"class"`
	Identity        string `json:"identity"`
	PhysicalSlot    string `json:"physical_slot,omitempty"`
	VirtualFunction bool   `json:"virtual_function"`
}
type Port struct {
	Name string `json:"name"`
	MAC  string `json:"mac"`
	PCI  string `json:"pci"`
}
type Disk struct {
	Device    string `json:"device"`
	Serial    string `json:"serial_number,omitempty"`
	Healthy   *bool  `json:"healthy"`
	Reason    string `json:"reason"`
	SmartExit int    `json:"smart_exit"`
}
type Observation struct {
	Source     string    `json:"source"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`
	Serial     string    `json:"serial_number"`
	Arch       string    `json:"arch"`
	PCI        []PCI     `json:"pci"`
	Ports      []Port    `json:"ports"`
	Disks      []Disk    `json:"disks"`
	Missing    []string  `json:"missing"`
}
type Check struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Expected int    `json:"expected,omitempty"`
	Observed int    `json:"observed,omitempty"`
}
type Report struct {
	Schema      string      `json:"schema"`
	Spec        Spec        `json:"spec"`
	Observation Observation `json:"observation"`
	Status      string      `json:"status"`
	Checks      []Check     `json:"checks"`
}

// Evaluate uses the existing gate vocabulary PASS/FAIL/BLOCKED. Missing data is
// BLOCKED, never zero observed hardware or a synthetic PASS. No WAIVED is minted.
func Evaluate(s Spec, o Observation, now time.Time) (*Report, error) {
	if s.Validate(now) != nil {
		return nil, ErrInvalid
	}
	if len(o.PCI) > 4096 || len(o.Ports) > 2048 || len(o.Disks) > 128 || len(o.Missing) > 128 {
		return nil, ErrInvalid
	}
	r := &Report{Schema: Schema, Spec: s, Observation: o, Status: "PASS", Checks: []Check{}}
	add := func(name, status, reason string, expected, observed int) {
		r.Checks = append(r.Checks, Check{name, status, reason, expected, observed})
		if status == "FAIL" {
			r.Status = "FAIL"
		} else if status == "BLOCKED" && r.Status != "FAIL" {
			r.Status = "BLOCKED"
		}
	}
	complete := o.Source == Source && len(o.Missing) == 0
	if o.Source != Source || o.StartedAt.IsZero() || o.FinishedAt.Before(o.StartedAt) || o.FinishedAt.After(now.Add(5*time.Second)) || now.Sub(o.FinishedAt) > 30*time.Second || o.FinishedAt.Sub(o.StartedAt) > 2*time.Minute {
		complete = false
		add("sources", "BLOCKED", "missing_or_stale_observation", 0, 0)
	} else if len(o.Missing) > 0 {
		add("sources", "BLOCKED", "incomplete_hardware_sources", 0, len(o.Missing))
	} else {
		add("sources", "PASS", "fresh_read_only_observation", 0, 0)
	}
	if o.Serial == "" {
		add("identity", "BLOCKED", "serial_unavailable", 0, 0)
	} else if o.Serial != s.Expected.Serial {
		add("identity", "FAIL", "serial_mismatch", 0, 0)
	} else {
		add("identity", "PASS", "serial_matches_bom", 0, 0)
	}
	if o.Arch == "" {
		add("architecture", "BLOCKED", "architecture_unavailable", 0, 0)
	} else if o.Arch != s.Expected.Arch {
		add("architecture", "FAIL", "architecture_mismatch", 0, 0)
	} else {
		add("architecture", "PASS", "architecture_matches_model", 0, 0)
	}
	slots := map[string]bool{}
	identities := map[string]bool{}
	for _, v := range s.Expected.GPUIdentities {
		identities[v] = true
	}
	nonGPU := map[string]bool{}
	for _, v := range s.Expected.NonGPUIdentities {
		nonGPU[v] = true
	}
	gpus := 0
	gpuComplete := complete
	nicComplete := complete
	physicalNICs := map[string]bool{}
	addresses := map[string]bool{}
	for _, device := range o.PCI {
		if !validPCIAddress(device.Address) || !pciIdentity.MatchString(device.Identity) || !classCode.MatchString(device.Class) || addresses[device.Address] {
			nicComplete, gpuComplete = false, false
			add("pci_inventory", "BLOCKED", "invalid_or_duplicate_pci_identity", 0, 0)
			continue
		}
		addresses[device.Address] = true
		if device.VirtualFunction {
			continue
		}
		if strings.HasPrefix(device.Class, "0x02") {
			physicalNICs[device.Address] = true
			if !physicalSlotName.MatchString(device.PhysicalSlot) || device.PhysicalSlot == "." || device.PhysicalSlot == ".." {
				nicComplete = false
			} else {
				slots[device.PhysicalSlot] = true
			}
		}
		if device.Class == "0x030000" || device.Class == "0x030200" || device.Class == "0x038000" || device.Class == "0x120000" {
			if identities[device.Identity] {
				gpus++
			} else if !nonGPU[device.Identity] {
				gpuComplete = false
			}
		}
	}
	bootMatches := 0
	portsComplete := complete
	seenPorts := map[string]bool{}
	for _, port := range o.Ports {
		mac, err := net.ParseMAC(port.MAC)
		if err != nil || len(mac) != 6 || mac[0]&1 != 0 || port.MAC != mac.String() || !netName.MatchString(port.Name) || !validPCIAddress(port.PCI) || seenPorts[port.Name] || port.MAC == "00:00:00:00:00:00" {
			portsComplete = false
			add("port_inventory", "BLOCKED", "invalid_port_identity", 0, 0)
			continue
		}
		seenPorts[port.Name] = true
		if port.MAC == s.Expected.BootMAC && physicalNICs[port.PCI] {
			bootMatches++
		}
	}
	if bootMatches > 1 {
		add("boot_port", "BLOCKED", "boot_mac_ambiguous_across_physical_ports", 0, bootMatches)
	} else if bootMatches == 1 {
		add("boot_port", "PASS", "boot_mac_present", 0, 0)
	} else if !portsComplete || !nicComplete {
		add("boot_port", "BLOCKED", "physical_port_inventory_incomplete", 0, 0)
	} else {
		add("boot_port", "FAIL", "bom_boot_mac_not_observed_on_physical_pci_nic", 0, 0)
	}
	if s.Expected.NICCount == nil {
		add("nic_count", "BLOCKED", "published_nic_count_missing", 0, len(slots))
	} else if !nicComplete {
		add("nic_count", "BLOCKED", "physical_nic_grouping_unavailable", *s.Expected.NICCount, len(slots))
	} else if len(slots) != *s.Expected.NICCount {
		add("nic_count", "FAIL", "physical_nic_count_mismatch", *s.Expected.NICCount, len(slots))
	} else {
		add("nic_count", "PASS", "physical_nic_count_matches", *s.Expected.NICCount, len(slots))
	}
	if s.Expected.GPUCount == nil {
		add("gpu_presence", "BLOCKED", "published_gpu_count_missing", 0, gpus)
	} else if *s.Expected.GPUCount > 0 && len(identities) == 0 {
		add("gpu_presence", "BLOCKED", "published_gpu_pci_identities_missing", *s.Expected.GPUCount, gpus)
	} else if !gpuComplete {
		add("gpu_presence", "BLOCKED", "accelerator_inventory_unclassified_or_incomplete", *s.Expected.GPUCount, gpus)
	} else if gpus != *s.Expected.GPUCount {
		add("gpu_presence", "FAIL", "gpu_presence_mismatch", *s.Expected.GPUCount, gpus)
	} else {
		add("gpu_presence", "PASS", "gpu_presence_matches", *s.Expected.GPUCount, gpus)
	}
	if len(o.Disks) < s.Expected.MinimumDisks {
		add("disk_health", "BLOCKED", "insufficient_disk_inventory", s.Expected.MinimumDisks, len(o.Disks))
	} else {
		status, reason := "PASS", "all_observed_disk_health_passed"
		seen := map[string]bool{}
		for _, disk := range o.Disks {
			if !strings.HasPrefix(disk.Device, "/dev/") || !diskName.MatchString(strings.TrimPrefix(disk.Device, "/dev/")) || seen[disk.Device] {
				if status != "FAIL" {
					status = "BLOCKED"
					reason = "invalid_disk_inventory"
				}
			}
			seen[disk.Device] = true
			if disk.Healthy == nil {
				if status != "FAIL" {
					status = "BLOCKED"
					reason = "disk_health_unavailable"
				}
			} else if !*disk.Healthy {
				status = "FAIL"
				reason = "disk_health_failed"
			}
		}
		add("disk_health", status, reason, s.Expected.MinimumDisks, len(o.Disks))
	}
	return r, nil
}
func DecodeSpec(raw []byte) (Spec, error) {
	var s Spec
	if len(raw) > 64<<10 || !utf8.Valid(raw) || uniqueSpecKeys(raw) != nil {
		return s, ErrInvalid
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&s) != nil {
		return s, ErrInvalid
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return s, ErrInvalid
	}
	return s, nil
}

// Detect duplicate or case-aliased keys before Go's case-insensitive decoding.
func uniqueSpecKeys(raw []byte) error {
	d := json.NewDecoder(strings.NewReader(string(raw)))
	var walk func(int) error
	walk = func(depth int) error {
		if depth > 8 {
			return ErrInvalid
		}
		token, err := d.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		seen := map[string]bool{}
		switch delimiter {
		case '{':
			for d.More() {
				token, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := token.(string)
				if !ok || key != strings.ToLower(key) || seen[key] {
					return ErrInvalid
				}
				seen[key] = true
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(depth + 1); err != nil {
					return err
				}
			}
		default:
			return ErrInvalid
		}
		_, err = d.Token()
		return err
	}
	if err := walk(0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	return nil
}

func validPCIAddress(value string) bool {
	if !pciAddress.MatchString(value) {
		return false
	}
	domain := strings.SplitN(value, ":", 2)[0]
	if len(domain) > 4 && domain[0] == '0' {
		return false
	}
	number, err := strconv.ParseUint(domain, 16, 32)
	return err == nil && number <= 0x7fffffff
}
