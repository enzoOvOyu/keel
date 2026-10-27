package precollect

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func pointer[T any](v T) *T { return &v }
func completeReport(gpuCount int, arch string, now time.Time) (*Report, Expectation) {
	e := Expectation{Serial: "POC-UNVERIFIED", Arch: arch, GPUCount: pointer(gpuCount), NICCount: pointer(1), Ports: []ExpectedPort{{ID: strings.Repeat("a", 32), ComponentID: strings.Repeat("b", 32), MAC: "02:00:00:00:00:01", PortIndex: "1"}}}
	r := &Report{Version: Version, Origin: "real", Serial: e.Serial, Arch: arch, OSRelease: "ID=fixture", CollectedAt: now.Add(-time.Minute), ConfigUnchanged: true, PCITopology: "retained PCI tree", GPUTopology: "retained GPU matrix", NVLinkStatus: "retained NVLink state", RDMADevices: []string{"mlx5_0"}, GPUs: []GPU{}, NICs: []NIC{{Name: "enp1s0", BDF: "0000:01:00.0", PermanentMAC: e.Ports[0].MAC, Link: "yes", SpeedMbps: pointer(int64(100000)), Driver: "mlx5_core", RDMALinks: []string{"mlx5_0/1"}, DOMState: "ready", ModulePresent: pointer(true), TemperatureC: pointer(35.0), RxPowerDBm: pointer(-2.0), TxPowerDBm: pointer(-1.0)}}}
	for i := 0; i < gpuCount; i++ {
		r.GPUs = append(r.GPUs, GPU{UUID: fmt.Sprintf("GPU-00000000-0000-0000-0000-%012d", i), BDF: fmt.Sprintf("0000:%02x:00.0", i+2), MemoryMiB: pointer(int64(81920)), UncorrectedECC: pointer(int64(0)), DriverVersion: "fixture-only", Present: true})
	}
	return r, e
}
func TestG1IndependentModelsAndFullFields(t *testing.T) {
	now := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	for _, profile := range []struct {
		count int
		arch  string
	}{{2, "amd64"}, {8, "arm64"}, {0, "amd64"}} {
		t.Run(fmt.Sprintf("%s-%d", profile.arch, profile.count), func(t *testing.T) {
			r, e := completeReport(profile.count, profile.arch, now)
			got := Evaluate(r, e, true, now, time.Hour)
			if got.Verdict != "PASS" || got.CompletenessPercent != 100 {
				t.Fatalf("complete independent model: %+v", got)
			}
		})
	}
}
func TestG1MissingSimulationStalenessAndBOMCannotPass(t *testing.T) {
	now := time.Now().UTC()
	cases := []struct {
		name   string
		change func(*Report, *Expectation)
		bmc    bool
		want   string
	}{
		{"simulation", func(r *Report, e *Expectation) { r.Origin = "sim" }, true, "BLOCKED"},
		{"expired", func(r *Report, e *Expectation) { r.CollectedAt = now.Add(-time.Hour) }, true, "BLOCKED"},
		{"DOM unsupported", func(r *Report, e *Expectation) { r.NICs[0].DOMState = "unsupported" }, true, "BLOCKED"},
		{"ECC unknown", func(r *Report, e *Expectation) { r.GPUs[0].UncorrectedECC = nil }, true, "BLOCKED"},
		{"configuration changed", func(r *Report, e *Expectation) { r.ConfigUnchanged = false }, true, "BLOCKED"},
		{"BOM MAC differs", func(r *Report, e *Expectation) { r.NICs[0].PermanentMAC = "02:00:00:00:00:02" }, true, "FAIL"},
		{"architecture differs", func(r *Report, e *Expectation) { r.Arch = "arm64" }, true, "FAIL"},
		{"model missing", func(r *Report, e *Expectation) { e.GPUCount = nil }, true, "BLOCKED"},
		{"BMC not observed", func(r *Report, e *Expectation) {}, false, "BLOCKED"},
		{"duplicate MAC", func(r *Report, e *Expectation) { r.NICs = append(r.NICs, r.NICs[0]) }, true, "FAIL"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, e := completeReport(2, "amd64", now)
			c.change(r, &e)
			got := Evaluate(r, e, c.bmc, now, time.Hour)
			if got.Verdict != c.want {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
func TestG1AbsentExpectedGPUDoesNotShrinkDenominator(t *testing.T) {
	now := time.Now()
	r, e := completeReport(8, "arm64", now)
	full := Evaluate(r, e, true, now, time.Hour)
	r.GPUs = r.GPUs[:7]
	missing := Evaluate(r, e, true, now, time.Hour)
	if missing.Verdict != "FAIL" || missing.Required != full.Required || missing.Present != full.Present-5 || missing.CompletenessPercent >= 100 {
		t.Fatalf("missing GPU lost expected fields: full=%+v missing=%+v", full, missing)
	}
}
func TestRawDecodeAndFixedParserEvidence(t *testing.T) {
	now := time.Now().UTC()
	config := []NetConfig{{Name: "eth0", MAC: "02:00:00:00:00:01", MTU: 1500, AdminUp: true}}
	b := Bundle{Version: Version, Origin: "sim", StartedAt: now.Add(-time.Second), CollectedAt: now, Serial: "fixture", Arch: "arm64", OSRelease: "ID=fixture", ConfigBefore: config, ConfigAfter: config, Interfaces: []InterfaceSeed{{Name: "eth0", BDF: "0000:01:00.0"}}, RDMADevices: []string{"mlx5_0"}, Probes: []Probe{
		{Kind: "gpu", State: "ready", Stdout: "GPU-00000000-0000-0000-0000-000000000001, 00000000:02:00.0, 81920, fixture, 0\n"},
		{Kind: "nic_permanent", Target: "eth0", State: "ready", Stdout: "Permanent address: 02:00:00:00:00:01\n"},
		{Kind: "nic_link", Target: "eth0", State: "ready", Stdout: "Speed: 100000Mb/s\nLink detected: yes\n"},
		{Kind: "nic_driver", Target: "eth0", State: "ready", Stdout: "driver: mlx5_core\nbus-info: 0000:01:00.0\nfirmware-version: fixture\n"},
		{Kind: "nic_dom", Target: "eth0", State: "ready", Stdout: "Module temperature: 35.00 degrees C\nReceiver signal average optical power: 1.0 mW / -2.0 dBm\nLaser output power: 1.0 mW / -1.0 dBm\n"},
		{Kind: "rdma_links", State: "ready", Stdout: `[{"ifname":"mlx5_0","netdev":"eth0","port":1,"state":"ACTIVE"}]`},
		{Kind: "ibv_devices", State: "ready", Stdout: "mlx5_0 0000000000000001\n"},
	}}
	raw, _ := json.Marshal(b)
	decoded, err := Decode(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Normalize(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if r.GPUs[0].BDF != "0000:02:00.0" || r.NICs[0].DOMState != "ready" || len(r.NICs[0].RDMALinks) != 1 || !r.ConfigUnchanged {
		t.Fatalf("normalized retained observations: %+v", r)
	}
	b.Probes[0].Stdout = "GPU-00000000-0000-0000-0000-000000000001, 00000000:02:00.0, N/A, fixture, [N/A]\n"
	r, err = Normalize(&b)
	if err != nil || r.GPUs[0].MemoryMiB != nil || r.GPUs[0].UncorrectedECC != nil {
		t.Fatalf("unknowns became facts: %+v %v", r, err)
	}
	b.Probes = append(b.Probes, b.Probes[0])
	raw, _ = json.Marshal(b)
	if _, err = Decode(raw, now); err == nil {
		t.Fatal("duplicate probe accepted")
	}
	if _, err = Decode(append(raw, []byte(` {}`)...), now); err == nil {
		t.Fatal("trailing document accepted")
	}
}
