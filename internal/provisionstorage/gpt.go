package provisionstorage

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"hash/crc32"
	"io"
	"regexp"
	"strings"
	"time"
)

const gptEntries = 128
const gptEntryBytes = 128
const gptHeaderBytes = 92

// These are partition type identifiers, not filesystem UUIDs or approval IDs.
// The root type is the generic Linux filesystem type; this does not claim DPS
// automatic root discovery or install a bootloader.
const EFITypeGUID = "c12a7328-f81f-11d2-ba4b-00a0c93ec93b"
const LinuxFilesystemTypeGUID = "0fc63daf-8483-4772-8e79-3d69d8477de4"

var partitionUUID = regexp.MustCompile(`^[a-f0-9]{8}-[a-f0-9]{4}-4[a-f0-9]{3}-[89ab][a-f0-9]{3}-[a-f0-9]{12}$`)

type GPTIdentity struct {
	Disk string `json:"disk_guid"`
	EFI  string `json:"efi_partition_guid"`
	Root string `json:"root_partition_guid"`
}

func NewGPTIdentity() (GPTIdentity, error) {
	var ids [3]string
	for i := range ids {
		var raw [16]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return GPTIdentity{}, err
		}
		raw[6] = (raw[6] & 15) | 64
		raw[8] = (raw[8] & 63) | 128
		s := hex.EncodeToString(raw[:])
		ids[i] = s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
	}
	id := GPTIdentity{Disk: ids[0], EFI: ids[1], Root: ids[2]}
	return id, id.validate()
}

func (id GPTIdentity) validate() error {
	if !partitionUUID.MatchString(id.Disk) || !partitionUUID.MatchString(id.EFI) || !partitionUUID.MatchString(id.Root) || id.Disk == id.EFI || id.Disk == id.Root || id.EFI == id.Root {
		return ErrDenied
	}
	return nil
}

// GPTSegment describes exact bytes at a byte offset. No function in this file
// opens a device, writes bytes, mounts a partition or issues a write permit.
type GPTSegment struct {
	Offset int64
	Data   []byte
}

// GPTImage is immutable prepared data, bound to the complete storage-plan
// digest and explicit disk/partition identities. A future live permit must bind
// these bytes, not simply the layout name or an unverified caller digest.
type GPTImage struct {
	planSHA  string
	identity GPTIdentity
	size     int64
	sector   int
	segments [2]GPTSegment
	digest   string
}

func (g *GPTImage) SHA256() string {
	if g == nil {
		return ""
	}
	return g.digest
}
func (g *GPTImage) Identity() GPTIdentity {
	if g == nil {
		return GPTIdentity{}
	}
	return g.identity
}
func (g *GPTImage) Segments() []GPTSegment {
	if g == nil {
		return nil
	}
	out := make([]GPTSegment, 2)
	for i, segment := range g.segments {
		out[i] = GPTSegment{Offset: segment.Offset, Data: bytes.Clone(segment.Data)}
	}
	return out
}

func guidDiskBytes(value string) ([]byte, error) {
	raw, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	if err != nil || len(raw) != 16 {
		return nil, ErrDenied
	}
	// EFI GUIDs encode the first u32/u16/u16 little endian, then eight bytes.
	raw[0], raw[1], raw[2], raw[3] = raw[3], raw[2], raw[1], raw[0]
	raw[4], raw[5] = raw[5], raw[4]
	raw[6], raw[7] = raw[7], raw[6]
	return raw, nil
}

// PrepareGPT deterministically materializes the already validated fixed
// two-partition plan. Historical plan validation does not establish current
// device identity: VerifyCurrent plus live authority remain required at writes.
func PrepareGPT(plan Plan, id GPTIdentity, now time.Time) (*GPTImage, error) {
	if plan.Validate(now) != nil || id.validate() != nil {
		return nil, ErrDenied
	}
	sector := int64(plan.Device.LogicalBlockBytes)
	blocks := uint64(plan.Device.SizeBytes / sector)
	arrayBytes := gptEntries * gptEntryBytes
	arrayBlocks := uint64(int64(arrayBytes) / sector)
	last := blocks - 1
	backupEntries := last - arrayBlocks
	firstUsable, lastUsable := 2+arrayBlocks, backupEntries-1
	entries := make([]byte, arrayBytes)
	for i, partition := range plan.Partitions {
		if partition.Number != i+1 || i > 1 || partition.StartBytes%sector != 0 || partition.SizeBytes%sector != 0 {
			return nil, ErrDenied
		}
		first := uint64(partition.StartBytes / sector)
		end := uint64((partition.StartBytes+partition.SizeBytes)/sector) - 1
		if first < firstUsable || end > lastUsable || first > end {
			return nil, ErrDenied
		}
		typ, unique, name := EFITypeGUID, id.EFI, "keel-efi"
		if i == 1 {
			typ, unique, name = LinuxFilesystemTypeGUID, id.Root, "keel-root"
		}
		typeBytes, err := guidDiskBytes(typ)
		if err != nil {
			return nil, err
		}
		uniqueBytes, err := guidDiskBytes(unique)
		if err != nil {
			return nil, err
		}
		entry := entries[i*gptEntryBytes : (i+1)*gptEntryBytes]
		copy(entry[:16], typeBytes)
		copy(entry[16:32], uniqueBytes)
		binary.LittleEndian.PutUint64(entry[32:40], first)
		binary.LittleEndian.PutUint64(entry[40:48], end)
		for j, c := range name {
			binary.LittleEndian.PutUint16(entry[56+j*2:], uint16(c))
		}
	}
	arrayCRC := crc32.ChecksumIEEE(entries)
	diskGUID, err := guidDiskBytes(id.Disk)
	if err != nil {
		return nil, err
	}
	header := func(my, alternate, array uint64) []byte {
		raw := make([]byte, int(sector))
		copy(raw, "EFI PART")
		binary.LittleEndian.PutUint32(raw[8:12], 0x00010000)
		binary.LittleEndian.PutUint32(raw[12:16], gptHeaderBytes)
		binary.LittleEndian.PutUint64(raw[24:32], my)
		binary.LittleEndian.PutUint64(raw[32:40], alternate)
		binary.LittleEndian.PutUint64(raw[40:48], firstUsable)
		binary.LittleEndian.PutUint64(raw[48:56], lastUsable)
		copy(raw[56:72], diskGUID)
		binary.LittleEndian.PutUint64(raw[72:80], array)
		binary.LittleEndian.PutUint32(raw[80:84], gptEntries)
		binary.LittleEndian.PutUint32(raw[84:88], gptEntryBytes)
		binary.LittleEndian.PutUint32(raw[88:92], arrayCRC)
		binary.LittleEndian.PutUint32(raw[16:20], crc32.ChecksumIEEE(raw[:gptHeaderBytes]))
		return raw
	}
	front, back := make([]byte, MiB), make([]byte, MiB)
	// Protective MBR occupies bytes 0..511 even on a 4Kn device; the rest of
	// the logical block is zero. It advertises logical block count, capped u32.
	front[447], front[448], front[449] = 0, 2, 0
	front[450] = 0xee
	front[451], front[452], front[453] = 0xff, 0xff, 0xff
	binary.LittleEndian.PutUint32(front[454:458], 1)
	protected := last
	if protected > 0xffffffff {
		protected = 0xffffffff
	}
	binary.LittleEndian.PutUint32(front[458:462], uint32(protected))
	front[510], front[511] = 0x55, 0xaa
	copy(front[sector:], header(1, last, 2))
	copy(front[2*sector:], entries)
	backOffset := plan.Device.SizeBytes - MiB
	copy(back[int64(backupEntries)*sector-backOffset:], entries)
	copy(back[int64(last)*sector-backOffset:], header(last, 1, backupEntries))
	g := &GPTImage{planSHA: plan.SHA256, identity: id, size: plan.Device.SizeBytes, sector: plan.Device.LogicalBlockBytes, segments: [2]GPTSegment{{Offset: 0, Data: front}, {Offset: backOffset, Data: back}}}
	h := sha256.New()
	_, _ = h.Write([]byte("Keel prepared GPT v1\x00"))
	metadata, _ := json.Marshal(struct {
		Plan     string
		Identity GPTIdentity
		Size     int64
		Sector   int
	}{g.planSHA, g.identity, g.size, g.sector})
	_, _ = h.Write(metadata)
	for _, segment := range g.segments {
		var offset [8]byte
		binary.LittleEndian.PutUint64(offset[:], uint64(segment.Offset))
		_, _ = h.Write(offset[:])
		_, _ = h.Write(segment.Data)
	}
	g.digest = hex.EncodeToString(h.Sum(nil))
	return g, nil
}

// InspectGPTEdges compares both exact reserved MiB regions against a freshly
// materialized plan, including both headers/arrays/CRCs, PMBR and zero padding.
// It does not verify actual geometry, filesystems, payload, erase completion,
// bootability or success; the caller supplies an already identified reader.
func InspectGPTEdges(plan Plan, id GPTIdentity, reader io.ReaderAt, now time.Time) (string, error) {
	if reader == nil {
		return "", ErrDenied
	}
	want, err := PrepareGPT(plan, id, now)
	if err != nil {
		return "", err
	}
	raw := make([]byte, MiB)
	for _, segment := range want.segments {
		n, err := reader.ReadAt(raw, segment.Offset)
		if err != nil || n != len(raw) || !bytes.Equal(raw, segment.Data) {
			return "", ErrDenied
		}
	}
	return want.digest, nil
}
