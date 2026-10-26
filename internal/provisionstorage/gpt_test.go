package provisionstorage

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"io"
	"testing"
	"time"
)

func gptIdentityFixture() GPTIdentity {
	return GPTIdentity{Disk: "01234567-89ab-4cde-8012-3456789abcde", EFI: "11234567-89ab-4cde-9012-3456789abcde", Root: "21234567-89ab-4cde-a012-3456789abcde"}
}

func gptPlanFixture(t *testing.T, sector int, size int64) (Plan, time.Time) {
	t.Helper()
	spec, observation, intent, now := storageFixture()
	intent.LogicalBlockBytes, intent.SizeBytes = sector, size
	observation.Disks[1].LogicalBlockBytes, observation.Disks[1].SizeBytes = sector, size
	plan, err := Build(spec, reportBytes(t, spec, observation, now), intent, now)
	if err != nil {
		t.Fatal(err)
	}
	return *plan, now
}

// Sparse in-memory readback only: no device/file creation or writer execution.
type gptReaderFixture struct {
	segments []GPTSegment
	short    bool
}

func (r gptReaderFixture) ReadAt(p []byte, offset int64) (int, error) {
	for _, segment := range r.segments {
		if offset >= segment.Offset && offset+int64(len(p)) <= segment.Offset+int64(len(segment.Data)) {
			n := copy(p, segment.Data[offset-segment.Offset:])
			if r.short {
				return n - 1, nil
			}
			return n, nil
		}
	}
	return 0, io.EOF
}

func TestStorageGPT512And4KnHeadersArraysAndProtectiveMBR(t *testing.T) {
	for _, sector := range []int{512, 4096} {
		plan, now := gptPlanFixture(t, sector, 32<<40)
		id := gptIdentityFixture()
		image, err := PrepareGPT(plan, id, now)
		if err != nil {
			t.Fatal(err)
		}
		segments := image.Segments()
		if len(segments) != 2 || segments[0].Offset != 0 || segments[1].Offset != plan.Device.SizeBytes-MiB || len(segments[0].Data) != int(MiB) || len(segments[1].Data) != int(MiB) {
			t.Fatal("write range escaped the two reserved edges")
		}
		front, back := segments[0].Data, segments[1].Data
		if front[510] != 0x55 || front[511] != 0xaa || front[450] != 0xee || binary.LittleEndian.Uint32(front[454:458]) != 1 {
			t.Fatal("invalid protective MBR")
		}
		if binary.LittleEndian.Uint32(front[458:462]) != 0xffffffff || !bytes.Equal(front[462:510], make([]byte, 48)) {
			t.Fatal("protective length not capped or hybrid entries present")
		}
		if sector == 4096 && !bytes.Equal(front[512:4096], make([]byte, 4096-512)) {
			t.Fatal("4Kn MBR padding not reserved")
		}
		primary := front[sector : sector+92]
		backup := back[len(back)-sector : len(back)-sector+92]
		blocks := uint64(plan.Device.SizeBytes / int64(sector))
		arrayBlocks := uint64(16384 / sector)
		for i, header := range [][]byte{primary, backup} {
			if string(header[:8]) != "EFI PART" || binary.LittleEndian.Uint32(header[8:12]) != 0x10000 || binary.LittleEndian.Uint32(header[12:16]) != 92 || binary.LittleEndian.Uint32(header[20:24]) != 0 {
				t.Fatal("GPT header format")
			}
			stored := binary.LittleEndian.Uint32(header[16:20])
			checked := bytes.Clone(header)
			clear(checked[16:20])
			if crc32.ChecksumIEEE(checked) != stored {
				t.Fatal("header checksum")
			}
			mine, alternate, array := uint64(1), blocks-1, uint64(2)
			if i == 1 {
				mine, alternate, array = blocks-1, 1, blocks-1-arrayBlocks
			}
			if binary.LittleEndian.Uint64(header[24:32]) != mine || binary.LittleEndian.Uint64(header[32:40]) != alternate || binary.LittleEndian.Uint64(header[72:80]) != array {
				t.Fatal("primary/backup locations")
			}
			if binary.LittleEndian.Uint64(header[40:48]) != 2+arrayBlocks || binary.LittleEndian.Uint64(header[48:56]) != blocks-2-arrayBlocks {
				t.Fatal("usable region includes GPT metadata")
			}
			if hex.EncodeToString(header[56:72]) != "67452301ab89de4c80123456789abcde" {
				t.Fatal("disk GUID byte order")
			}
			if binary.LittleEndian.Uint32(header[80:84]) != 128 || binary.LittleEndian.Uint32(header[84:88]) != 128 {
				t.Fatal("entry geometry")
			}
		}
		entries := front[2*sector : 2*sector+16384]
		backupStart := len(back) - sector - 16384
		if !bytes.Equal(entries, back[backupStart:backupStart+16384]) || crc32.ChecksumIEEE(entries) != binary.LittleEndian.Uint32(primary[88:92]) || binary.LittleEndian.Uint32(primary[88:92]) != binary.LittleEndian.Uint32(backup[88:92]) {
			t.Fatal("array identity/checksum")
		}
		if hex.EncodeToString(entries[:16]) != "28732ac11ff8d211ba4b00a0c93ec93b" || hex.EncodeToString(entries[128:144]) != "af3dc60f838472478e793d69d8477de4" {
			t.Fatal("incorrect partition types")
		}
		for i, partition := range plan.Partitions {
			entry := entries[i*128 : (i+1)*128]
			if binary.LittleEndian.Uint64(entry[32:40]) != uint64(partition.StartBytes/int64(sector)) || binary.LittleEndian.Uint64(entry[40:48]) != uint64((partition.StartBytes+partition.SizeBytes)/int64(sector)-1) {
				t.Fatal("approved partition range changed")
			}
			if !bytes.Equal(entry[48:56], make([]byte, 8)) {
				t.Fatal("unexpected partition attributes")
			}
		}
		if !bytes.Equal(entries[256:], make([]byte, 16384-256)) {
			t.Fatal("unexpected extra partition")
		}
		got, err := InspectGPTEdges(plan, id, gptReaderFixture{segments: segments}, now)
		if err != nil || got != image.SHA256() {
			t.Fatal("exact readback failed", err)
		}
		second, err := PrepareGPT(plan, id, now)
		if err != nil || second.SHA256() != got {
			t.Fatal("non-deterministic GPT")
		}
		small, at := gptPlanFixture(t, sector, 1<<40)
		shortDisk, err := PrepareGPT(small, id, at)
		if err != nil {
			t.Fatal(err)
		}
		count := binary.LittleEndian.Uint32(shortDisk.Segments()[0].Data[458:462])
		if uint64(count) != uint64((1<<40)/int64(sector)-1) {
			t.Fatal("protective block count used bytes or 512-byte units on 4Kn")
		}
	}
}

func TestStorageGPTRejectsCorruptionForeignIdentityAndPartialRead(t *testing.T) {
	plan, now := gptPlanFixture(t, 512, 1<<40)
	id := gptIdentityFixture()
	image, err := PrepareGPT(plan, id, now)
	if err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func([]GPTSegment){
		"pmbr":             func(s []GPTSegment) { s[0].Data[450] = 0x83 },
		"primary_crc":      func(s []GPTSegment) { s[0].Data[512+16] ^= 1 },
		"backup_crc":       func(s []GPTSegment) { s[1].Data[len(s[1].Data)-512+16] ^= 1 },
		"primary_array":    func(s []GPTSegment) { s[0].Data[1024+16] ^= 1 },
		"backup_array":     func(s []GPTSegment) { s[1].Data[len(s[1].Data)-512-16384] ^= 1 },
		"hidden_partition": func(s []GPTSegment) { s[0].Data[1024+256] = 1 },
		"reserved_padding": func(s []GPTSegment) { s[0].Data[512+100] = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			segments := image.Segments()
			change(segments)
			if _, err := InspectGPTEdges(plan, id, gptReaderFixture{segments: segments}, now); err == nil {
				t.Fatal("changed bytes accepted")
			}
		})
	}
	if _, err := InspectGPTEdges(plan, id, gptReaderFixture{segments: image.Segments(), short: true}, now); err == nil {
		t.Fatal("partial read accepted")
	}
	foreign := id
	foreign.Root = "31234567-89ab-4cde-b012-3456789abcde"
	if _, err := InspectGPTEdges(plan, foreign, gptReaderFixture{segments: image.Segments()}, now); err == nil {
		t.Fatal("foreign partition identity accepted")
	}
	if _, err := PrepareGPT(plan, id, now.Add(11*time.Minute)); err == nil {
		t.Fatal("expired plan prepared")
	}
	bad := id
	bad.Root = bad.Disk
	if _, err := PrepareGPT(plan, bad, now); err == nil {
		t.Fatal("duplicate GUIDs accepted")
	}
	changed := plan
	changed.Partitions = append([]Partition{}, plan.Partitions...)
	changed.Partitions[1].StartBytes += MiB
	if _, err := PrepareGPT(changed, id, now); err == nil {
		t.Fatal("changed approved layout accepted")
	}
	// Copies returned to a future executor cannot mutate the prepared object.
	copyOf := image.Segments()
	copyOf[0].Data[0] = 1
	if image.Segments()[0].Data[0] != 0 {
		t.Fatal("prepared bytes mutated by caller")
	}
}
