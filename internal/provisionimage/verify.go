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
	"strconv"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

const MaxOverlayBytes int64 = 160 << 20

// Contents is byte-verified but not authenticated until the caller verifies the
// embedded manifest using its independently provisioned signing trust root and
// binds the helper-set config and component metadata to those signed records.
type Contents struct {
	Metadata  Metadata
	Manifest  []byte
	HelperSet []byte
}

// ReadOverlay parses one bounded gzip/newc archive in memory without extracting
// any path or executing any binary. It accepts only the exact owned file set.
func ReadOverlay(ctx context.Context, reader io.Reader) (*Contents, error) {
	buffered := bufio.NewReader(io.LimitReader(contextReader{ctx, reader}, MaxOverlayBytes+1))
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
	files := map[string][]byte{}
	directories := []string{"usr", "usr/lib", "usr/lib/keel", "usr/local", "usr/local/libexec"}
	entries := []struct {
		name  string
		mode  uint64
		limit int64
	}{}
	for _, directory := range directories {
		entries = append(entries, struct {
			name  string
			mode  uint64
			limit int64
		}{directory, 0040755, 0})
	}
	for _, file := range []struct {
		name  string
		limit int64
	}{{"usr/lib/keel/installer-components.json", 64 << 10}, {"usr/lib/keel/approved-media-manifest.json", 16 << 20}, {"usr/lib/keel/approved-helper-set.json", 64 << 10}, {PreflightPath, MaxBinaryBytes}, {SSHHelperPath, MaxBinaryBytes}} {
		mode := uint64(0100444)
		if file.name == PreflightPath || file.name == SSHHelperPath {
			mode = 0100555
		}
		entries = append(entries, struct {
			name  string
			mode  uint64
			limit int64
		}{file.name, mode, file.limit})
	}
	entries = append(entries, struct {
		name  string
		mode  uint64
		limit int64
	}{"TRAILER!!!", 0, 0})
	for _, entry := range entries {
		raw, err := archive.entry(entry.name, entry.mode, entry.limit)
		if err != nil {
			return nil, err
		}
		if entry.limit > 0 {
			files[entry.name] = raw
		}
	}
	var extra [1]byte
	if n, err := compressed.Read(extra[:]); n != 0 || err != io.EOF {
		return nil, ErrInvalid
	}
	if _, err := buffered.ReadByte(); err != io.EOF {
		return nil, ErrInvalid
	}
	var metadata Metadata
	rawMetadata := files["usr/lib/keel/installer-components.json"]
	if json.Unmarshal(rawMetadata, &metadata) != nil {
		return nil, ErrInvalid
	}
	canonical, _ := json.Marshal(metadata)
	if !bytes.Equal(rawMetadata, canonical) || metadata.Schema != Schema || metadata.Helpers.Validate() != nil || metadata.Arch != metadata.Helpers.Arch || len(metadata.Components) != 2 {
		return nil, ErrInvalid
	}
	helpers, err := mediaboot.DecodeHelperSet(files["usr/lib/keel/approved-helper-set.json"])
	if err != nil || helpers != metadata.Helpers {
		return nil, ErrInvalid
	}
	manifest := files["usr/lib/keel/approved-media-manifest.json"]
	manifestHash := sha256.Sum256(manifest)
	config := files["usr/lib/keel/approved-helper-set.json"]
	configHash := sha256.Sum256(config)
	if metadata.ApprovedManifestSHA256 != hex.EncodeToString(manifestHash[:]) || metadata.ApprovedHelperSetSHA256 != hex.EncodeToString(configHash[:]) {
		return nil, ErrInvalid
	}
	for i, want := range []struct{ path, id, hash string }{{PreflightPath, helpers.PreflightMediaID, helpers.PreflightSHA256}, {SSHHelperPath, helpers.SSHHelperMediaID, helpers.SSHHelperSHA256}} {
		data := files[want.path]
		component := metadata.Components[i]
		if component.Path != want.path || component.MediaID != want.id || component.SHA256 != want.hash || component.Size != int64(len(data)) {
			return nil, ErrInvalid
		}
		if err := verifyBinary(ctx, Input{MediaID: want.id, SHA256: want.hash, Size: int64(len(data)), File: bytes.NewReader(data)}, metadata.Arch); err != nil {
			return nil, err
		}
	}
	return &Contents{Metadata: metadata, Manifest: manifest, HelperSet: config}, nil
}

type newcReader struct {
	reader io.Reader
	offset int64
	inode  uint64
}

func (r *newcReader) read(raw []byte) error {
	n, err := io.ReadFull(r.reader, raw)
	r.offset += int64(n)
	return err
}
func (r *newcReader) pad() error {
	var padding [3]byte
	size := (4 - r.offset%4) % 4
	if err := r.read(padding[:size]); err != nil {
		return ErrInvalid
	}
	for _, v := range padding[:size] {
		if v != 0 {
			return ErrInvalid
		}
	}
	return nil
}
func (r *newcReader) entry(name string, mode uint64, limit int64) ([]byte, error) {
	if r.pad() != nil {
		return nil, ErrInvalid
	}
	var header [110]byte
	if r.read(header[:]) != nil || string(header[:6]) != "070701" {
		return nil, ErrInvalid
	}
	fields := [13]uint64{}
	for i := range fields {
		v, err := strconv.ParseUint(string(header[6+i*8:14+i*8]), 16, 32)
		if err != nil {
			return nil, ErrInvalid
		}
		fields[i] = v
	}
	r.inode++
	if fields[0] != r.inode || fields[1] != mode || fields[2] != 0 || fields[3] != 0 || fields[4] != 1 || fields[5] != 0 || fields[6] > uint64(limit) || fields[7] != 0 || fields[8] != 0 || fields[9] != 0 || fields[10] != 0 || fields[11] != uint64(len(name)+1) || fields[12] != 0 {
		return nil, ErrInvalid
	}
	path := make([]byte, len(name)+1)
	if r.read(path) != nil || string(path) != name+"\x00" || r.pad() != nil {
		return nil, ErrInvalid
	}
	data := make([]byte, int(fields[6]))
	if r.read(data) != nil || r.pad() != nil {
		return nil, ErrInvalid
	}
	return data, nil
}
