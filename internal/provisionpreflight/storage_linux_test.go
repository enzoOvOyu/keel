//go:build linux

package provisionpreflight

import (
	"strconv"
	"testing"
)

func storageHost() *fixtureHost {
	f := &fixtureHost{files: map[string]string{}, directories: map[string][]string{}, links: map[string]string{}, existing: map[string]bool{}}
	base := "/sys/block/nvme0n1"
	f.files[base+"/wwid"] = "eui.0123456789abcdef"
	f.files[base+"/device/serial"] = "namespace-serial"
	f.files[base+"/size"] = strconv.FormatInt((1<<40)/512, 10)
	f.files[base+"/queue/logical_block_size"] = "4096"
	f.files[base+"/diskseq"] = "71"
	f.files[base+"/dev"] = "259:0"
	f.files[base+"/ro"] = "0"
	f.files[base+"/removable"] = "0"
	f.directories[base] = []string{"queue", "holders", "nvme0n1p1"}
	f.directories[base+"/holders"] = []string{}
	f.existing[base+"/nvme0n1p1/partition"] = true
	f.files[base+"/nvme0n1p1/dev"] = "259:1"
	f.directories[base+"/nvme0n1p1/holders"] = []string{}
	f.files["/proc/self/mountinfo"] = "1 0 0:1 / / rw - rootfs rootfs rw"
	f.files["/proc/swaps"] = "Filename Type Size Used Priority\n"
	return f
}
func TestCollectDiskStorageUsesSectorUnitsAndExplicitUsage(t *testing.T) {
	f := storageHost()
	d := Disk{Device: "/dev/nvme0n1"}
	collectDiskStorage(f, "nvme0n1", &d)
	if d.SizeBytes != 1<<40 || d.LogicalBlockBytes != 4096 || d.DiskSequence != 71 || d.Serial != "namespace-serial" || d.WWID != "eui.0123456789abcdef" || d.InUse == nil || *d.InUse || d.ReadOnly == nil || *d.ReadOnly {
		t.Fatalf("wrong storage facts: %+v", d)
	}
}
func TestCollectDiskStorageCannotInferMissingOrFreeDevices(t *testing.T) {
	for name, edit := range map[string]func(*fixtureHost){
		"partition_mounted": func(f *fixtureHost) {
			f.files["/proc/self/mountinfo"] = "1 0 259:1 / /target rw - ext4 /dev/nvme0n1p1 rw"
		},
		"whole_mounted": func(f *fixtureHost) {
			f.files["/proc/self/mountinfo"] = "1 0 259:0 / /target rw - ext4 /dev/nvme0n1 rw"
		},
		"partition_holder": func(f *fixtureHost) { f.directories["/sys/block/nvme0n1/nvme0n1p1/holders"] = []string{"dm-0"} },
		"whole_holder":     func(f *fixtureHost) { f.directories["/sys/block/nvme0n1/holders"] = []string{"md0"} },
		"active_swap":      func(f *fixtureHost) { f.files["/proc/swaps"] += "/swapfile file 1024 0 -2\n" },
		"missing_mounts":   func(f *fixtureHost) { delete(f.files, "/proc/self/mountinfo") },
		"missing_holders":  func(f *fixtureHost) { delete(f.directories, "/sys/block/nvme0n1/holders") },
		"bad_mounts":       func(f *fixtureHost) { f.files["/proc/self/mountinfo"] = "malformed" },
		"bad_swap_header":  func(f *fixtureHost) { f.files["/proc/swaps"] = "unexpected" },
	} {
		t.Run(name, func(t *testing.T) {
			f := storageHost()
			edit(f)
			d := Disk{}
			collectDiskStorage(f, "nvme0n1", &d)
			if d.InUse != nil && !*d.InUse {
				t.Fatal("busy or unknown device reported free")
			}
		})
	}
	for _, bad := range []string{"-1", "+1", "0002", "18446744073709551615", "bad"} {
		f := storageHost()
		f.files["/sys/block/nvme0n1/size"] = bad
		d := Disk{}
		collectDiskStorage(f, "nvme0n1", &d)
		if d.SizeBytes != 0 {
			t.Fatal("invalid size accepted", bad)
		}
	}
	f := storageHost()
	d := Disk{Serial: "contradictory-smart-serial"}
	collectDiskStorage(f, "nvme0n1", &d)
	if d.Serial != "" {
		t.Fatal("conflicting serial sources guessed")
	}
}

// A reattachment between sysfs reads must not leave a usable old diskseq.
type replacingDiskSource struct {
	hostSource
	reads int
}

func (r *replacingDiskSource) read(path string, limit int64) (string, error) {
	if path == "/sys/block/nvme0n1/diskseq" {
		r.reads++
		if r.reads > 1 {
			return "72", nil
		}
	}
	return r.hostSource.read(path, limit)
}
func TestCollectDiskStorageDetectsReplacementDuringCollection(t *testing.T) {
	source := &replacingDiskSource{hostSource: storageHost()}
	d := Disk{}
	collectDiskStorage(source, "nvme0n1", &d)
	if d.DiskSequence != 0 || d.InUse != nil {
		t.Fatal("replacement retained usable storage snapshot")
	}
}
