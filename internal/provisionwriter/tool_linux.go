//go:build linux

package provisionwriter

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
	"golang.org/x/sys/unix"
)

const childSchema = "keel.private-writer-tool/v1"

var childJail = regexp.MustCompile(`^/run/keel-writer-[0-9]{1,16}/jail-[a-f0-9]{32}$`)
var childNonce = regexp.MustCompile(`^[a-f0-9]{32}$`)

type toolRequest struct {
	Schema         string                     `json:"schema"`
	Nonce          string                     `json:"nonce"`
	Operation      string                     `json:"operation"`
	Arch           string                     `json:"arch"`
	Tool           mediaboot.RuntimeComponent `json:"tool"`
	ToolSize       int64                      `json:"tool_size"`
	Self           mediaboot.RuntimeComponent `json:"self"`
	SelfSize       int64                      `json:"self_size"`
	InputSize      int64                      `json:"input_size"`
	InputSHA256    string                     `json:"input_sha256"`
	Sector         int                        `json:"sector"`
	Offset         int64                      `json:"offset"`
	Size           int64                      `json:"size"`
	BackingDevice  uint64                     `json:"backing_device"`
	BackingRdevice uint64                     `json:"backing_rdevice"`
	BackingInode   uint64                     `json:"backing_inode"`
	RootUUID       string                     `json:"root_uuid"`
	FATID          string                     `json:"fat_id"`
	Jail           string                     `json:"jail"`
	CreatedAt      time.Time                  `json:"created_at"`
	StartBefore    time.Time                  `json:"start_before"`
}

func sealBytes(name string, raw []byte, executable bool) (*os.File, error) {
	flags := unix.MFD_CLOEXEC | unix.MFD_ALLOW_SEALING
	if executable {
		flags |= unix.MFD_EXEC
	} else {
		flags |= unix.MFD_NOEXEC_SEAL
	}
	fd, err := unix.MemfdCreate(name, flags)
	if err != nil {
		return nil, ErrDenied
	}
	f := os.NewFile(uintptr(fd), name)
	failed := func() (*os.File, error) { f.Close(); return nil, ErrDenied }
	if n, e := f.Write(raw); e != nil || n != len(raw) {
		return failed()
	}
	mode := os.FileMode(0400)
	if executable {
		mode = 0500
	}
	if f.Chmod(mode) != nil {
		return failed()
	}
	if _, err = unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return failed()
	}
	// Reopen read-only; sealing alone must not leave a writable descriptor in
	// the child that can be redirected to a different kind of input.
	ro, err := unix.Open("/proc/self/fd/"+strconv.Itoa(fd), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return failed()
	}
	f.Close()
	return os.NewFile(uintptr(ro), name), nil
}

func sealedSelf(ctx context.Context, set mediaboot.RuntimeSet) (*os.File, error) {
	fd, err := unix.Open("/proc/self/exe", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrDenied
	}
	f := os.NewFile(uintptr(fd), "accepted-installer-init")
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > provisionimage.MaxBinaryBytes {
		return nil, ErrDenied
	}
	raw, err := io.ReadAll(io.LimitReader(f, provisionimage.MaxBinaryBytes+1))
	if err != nil || int64(len(raw)) != info.Size() || provisionimage.VerifyStaticBinary(ctx, provisionimage.Input{MediaID: set.Init.MediaID, SHA256: set.Init.SHA256, Size: info.Size(), File: bytes.NewReader(raw)}, set.Arch) != nil {
		return nil, ErrDenied
	}
	return sealBytes("keel-writer-private-init", raw, true)
}

func (w *Linux) runTool(ctx context.Context, name string, input, targetRoot *os.File) error {
	if ctx.Err() != nil || w.self == nil || w.permit == nil || !w.permit.ExpiresAt.After(time.Now()) || input == nil {
		return ErrDenied
	}
	tool := w.tools[name]
	if tool == nil {
		return ErrDenied
	}
	toolInfo, err := tool.Stat()
	if err != nil {
		return ErrDenied
	}
	selfInfo, err := w.self.Stat()
	if err != nil {
		return ErrDenied
	}
	var nonce [16]byte
	if _, err = rand.Read(nonce[:]); err != nil {
		return ErrDenied
	}
	nonceText := hex.EncodeToString(nonce[:])
	jail := filepath.Join(w.work, "jail-"+nonceText)
	if !childJail.MatchString(jail) || os.Mkdir(jail, 0700) != nil {
		return ErrDenied
	}
	defer os.Remove(jail)
	now := time.Now().UTC()
	c := toolRequest{Schema: childSchema, Nonce: nonceText, Operation: name, Arch: w.runtime.Arch, ToolSize: toolInfo.Size(), Self: w.runtime.Init, SelfSize: selfInfo.Size(), Sector: w.plan.Device.LogicalBlockBytes, RootUUID: w.request.GPTIdentity.Root, FATID: efiVolumeID(w.request.GPTIdentity.EFI), Jail: jail, CreatedAt: now, StartBefore: now.Add(3 * time.Second)}
	switch name {
	case "unsquashfs":
		c.Tool = w.runtime.Writer.UnsquashFS
		c.InputSHA256 = w.request.RootFSSHA256
		info, e := input.Stat()
		if e != nil {
			return ErrDenied
		}
		c.InputSize = info.Size()
	case "mkfs.ext4", "mkfs.vfat":
		index := 0
		c.Tool = w.runtime.Writer.MkfsVFAT
		if name == "mkfs.ext4" {
			index = 1
			c.Tool = w.runtime.Writer.MkfsExt4
		}
		loop := w.loops[index]
		if loop == nil || loop.check() != nil {
			return ErrDenied
		}
		c.Offset, c.Size = loop.partition.StartBytes, loop.partition.SizeBytes
		c.BackingDevice, c.BackingRdevice, c.BackingInode = uint64(loop.backing.Dev), uint64(loop.backing.Rdev), loop.backing.Ino
	default:
		return ErrDenied
	}
	if targetRoot == nil {
		targetRoot, err = sealBytes("keel-writer-unused-root", []byte("{}"), false)
		if err != nil {
			return ErrDenied
		}
		defer targetRoot.Close()
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return ErrDenied
	}
	config, err := sealBytes("keel-writer-private-operation", raw, false)
	if err != nil {
		return ErrDenied
	}
	defer config.Close()
	command := exec.CommandContext(ctx, "/proc/self/fd/3", "--keel-writer-tool", name)
	command.Args[0] = "keel-writer-private-init"
	command.ExtraFiles = []*os.File{w.self, tool, input, targetRoot, config}
	command.Env = []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	command.Stdin = nil
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	command.SysProcAttr = &syscall.SysProcAttr{Cloneflags: unix.CLONE_NEWNS | unix.CLONE_NEWNET | unix.CLONE_NEWPID | unix.CLONE_NEWIPC | unix.CLONE_NEWUTS}
	command.WaitDelay = 2 * time.Second
	// Killing the PID namespace init also kills any descendants. No ordinary
	// host process or archive-supplied executable participates in this path.
	if command.Run() != nil || ctx.Err() != nil {
		return ErrDenied
	}
	return nil
}

// RunToolChild is dispatched before the normal initrd main. It is an internal
// process boundary, not a CLI that authorizes disk writes. Ordinary host calls
// fail before mounts/execution. The parent constructs sealed descriptors only
// after live authorization; the child validates them and executes fixed argv.
func RunToolChild(args []string) (bool, int) {
	if len(args) == 0 || args[0] != "--keel-writer-tool" {
		return false, 0
	}
	if len(args) != 2 {
		return true, 125
	}
	if runToolChild(args[1]) != nil {
		return true, 125
	}
	return true, 0
}

func runToolChild(operation string) error {
	if requireInitramfs() != nil || os.Getppid() != 0 {
		return ErrDenied
	}
	if operation != "unsquashfs" && operation != "mkfs.ext4" && operation != "mkfs.vfat" {
		return ErrDenied
	}
	runtime.LockOSThread()
	config := os.NewFile(7, "sealed-writer-operation")
	if config == nil {
		return ErrDenied
	}
	configCopy, err := duplicateSealed(config, 0, false)
	if err != nil {
		return ErrDenied
	}
	defer configCopy.Close()
	info, err := configCopy.Stat()
	if err != nil || info.Size() > 16<<10 {
		return ErrDenied
	}
	raw, err := io.ReadAll(io.LimitReader(configCopy, (16<<10)+1))
	if err != nil || len(raw) > 16<<10 {
		return ErrDenied
	}
	var c toolRequest
	if json.Unmarshal(raw, &c) != nil {
		return ErrDenied
	}
	canonical, _ := json.Marshal(c)
	now := time.Now()
	if !bytes.Equal(raw, canonical) || c.Schema != childSchema || !childNonce.MatchString(c.Nonce) || c.Operation != operation || c.Arch != runtime.GOARCH || !childJail.MatchString(c.Jail) || !strings.HasSuffix(c.Jail, "/jail-"+c.Nonce) || c.CreatedAt.After(now.Add(time.Second)) || !c.StartBefore.After(now) || c.StartBefore.Sub(c.CreatedAt) > 3*time.Second || c.CreatedAt.Before(now.Add(-3*time.Second)) || (c.Sector != 512 && c.Sector != 4096) {
		return ErrDenied
	}
	self := os.NewFile(3, "sealed-writer-self")
	tool := os.NewFile(4, "sealed-writer-tool")
	input := os.NewFile(5, "bounded-writer-input")
	target := os.NewFile(6, "bounded-writer-root")
	defer func() {
		runtime.KeepAlive(self)
		runtime.KeepAlive(tool)
		runtime.KeepAlive(input)
		runtime.KeepAlive(target)
		runtime.KeepAlive(config)
	}()
	selfCopy, err := duplicateSealed(self, c.SelfSize, true)
	if err != nil {
		return ErrDenied
	}
	defer selfCopy.Close()
	var selfStat, exeStat unix.Stat_t
	if unix.Fstat(3, &selfStat) != nil || unix.Stat("/proc/self/exe", &exeStat) != nil || selfStat.Dev != exeStat.Dev || selfStat.Ino != exeStat.Ino {
		return ErrDenied
	}
	toolCopy, err := duplicateSealed(tool, c.ToolSize, true)
	if err != nil {
		return ErrDenied
	}
	defer toolCopy.Close()
	for _, binary := range []struct {
		component mediaboot.RuntimeComponent
		size      int64
		file      *os.File
	}{{c.Self, c.SelfSize, selfCopy}, {c.Tool, c.ToolSize, toolCopy}} {
		if provisionimage.VerifyStaticBinary(context.Background(), provisionimage.Input{MediaID: binary.component.MediaID, SHA256: binary.component.SHA256, Size: binary.size, File: binary.file}, c.Arch) != nil {
			return ErrDenied
		}
	}
	if operation == "unsquashfs" {
		image, err := duplicateSealed(input, c.InputSize, false)
		if err != nil {
			return ErrDenied
		}
		defer image.Close()
		if c.InputSize < 96 || c.InputSize > 8<<30 || len(c.InputSHA256) != 64 {
			return ErrDenied
		}
		// Parent already hashed these same immutable bytes. Repeating a full
		// multi-GiB hash here would expire a valid live check before extraction.
		var st unix.Stat_t
		if unix.Fstat(6, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 {
			return ErrDenied
		}
	} else {
		if validateToolLoop(input, c) != nil {
			return ErrDenied
		}
	}
	if setupToolJail(c, target, operation == "unsquashfs") != nil {
		return ErrDenied
	}
	// Only immutable executable/input descriptors survive into the tool.
	_ = unix.Close(3)
	_ = unix.Close(6)
	_ = unix.Close(7)
	_ = selfCopy.Close()
	_ = toolCopy.Close()
	_ = configCopy.Close()
	if dropToolPrivileges() != nil {
		return ErrDenied
	}
	argv, env, err := toolArgv(c)
	if err != nil {
		return ErrDenied
	}
	return unix.Exec("/proc/self/fd/4", argv, env)
}

func validateToolLoop(input *os.File, c toolRequest) error {
	if input == nil || c.Offset < 1<<20 || c.Size < 1<<20 {
		return ErrDenied
	}
	var st unix.Stat_t
	if unix.Fstat(int(input.Fd()), &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFBLK || unix.Major(uint64(st.Rdev)) != 7 {
		return ErrDenied
	}
	status, err := unix.IoctlLoopGetStatus64(int(input.Fd()))
	if err != nil || status.Device != c.BackingDevice || status.Rdevice != c.BackingRdevice || status.Inode != c.BackingInode || status.Offset != uint64(c.Offset) || status.Sizelimit != uint64(c.Size) || status.Flags != unix.LO_FLAGS_AUTOCLEAR {
		return ErrDenied
	}
	size, err := ioctlUint64(int(input.Fd()), unix.BLKGETSIZE64)
	if err != nil || size != uint64(c.Size) {
		return ErrDenied
	}
	sector, err := unix.IoctlGetInt(int(input.Fd()), unix.BLKSSZGET)
	if err != nil || sector != c.Sector {
		return ErrDenied
	}
	return nil
}

func setupToolJail(c toolRequest, target *os.File, extract bool) error {
	var st unix.Stat_t
	if unix.Lstat(c.Jail, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != 0 || st.Mode&0077 != 0 {
		return ErrDenied
	}
	if unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, "") != nil {
		return ErrDenied
	}
	if unix.Mount("tmpfs", c.Jail, "tmpfs", unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "size=16m,mode=0755") != nil {
		return ErrDenied
	}
	for _, path := range []string{"proc", "target", "etc"} {
		if os.Mkdir(filepath.Join(c.Jail, path), 0755) != nil {
			return ErrDenied
		}
	}
	if os.WriteFile(filepath.Join(c.Jail, "etc/mke2fs.conf"), []byte(mke2fsConfig), 0444) != nil {
		return ErrDenied
	}
	if extract {
		if target == nil || unix.Mount("/proc/self/fd/6", filepath.Join(c.Jail, "target"), "", unix.MS_BIND, "") != nil {
			return ErrDenied
		}
		if unix.Mount("", filepath.Join(c.Jail, "target"), "", unix.MS_BIND|unix.MS_REMOUNT|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "") != nil {
			return ErrDenied
		}
	}
	if unix.Mount("proc", filepath.Join(c.Jail, "proc"), "proc", unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "hidepid=2") != nil {
		return ErrDenied
	}
	if unix.Chdir(c.Jail) != nil || unix.Chroot(".") != nil || unix.Chdir("/") != nil {
		return ErrDenied
	}
	if unix.Mount("", "/", "", unix.MS_REMOUNT|unix.MS_RDONLY|unix.MS_NOSUID|unix.MS_NODEV|unix.MS_NOEXEC, "") != nil {
		return ErrDenied
	}
	return nil
}

const mke2fsConfig = "[defaults]\nbase_features = sparse_super,large_file,filetype,resize_inode,dir_index,ext_attr\ninode_size = 256\ninode_ratio = 16384\nenable_periodic_fsck = 0\n[fs_types]\next4 = {\nfeatures = has_journal,extent,huge_file,flex_bg,metadata_csum,64bit,dir_nlink,extra_isize\n}\n"

func dropToolPrivileges() error {
	allowed := map[int]bool{unix.CAP_CHOWN: true, unix.CAP_DAC_OVERRIDE: true, unix.CAP_FOWNER: true, unix.CAP_FSETID: true, unix.CAP_SETFCAP: true}
	for capability := 0; capability < 64; capability++ {
		if !allowed[capability] {
			err := unix.Prctl(unix.PR_CAPBSET_DROP, uintptr(capability), 0, 0, 0)
			if err == unix.EINVAL && capability > unix.CAP_LAST_CAP {
				break
			}
			if err != nil {
				return ErrDenied
			}
		}
	}
	var data [2]unix.CapUserData
	for capability := range allowed {
		index := capability / 32
		bit := uint32(1) << uint(capability%32)
		data[index].Permitted |= bit
		data[index].Effective |= bit
	}
	if unix.Capset(&unix.CapUserHeader{Version: unix.LINUX_CAPABILITY_VERSION_3}, &data[0]) != nil || unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != nil || unix.Prctl(unix.PR_CAP_AMBIENT, unix.PR_CAP_AMBIENT_CLEAR_ALL, 0, 0, 0) != nil {
		return ErrDenied
	}
	return nil
}

func toolArgv(c toolRequest) ([]string, []string, error) {
	env := []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin:/sbin:/bin", "MKE2FS_CONFIG=/etc/mke2fs.conf"}
	switch c.Operation {
	case "unsquashfs":
		return []string{"unsquashfs", "-strict-errors", "-no-progress", "-processors", "1", "-mem", "128M", "-no-wildcards", "-dest", "/target", "/proc/self/fd/5"}, env, nil
	case "mkfs.ext4":
		if !regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`).MatchString(c.RootUUID) || c.Size < 16<<30 || c.Size%4096 != 0 {
			return nil, nil, ErrDenied
		}
		return []string{"mkfs.ext4", "-F", "-t", "ext4", "-b", "4096", "-I", "256", "-m", "0", "-U", c.RootUUID, "-L", "keel-root", "-E", "nodiscard,lazy_itable_init=0,lazy_journal_init=0", "/proc/self/fd/5", strconv.FormatInt(c.Size/4096, 10)}, env, nil
	case "mkfs.vfat":
		if !regexp.MustCompile(`^[a-f0-9]{8}$`).MatchString(c.FATID) || c.Size != 512<<20 || (c.Sector != 512 && c.Sector != 4096) {
			return nil, nil, ErrDenied
		}
		return []string{"mkfs.vfat", "-I", "-F", "32", "-S", strconv.Itoa(c.Sector), "-s", "1", "-n", "KEEL_EFI", "-i", c.FATID, "/proc/self/fd/5", strconv.FormatInt(c.Size/1024, 10)}, env, nil
	}
	return nil, nil, ErrDenied
}
