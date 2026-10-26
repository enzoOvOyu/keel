//go:build linux

package provisioninitrd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
	"golang.org/x/sys/unix"
)

const MaxStagedRootFSBytes int64 = 8 << 30
const stageMemoryReserve uint64 = 512 << 20

// StagedInstallation owns sealed immutable inputs only. It creates no block
// device, mount or installed filesystem and cannot issue a write permit.
// Borrowed descriptors must remain open until the caller finishes its writer.
type StagedInstallation struct {
	rootfs, target  *os.File
	tools           map[string]*os.File
	runtime         mediaboot.RuntimeSet
	runtimeSHA      string
	modulesPath     string
	evidence        provisionimage.RootFSEvidence
	targetEvidence  provisionimage.VerifiedTarget
	contentEvidence provisionimage.RootFSContentsEvidence
	closed          bool
}

func (s *StagedInstallation) Close() error {
	if s == nil || s.closed {
		return nil
	}
	s.closed = true
	for _, f := range []*os.File{s.rootfs, s.target} {
		if f != nil {
			_ = f.Close()
		}
	}
	for _, f := range s.tools {
		if f != nil {
			_ = f.Close()
		}
	}
	return nil
}
func (s *StagedInstallation) RootFSFile() *os.File {
	if s == nil || s.closed {
		return nil
	}
	return s.rootfs
}
func (s *StagedInstallation) TargetUKIFile() *os.File {
	if s == nil || s.closed {
		return nil
	}
	return s.target
}
func (s *StagedInstallation) ToolFile(name string) *os.File {
	if s == nil || s.closed {
		return nil
	}
	return s.tools[name]
}
func (s *StagedInstallation) Runtime() mediaboot.RuntimeSet {
	if s == nil || s.closed {
		return mediaboot.RuntimeSet{}
	}
	out := s.runtime
	if out.Writer != nil {
		copy := *out.Writer
		out.Writer = &copy
	}
	return out
}
func (s *StagedInstallation) RuntimeSHA256() string {
	if s == nil || s.closed {
		return ""
	}
	return s.runtimeSHA
}
func (s *StagedInstallation) Evidence() provisionimage.RootFSEvidence {
	if s == nil || s.closed {
		return provisionimage.RootFSEvidence{}
	}
	return s.evidence
}
func (s *StagedInstallation) TargetEvidence() provisionimage.VerifiedTarget {
	if s == nil || s.closed {
		return provisionimage.VerifiedTarget{}
	}
	return s.targetEvidence
}

func (s *StagedInstallation) ContentsEvidence() provisionimage.RootFSContentsEvidence {
	if s == nil || s.closed {
		return provisionimage.RootFSContentsEvidence{}
	}
	return s.contentEvidence
}

func stageHash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

// StageInstallation must receive the controller's runtime config digest from
// an authenticated nonce-bound review of this same boot. The controller has
// independently verified the actual initrd signature before activation; a
// digest/public key embedded only in a self-reported manifest is not accepted.
func StageInstallation(ctx context.Context, input Input, files *Files, source ArtifactSource, runtimeSHA string) (out *StagedInstallation, resultErr error) {
	if RequireInitramfs() != nil || files == nil || source == nil || !digest.MatchString(runtimeSHA) || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	manifest, _, err := provisionboot.VerifyBootFiles(files.Manifest, input.ManifestSHA256, files.Preflight, files.Seed, time.Now())
	if err != nil || manifest.GrantID != input.GrantID || manifest.MAC != input.BootMAC || manifest.Media.ValidateWritable() != nil || manifest.Arch != runtime.GOARCH {
		return nil, ErrUnavailable
	}
	var rootfs, ssh mediaboot.Artifact
	for _, a := range manifest.Media.Artifacts {
		if a.Role == "rootfs" {
			rootfs = a
		}
		if a.Role == "ssh-helper" {
			ssh = a
		}
	}
	target := manifest.Media.Profile.Recipe.TargetBoot
	if rootfs.Size < 96 || rootfs.Size > MaxStagedRootFSBytes {
		return nil, ErrUnavailable
	}
	// Check actual available RAM before allocating a bounded tmpfs/memfd image.
	// Account for target UKI, verified tool copies and bounded tool caches too.
	available, err := availableStageMemory()
	needed := uint64(rootfs.Size) + uint64(target.Size) + 6*uint64(provisionimage.MaxBinaryBytes) + (256 << 20) + stageMemoryReserve
	if err != nil || available < needed {
		return nil, ErrUnavailable
	}
	set, err := loadWriterRuntime(runtimeSHA)
	if err != nil || set.Arch != manifest.Arch || set.Preflight.MediaID != manifest.Media.Profile.Recipe.PreflightMediaID || set.Preflight.SHA256 != manifest.Media.Profile.Recipe.PreflightSHA256 || set.SSHHelper.MediaID != ssh.MediaID || set.SSHHelper.SHA256 != ssh.SHA256 {
		return nil, ErrUnavailable
	}
	out = &StagedInstallation{runtime: set, runtimeSHA: runtimeSHA, modulesPath: "usr/lib/modules/" + target.KernelRelease + "/modules.dep", tools: map[string]*os.File{}}
	defer func() {
		if resultErr != nil {
			out.Close()
			out = nil
		}
	}()
	for _, tool := range []struct {
		name, path string
		component  mediaboot.RuntimeComponent
	}{
		{"unsquashfs", "/" + provisionimage.UnsquashFSPath, set.Writer.UnsquashFS},
		{"mkfs.ext4", "/" + provisionimage.MkfsExt4Path, set.Writer.MkfsExt4},
		{"mkfs.vfat", "/" + provisionimage.MkfsVFATPath, set.Writer.MkfsVFAT},
	} {
		out.tools[tool.name], err = stageTool(ctx, tool.path, tool.component, set.Arch)
		if err != nil {
			return out, err
		}
	}
	archive, err := newStageFile("keel-approved-rootfs")
	if err != nil {
		return out, err
	}
	defer archive.Close()
	writer := &stageBoundedWriter{writer: archive, limit: rootfs.Size}
	if source.CopyArtifact(ctx, "rootfs", rootfs.Size, writer) != nil || writer.written != rootfs.Size || writer.overflow || ctx.Err() != nil {
		return out, ErrUnavailable
	}
	out.rootfs, err = finishStageFile(archive)
	if err != nil {
		return out, err
	}
	archiveEvidence, err := provisionimage.VerifyRootFSArchive(ctx, out.rootfs, rootfs)
	if err != nil {
		return out, ErrUnavailable
	}
	out.evidence = *archiveEvidence
	uki, err := newStageFile("keel-approved-target-uki")
	if err != nil {
		return out, err
	}
	defer uki.Close()
	if err = out.cat(ctx, target.Path, target.Size, uki); err != nil {
		return out, err
	}
	info, err := uki.Stat()
	if err != nil || info.Size() != target.Size {
		return out, ErrUnavailable
	}
	out.target, err = finishStageFile(uki)
	if err != nil {
		return out, err
	}
	verified, err := provisionimage.VerifyRootFSContents(ctx, manifest.Media, out.target, out)
	if err != nil {
		return out, ErrUnavailable
	}
	out.targetEvidence = verified.Target
	out.contentEvidence = *verified
	return out, nil
}

func loadWriterRuntime(expected string) (mediaboot.RuntimeSet, error) {
	var empty mediaboot.RuntimeSet
	if !digest.MatchString(expected) {
		return empty, ErrUnavailable
	}
	fd, err := unix.Open("/usr/lib/keel/approved-runtime-set.json", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return empty, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), "approved-runtime-set")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&0222 != 0 || st.Size < 1 || st.Size > 64<<10 {
		return empty, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || int64(len(raw)) != st.Size || stageHash(raw) != expected {
		return empty, ErrUnavailable
	}
	set, err := mediaboot.DecodeRuntimeSet(raw)
	if err != nil || set.Schema != mediaboot.WriterRuntimeSetSchema || set.Writer == nil {
		return empty, ErrUnavailable
	}
	return set, nil
}

func stageTool(ctx context.Context, path string, component mediaboot.RuntimeComponent, arch string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 64 || info.Size() > provisionimage.MaxBinaryBytes {
		return nil, ErrUnavailable
	}
	raw, err := readHelper(ctx, path, mediaboot.Artifact{MediaID: component.MediaID, SHA256: component.SHA256, Size: info.Size()}, arch)
	if err != nil {
		return nil, err
	}
	f, err := sealedFile("keel-approved-writer-tool", raw, true)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return reopenStageReadOnly(f)
}

func newStageFile(name string) (*os.File, error) {
	fd, err := unix.MemfdCreate(name, unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING|unix.MFD_NOEXEC_SEAL)
	if err != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), name)
	if f.Chmod(0400) != nil {
		f.Close()
		return nil, ErrUnavailable
	}
	return f, nil
}
func finishStageFile(f *os.File) (*os.File, error) {
	if _, err := unix.FcntlInt(f.Fd(), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return nil, ErrUnavailable
	}
	return reopenStageReadOnly(f)
}
func reopenStageReadOnly(f *os.File) (*os.File, error) {
	fd, err := unix.Open(fmt.Sprintf("/proc/self/fd/%d", f.Fd()), unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	return os.NewFile(uintptr(fd), "sealed-approved-input"), nil
}

func (s *StagedInstallation) ReadFile(ctx context.Context, path string, limit int64) ([]byte, error) {
	// Public access remains an exact allowlist. It cannot turn this object into
	// an arbitrary archive path reader or pass caller text to the tool parser.
	allowed := path == "usr/lib/os-release" || path == "usr/bin/cloud-init" || path == "usr/local/libexec/keel-firstboot" || path == provisionimage.SSHHelperPath || path == provisionimage.NoCloudPolicyPath || (s != nil && s.modulesPath != "" && path == s.modulesPath)
	if !allowed || limit < 1 || limit > provisionimage.MaxBinaryBytes {
		return nil, ErrUnavailable
	}
	var output bytes.Buffer
	if err := s.cat(ctx, path, limit, &output); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
func (s *StagedInstallation) cat(ctx context.Context, path string, limit int64, output io.Writer) error {
	if s == nil || s.closed || s.rootfs == nil || s.tools["unsquashfs"] == nil || limit < 1 || output == nil {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	// Paths are fixed in this module, never user argv. -cat never extracts or
	// executes rootfs content. Strict errors and literal matching are mandatory.
	cmd := exec.CommandContext(ctx, "/proc/self/fd/3", "-cat", "-no-wildcards", "-strict-errors", "-processors", "1", "-mem", "128M", "/proc/self/fd/4", path)
	cmd.Args[0] = "unsquashfs"
	cmd.ExtraFiles = []*os.File{s.tools["unsquashfs"], s.rootfs}
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL=C"}
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	bounded := &stageBoundedWriter{writer: output, limit: limit}
	cmd.Stdout = bounded
	if cmd.Run() != nil || bounded.overflow || ctx.Err() != nil {
		return ErrUnavailable
	}
	return nil
}

type stageBoundedWriter struct {
	writer         io.Writer
	limit, written int64
	overflow       bool
}

func (w *stageBoundedWriter) Write(raw []byte) (int, error) {
	if w.overflow || int64(len(raw)) > w.limit-w.written {
		w.overflow = true
		return 0, ErrUnavailable
	}
	n, err := w.writer.Write(raw)
	w.written += int64(n)
	if err == nil && n != len(raw) {
		err = io.ErrShortWrite
	}
	return n, err
}

func availableStageMemory() (uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, ErrUnavailable
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return 0, ErrUnavailable
	}
	for _, line := range strings.Split(string(raw), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 3 && parts[0] == "MemAvailable:" && parts[2] == "kB" {
			n, err := strconv.ParseUint(parts[1], 10, 53)
			if err != nil || n == 0 {
				return 0, ErrUnavailable
			}
			return n * 1024, nil
		}
	}
	return 0, ErrUnavailable
}
