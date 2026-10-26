//go:build linux

package provisionwriter

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
	"golang.org/x/sys/unix"
)

func (w *Linux) Perform(ctx context.Context, stage string) (*provisionboot.WriteReadback, error) {
	if w == nil || requireInitramfs() != nil || ctx.Err() != nil {
		return nil, ErrDenied
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.consumed || w.permit == nil || w.quarantined || w.completed || w.nextStage >= len(stages) || stages[w.nextStage] != stage || !w.permit.ExpiresAt.After(time.Now()) || w.checkDisk() != nil {
		return nil, ErrDenied
	}
	// The private bindRun capability can only be assigned by the checked Run
	// workflow, never by calling Perform directly after constructing an adapter.
	var readback *provisionboot.WriteReadback
	var err error
	switch stage {
	case "gpt.write":
		if provisionstorage.VerifyCurrent(w.plan, w.report, time.Now()) != nil {
			return nil, ErrDenied
		}
		w.started = true
		segments := w.gpt.Segments()
		// Backup first, then primary. Any partial attempt remains unknown.
		for _, index := range []int{1, 0} {
			segment := segments[index]
			for offset := 0; offset < len(segment.Data); {
				if ctx.Err() != nil || w.checkDisk() != nil {
					return nil, ErrDenied
				}
				end := offset + (64 << 10)
				if end > len(segment.Data) {
					end = len(segment.Data)
				}
				n, e := w.disk.WriteAt(segment.Data[offset:end], segment.Offset+int64(offset))
				if e != nil || n != end-offset {
					return nil, ErrDenied
				}
				offset = end
			}
		}
		if ctx.Err() != nil || w.disk.Sync() != nil {
			return nil, ErrDenied
		}
		_, err = provisionstorage.InspectGPTEdges(w.plan, w.request.GPTIdentity, w.disk, time.Now())
	case "efi.format", "root.format":
		index := 0
		name := "efi"
		tool := "mkfs.vfat"
		kind := "vfat"
		if stage == "root.format" {
			index = 1
			name = "root"
			tool = "mkfs.ext4"
			kind = "ext4"
		}
		w.loops[index], err = newLoop(w.disk, w.plan.Partitions[index], w.plan.Device.LogicalBlockBytes)
		if err != nil {
			return nil, ErrDenied
		}
		if err = w.runTool(ctx, tool, w.loops[index].file, nil); err != nil {
			return nil, err
		}
		if ctx.Err() != nil || w.checkDisk() != nil || w.loops[index].check() != nil {
			return nil, ErrDenied
		}
		source := fmt.Sprintf("/proc/self/fd/%d", w.loops[index].file.Fd())
		err = unix.Mount(source, filepath.Join(w.work, name), kind, unix.MS_NODEV|unix.MS_NOSUID|unix.MS_NOEXEC, "")
		if err == nil {
			w.mounted[index] = true
		}
	case "root.extract":
		root, openErr := openMountRoot(filepath.Join(w.work, "root"))
		if openErr != nil {
			return nil, ErrDenied
		}
		defer root.Close()
		err = w.runTool(ctx, "unsquashfs", w.rootfs, root)
		if err == nil {
			err = w.verifyExtracted(ctx)
		}
	case "boot.install":
		root, openErr := openMountRoot(filepath.Join(w.work, "efi"))
		if openErr != nil {
			return nil, ErrDenied
		}
		defer root.Close()
		if w.boot.Profile.Recipe.TargetBoot.Validate() != nil {
			return nil, ErrDenied
		}
		path, e := targetFallback(w.runtime.Arch)
		if e != nil {
			return nil, ErrDenied
		}
		err = writeOwned(ctx, root, path, io.NewSectionReader(w.uki, 0, w.boot.Profile.Recipe.TargetBoot.Size), w.boot.Profile.Recipe.TargetBoot.Size, 0600, false)
	case "seed.install":
		err = w.installSeed(ctx)
	case "final.sync":
		if err = w.verifyInstalled(ctx); err != nil {
			return nil, err
		}
		for _, name := range []string{"root", "efi"} {
			root, openErr := openMountRoot(filepath.Join(w.work, name))
			if openErr != nil {
				return nil, ErrDenied
			}
			e := unix.Syncfs(int(root.Fd()))
			_ = root.Close()
			if e != nil || ctx.Err() != nil {
				return nil, ErrDenied
			}
		}
		for index, name := range []string{"efi", "root"} {
			if ctx.Err() != nil {
				return nil, ErrDenied
			}
			if unix.Unmount(filepath.Join(w.work, name), 0) != nil {
				return nil, ErrDenied
			}
			w.mounted[index] = false
		}
		for _, loop := range w.loops {
			if loop == nil || loop.check() != nil || loop.file.Sync() != nil || ctx.Err() != nil {
				return nil, ErrDenied
			}
		}
		err = w.disk.Sync()
	case "final.verify":
		if w.readback == nil || w.mounted[0] || w.mounted[1] || w.verifyFilesystems() != nil {
			return nil, ErrDenied
		}
		got, e := provisionstorage.InspectGPTEdges(w.plan, w.request.GPTIdentity, w.disk, time.Now())
		if e != nil || got != w.request.GPTSHA256 {
			return nil, ErrDenied
		}
		readback = w.readback
	default:
		return nil, ErrDenied
	}
	if err != nil || ctx.Err() != nil || w.checkDisk() != nil {
		return nil, ErrDenied
	}
	w.nextStage++
	if stage == "final.verify" {
		w.completed = true
	}
	return readback, nil
}

func targetFallback(arch string) (string, error) {
	if arch == "amd64" {
		return "EFI/BOOT/BOOTX64.EFI", nil
	}
	if arch == "arm64" {
		return "EFI/BOOT/BOOTAA64.EFI", nil
	}
	return "", ErrDenied
}

func (w *Linux) verifyExtracted(ctx context.Context) error {
	root, err := openMountRoot(filepath.Join(w.work, "root"))
	if err != nil {
		return ErrDenied
	}
	defer root.Close()
	uki, err := openOwnedRead(root, w.boot.Profile.Recipe.TargetBoot.Path, w.boot.Profile.Recipe.TargetBoot.Size)
	if err != nil {
		return ErrDenied
	}
	defer uki.Close()
	actual, err := provisionimage.VerifyRootFSContents(ctx, w.boot, uki, mountedRootSource{root})
	if err != nil {
		return ErrDenied
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(w.expectedContents)
	if !bytes.Equal(a, b) {
		return ErrDenied
	}
	// Stale cloud-init instance state must not suppress this installation's
	// first-boot identity, account and SSH-key regeneration configuration.
	for _, path := range []string{"var/lib/cloud/instances", "var/lib/cloud/data"} {
		if !emptyDirectoryOrAbsent(root, path) {
			return ErrDenied
		}
	}
	if !absentOwned(root, "var/lib/cloud/instance") {
		return ErrDenied
	}
	return nil
}

func (w *Linux) installSeed(ctx context.Context) error {
	root, err := openMountRoot(filepath.Join(w.work, "root"))
	if err != nil {
		return ErrDenied
	}
	defer root.Close()
	if len(w.firstBootSecret) == 0 || w.request.FirstBootBinding == nil {
		return ErrDenied
	}
	secretDirectory, err := mkdirOwned(root, strings.TrimPrefix(provisionfirstboot.StateDirectory, "/"))
	if err != nil {
		return ErrDenied
	}
	err = unix.Fchmod(secretDirectory, 0700)
	_ = unix.Close(secretDirectory)
	if err != nil {
		return ErrDenied
	}
	fat := efiVolumeID(w.request.GPTIdentity.EFI)
	fstab := fmt.Sprintf("UUID=%s / ext4 defaults 0 1\nUUID=%s-%s /boot/efi vfat umask=0077 0 2\n", w.request.GPTIdentity.Root, strings.ToUpper(fat[:4]), strings.ToUpper(fat[4:]))
	for _, file := range []struct {
		path, data string
		mode       uint32
		replace    bool
	}{
		{"var/lib/cloud/seed/nocloud/user-data", w.seed.UserData, 0600, false},
		{"var/lib/cloud/seed/nocloud/meta-data", w.seed.MetaData, 0600, false},
		{"etc/fstab", fstab, 0644, true},
		{"etc/machine-id", "", 0444, true},
		{provisionfirstboot.UnitPath, provisionfirstboot.Unit, 0644, false},
	} {
		if writeOwned(ctx, root, file.path, strings.NewReader(file.data), int64(len(file.data)), file.mode, file.replace) != nil {
			return ErrDenied
		}
	}
	for _, file := range []struct {
		path string
		data []byte
	}{
		{strings.TrimPrefix(provisionfirstboot.BindingPath, "/"), w.firstBootSecret},
		{strings.TrimPrefix(provisionfirstboot.CAPath, "/"), w.bootCA},
	} {
		if writeOwned(ctx, root, file.path, bytes.NewReader(file.data), int64(len(file.data)), 0600, false) != nil {
			return ErrDenied
		}
	}
	if symlinkOwned(root, provisionfirstboot.UnitEnablePath, provisionfirstboot.UnitEnableTarget) != nil {
		return ErrDenied
	}
	// The mount point is created without following image-provided symlinks.
	fd, err := mkdirOwned(root, "boot/efi")
	if err != nil {
		return ErrDenied
	}
	return unix.Close(fd)
}

type mountedRootSource struct{ root *os.File }

func (s mountedRootSource) ReadFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	f, err := openOwnedRead(s.root, path, limit)
	if err != nil {
		return nil, ErrDenied
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit || ctx.Err() != nil {
		return nil, ErrDenied
	}
	return b, nil
}

func (w *Linux) verifyInstalled(ctx context.Context) error {
	if err := w.verifyExtractedAfterSeed(ctx); err != nil {
		return err
	}
	root, err := openMountRoot(filepath.Join(w.work, "root"))
	if err != nil {
		return ErrDenied
	}
	defer root.Close()
	for _, file := range []struct{ path, want string }{{"var/lib/cloud/seed/nocloud/user-data", w.seed.UserData}, {"var/lib/cloud/seed/nocloud/meta-data", w.seed.MetaData}} {
		got, err := (mountedRootSource{root}).ReadFile(ctx, file.path, int64(len(file.want)))
		if err != nil || string(got) != file.want {
			return ErrDenied
		}
	}
	for _, file := range []struct {
		path string
		want []byte
	}{{strings.TrimPrefix(provisionfirstboot.BindingPath, "/"), w.firstBootSecret}, {strings.TrimPrefix(provisionfirstboot.CAPath, "/"), w.bootCA}, {provisionfirstboot.UnitPath, []byte(provisionfirstboot.Unit)}} {
		got, e := (mountedRootSource{root}).ReadFile(ctx, file.path, int64(len(file.want)))
		if e != nil || !bytes.Equal(got, file.want) {
			clear(got)
			return ErrDenied
		}
		clear(got)
	}
	if checkOwnedSymlink(root, provisionfirstboot.UnitEnablePath, provisionfirstboot.UnitEnableTarget) != nil {
		return ErrDenied
	}
	fat := efiVolumeID(w.request.GPTIdentity.EFI)
	fstab := fmt.Sprintf("UUID=%s / ext4 defaults 0 1\nUUID=%s-%s /boot/efi vfat umask=0077 0 2\n", w.request.GPTIdentity.Root, strings.ToUpper(fat[:4]), strings.ToUpper(fat[4:]))
	got, err := (mountedRootSource{root}).ReadFile(ctx, "etc/fstab", int64(len(fstab)))
	if err != nil || string(got) != fstab {
		return ErrDenied
	}
	machineID, err := openOwnedRead(root, "etc/machine-id", 1)
	if err != nil {
		return ErrDenied
	}
	info, e := machineID.Stat()
	_ = machineID.Close()
	if e != nil || info.Size() != 0 {
		return ErrDenied
	}
	efi, err := openMountRoot(filepath.Join(w.work, "efi"))
	if err != nil {
		return ErrDenied
	}
	defer efi.Close()
	fallback, err := targetFallback(w.runtime.Arch)
	if err != nil {
		return ErrDenied
	}
	target := w.boot.Profile.Recipe.TargetBoot
	f, err := openOwnedRead(efi, fallback, target.Size)
	if err != nil {
		return ErrDenied
	}
	defer f.Close()
	if hashFile(ctx, f, target.Size) != target.SHA256 {
		return ErrDenied
	}
	w.readback = &provisionboot.WriteReadback{GPTSHA256: w.request.GPTSHA256, RootFSSHA256: w.request.RootFSSHA256, TargetUKISHA256: w.request.TargetUKISHA256, SSHHelperSHA256: w.boot.Profile.Recipe.SSHHelperSHA256, UserDataSHA256: digestBytes([]byte(w.seed.UserData)), MetaDataSHA256: digestBytes([]byte(w.seed.MetaData))}
	return nil
}

func (w *Linux) verifyExtractedAfterSeed(ctx context.Context) error {
	root, err := openMountRoot(filepath.Join(w.work, "root"))
	if err != nil {
		return ErrDenied
	}
	defer root.Close()
	uki, err := openOwnedRead(root, w.boot.Profile.Recipe.TargetBoot.Path, w.boot.Profile.Recipe.TargetBoot.Size)
	if err != nil {
		return ErrDenied
	}
	defer uki.Close()
	actual, err := provisionimage.VerifyRootFSContents(ctx, w.boot, uki, mountedRootSource{root})
	if err != nil {
		return ErrDenied
	}
	a, _ := json.Marshal(actual)
	b, _ := json.Marshal(w.expectedContents)
	if !bytes.Equal(a, b) {
		return ErrDenied
	}
	return nil
}

func efiVolumeID(uuid string) string { return digestBytes([]byte("Keel EFI volume v1\x00" + uuid))[:8] }

func (w *Linux) verifyFilesystems() error {
	for _, loop := range w.loops {
		if loop == nil || loop.check() != nil {
			return ErrDenied
		}
	}
	// Read native filesystem identities after clean unmount. This verifies
	// labels/UUID/geometry, not every inode or the boot behavior of an OS.
	efi := make([]byte, 512)
	if n, err := w.loops[0].file.ReadAt(efi, 0); err != nil || n != len(efi) {
		return ErrDenied
	}
	fatID, err := hex.DecodeString(efiVolumeID(w.request.GPTIdentity.EFI))
	if err != nil {
		return ErrDenied
	}
	id := binary.BigEndian.Uint32(fatID)
	// Linux fat_set_state marks bit zero of the FAT32 extended BPB state
	// byte while mounted and clears it on clean unmount.
	if efi[510] != 0x55 || efi[511] != 0xaa || efi[65]&1 != 0 || int(binary.LittleEndian.Uint16(efi[11:13])) != w.plan.Device.LogicalBlockBytes || efi[13] != 1 || string(efi[82:90]) != "FAT32   " || binary.LittleEndian.Uint32(efi[67:71]) != id || string(efi[71:82]) != "KEEL_EFI   " {
		return ErrDenied
	}
	sector := int64(w.plan.Device.LogicalBlockBytes)
	reserved := uint32(binary.LittleEndian.Uint16(efi[14:16]))
	fatSectors := binary.LittleEndian.Uint32(efi[36:40])
	totalSectors := binary.LittleEndian.Uint32(efi[32:36])
	if reserved == 0 || efi[16] != 2 || fatSectors == 0 || int64(totalSectors)*sector != w.plan.Partitions[0].SizeBytes || uint64(reserved)+2*uint64(fatSectors) >= uint64(totalSectors) {
		return ErrDenied
	}
	// Also reject either FAT copy carrying a dirty or IO-error indication.
	for copyIndex := int64(0); copyIndex < 2; copyIndex++ {
		var flags [4]byte
		offset := (int64(reserved)+copyIndex*int64(fatSectors))*sector + 4
		if n, err := w.loops[0].file.ReadAt(flags[:], offset); err != nil || n != len(flags) || binary.LittleEndian.Uint32(flags[:])&0x0c000000 != 0x0c000000 {
			return ErrDenied
		}
	}
	root := make([]byte, 1024)
	if n, err := w.loops[1].file.ReadAt(root, 1024); err != nil || n != len(root) {
		return ErrDenied
	}
	uuid, err := hex.DecodeString(strings.ReplaceAll(w.request.GPTIdentity.Root, "-", ""))
	if err != nil {
		return ErrDenied
	}
	blocks := uint64(binary.LittleEndian.Uint32(root[4:8])) | uint64(binary.LittleEndian.Uint32(root[0x150:0x154]))<<32
	if binary.LittleEndian.Uint16(root[56:58]) != 0xef53 || binary.LittleEndian.Uint16(root[58:60]) != 1 || binary.LittleEndian.Uint32(root[0x194:0x198]) != 0 || binary.LittleEndian.Uint32(root[24:28]) != 2 || !bytes.Equal(root[104:120], uuid) || string(bytes.TrimRight(root[120:136], "\x00")) != "keel-root" || blocks != uint64(w.plan.Partitions[1].SizeBytes/4096) {
		return ErrDenied
	}
	return nil
}
