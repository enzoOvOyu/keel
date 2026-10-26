package provisionimage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/hex"
	"io"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

// VerifiedTarget is byte evidence only. Approval, current permission, target
// root contents, firmware trust and actual boot behavior are separate checks.
type VerifiedTarget struct {
	SHA256          string `json:"sha256"`
	Size            int64  `json:"size"`
	Arch            string `json:"arch"`
	KernelRelease   string `json:"kernel_release"`
	CommandLine     string `json:"command_line"`
	OSReleaseSHA256 string `json:"os_release_sha256"`
	EFIFallback     string `json:"efi_fallback"`
}

type targetReader struct {
	ctx context.Context
	r   io.ReaderAt
	n   int64
}

func (r targetReader) ReadAt(p []byte, off int64) (int, error) {
	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}
	if off < 0 || off > r.n || int64(len(p)) > r.n-off {
		return 0, io.ErrUnexpectedEOF
	}
	return r.r.ReadAt(p, off)
}

// VerifyTargetUKI hashes and inspects one bounded PE32+ image without executing
// or extracting it. This intentionally supports one fixed local-root profile,
// not multi-profile/addon selection or network-root command lines. A valid PE
// layout does not attest the behavior of approved executable code or SecureBoot.
func VerifyTargetUKI(ctx context.Context, reader io.ReaderAt, size int64, target *mediaboot.TargetBoot, arch string) (*VerifiedTarget, error) {
	if ctx.Err() != nil || reader == nil || target.Validate() != nil || size != target.Size {
		return nil, ErrInvalid
	}
	fallback, err := mediaboot.TargetEFIFallback(arch)
	if err != nil {
		return nil, ErrInvalid
	}
	r := targetReader{ctx: ctx, r: reader, n: size}
	h := sha256.New()
	if n, err := io.CopyBuffer(h, io.NewSectionReader(r, 0, size), make([]byte, 128<<10)); err != nil || n != size || hex.EncodeToString(h.Sum(nil)) != target.SHA256 {
		return nil, ErrInvalid
	}
	f, err := pe.NewFile(r)
	if err != nil {
		return nil, ErrInvalid
	}
	defer f.Close()
	optional, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	machine := uint16(pe.IMAGE_FILE_MACHINE_AMD64)
	if arch == "arm64" {
		machine = pe.IMAGE_FILE_MACHINE_ARM64
	}
	if !ok || f.Machine != machine || optional.Magic != 0x20b || optional.Subsystem != pe.IMAGE_SUBSYSTEM_EFI_APPLICATION || optional.SizeOfHeaders < 64 || int64(optional.SizeOfHeaders) >= size || f.Characteristics&pe.IMAGE_FILE_EXECUTABLE_IMAGE == 0 || f.Characteristics&pe.IMAGE_FILE_DLL != 0 || len(f.Sections) < 5 || len(f.Sections) > 64 {
		return nil, ErrInvalid
	}
	sections := map[string]*pe.Section{}
	entry := false
	for i, section := range f.Sections {
		if section.Name == "" || sections[section.Name] != nil || section.Name == ".profile" || uint64(section.Offset)+uint64(section.Size) > uint64(size) || (section.Size > 0 && section.Offset < optional.SizeOfHeaders) || uint64(section.VirtualAddress)+uint64(section.VirtualSize) > uint64(optional.SizeOfImage) {
			return nil, ErrInvalid
		}
		for _, prior := range f.Sections[:i] {
			if section.Size > 0 && prior.Size > 0 && uint64(section.Offset) < uint64(prior.Offset)+uint64(prior.Size) && uint64(prior.Offset) < uint64(section.Offset)+uint64(section.Size) {
				return nil, ErrInvalid
			}
			if section.VirtualSize > 0 && prior.VirtualSize > 0 && uint64(section.VirtualAddress) < uint64(prior.VirtualAddress)+uint64(prior.VirtualSize) && uint64(prior.VirtualAddress) < uint64(section.VirtualAddress)+uint64(section.VirtualSize) {
				return nil, ErrInvalid
			}
		}
		sections[section.Name] = section
		if section.Name == ".text" && section.Characteristics&pe.IMAGE_SCN_MEM_EXECUTE != 0 && optional.AddressOfEntryPoint >= section.VirtualAddress && uint64(optional.AddressOfEntryPoint) < uint64(section.VirtualAddress)+uint64(section.VirtualSize) && section.Size > 0 {
			entry = true
		}
	}
	if !entry {
		return nil, ErrInvalid
	}
	for _, name := range []string{".linux", ".initrd"} {
		s := sections[name]
		if s == nil || s.VirtualSize == 0 || s.VirtualSize > s.Size {
			return nil, ErrInvalid
		}
	}
	text := func(name string, limit uint32) ([]byte, error) {
		s := sections[name]
		if s == nil || s.VirtualSize == 0 || s.VirtualSize > limit || s.Size > limit+4096 || s.VirtualSize > s.Size {
			return nil, ErrInvalid
		}
		raw, err := s.Data()
		if err != nil || uint32(len(raw)) != s.Size {
			return nil, ErrInvalid
		}
		// File-alignment padding must not hide a second command or identity.
		meaningful := bytes.TrimRight(raw[:s.VirtualSize], "\x00")
		if len(meaningful) == 0 || bytes.IndexByte(meaningful, 0) >= 0 || !utf8.Valid(meaningful) || !allZero(raw[s.VirtualSize:]) {
			return nil, ErrInvalid
		}
		return meaningful, nil
	}
	cmdline, err := text(".cmdline", 4096)
	if err != nil || string(cmdline) != target.CommandLine {
		return nil, ErrInvalid
	}
	uname, err := text(".uname", 256)
	if err != nil || string(uname) != target.KernelRelease {
		return nil, ErrInvalid
	}
	osrel, err := text(".osrel", 64<<10)
	if err != nil || !bytes.Contains(osrel, []byte("ID=")) {
		return nil, ErrInvalid
	}
	if ctx.Err() != nil {
		return nil, ErrInvalid
	}
	return &VerifiedTarget{SHA256: target.SHA256, Size: size, Arch: arch, KernelRelease: target.KernelRelease, CommandLine: target.CommandLine, OSReleaseSHA256: rawSHA(osrel), EFIFallback: fallback}, nil
}

func allZero(raw []byte) bool {
	for _, v := range raw {
		if v != 0 {
			return false
		}
	}
	return true
}
