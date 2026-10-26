package provisionstorage

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
)

func storageFixture() (provisionpreflight.Spec, provisionpreflight.Observation, Intent, time.Time) {
	now := time.Date(2026, 10, 26, 2, 0, 0, 0, time.UTC)
	one, zero, yes, no := 1, 0, true, false
	s := provisionpreflight.Spec{Schema: provisionpreflight.Schema, ProjectID: strings.Repeat("1", 32), RequestID: strings.Repeat("2", 32), MachineID: strings.Repeat("3", 32), AttemptID: strings.Repeat("4", 32), CommandID: strings.Repeat("5", 32), FencingToken: 23, SnapshotSHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(10 * time.Minute), Expected: provisionpreflight.Expectation{Serial: "storage-server", Arch: "amd64", BootMAC: "02:00:00:00:00:01", NICCount: &one, GPUCount: &zero, MinimumDisks: 1}}
	intent := Intent{Schema: IntentSchema, Layout: Layout, WWID: "eui.0123456789abcdef", Serial: "selected-namespace", SizeBytes: 1 << 40, LogicalBlockBytes: 4096, EraseSelectedDisk: true}
	d := provisionpreflight.Disk{Device: "/dev/nvme1n1", Healthy: &yes, Reason: "fixture", WWID: intent.WWID, Serial: intent.Serial, SizeBytes: intent.SizeBytes, LogicalBlockBytes: intent.LogicalBlockBytes, DeviceNumber: "259:3", DiskSequence: 71, ReadOnly: &no, Removable: &no, InUse: &no}
	other := d
	other.Device = "/dev/sda"
	other.WWID = "naa.1234567890abcdef"
	other.Serial = "unselected-disk"
	other.DeviceNumber = "8:0"
	other.DiskSequence = 70
	o := provisionpreflight.Observation{Source: provisionpreflight.Source, Firmware: "uefi", StartedAt: now.Add(-time.Second), FinishedAt: now, Serial: s.Expected.Serial, Arch: s.Expected.Arch, PCI: []provisionpreflight.PCI{{Address: "0000:01:00.0", Class: "0x020000", Identity: "15b3:1021", PhysicalSlot: "slot-1"}}, Ports: []provisionpreflight.Port{{Name: "eth0", MAC: s.Expected.BootMAC, PCI: "0000:01:00.0"}}, Disks: []provisionpreflight.Disk{other, d}}
	return s, o, intent, now
}
func reportBytes(t *testing.T, s provisionpreflight.Spec, o provisionpreflight.Observation, now time.Time) []byte {
	t.Helper()
	r, err := provisionpreflight.Evaluate(s, o, now)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestStoragePlanSelectsExplicitIdentityAndAlignedGeometry(t *testing.T) {
	for _, sector := range []int{512, 4096} {
		t.Run(string(rune('0'+sector/512)), func(t *testing.T) {
			s, o, i, now := storageFixture()
			i.LogicalBlockBytes = sector
			o.Disks[1].LogicalBlockBytes = sector
			raw := reportBytes(t, s, o, now)
			p, err := Build(s, raw, i, now)
			if err != nil {
				t.Fatal(err)
			}
			if p.Device.Node != "/dev/nvme1n1" || p.Device.Number != "259:3" || p.EvidenceSHA256 != hash(raw) || len(p.Partitions) != 2 {
				t.Fatalf("not explicitly bound: %+v", p)
			}
			end := int64(0)
			for _, part := range p.Partitions {
				if part.StartBytes%MiB != 0 || part.SizeBytes%MiB != 0 || part.StartBytes < end {
					t.Fatal("unaligned or overlapping layout")
				}
				end = part.StartBytes + part.SizeBytes
			}
			if end > i.SizeBytes-MiB || p.Partitions[1].SizeBytes < MinimumRootBytes {
				t.Fatal("GPT space or root bound lost")
			}
			encoded, _ := json.Marshal(p)
			decoded, err := Decode(encoded, now)
			if err != nil || decoded.SHA256 != p.SHA256 {
				t.Fatal("canonical plan did not round-trip", err)
			}
			o.StartedAt = now.Add(time.Second)
			o.FinishedAt = now.Add(2 * time.Second)
			if VerifyCurrent(*p, reportBytes(t, s, o, now.Add(2*time.Second)), now.Add(2*time.Second)) != nil {
				t.Fatal("unchanged fresh device denied")
			}
		})
	}
}
func TestStoragePlanRejectsMissingUnsafeOrAmbiguousFacts(t *testing.T) {
	cases := map[string]func(*provisionpreflight.Spec, *provisionpreflight.Observation, *Intent){
		"no_erase_consent": func(_ *provisionpreflight.Spec, _ *provisionpreflight.Observation, i *Intent) {
			i.EraseSelectedDisk = false
		},
		"no_selection":       func(_ *provisionpreflight.Spec, _ *provisionpreflight.Observation, i *Intent) { i.WWID = "" },
		"path_as_identity":   func(_ *provisionpreflight.Spec, _ *provisionpreflight.Observation, i *Intent) { i.WWID = "/dev/sda" },
		"unsupported_layout": func(_ *provisionpreflight.Spec, _ *provisionpreflight.Observation, i *Intent) { i.Layout = "auto" },
		"wrong_serial":       func(_ *provisionpreflight.Spec, _ *provisionpreflight.Observation, i *Intent) { i.Serial = "other" },
		"wrong_geometry":     func(_ *provisionpreflight.Spec, _ *provisionpreflight.Observation, i *Intent) { i.SizeBytes -= MiB },
		"unknown_usage":      func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, _ *Intent) { o.Disks[1].InUse = nil },
		"mounted_disk": func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, _ *Intent) {
			yes := true
			o.Disks[1].InUse = &yes
		},
		"readonly_disk": func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, _ *Intent) {
			yes := true
			o.Disks[1].ReadOnly = &yes
		},
		"removable_disk": func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, _ *Intent) {
			yes := true
			o.Disks[1].Removable = &yes
		},
		"missing_sequence": func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, _ *Intent) {
			o.Disks[1].DiskSequence = 0
		},
		"duplicate_identity": func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, i *Intent) {
			o.Disks[0].WWID = i.WWID
			o.Disks[0].Serial = i.Serial
		},
		"duplicate_device_number": func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, _ *Intent) {
			o.Disks[0].DeviceNumber = o.Disks[1].DeviceNumber
		},
		"unsupported_firmware": func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, _ *Intent) { o.Firmware = "bios" },
		"missing_firmware":     func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, _ *Intent) { o.Firmware = "" },
		"bad_health": func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, _ *Intent) {
			no := false
			o.Disks[1].Healthy = &no
		},
		"stale_observation": func(_ *provisionpreflight.Spec, o *provisionpreflight.Observation, _ *Intent) {
			o.StartedAt = o.StartedAt.Add(-time.Minute)
			o.FinishedAt = o.FinishedAt.Add(-time.Minute)
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			s, o, i, now := storageFixture()
			edit(&s, &o, &i)
			if _, err := Build(s, reportBytes(t, s, o, now), i, now); err == nil {
				t.Fatal("unsafe disk plan admitted")
			}
		})
	}
}
func TestStoragePlanRevalidationRejectsReattachedOrChangedDevices(t *testing.T) {
	for name, edit := range map[string]func(*provisionpreflight.Observation){
		"renumbered":            func(o *provisionpreflight.Observation) { o.Disks[1].Device = "/dev/nvme2n1" },
		"reattached":            func(o *provisionpreflight.Observation) { o.Disks[1].DiskSequence++ },
		"changed_device_number": func(o *provisionpreflight.Observation) { o.Disks[1].DeviceNumber = "259:4" },
		"changed_wwid":          func(o *provisionpreflight.Observation) { o.Disks[1].WWID = "eui.ffffffffffffffff" },
	} {
		t.Run(name, func(t *testing.T) {
			s, o, i, now := storageFixture()
			p, err := Build(s, reportBytes(t, s, o, now), i, now)
			if err != nil {
				t.Fatal(err)
			}
			edit(&o)
			if VerifyCurrent(*p, reportBytes(t, s, o, now), now) == nil {
				t.Fatal("changed target retained authorization binding")
			}
		})
	}
}
func TestStoragePlanRejectsTamperingAndForgedPASS(t *testing.T) {
	s, o, i, now := storageFixture()
	raw := reportBytes(t, s, o, now)
	p, err := Build(s, raw, i, now)
	if err != nil {
		t.Fatal(err)
	}
	p.Partitions[1].StartBytes = MiB
	seal(p)
	if p.Validate(now) == nil {
		t.Fatal("overlap hidden by self-consistent digest")
	}
	raw = []byte(strings.Replace(string(raw), `"serial_number":"storage-server"`, `"serial_number":"forged-server"`, 1))
	if _, err := Build(s, raw, i, now); err == nil {
		t.Fatal("foreign frozen expectation admitted")
	}
	s, o, i, now = storageFixture()
	raw = reportBytes(t, s, o, now)
	p, _ = Build(s, raw, i, now)
	encoded, _ := json.Marshal(p)
	for _, bad := range [][]byte{append(append([]byte{}, encoded...), encoded...), []byte(strings.Replace(string(encoded), `"schema":`, `"Schema":`, 1)), []byte(strings.Replace(string(encoded), `"schema":`, `"unexpected":true,"schema":`, 1))} {
		if _, err := Decode(bad, now); err == nil {
			t.Fatal("ambiguous plan admitted")
		}
	}
	if VerifyCurrent(*p, raw, s.ExpiresAt) == nil {
		t.Fatal("expired target binding admitted")
	}
}
