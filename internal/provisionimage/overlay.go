// Package provisionimage creates deterministic read-only helper overlays for an
// independently reviewed installation initrd. An overlay is not a bootable OS,
// a disk installer, an authorization grant or proof of image acceptance.
package provisionimage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

const Schema = "keel.installer-components/v1"
const PreflightPath = "usr/local/libexec/keel-preflight"
const SSHHelperPath = "usr/local/libexec/keel-ssh-runner"
const MaxBinaryBytes int64 = 64 << 20

var ErrInvalid = errors.New("invalid approved installer helper or overlay input")

type Binary interface {
	io.ReaderAt
	io.ReadSeeker
}
type Input struct {
	MediaID string
	SHA256  string
	Size    int64
	File    Binary
}
type Component struct {
	Path    string `json:"path"`
	MediaID string `json:"media_id"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}
type Metadata struct {
	Schema                  string              `json:"schema"`
	Arch                    string              `json:"arch"`
	Helpers                 mediaboot.HelperSet `json:"helpers"`
	ApprovedHelperSetSHA256 string              `json:"approved_helper_set_sha256"`
	ApprovedManifestSHA256  string              `json:"approved_manifest_sha256"`
	Components              []Component         `json:"components"`
}
type Result struct {
	Metadata Metadata `json:"metadata"`
	SHA256   string   `json:"sha256"`
	Size     int64    `json:"size"`
}

// WriteOverlay never executes binaries or touches a filesystem. The caller
// verifies the independently trusted signed manifest and publishes the output
// atomically only after success. Digests are checked both before and while
// writing, so a concurrently changed input never becomes a successful output.
func WriteOverlay(ctx context.Context, w io.Writer, helpers mediaboot.HelperSet, approvedManifest, helperConfig []byte, preflight, ssh Input) (*Result, error) {
	arch := helpers.Arch
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if w == nil || (arch != "amd64" && arch != "arm64") || helpers.Validate() != nil || len(approvedManifest) == 0 || len(approvedManifest) > 16<<20 || preflight.MediaID != helpers.PreflightMediaID || ssh.MediaID != helpers.SSHHelperMediaID || preflight.SHA256 != helpers.PreflightSHA256 || ssh.SHA256 != helpers.SSHHelperSHA256 {
		return nil, ErrInvalid
	}
	decoded, err := mediaboot.DecodeHelperSet(helperConfig)
	if err != nil || decoded != helpers {
		return nil, ErrInvalid
	}
	for _, input := range []Input{preflight, ssh} {
		if err := verifyBinary(ctx, input, arch); err != nil {
			return nil, err
		}
	}
	sum := sha256.Sum256(approvedManifest)
	configSum := sha256.Sum256(helperConfig)
	metadata := Metadata{ApprovedHelperSetSHA256: hex.EncodeToString(configSum[:]), Schema: Schema, Arch: arch, Helpers: helpers, ApprovedManifestSHA256: hex.EncodeToString(sum[:]), Components: []Component{{Path: PreflightPath, MediaID: preflight.MediaID, SHA256: preflight.SHA256, Size: preflight.Size}, {Path: SSHHelperPath, MediaID: ssh.MediaID, SHA256: ssh.SHA256, Size: ssh.Size}}}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	counter := &countingWriter{writer: io.MultiWriter(w, hash)}
	compressed, err := gzip.NewWriterLevel(counter, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	// gzip defaults to no name/comment/mtime and OS=255, independent of input files.
	archive := cpioWriter{writer: compressed}
	for _, directory := range []string{"usr", "usr/lib", "usr/lib/keel", "usr/local", "usr/local/libexec"} {
		if err := archive.entry(directory, 0040755, 0, nil); err != nil {
			return nil, err
		}
	}
	for _, file := range []struct {
		path string
		data []byte
	}{{"usr/lib/keel/installer-components.json", raw}, {"usr/lib/keel/approved-media-manifest.json", approvedManifest}, {"usr/lib/keel/approved-helper-set.json", helperConfig}} {
		if err := archive.entry(file.path, 0100444, int64(len(file.data)), bytes.NewReader(file.data)); err != nil {
			return nil, err
		}
	}
	for i, input := range []Input{preflight, ssh} {
		if _, err := input.File.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		hash := sha256.New()
		if err := archive.entry(metadata.Components[i].Path, 0100555, input.Size, io.TeeReader(contextReader{ctx, input.File}, hash)); err != nil {
			return nil, err
		}
		var extra [1]byte
		n, err := input.File.Read(extra[:])
		if n != 0 || err != io.EOF || hex.EncodeToString(hash.Sum(nil)) != input.SHA256 {
			return nil, ErrInvalid
		}
	}
	if err := archive.entry("TRAILER!!!", 0, 0, nil); err != nil {
		return nil, err
	}
	if err := compressed.Close(); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return &Result{Metadata: metadata, SHA256: hex.EncodeToString(hash.Sum(nil)), Size: counter.size}, nil
}
func verifyBinary(ctx context.Context, in Input, arch string) error {
	if in.File == nil || in.Size < 64 || in.Size > MaxBinaryBytes {
		return ErrInvalid
	}
	if _, err := in.File.Seek(0, io.SeekStart); err != nil {
		return ErrInvalid
	}
	hash := sha256.New()
	size, err := io.Copy(hash, io.LimitReader(contextReader{ctx, in.File}, in.Size+1))
	if err != nil {
		return err
	}
	if size != in.Size || hex.EncodeToString(hash.Sum(nil)) != in.SHA256 {
		return ErrInvalid
	}
	executable, err := elf.NewFile(io.NewSectionReader(in.File, 0, in.Size))
	if err != nil {
		return ErrInvalid
	}
	defer executable.Close()
	machine := elf.EM_X86_64
	if arch == "arm64" {
		machine = elf.EM_AARCH64
	}
	if executable.Class != elf.ELFCLASS64 || executable.Data != elf.ELFDATA2LSB || executable.Machine != machine || (executable.Type != elf.ET_EXEC && executable.Type != elf.ET_DYN) || len(executable.Progs) == 0 || len(executable.Progs) > 128 || len(executable.Sections) > 4096 {
		return ErrInvalid
	}
	load, entry := false, false
	for _, program := range executable.Progs {
		if program.Type == elf.PT_INTERP {
			return ErrInvalid
		}
		if program.Type == elf.PT_LOAD {
			if program.Filesz > uint64(in.Size) || program.Off > uint64(in.Size)-program.Filesz {
				return ErrInvalid
			}
			if program.Memsz < program.Filesz || program.Memsz > 1<<30 {
				return ErrInvalid
			}
			load = true
			if program.Flags&elf.PF_X != 0 && executable.Entry >= program.Vaddr && executable.Entry-program.Vaddr < program.Memsz {
				entry = true
			}
		}
	}
	libraries, err := executable.ImportedLibraries()
	if err != nil || len(libraries) > 0 || !load || !entry {
		return ErrInvalid
	}
	return nil
}

type cpioWriter struct {
	writer io.Writer
	offset int64
	inode  uint32
}

func (c *cpioWriter) write(raw []byte) error {
	n, err := c.writer.Write(raw)
	c.offset += int64(n)
	if err == nil && n != len(raw) {
		return io.ErrShortWrite
	}
	return err
}
func (c *cpioWriter) pad() error {
	padding := (4 - c.offset%4) % 4
	return c.write(make([]byte, padding))
}
func (c *cpioWriter) entry(name string, mode uint32, size int64, data io.Reader) error {
	if size < 0 || size > 1<<30 || len(name) < 1 || len(name) > 255 {
		return ErrInvalid
	}
	if err := c.pad(); err != nil {
		return err
	}
	c.inode++
	header := fmt.Sprintf("070701%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x%08x", c.inode, mode, 0, 0, 1, 0, size, 0, 0, 0, 0, len(name)+1, 0)
	if len(header) != 110 {
		return ErrInvalid
	}
	if err := c.write([]byte(header + name + "\x00")); err != nil {
		return err
	}
	if err := c.pad(); err != nil {
		return err
	}
	if size > 0 {
		if data == nil {
			return ErrInvalid
		}
		n, err := io.CopyN(c.writer, data, size)
		c.offset += n
		if err != nil {
			return err
		}
	}
	return c.pad()
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(raw []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(raw)
}

type countingWriter struct {
	writer io.Writer
	size   int64
}

func (w *countingWriter) Write(raw []byte) (int, error) {
	n, err := w.writer.Write(raw)
	w.size += int64(n)
	return n, err
}
