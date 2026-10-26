//go:build linux

package provisionfirstboot

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

// LinuxCollector reads an already booted installed OS. It never executes a
// command, writes/generates/replaces a host key, mounts a disk or changes trust.
// cloud-final must have regenerated the sole Ed25519 host key before this unit.
type LinuxCollector struct{}

func (LinuxCollector) Collect(ctx context.Context, b Binding) (Report, error) {
	var out Report
	if os.Geteuid() != 0 || b.Validate(time.Now()) != nil || ctx.Err() != nil {
		return out, ErrDenied
	}
	var rootFS unix.Statfs_t
	if unix.Statfs("/", &rootFS) != nil || rootFS.Type != unix.EXT4_SUPER_MAGIC {
		return out, ErrDenied
	}
	kernel, err := readSmall("/proc/sys/kernel/random/boot_id", 64)
	if err != nil || !bootID.MatchString(kernel) || kernel == b.InstallerKernelBootID {
		return out, ErrDenied
	}
	name, err := os.Hostname()
	if err != nil {
		return out, ErrDenied
	}
	serial, err := readSmall("/sys/class/dmi/id/product_serial", 256)
	if err != nil || serial == "" {
		serial, err = readSmall("/sys/firmware/devicetree/base/serial-number", 256)
	}
	if err != nil || serial != b.StoragePlan.Spec.Expected.Serial {
		return out, ErrDenied
	}
	iface, _, err := FindNetwork(b)
	if err != nil || iface.HardwareAddr.String() != b.StoragePlan.Spec.Expected.BootMAC {
		return out, ErrDenied
	}
	rootNumber, efiNumber, err := installedMounts()
	if err != nil {
		return out, ErrDenied
	}
	rootPath, err := sysPartition(rootNumber, 2)
	if err != nil {
		return out, ErrDenied
	}
	efiPath, err := sysPartition(efiNumber, 1)
	if err != nil || filepath.Dir(rootPath) != filepath.Dir(efiPath) {
		return out, ErrDenied
	}
	if len(b.StoragePlan.Partitions) != 2 || partitionGeometry(rootPath, b.StoragePlan.Partitions[1]) != nil || partitionGeometry(efiPath, b.StoragePlan.Partitions[0]) != nil {
		return out, ErrDenied
	}
	diskPath := filepath.Dir(rootPath)
	wwid, err := readSmall(diskPath+"/wwid", 512)
	if err != nil || wwid == "" {
		wwid, err = readSmall(diskPath+"/device/wwid", 512)
	}
	if err != nil || wwid != b.StoragePlan.Device.WWID {
		return out, ErrDenied
	}
	diskSerial, err := readSmall(diskPath+"/device/serial", 256)
	if err != nil || diskSerial != b.StoragePlan.Device.Serial {
		return out, ErrDenied
	}
	diskNumber, err := readSmall(diskPath+"/dev", 32)
	if err != nil {
		return out, ErrDenied
	}
	disk, err := openDisk(diskPath, diskNumber)
	if err != nil {
		return out, ErrDenied
	}
	defer disk.Close()
	size, err := ioctl64(int(disk.Fd()), unix.BLKGETSIZE64)
	if err != nil || size != uint64(b.StoragePlan.Device.SizeBytes) {
		return out, ErrDenied
	}
	logical, err := unix.IoctlGetInt(int(disk.Fd()), unix.BLKSSZGET)
	if err != nil || logical != b.StoragePlan.Device.LogicalBlockBytes {
		return out, ErrDenied
	}
	sequence, err := ioctl64(int(disk.Fd()), unix.BLKGETDISKSEQ)
	if err != nil || sequence == 0 {
		return out, ErrDenied
	}
	// Device number/diskseq may legitimately change across a reboot. Stable
	// WWID/serial/geometry plus both exact GPT regions bind the installed disk.
	if _, err = provisionstorage.InspectGPTEdges(b.StoragePlan, b.GPTIdentity, disk, time.Now()); err != nil {
		return out, ErrDenied
	}
	if after, err := ioctl64(int(disk.Fd()), unix.BLKGETDISKSEQ); err != nil || after != sequence {
		return out, ErrDenied
	}
	fallback, err := mediaboot.TargetEFIFallback(b.StoragePlan.Spec.Expected.Arch)
	if err != nil {
		return out, ErrDenied
	}
	if err = hashInstalledUKI("/boot/efi/"+fallback, b.TargetUKISHA256); err != nil {
		return out, err
	}
	public, fingerprint, modified, err := readHostKey(b.CreatedAt)
	if err != nil || ctx.Err() != nil {
		return out, ErrDenied
	}
	return Report{ObservedAt: time.Now().UTC().Truncate(time.Microsecond), KernelBootID: kernel, Hostname: name, Serial: serial, BootMAC: iface.HardwareAddr.String(), Arch: runtime.GOARCH, DiskWWID: wwid, DiskSerial: diskSerial, DiskSizeBytes: int64(size), LogicalBlockBytes: logical, RootPartitionGUID: b.GPTIdentity.Root, EFIPartitionGUID: b.GPTIdentity.EFI, HostPublicKey: public, HostFingerprint: fingerprint, HostKeyModifiedAt: modified}, nil
}

func readSmall(path string, limit int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", ErrDenied
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return "", ErrDenied
	}
	return strings.TrimSpace(strings.TrimRight(string(raw), "\x00")), nil
}

func installedMounts() (string, string, error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return "", "", ErrDenied
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 {
		return "", "", ErrDenied
	}
	root, efi := "", ""
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		sep := 0
		for i := 6; i < len(fields); i++ {
			if fields[i] == "-" {
				sep = i
				break
			}
		}
		if sep < 6 || len(fields) < sep+4 {
			continue
		}
		if fields[4] == "/" {
			if root != "" || fields[3] != "/" || fields[sep+1] != "ext4" {
				return "", "", ErrDenied
			}
			root = fields[2]
		}
		if fields[4] == "/boot/efi" {
			if efi != "" || fields[3] != "/" || fields[sep+1] != "vfat" {
				return "", "", ErrDenied
			}
			efi = fields[2]
		}
	}
	if root == "" || efi == "" {
		return "", "", ErrDenied
	}
	for path, want := range map[string]string{"/": root, "/boot/efi": efi} {
		var st unix.Stat_t
		if unix.Stat(path, &st) != nil || fmt.Sprintf("%d:%d", unix.Major(st.Dev), unix.Minor(st.Dev)) != want {
			return "", "", ErrDenied
		}
	}
	return root, efi, nil
}

func partitionGeometry(path string, part provisionstorage.Partition) error {
	start, err := readSmall(path+"/start", 32)
	if err != nil {
		return ErrDenied
	}
	size, err := readSmall(path+"/size", 32)
	if err != nil {
		return ErrDenied
	}
	a, err := strconv.ParseUint(start, 10, 64)
	if err != nil || strconv.FormatUint(a, 10) != start {
		return ErrDenied
	}
	b, err := strconv.ParseUint(size, 10, 64)
	if err != nil || strconv.FormatUint(b, 10) != size {
		return ErrDenied
	}
	if part.StartBytes%512 != 0 || part.SizeBytes%512 != 0 || a != uint64(part.StartBytes/512) || b != uint64(part.SizeBytes/512) {
		return ErrDenied
	}
	return nil
}

func sysPartition(number string, partition int) (string, error) {
	var major, minor uint32
	if _, err := fmt.Sscanf(number, "%d:%d", &major, &minor); err != nil || fmt.Sprintf("%d:%d", major, minor) != number {
		return "", ErrDenied
	}
	path, err := filepath.EvalSymlinks("/sys/dev/block/" + number)
	if err != nil || !strings.HasPrefix(path, "/sys/devices/") {
		return "", ErrDenied
	}
	part, err := readSmall(path+"/partition", 16)
	if err != nil || part != strconv.Itoa(partition) {
		return "", ErrDenied
	}
	return path, nil
}

func openDisk(sysPath, number string) (*os.File, error) {
	uevent, err := readSmall(sysPath+"/uevent", 4096)
	if err != nil {
		return nil, ErrDenied
	}
	name := ""
	for _, line := range strings.Split(uevent, "\n") {
		if strings.HasPrefix(line, "DEVNAME=") {
			if name != "" {
				return nil, ErrDenied
			}
			name = strings.TrimPrefix(line, "DEVNAME=")
		}
	}
	if !regexpDisk.MatchString(name) {
		return nil, ErrDenied
	}
	fd, err := unix.Open("/dev/"+name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrDenied
	}
	f := os.NewFile(uintptr(fd), "installed-root-disk")
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFBLK || fmt.Sprintf("%d:%d", unix.Major(st.Rdev), unix.Minor(st.Rdev)) != number {
		f.Close()
		return nil, ErrDenied
	}
	return f, nil
}

func ioctl64(fd int, request uint) (uint64, error) {
	var value uint64
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(request), uintptr(unsafe.Pointer(&value)))
	if errno != 0 {
		return 0, ErrDenied
	}
	return value, nil
}

func readHostKey(created time.Time) (string, string, time.Time, error) {
	raw, stat, err := readProtected("/etc/ssh/ssh_host_ed25519_key", 16<<10, true)
	if err != nil {
		return "", "", time.Time{}, ErrDenied
	}
	defer clear(raw)
	modified := time.Unix(stat.Mtim.Sec, stat.Mtim.Nsec).UTC()
	if modified.Before(created) {
		return "", "", time.Time{}, ErrDenied
	}
	private, err := ssh.ParseRawPrivateKey(raw)
	if err != nil {
		return "", "", time.Time{}, ErrDenied
	}
	var key ed25519.PrivateKey
	switch typed := private.(type) {
	case *ed25519.PrivateKey:
		key = *typed
	case ed25519.PrivateKey:
		key = typed
	default:
		return "", "", time.Time{}, ErrDenied
	}
	defer clear(key)
	public, err := ssh.NewPublicKey(key.Public())
	if err != nil {
		return "", "", time.Time{}, ErrDenied
	}
	pubRaw, _, err := readProtected("/etc/ssh/ssh_host_ed25519_key.pub", 4096, false)
	if err != nil {
		return "", "", time.Time{}, ErrDenied
	}
	parsed, _, options, rest, err := ssh.ParseAuthorizedKey(pubRaw)
	if err != nil || len(options) != 0 || len(rest) != 0 || !bytesEqual(parsed.Marshal(), public.Marshal()) {
		return "", "", time.Time{}, ErrDenied
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public))), ssh.FingerprintSHA256(public), modified, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
