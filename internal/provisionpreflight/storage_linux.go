//go:build linux

package provisionpreflight

import (
	"fmt"
	"strconv"
	"strings"
)

// collectDiskStorage adds read-only facts, never eligibility or a disk choice.
// Missing geometry/usage remains explicit unknown without rewriting SMART.
func collectDiskStorage(source hostSource, name string, disk *Disk) {
	if !diskName.MatchString(name) {
		return
	}
	base := "/sys/block/" + name
	for _, path := range []string{base + "/wwid", base + "/device/wwid"} {
		if value, err := source.read(path, 512); err == nil && value != "" {
			disk.WWID = value
			break
		}
	}
	if serial, err := source.read(base+"/device/serial", 256); err == nil && serial != "" {
		if disk.Serial == "" || disk.Serial == serial {
			disk.Serial = serial
		} else {
			disk.Serial = ""
		}
	}
	// The block size attribute is a count of 512-byte sectors, including 4Kn
	// devices. queue/logical_block_size is a separate addressability constraint.
	if sectors, ok := diskUint(source, base+"/size"); ok && sectors > 0 && sectors <= (1<<50)/512 {
		disk.SizeBytes = int64(sectors * 512)
	}
	if logical, ok := diskUint(source, base+"/queue/logical_block_size"); ok && (logical == 512 || logical == 4096) {
		disk.LogicalBlockBytes = int(logical)
	}
	if sequence, ok := diskUint(source, base+"/diskseq"); ok && sequence > 0 {
		disk.DiskSequence = sequence
	}
	disk.ReadOnly = diskFlag(source, base+"/ro")
	disk.Removable = diskFlag(source, base+"/removable")
	if device, err := source.read(base+"/dev", 32); err == nil && validDeviceNumber(device) {
		disk.DeviceNumber = device
	}
	disk.InUse = diskUsage(source, base, disk.DeviceNumber)
	// Detect replacement/resizing across this multi-file snapshot. The real
	// writer still needs an exclusive block-device fd and immediate ioctls.
	sequence, sequenceOK := diskUint(source, base+"/diskseq")
	sectors, sizeOK := diskUint(source, base+"/size")
	logical, logicalOK := diskUint(source, base+"/queue/logical_block_size")
	number, numberErr := source.read(base+"/dev", 32)
	if !sequenceOK || sequence != disk.DiskSequence || sequence == 0 || !sizeOK || sectors > (1<<50)/512 || int64(sectors*512) != disk.SizeBytes || !logicalOK || logical != uint64(disk.LogicalBlockBytes) || numberErr != nil || number != disk.DeviceNumber {
		disk.DiskSequence = 0
		disk.InUse = nil
	}

}
func diskUint(source hostSource, path string) (uint64, bool) {
	raw, err := source.read(path, 32)
	value, parse := strconv.ParseUint(raw, 10, 64)
	return value, err == nil && parse == nil && strconv.FormatUint(value, 10) == raw
}
func diskFlag(source hostSource, path string) *bool {
	n, ok := diskUint(source, path)
	if !ok || n > 1 {
		return nil
	}
	value := n == 1
	return &value
}
func validDeviceNumber(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return false
	}
	major, err := strconv.ParseUint(parts[0], 10, 12)
	minor, err2 := strconv.ParseUint(parts[1], 10, 20)
	return err == nil && err2 == nil && fmt.Sprintf("%d:%d", major, minor) == value
}

// Usage is known only when all whole-disk/partition holders and the current
// mount/swap tables are readable. Any active swap blocks every candidate: no
// guessing through swap aliases or files. The accepted initrd must run in the
// real host mount namespace, not a container that hides existing mounts.
func diskUsage(source hostSource, base, device string) *bool {
	if !validDeviceNumber(device) {
		return nil
	}
	devices := map[string]bool{device: true}
	paths := []string{base}
	names, err := source.names(base, 512)
	if err != nil {
		return nil
	}
	for _, child := range names {
		if !netName.MatchString(child) || child == "." || child == ".." {
			return nil
		}
		path := base + "/" + child
		partition, err := source.exists(path + "/partition")
		if err != nil {
			return nil
		}
		if !partition {
			continue
		}
		number, err := source.read(path+"/dev", 32)
		if err != nil || !validDeviceNumber(number) || devices[number] {
			return nil
		}
		devices[number] = true
		paths = append(paths, path)
	}
	busy := false
	for _, path := range paths {
		holders, err := source.names(path+"/holders", 128)
		if err != nil {
			return nil
		}
		busy = busy || len(holders) > 0
	}
	mounts, err := source.read("/proc/self/mountinfo", 1<<20)
	if err != nil || mounts == "" {
		return nil
	}
	for _, line := range strings.Split(mounts, "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 || !validDeviceNumber(fields[2]) {
			return nil
		}
		separator := -1
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				separator = i
				break
			}
		}
		if separator < 6 || len(fields)-separator != 4 {
			return nil
		}
		busy = busy || devices[fields[2]]
	}
	swaps, err := source.read("/proc/swaps", 1<<20)
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(swaps), "\n")
	header := strings.Fields(lines[0])
	if len(header) != 5 || strings.Join(header, " ") != "Filename Type Size Used Priority" {
		return nil
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) != "" {
			busy = true
		}
	}
	return &busy
}
