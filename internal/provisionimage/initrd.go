package provisionimage

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

const InitrdSchema = "keel.installer-initrd/v1"
const InitPath = "init"
const SMARTPath = "usr/sbin/smartctl"
const BootCAPath = "etc/keel/boot-ca.pem"
const MaxInitrdBytes int64 = 288 << 20

type RuntimeMetadata struct {
	Schema                 string               `json:"schema"`
	Arch                   string               `json:"arch"`
	Runtime                mediaboot.RuntimeSet `json:"runtime"`
	ApprovedRuntimeSHA256  string               `json:"approved_runtime_sha256"`
	ApprovedManifestSHA256 string               `json:"approved_manifest_sha256"`
	Components             []Component          `json:"components"`
}
type InitrdResult struct {
	Metadata RuntimeMetadata `json:"metadata"`
	SHA256   string          `json:"sha256"`
	Size     int64           `json:"size"`
}
type InitrdContents struct {
	Metadata   RuntimeMetadata
	Manifest   []byte
	RuntimeSet []byte
}

var runtimeDirectories = []string{"dev", "proc", "sys", "run", "etc", "etc/keel", "usr", "usr/lib", "usr/lib/keel", "usr/local", "usr/local/libexec", "usr/sbin"}
var runtimePaths = []string{InitPath, PreflightPath, SSHHelperPath, SMARTPath, BootCAPath}

func rawSHA(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }

func verifyRuntimeInput(ctx context.Context, in Input, arch string, index int) error {
	if index < 4 {
		return VerifyStaticBinary(ctx, in, arch)
	}
	if index != 4 || in.File == nil || in.Size < 1 || in.Size > 1<<20 || ctx.Err() != nil {
		return ErrInvalid
	}
	if _, err := in.File.Seek(0, io.SeekStart); err != nil {
		return ErrInvalid
	}
	raw, err := io.ReadAll(io.LimitReader(contextReader{ctx, in.File}, in.Size+1))
	if err != nil || int64(len(raw)) != in.Size || rawSHA(raw) != in.SHA256 {
		return ErrInvalid
	}
	if _, err = ParseBootRoots(raw, time.Now()); err != nil {
		return ErrInvalid
	}
	return nil
}

// WriteInitrd produces the complete read-only runtime archive, without booting,
// extracting, running a component, or requiring a future OS/initrd recipe.
// The caller authenticates the signed approval records before this byte layer.
func WriteInitrd(ctx context.Context, w io.Writer, set mediaboot.RuntimeSet, manifest, config []byte, inputs []Input) (*InitrdResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	decoded, err := mediaboot.DecodeRuntimeSet(config)
	if w == nil || err != nil || decoded != set || set.Validate() != nil || len(manifest) == 0 || len(manifest) > 16<<20 || len(inputs) != 5 {
		return nil, ErrInvalid
	}
	metadata := RuntimeMetadata{Schema: InitrdSchema, Arch: set.Arch, Runtime: set, ApprovedRuntimeSHA256: rawSHA(config), ApprovedManifestSHA256: rawSHA(manifest), Components: []Component{}}
	for i, want := range set.Components() {
		in := inputs[i]
		if in.MediaID != want.MediaID || in.SHA256 != want.SHA256 || verifyRuntimeInput(ctx, in, set.Arch, i) != nil {
			return nil, ErrInvalid
		}
		metadata.Components = append(metadata.Components, Component{Path: runtimePaths[i], MediaID: in.MediaID, SHA256: in.SHA256, Size: in.Size})
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	hash := sha256.New()
	counter := &countingWriter{writer: io.MultiWriter(w, hash)}
	compressed, err := gzip.NewWriterLevel(counter, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	archive := cpioWriter{writer: compressed}
	for _, dir := range runtimeDirectories {
		if err = archive.entry(dir, 0040755, 0, nil); err != nil {
			return nil, err
		}
	}
	for _, file := range []struct {
		path string
		data []byte
	}{{"usr/lib/keel/installer-runtime.json", encoded}, {"usr/lib/keel/approved-media-manifest.json", manifest}, {"usr/lib/keel/approved-runtime-set.json", config}} {
		if err = archive.entry(file.path, 0100444, int64(len(file.data)), bytes.NewReader(file.data)); err != nil {
			return nil, err
		}
	}
	for i, in := range inputs {
		if _, err = in.File.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		h := sha256.New()
		mode := uint32(0100555)
		if i == 4 {
			mode = 0100444
		}
		if err = archive.entry(runtimePaths[i], mode, in.Size, io.TeeReader(contextReader{ctx, in.File}, h)); err != nil {
			return nil, err
		}
		var extra [1]byte
		n, e := in.File.Read(extra[:])
		if n != 0 || e != io.EOF || hex.EncodeToString(h.Sum(nil)) != in.SHA256 {
			return nil, ErrInvalid
		}
	}
	if err = archive.entry("TRAILER!!!", 0, 0, nil); err != nil {
		return nil, err
	}
	if err = compressed.Close(); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if counter.size > MaxInitrdBytes {
		return nil, ErrInvalid
	}
	return &InitrdResult{Metadata: metadata, SHA256: hex.EncodeToString(hash.Sum(nil)), Size: counter.size}, nil
}

// ReadInitrd accepts only this exact archive layout and metadata, with bounded
// expansion, canonical modes, no links/devices, no extraction and no execution.
// Its embedded public manifest must still be authenticated independently.
func ReadInitrd(ctx context.Context, reader io.Reader) (*InitrdContents, error) {
	if reader == nil || ctx.Err() != nil {
		return nil, ErrInvalid
	}
	buffered := bufio.NewReader(io.LimitReader(contextReader{ctx, reader}, MaxInitrdBytes+1))
	compressed, err := gzip.NewReader(buffered)
	if err != nil {
		return nil, ErrInvalid
	}
	defer compressed.Close()
	compressed.Multistream(false)
	if compressed.Name != "" || compressed.Comment != "" || len(compressed.Extra) != 0 || !compressed.ModTime.IsZero() || compressed.OS != 255 {
		return nil, ErrInvalid
	}
	archive := newcReader{reader: compressed}
	for _, dir := range runtimeDirectories {
		if _, err = archive.entry(dir, 0040755, 0); err != nil {
			return nil, err
		}
	}
	files := map[string][]byte{}
	for _, entry := range []struct {
		path  string
		limit int64
	}{{"usr/lib/keel/installer-runtime.json", 64 << 10}, {"usr/lib/keel/approved-media-manifest.json", 16 << 20}, {"usr/lib/keel/approved-runtime-set.json", 64 << 10}, {InitPath, MaxBinaryBytes}, {PreflightPath, MaxBinaryBytes}, {SSHHelperPath, MaxBinaryBytes}, {SMARTPath, MaxBinaryBytes}, {BootCAPath, 1 << 20}} {
		mode := uint64(0100444)
		if entry.path == InitPath || entry.path == PreflightPath || entry.path == SSHHelperPath || entry.path == SMARTPath {
			mode = 0100555
		}
		data, e := archive.entry(entry.path, mode, entry.limit)
		if e != nil || len(data) == 0 {
			return nil, ErrInvalid
		}
		files[entry.path] = data
	}
	if _, err = archive.entry("TRAILER!!!", 0, 0); err != nil {
		return nil, err
	}
	var extra [1]byte
	if n, e := compressed.Read(extra[:]); n != 0 || e != io.EOF {
		return nil, ErrInvalid
	}
	if _, err = buffered.ReadByte(); err != io.EOF {
		return nil, ErrInvalid
	}
	var metadata RuntimeMetadata
	raw := files["usr/lib/keel/installer-runtime.json"]
	if json.Unmarshal(raw, &metadata) != nil {
		return nil, ErrInvalid
	}
	canonical, err := json.Marshal(metadata)
	if err != nil || !bytes.Equal(raw, canonical) || metadata.Schema != InitrdSchema || metadata.Runtime.Validate() != nil || metadata.Arch != metadata.Runtime.Arch || len(metadata.Components) != 5 {
		return nil, ErrInvalid
	}
	config := files["usr/lib/keel/approved-runtime-set.json"]
	set, err := mediaboot.DecodeRuntimeSet(config)
	manifest := files["usr/lib/keel/approved-media-manifest.json"]
	if err != nil || set != metadata.Runtime || metadata.ApprovedRuntimeSHA256 != rawSHA(config) || metadata.ApprovedManifestSHA256 != rawSHA(manifest) {
		return nil, ErrInvalid
	}
	for i, want := range set.Components() {
		c := metadata.Components[i]
		data := files[runtimePaths[i]]
		if c.Path != runtimePaths[i] || c.MediaID != want.MediaID || c.SHA256 != want.SHA256 || c.Size != int64(len(data)) || verifyRuntimeInput(ctx, Input{MediaID: c.MediaID, SHA256: c.SHA256, Size: c.Size, File: bytes.NewReader(data)}, set.Arch, i) != nil {
			return nil, ErrInvalid
		}
	}
	return &InitrdContents{Metadata: metadata, Manifest: manifest, RuntimeSet: config}, nil
}
