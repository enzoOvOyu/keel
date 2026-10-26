package provisionpreflight

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func fixture() (Spec, Observation, time.Time) {
	now := time.Date(2026, 10, 26, 1, 0, 0, 0, time.UTC)
	one, yes := 1, true
	s := Spec{Schema: Schema, ProjectID: strings.Repeat("1", 32), RequestID: strings.Repeat("2", 32), MachineID: strings.Repeat("3", 32), AttemptID: strings.Repeat("4", 32), CommandID: strings.Repeat("5", 32), FencingToken: 17, SnapshotSHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(time.Minute), Expected: Expectation{Serial: "fixture-server", Arch: "amd64", BootMAC: "02:00:00:00:00:01", NICCount: &one, GPUCount: &one, GPUIdentities: []string{"10de:2901"}, NonGPUIdentities: []string{"1a03:2000"}, MinimumDisks: 1}}
	o := Observation{Source: Source, StartedAt: now.Add(-time.Second), FinishedAt: now, Serial: s.Expected.Serial, Arch: "amd64", PCI: []PCI{
		{Address: "0000:01:00.0", Class: "0x020000", Identity: "15b3:1021", PhysicalSlot: "slot-1"},
		{Address: "0000:01:00.1", Class: "0x020000", Identity: "15b3:1021", PhysicalSlot: "slot-1"},
		{Address: "0000:01:00.2", Class: "0x020000", Identity: "15b3:1021", VirtualFunction: true},
		{Address: "0000:02:00.0", Class: "0x030200", Identity: "10de:2901", PhysicalSlot: "slot-2"},
		{Address: "0000:03:00.0", Class: "0x030000", Identity: "1a03:2000"},
	}, Ports: []Port{{Name: "eth0", MAC: s.Expected.BootMAC, PCI: "0000:01:00.0"}}, Disks: []Disk{{Device: "/dev/nvme0n1", Healthy: &yes, Reason: "fixture"}}}
	return s, o, now
}
func TestEvaluateHardwareBoundaries(t *testing.T) {
	cases := []struct {
		name, status, check, reason string
		edit                        func(*Spec, *Observation)
	}{
		{"one_card_two_ports_and_VF", "PASS", "nic_count", "physical_nic_count_matches", func(*Spec, *Observation) {}},
		{"missing_count_not_zero", "BLOCKED", "nic_count", "published_nic_count_missing", func(s *Spec, _ *Observation) { s.Expected.NICCount = nil }},
		{"unknown_physical_slot", "BLOCKED", "nic_count", "physical_nic_grouping_unavailable", func(_ *Spec, o *Observation) { o.PCI[0].PhysicalSlot = "" }},
		{"incomplete_PCI_not_false_mismatch", "BLOCKED", "nic_count", "physical_nic_grouping_unavailable", func(_ *Spec, o *Observation) {
			o.PCI = nil
			o.Ports = nil
			o.Missing = []string{"pci_inventory_unavailable"}
		}},
		{"stale_sources", "BLOCKED", "sources", "missing_or_stale_observation", func(_ *Spec, o *Observation) {
			o.StartedAt = o.StartedAt.Add(-time.Minute)
			o.FinishedAt = o.FinishedAt.Add(-time.Minute)
		}},
		{"serial_mismatch", "FAIL", "identity", "serial_mismatch", func(_ *Spec, o *Observation) { o.Serial = "another-server" }},
		{"unknown_arch", "BLOCKED", "architecture", "architecture_unavailable", func(_ *Spec, o *Observation) { o.Arch = "" }},
		{"wrong_boot_mac", "FAIL", "boot_port", "bom_boot_mac_not_observed_on_physical_pci_nic", func(_ *Spec, o *Observation) { o.Ports[0].MAC = "02:00:00:00:00:09" }},
		{"boot_port_cannot_be_GPU", "FAIL", "boot_port", "bom_boot_mac_not_observed_on_physical_pci_nic", func(_ *Spec, o *Observation) { o.Ports[0].PCI = "0000:02:00.0" }},
		{"boot_port_cannot_be_VF", "FAIL", "boot_port", "bom_boot_mac_not_observed_on_physical_pci_nic", func(_ *Spec, o *Observation) { o.Ports[0].PCI = "0000:01:00.2" }},
		{"GPU_without_published_ID", "BLOCKED", "gpu_presence", "published_gpu_pci_identities_missing", func(s *Spec, _ *Observation) { s.Expected.GPUIdentities = nil }},
		{"BMC_not_implicitly_ignored", "BLOCKED", "gpu_presence", "accelerator_inventory_unclassified_or_incomplete", func(s *Spec, _ *Observation) { s.Expected.NonGPUIdentities = nil }},
		{"zero_expected_still_classifies_GPU", "BLOCKED", "gpu_presence", "accelerator_inventory_unclassified_or_incomplete", func(s *Spec, _ *Observation) { n := 0; s.Expected.GPUCount = &n; s.Expected.GPUIdentities = nil }},
		{"known_GPU_unexpected", "FAIL", "gpu_presence", "gpu_presence_mismatch", func(s *Spec, _ *Observation) { n := 0; s.Expected.GPUCount = &n }},
		{"GPU_absent", "FAIL", "gpu_presence", "gpu_presence_mismatch", func(_ *Spec, o *Observation) { o.PCI = append(o.PCI[:3], o.PCI[4:]...) }},
		{"unknown_SMART", "BLOCKED", "disk_health", "disk_health_unavailable", func(_ *Spec, o *Observation) { o.Disks[0].Healthy = nil }},
		{"disk_unhealthy_overrides_missing", "FAIL", "disk_health", "disk_health_failed", func(_ *Spec, o *Observation) {
			no := false
			o.Disks[0].Healthy = &no
			o.Missing = []string{"other_source"}
		}},
		{"no_disks", "BLOCKED", "disk_health", "insufficient_disk_inventory", func(_ *Spec, o *Observation) { o.Disks = nil }},
		{"duplicate_disk", "BLOCKED", "disk_health", "invalid_disk_inventory", func(_ *Spec, o *Observation) { o.Disks = append(o.Disks, o.Disks[0]) }},
		{"malformed_pci_class", "BLOCKED", "pci_inventory", "invalid_or_duplicate_pci_identity", func(_ *Spec, o *Observation) { o.PCI[0].Class = "0x02garbage" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, o, now := fixture()
			tc.edit(&s, &o)
			r, err := Evaluate(s, o, now)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != tc.status {
				t.Fatalf("status=%s checks=%+v", r.Status, r.Checks)
			}
			for _, c := range r.Checks {
				if c.Name == tc.check && c.Reason == tc.reason {
					return
				}
			}
			t.Fatalf("missing %s/%s: %+v", tc.check, tc.reason, r.Checks)
		})
	}
}
func TestSpecRejectsAmbiguousOrUnboundDocuments(t *testing.T) {
	s, _, now := fixture()
	raw, _ := json.Marshal(s)
	for _, document := range [][]byte{
		append(append([]byte{}, raw...), raw...),
		[]byte(strings.Replace(string(raw), `"schema":`, `"Schema":`, 1)),
		[]byte(strings.Replace(string(raw), `"schema":`, `"schema":"other","schema":`, 1)),
		[]byte(strings.Replace(string(raw), `"arch":"amd64"`, `"arch":"amd64","arch":"arm64"`, 1)),
		[]byte(strings.Replace(string(raw), `"schema":`, `"unknown":true,"schema":`, 1)),
		[]byte(strings.Repeat(" ", 65<<10)),
	} {
		if _, err := DecodeSpec(document); err == nil {
			t.Fatal("accepted ambiguous/invalid document")
		}
	}
	decoded, err := DecodeSpec(raw)
	if err != nil || decoded.Validate(now) != nil {
		t.Fatal("valid spec rejected", err)
	}
	for _, edit := range []func(*Spec){func(s *Spec) { s.FencingToken = 0 }, func(s *Spec) { s.ExpiresAt = now }, func(s *Spec) { s.RequestID = "" }, func(s *Spec) { s.Expected.NonGPUIdentities = s.Expected.GPUIdentities }} {
		s, _, now := fixture()
		edit(&s)
		if s.Validate(now) == nil {
			t.Fatal("accepted unbound spec")
		}
	}
}
func TestModelExpectationDoesNotInventCounts(t *testing.T) {
	e, err := ExpectationFromModel("fixture", "02:00:00:00:00:01", []byte(`{"cpu_arch":"arm64"}`), []byte(`{}`))
	if err != nil || e.NICCount != nil || e.GPUCount != nil || e.Arch != "arm64" {
		t.Fatalf("%+v %v", e, err)
	}
	e, err = ExpectationFromModel("fixture", "02:00:00:00:00:01", []byte(`{"cpu_arch":"amd64","gpu_count":0,"non_gpu_pci_ids":["1a03:2000"]}`), []byte(`{"nic_count":2}`))
	if err != nil || *e.GPUCount != 0 || *e.NICCount != 2 {
		t.Fatalf("%+v %v", e, err)
	}
	for _, hw := range []string{`{"gpu_pci_ids":null}`, `{"gpu_pci_ids":[]}`, `{"gpu_pci_ids":["10DE:2901"]}`, `{"gpu_pci_ids":["10de:2901","10de:2901"]}`, `{"gpu_pci_ids":["10de:2901"],"non_gpu_pci_ids":["10de:2901"]}`} {
		if _, _, err := ModelPCIIdentities([]byte(hw)); err == nil {
			t.Fatal("accepted invalid identities", hw)
		}
	}
	for _, value := range []string{`null`, `"2"`, `-1`, `0.1`, `257`} {
		if _, err := ExpectationFromModel("fixture", "02:00:00:00:00:01", []byte(`{"cpu_arch":"amd64","gpu_count":`+value+`}`), []byte(`{}`)); err == nil {
			t.Fatal("accepted invalid count", value)
		}
	}
}
