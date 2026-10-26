package provisionimage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"math/bits"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

const RootFSSchema = "keel.rootfs-byte-evidence/v1"
const NoCloudPolicyPath = "etc/cloud/cloud.cfg.d/99-keel-datasource.cfg"
const NoCloudPolicy = "datasource_list: [ NoCloud ]\n"

// RootFSEvidence records immutable archive-byte checks. It does not attest
// cloud-init service enablement, successful decompression, installed capacity,
// actual boot behavior or write permission.
type RootFSEvidence struct {
	Schema      string `json:"schema"`
	MediaID     string `json:"media_id"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	BytesUsed   uint64 `json:"bytes_used"`
	Inodes      uint32 `json:"inodes"`
	BlockBytes  uint32 `json:"block_bytes"`
	Compression uint16 `json:"compression"`
}

type RootFSFileSource interface {
	ReadFile(context.Context, string, int64) ([]byte, error)
}

// RootFSContentsEvidence describes fixed files read through a bounded,
// read-only decoder of the same sealed archive. It is not a service/boot test.
type RootFSContentsEvidence struct {
	Target              VerifiedTarget `json:"target"`
	SSHHelperSHA256     string         `json:"ssh_helper_sha256"`
	ModulesSHA256       string         `json:"modules_dep_sha256"`
	CloudInitSHA256     string         `json:"cloud_init_sha256"`
	NoCloudPolicySHA256 string         `json:"nocloud_policy_sha256"`
	FirstBootSHA256     string         `json:"first_boot_sha256"`
}

func VerifyRootFSContents(ctx context.Context, plan mediaboot.Plan, target io.ReaderAt, source RootFSFileSource) (*RootFSContentsEvidence, error) {
	if ctx.Err() != nil || plan.ValidateWritable() != nil || source == nil {
		return nil, ErrInvalid
	}
	var root, ssh mediaboot.Artifact
	for _, a := range plan.Artifacts {
		if a.Role == "rootfs" {
			root = a
		}
		if a.Role == "ssh-helper" {
			ssh = a
		}
	}
	contract := plan.Profile.Recipe.TargetBoot
	verified, err := VerifyTargetUKI(ctx, target, contract.Size, contract, root.Arch)
	if err != nil {
		return nil, ErrInvalid
	}
	read := func(path string, limit int64) ([]byte, error) {
		raw, err := source.ReadFile(ctx, path, limit)
		if err != nil || int64(len(raw)) > limit || ctx.Err() != nil {
			return nil, ErrInvalid
		}
		return raw, nil
	}
	osRelease, err := read("usr/lib/os-release", 64<<10)
	if err != nil || rawSHA(osRelease) != verified.OSReleaseSHA256 {
		return nil, ErrInvalid
	}
	modules, err := read("usr/lib/modules/"+contract.KernelRelease+"/modules.dep", 8<<20)
	if err != nil {
		return nil, ErrInvalid
	}
	helper, err := read(SSHHelperPath, MaxBinaryBytes)
	if err != nil || int64(len(helper)) != ssh.Size || VerifyStaticBinary(ctx, Input{MediaID: ssh.MediaID, SHA256: ssh.SHA256, Size: ssh.Size, File: bytes.NewReader(helper)}, root.Arch) != nil {
		return nil, ErrInvalid
	}
	cloudInit, err := read("usr/bin/cloud-init", 1<<20)
	if err != nil || len(cloudInit) == 0 {
		return nil, ErrInvalid
	}
	firstBoot, err := read("usr/local/libexec/keel-firstboot", MaxBinaryBytes)
	firstBootSHA := rawSHA(firstBoot)
	if err != nil || VerifyStaticBinary(ctx, Input{MediaID: root.MediaID, SHA256: firstBootSHA, Size: int64(len(firstBoot)), File: bytes.NewReader(firstBoot)}, root.Arch) != nil {
		return nil, ErrInvalid
	}
	policy, err := read(NoCloudPolicyPath, 4096)
	if err != nil || string(policy) != NoCloudPolicy || ctx.Err() != nil {
		return nil, ErrInvalid
	}
	return &RootFSContentsEvidence{Target: *verified, SSHHelperSHA256: ssh.SHA256, ModulesSHA256: rawSHA(modules), CloudInitSHA256: rawSHA(cloudInit), NoCloudPolicySHA256: rawSHA(policy), FirstBootSHA256: firstBootSHA}, nil
}

// VerifyRootFSArchive hashes the whole pinned object, then checks the v4
// superblock and table ranges. It rejects appended payloads, offsets outside
// the object and unbounded superblocks. Full metadata/file decoding remains the
// independently approved unsquashfs reader's job; magic alone is insufficient.
func VerifyRootFSArchive(ctx context.Context, reader io.ReaderAt, artifact mediaboot.Artifact) (*RootFSEvidence, error) {
	if ctx.Err() != nil || reader == nil || artifact.Role != "rootfs" || artifact.Size < 96 || artifact.Size > 64<<30 || len(artifact.SHA256) != 64 {
		return nil, ErrInvalid
	}
	r := targetReader{ctx: ctx, r: reader, n: artifact.Size}
	h := sha256.New()
	if n, err := io.CopyBuffer(h, io.NewSectionReader(r, 0, artifact.Size), make([]byte, 128<<10)); err != nil || n != artifact.Size || hex.EncodeToString(h.Sum(nil)) != artifact.SHA256 {
		return nil, ErrInvalid
	}
	var raw [96]byte
	if _, err := r.ReadAt(raw[:], 0); err != nil || binary.LittleEndian.Uint32(raw[:4]) != 0x73717368 {
		return nil, ErrInvalid
	}
	u16 := func(off int) uint16 { return binary.LittleEndian.Uint16(raw[off : off+2]) }
	u32 := func(off int) uint32 { return binary.LittleEndian.Uint32(raw[off : off+4]) }
	u64 := func(off int) uint64 { return binary.LittleEndian.Uint64(raw[off : off+8]) }
	inodes, block, fragments := u32(4), u32(12), u32(16)
	compression, blockLog, flags, ids := u16(20), u16(22), u16(24), u16(26)
	rootInode, used := u64(32), u64(40)
	idTable, xattrTable, inodeTable, directoryTable, fragmentTable, lookupTable := u64(48), u64(56), u64(64), u64(72), u64(80), u64(88)
	if u16(28) != 4 || u16(30) != 0 || inodes == 0 || inodes > 2_000_000 || block < 4096 || block > 1<<20 || bits.OnesCount32(block) != 1 || uint16(bits.TrailingZeros32(block)) != blockLog || compression < 1 || compression > 6 || flags&0xf000 != 0 || ids == 0 || used < 96 || used > uint64(artifact.Size) || uint64(artifact.Size)-used > 4095 || inodeTable < 96 || inodeTable >= directoryTable || directoryTable >= used || idTable < directoryTable || idTable >= used || rootInode&0xffff >= 8192 || rootInode>>16 >= directoryTable-inodeTable || fragments > inodes {
		return nil, ErrInvalid
	}
	// Sentinel -1 is allowed only for optional tables, never required tables.
	for _, table := range []uint64{xattrTable, fragmentTable, lookupTable} {
		if table != ^uint64(0) && (table < directoryTable || table >= used) {
			return nil, ErrInvalid
		}
	}
	if fragments > 0 && fragmentTable == ^uint64(0) {
		return nil, ErrInvalid
	}
	// ID/fragment/lookup index arrays must fit without integer wraparound.
	for _, index := range []struct{ start, bytes uint64 }{
		{idTable, (uint64(ids)*4 + 8191) / 8192 * 8},
		{fragmentTable, (uint64(fragments)*16 + 8191) / 8192 * 8},
		{lookupTable, (uint64(inodes)*8 + 8191) / 8192 * 8},
	} {
		if index.start != ^uint64(0) && index.bytes > used-index.start {
			return nil, ErrInvalid
		}
	}
	if used < uint64(artifact.Size) {
		padding := make([]byte, int(uint64(artifact.Size)-used))
		if _, err := r.ReadAt(padding, int64(used)); err != nil || !bytes.Equal(padding, make([]byte, len(padding))) {
			return nil, ErrInvalid
		}
	}
	if ctx.Err() != nil {
		return nil, ErrInvalid
	}
	return &RootFSEvidence{Schema: RootFSSchema, MediaID: artifact.MediaID, SHA256: artifact.SHA256, Size: artifact.Size, BytesUsed: used, Inodes: inodes, BlockBytes: block, Compression: compression}, nil
}
