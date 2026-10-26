//go:build linux

package provisionwriter

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
	"github.com/enzoOvOyu/keel/internal/provisionseed"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
	"golang.org/x/sys/unix"
)

type LinuxInputs struct {
	StoragePlan      provisionstorage.Plan
	GPTIdentity      provisionstorage.GPTIdentity
	ManifestSHA256   string
	Manifest         []byte
	Preflight        []byte
	Seed             provisionseed.Seed
	Runtime          mediaboot.RuntimeSet
	RootFS           *os.File
	TargetUKI        *os.File
	Tools            map[string]*os.File
	ExpectedContents provisionimage.RootFSContentsEvidence
	FirstBootSecret  []byte
	BootCA           []byte
}

// Linux retains the whole-disk exclusive open for its lifetime. Two newly
// allocated, bounded loop mappings reference this same open file; it never
// releases the claim to reopen /dev/* partition names after a GPT rescan.
type Linux struct {
	mu               sync.Mutex
	plan             provisionstorage.Plan
	boot             mediaboot.Plan
	seed             provisionseed.Seed
	runtime          mediaboot.RuntimeSet
	request          provisionboot.WritePreparation
	gpt              *provisionstorage.GPTImage
	disk             *os.File
	rootfs           *os.File
	uki              *os.File
	self             *os.File
	tools            map[string]*os.File
	work             string
	loops            [2]*loopDevice
	mounted          [2]bool
	report           []byte
	consumed         bool
	started          bool
	completed        bool
	quarantined      bool
	nextStage        int
	permit           *provisionboot.WritePermit
	readback         *provisionboot.WriteReadback
	expectedContents provisionimage.RootFSContentsEvidence
	firstBootSecret  []byte
	bootCA           []byte
	rebootAttempted  bool
}

var heldUnknown = struct {
	sync.Mutex
	writers map[*Linux]struct{}
}{writers: map[*Linux]struct{}{}}

func requireInitramfs() error {
	if os.Getpid() != 1 || os.Geteuid() != 0 {
		return ErrDenied
	}
	var root unix.Statfs_t
	if unix.Statfs("/", &root) != nil || (root.Type != unix.RAMFS_MAGIC && root.Type != unix.TMPFS_MAGIC) {
		return ErrDenied
	}
	return nil
}

func digestBytes(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }

// NewLinux validates content and opens an exclusive device, but performs no
// block write, format, mount or tool execution. The final fresh report and
// independently issued live permit are still required before Run.
func NewLinux(ctx context.Context, input LinuxInputs) (_ *Linux, resultErr error) {
	if requireInitramfs() != nil || ctx.Err() != nil {
		return nil, ErrDenied
	}
	if checkLinuxCapabilities() != nil {
		return nil, ErrDenied
	}
	// Clone mutable caller-owned configuration before any IO.
	raw, err := json.Marshal(input.StoragePlan)
	if err != nil {
		return nil, ErrDenied
	}
	plan, err := provisionstorage.Decode(raw, time.Now())
	if err != nil {
		return nil, ErrDenied
	}
	manifest, spec, err := provisionboot.VerifyBootFiles(bytes.Clone(input.Manifest), input.ManifestSHA256, bytes.Clone(input.Preflight), input.Seed, time.Now())
	if err != nil || manifest.Media.ValidateWritable() != nil || manifest.Arch != runtime.GOARCH {
		return nil, ErrDenied
	}
	a, _ := json.Marshal(spec)
	b, _ := json.Marshal(plan.Spec)
	if !bytes.Equal(a, b) {
		return nil, ErrDenied
	}
	runtimeRaw, err := json.Marshal(input.Runtime)
	if err != nil {
		return nil, ErrDenied
	}
	runtimeSet, err := mediaboot.DecodeRuntimeSet(runtimeRaw)
	if err != nil || runtimeSet.Schema != mediaboot.WriterRuntimeSetSchema || runtimeSet.Writer == nil || runtimeSet.Arch != manifest.Arch {
		return nil, ErrDenied
	}
	image, err := provisionstorage.PrepareGPT(*plan, input.GPTIdentity, time.Now())
	if err != nil {
		return nil, ErrDenied
	}
	var rootfs mediaboot.Artifact
	for _, artifact := range manifest.Media.Artifacts {
		if artifact.Role == "rootfs" {
			rootfs = artifact
		}
	}
	w := &Linux{plan: *plan, boot: manifest.Media, seed: input.Seed, runtime: runtimeSet, gpt: image, tools: map[string]*os.File{}, expectedContents: input.ExpectedContents}
	defer func() {
		if resultErr != nil {
			_ = w.Close()
		}
	}()
	w.rootfs, err = duplicateSealed(input.RootFS, rootfs.Size, false)
	if err != nil || hashFile(ctx, w.rootfs, rootfs.Size) != rootfs.SHA256 {
		return nil, ErrDenied
	}
	target := manifest.Media.Profile.Recipe.TargetBoot
	w.uki, err = duplicateSealed(input.TargetUKI, target.Size, false)
	if err != nil {
		return nil, ErrDenied
	}
	if _, err = provisionimage.VerifyTargetUKI(ctx, w.uki, target.Size, target, manifest.Arch); err != nil {
		return nil, ErrDenied
	}
	for name, component := range map[string]mediaboot.RuntimeComponent{"unsquashfs": runtimeSet.Writer.UnsquashFS, "mkfs.ext4": runtimeSet.Writer.MkfsExt4, "mkfs.vfat": runtimeSet.Writer.MkfsVFAT} {
		file, err := duplicateSealed(input.Tools[name], 0, true)
		if err != nil {
			return nil, ErrDenied
		}
		w.tools[name] = file
		info, err := file.Stat()
		if err != nil || provisionimage.VerifyStaticBinary(ctx, provisionimage.Input{MediaID: component.MediaID, SHA256: component.SHA256, Size: info.Size(), File: file}, manifest.Arch) != nil {
			return nil, ErrDenied
		}
	}
	w.self, err = sealedSelf(ctx, runtimeSet)
	if err != nil {
		return nil, ErrDenied
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return nil, ErrDenied
	}
	w.request = provisionboot.WritePreparation{Nonce: hex.EncodeToString(nonce[:]), StoragePlanSHA256: plan.SHA256, GPTIdentity: input.GPTIdentity, GPTSHA256: image.SHA256(), RootFSSHA256: rootfs.SHA256, TargetUKISHA256: target.SHA256, RuntimeInputSHA256: digestBytes(runtimeRaw)}
	secretRaw := bytes.Clone(input.FirstBootSecret)
	secret, err := provisionfirstboot.DecodeSecret(secretRaw, time.Now())
	if err != nil {
		clear(secretRaw)
		return nil, ErrDenied
	}
	defer func() { secret.PrivateKey = "" }()
	ca := bytes.Clone(input.BootCA)
	if _, err = provisionimage.ParseBootRoots(ca, time.Now()); err != nil || digestBytes(ca) != runtimeSet.BootCA.SHA256 {
		clear(secretRaw)
		return nil, ErrDenied
	}
	binding := secret.Binding
	if binding.GrantID != manifest.GrantID || binding.StoragePlan.SHA256 != plan.SHA256 || binding.GPTIdentity != input.GPTIdentity || binding.BootCASHA256 != digestBytes(ca) || binding.TargetUKISHA256 != target.SHA256 || binding.RuntimeInputSHA256 != w.request.RuntimeInputSHA256 {
		clear(secretRaw)
		return nil, ErrDenied
	}
	w.firstBootSecret, w.bootCA = secretRaw, ca
	w.request.FirstBootBinding = &binding
	// Only the explicitly selected validated node can be opened; symlinks and
	// a block device already claimed by a filesystem/holder are refused.
	fd, err := unix.Open(plan.Device.Node, unix.O_RDWR|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrDenied
	}
	w.disk = os.NewFile(uintptr(fd), "explicit-selected-install-disk")
	if w.checkDisk() != nil {
		return nil, ErrDenied
	}
	var run unix.Statfs_t
	if unix.Statfs("/run", &run) != nil || run.Type != unix.TMPFS_MAGIC {
		return nil, ErrDenied
	}
	w.work, err = os.MkdirTemp("/run", "keel-writer-*")
	if err != nil {
		return nil, ErrDenied
	}
	for _, name := range []string{"root", "efi"} {
		if os.Mkdir(filepath.Join(w.work, name), 0700) != nil {
			return nil, ErrDenied
		}
	}
	return w, nil
}

func (w *Linux) Request() provisionboot.WritePreparation {
	if w == nil {
		return provisionboot.WritePreparation{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	raw, _ := json.Marshal(w.request)
	var copy provisionboot.WritePreparation
	if json.Unmarshal(raw, &copy) != nil {
		return provisionboot.WritePreparation{}
	}
	return copy
}

func (w *Linux) BindFreshReport(raw []byte) error {
	if w == nil || requireInitramfs() != nil {
		return ErrDenied
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.consumed || w.quarantined || w.disk == nil || provisionstorage.VerifyCurrent(w.plan, raw, time.Now()) != nil || w.checkDisk() != nil {
		return ErrDenied
	}
	w.report = bytes.Clone(raw)
	return nil
}

func (w *Linux) VerifyBeforeWrite(ctx context.Context, request provisionboot.WritePreparation) error {
	if w == nil || requireInitramfs() != nil || ctx.Err() != nil {
		return ErrDenied
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.consumed || w.quarantined || !samePreparation(w.request, request) || w.disk == nil || provisionstorage.VerifyCurrent(w.plan, w.report, time.Now()) != nil || w.checkDisk() != nil {
		return ErrDenied
	}
	w.consumed = true
	return nil
}

func (w *Linux) bindRun(request provisionboot.WritePreparation, permit *provisionboot.WritePermit) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.consumed || w.permit != nil || w.quarantined || !samePreparation(w.request, request) || !permitMatches(permit, request, time.Now()) {
		return ErrDenied
	}
	p := *permit
	w.permit = &p
	return nil
}

func (w *Linux) Quarantine() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.quarantined = true
	w.mu.Unlock()
	// Keep descriptors and mount ownership alive even if the initrd caller
	// returns to its PID1 wait loop. There is no automatic release/retry API.
	heldUnknown.Lock()
	heldUnknown.writers[w] = struct{}{}
	heldUnknown.Unlock()
}

// Close is safe only before any operation or after full verified completion.
// Unknown writers deliberately retain local resources until explicit recovery.
func (w *Linux) Close() error {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.quarantined || (w.started && !w.completed) {
		return ErrDenied
	}
	for _, mounted := range w.mounted {
		if mounted {
			return ErrDenied
		}
	}
	for i, l := range w.loops {
		if l != nil {
			if l.Close() != nil {
				w.quarantined = true
				heldUnknown.Lock()
				heldUnknown.writers[w] = struct{}{}
				heldUnknown.Unlock()
				return ErrDenied
			}
			w.loops[i] = nil
		}
	}
	for _, file := range []*os.File{w.disk, w.rootfs, w.uki, w.self} {
		if file != nil {
			_ = file.Close()
		}
	}
	w.disk, w.rootfs, w.uki, w.self = nil, nil, nil, nil
	for name, file := range w.tools {
		if file != nil {
			_ = file.Close()
		}
		delete(w.tools, name)
	}
	clear(w.firstBootSecret)
	w.firstBootSecret = nil
	if w.work != "" {
		_ = os.Remove(filepath.Join(w.work, "root"))
		_ = os.Remove(filepath.Join(w.work, "efi"))
		_ = os.Remove(w.work)
	}
	return nil
}

func duplicateSealed(source *os.File, size int64, executable bool) (*os.File, error) {
	if source == nil {
		return nil, ErrDenied
	}
	fd, err := unix.FcntlInt(source.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, ErrDenied
	}
	f := os.NewFile(uintptr(fd), "verified-sealed-input")
	failed := func() (*os.File, error) { _ = f.Close(); return nil, ErrDenied }
	var st unix.Stat_t
	seals, err := unix.FcntlInt(f.Fd(), unix.F_GET_SEALS, 0)
	want := unix.F_SEAL_WRITE | unix.F_SEAL_GROW | unix.F_SEAL_SHRINK | unix.F_SEAL_SEAL
	if err != nil || seals&want != want || unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&0222 != 0 || st.Size < 1 || (size > 0 && st.Size != size) || (executable && (st.Size > provisionimage.MaxBinaryBytes || st.Mode&0111 == 0)) {
		return failed()
	}
	return f, nil
}

func hashFile(ctx context.Context, f *os.File, size int64) string {
	if f == nil || size < 1 {
		return ""
	}
	h := sha256.New()
	buffer := make([]byte, 128<<10)
	var offset int64
	for offset < size {
		if ctx.Err() != nil {
			return ""
		}
		length := int64(len(buffer))
		if length > size-offset {
			length = size - offset
		}
		n, err := f.ReadAt(buffer[:length], offset)
		if err != nil || n != int(length) {
			return ""
		}
		_, _ = h.Write(buffer[:n])
		offset += int64(n)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func ioctlUint64(fd int, request uint) (uint64, error) {
	var value uint64
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(request), uintptr(unsafe.Pointer(&value)))
	if errno != 0 {
		return 0, errno
	}
	return value, nil
}

func (w *Linux) checkDisk() error {
	if w.disk == nil {
		return ErrDenied
	}
	fd := int(w.disk.Fd())
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFBLK || fmt.Sprintf("%d:%d", unix.Major(uint64(st.Rdev)), unix.Minor(uint64(st.Rdev))) != w.plan.Device.Number {
		return ErrDenied
	}
	size, err := ioctlUint64(fd, unix.BLKGETSIZE64)
	if err != nil || size != uint64(w.plan.Device.SizeBytes) {
		return ErrDenied
	}
	sequence, err := ioctlUint64(fd, unix.BLKGETDISKSEQ)
	if err != nil || sequence != w.plan.Device.Sequence {
		return ErrDenied
	}
	sector, err := unix.IoctlGetInt(fd, unix.BLKSSZGET)
	if err != nil || sector != w.plan.Device.LogicalBlockBytes {
		return ErrDenied
	}
	ro, err := unix.IoctlGetInt(fd, unix.BLKROGET)
	if err != nil || ro != 0 {
		return ErrDenied
	}
	base := "/sys/dev/block/" + w.plan.Device.Number
	if _, err = os.Stat(base + "/partition"); !os.IsNotExist(err) {
		return ErrDenied
	}
	read := func(path string) string {
		f, err := os.Open(path)
		if err != nil {
			return ""
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, 1025))
		if err != nil || len(b) > 1024 {
			return ""
		}
		return strings.TrimSpace(string(b))
	}
	wwid := read(base + "/wwid")
	if wwid == "" {
		wwid = read(base + "/device/wwid")
	}
	if wwid != w.plan.Device.WWID || read(base+"/device/serial") != w.plan.Device.Serial || read(base+"/removable") != "0" || read(base+"/diskseq") != strconv.FormatUint(w.plan.Device.Sequence, 10) {
		return ErrDenied
	}
	return nil
}

func checkLinuxCapabilities() error {
	var control unix.Stat_t
	if unix.Lstat("/dev/loop-control", &control) != nil || control.Mode&unix.S_IFMT != unix.S_IFCHR || control.Uid != 0 || unix.Major(uint64(control.Rdev)) != 10 || unix.Minor(uint64(control.Rdev)) != 237 {
		return ErrDenied
	}
	read := func(path string, limit int64) ([]byte, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, ErrDenied
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, limit+1))
		if err != nil || int64(len(b)) > limit {
			return nil, ErrDenied
		}
		return b, nil
	}
	maxPart, err := read("/sys/module/loop/parameters/max_part", 32)
	if err != nil || strings.TrimSpace(string(maxPart)) != "0" {
		return ErrDenied
	}
	filesystems, err := read("/proc/filesystems", 64<<10)
	if err != nil || !requiredFilesystems(filesystems) {
		return ErrDenied
	}
	return nil
}

func requiredFilesystems(raw []byte) bool {
	found := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 1 {
			found[fields[0]] = true
		} else if len(fields) == 2 && fields[0] == "nodev" {
			found[fields[1]] = true
		}
	}
	return found["ext4"] && found["vfat"] && found["tmpfs"] && found["proc"]
}
