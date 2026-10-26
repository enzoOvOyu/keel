package provisionimage

import (
	"bytes"
	"context"
	"debug/pe"
	"encoding/binary"
	"testing"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

// PE structure fixtures are not executable OS images or hardware evidence.
func targetPEFixture(t *testing.T, arch string) ([]byte, *mediaboot.TargetBoot) {
	t.Helper()
	sections := []struct {
		name string
		data []byte
	}{
		{".text", []byte{0xc3}}, {".linux", bytes.Repeat([]byte{1}, 64)}, {".initrd", []byte{1}},
		{".cmdline", []byte(mediaboot.TargetCommandLine + "\x00")}, {".uname", []byte("6.12.0-keel\x00")}, {".osrel", []byte("ID=keel\nVERSION_ID=1\n\x00")},
	}
	raw := make([]byte, 1024+len(sections)*512)
	copy(raw, "MZ")
	binary.LittleEndian.PutUint32(raw[0x3c:], 0x80)
	copy(raw[0x80:], "PE\x00\x00")
	machine := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
	if arch == "arm64" {
		machine = pe.IMAGE_FILE_MACHINE_ARM64
	}
	header := pe.FileHeader{Machine: machine, NumberOfSections: uint16(len(sections)), SizeOfOptionalHeader: uint16(binary.Size(pe.OptionalHeader64{})), Characteristics: pe.IMAGE_FILE_EXECUTABLE_IMAGE}
	var encoded bytes.Buffer
	if err := binary.Write(&encoded, binary.LittleEndian, header); err != nil {
		t.Fatal(err)
	}
	optional := pe.OptionalHeader64{Magic: 0x20b, AddressOfEntryPoint: 4096, ImageBase: 0x100000, SectionAlignment: 4096, FileAlignment: 512, SizeOfImage: uint32((len(sections) + 1) * 4096), SizeOfHeaders: 1024, Subsystem: pe.IMAGE_SUBSYSTEM_EFI_APPLICATION, NumberOfRvaAndSizes: 16}
	if err := binary.Write(&encoded, binary.LittleEndian, optional); err != nil {
		t.Fatal(err)
	}
	for i, s := range sections {
		h := pe.SectionHeader32{VirtualSize: uint32(len(s.data)), VirtualAddress: uint32((i + 1) * 4096), SizeOfRawData: 512, PointerToRawData: uint32(1024 + i*512), Characteristics: pe.IMAGE_SCN_MEM_READ}
		copy(h.Name[:], s.name)
		if i == 0 {
			h.Characteristics |= pe.IMAGE_SCN_MEM_EXECUTE
		}
		if err := binary.Write(&encoded, binary.LittleEndian, h); err != nil {
			t.Fatal(err)
		}
		copy(raw[1024+i*512:], s.data)
	}
	copy(raw[0x84:], encoded.Bytes())
	target := &mediaboot.TargetBoot{Schema: mediaboot.TargetBootSchema, Path: mediaboot.TargetUKIPath, SHA256: rawSHA(raw), Size: int64(len(raw)), KernelRelease: "6.12.0-keel", CommandLine: mediaboot.TargetCommandLine}
	return raw, target
}

func TestTargetUKIFixedProfileAndArchitecture(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			raw, target := targetPEFixture(t, arch)
			got, err := VerifyTargetUKI(context.Background(), bytes.NewReader(raw), int64(len(raw)), target, arch)
			fallback, _ := mediaboot.TargetEFIFallback(arch)
			if err != nil || got.EFIFallback != fallback || got.OSReleaseSHA256 != rawSHA([]byte("ID=keel\nVERSION_ID=1\n")) {
				t.Fatalf("target verification: %#v %v", got, err)
			}
			other := "arm64"
			if arch == "arm64" {
				other = "amd64"
			}
			if _, err := VerifyTargetUKI(context.Background(), bytes.NewReader(raw), int64(len(raw)), target, other); err == nil {
				t.Fatal("cross-architecture UKI accepted")
			}
		})
	}
}

func TestTargetUKIRejectsAmbiguityAndWrongEmbeddedContents(t *testing.T) {
	for name, change := range map[string]func([]byte, *mediaboot.TargetBoot){
		"changed_bytes":  func(b []byte, _ *mediaboot.TargetBoot) { b[len(b)-1] ^= 1 },
		"wrong_cmdline":  func(b []byte, _ *mediaboot.TargetBoot) { b[1024+3*512] = 'x' },
		"hidden_cmdline": func(b []byte, _ *mediaboot.TargetBoot) { b[1024+3*512+250] = 'x' },
		"wrong_kernel":   func(b []byte, _ *mediaboot.TargetBoot) { b[1024+4*512] = '9' },
		"multi_profile":  func(b []byte, _ *mediaboot.TargetBoot) { off := 0x84 + 20 + 240; copy(b[off:off+8], ".profile") },
		"section_overlap": func(b []byte, _ *mediaboot.TargetBoot) {
			off := 0x84 + 20 + 240 + 40
			binary.LittleEndian.PutUint32(b[off+20:], 1024)
		},
		"missing_linux": func(b []byte, _ *mediaboot.TargetBoot) {
			off := 0x84 + 20 + 240 + 40
			copy(b[off:off+8], ".wrong\x00\x00")
		},
		"wrong_size": func(_ []byte, target *mediaboot.TargetBoot) { target.Size++ },
	} {
		t.Run(name, func(t *testing.T) {
			raw, target := targetPEFixture(t, "amd64")
			change(raw, target)
			if name != "changed_bytes" {
				target.SHA256 = rawSHA(raw)
			}
			if _, err := VerifyTargetUKI(context.Background(), bytes.NewReader(raw), int64(len(raw)), target, "amd64"); err == nil {
				t.Fatal("invalid target image accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	raw, target := targetPEFixture(t, "amd64")
	if _, err := VerifyTargetUKI(ctx, bytes.NewReader(raw), int64(len(raw)), target, "amd64"); err == nil {
		t.Fatal("canceled UKI check accepted")
	}
}
