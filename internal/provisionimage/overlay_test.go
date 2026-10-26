package provisionimage

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

// Minimal format fixture, never executed and not an actual helper/boot image.
func fixtureELF(arch string) []byte {
	raw := make([]byte, 160)
	copy(raw, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(raw[16:], 2)
	machine := uint16(62)
	if arch == "arm64" {
		machine = 183
	}
	binary.LittleEndian.PutUint16(raw[18:], machine)
	binary.LittleEndian.PutUint32(raw[20:], 1)
	binary.LittleEndian.PutUint64(raw[24:], 0x400078)
	binary.LittleEndian.PutUint64(raw[32:], 64)
	binary.LittleEndian.PutUint16(raw[52:], 64)
	binary.LittleEndian.PutUint16(raw[54:], 56)
	binary.LittleEndian.PutUint16(raw[56:], 1)
	binary.LittleEndian.PutUint32(raw[64:], 1)
	binary.LittleEndian.PutUint32(raw[68:], 5)
	binary.LittleEndian.PutUint64(raw[80:], 0x400000)
	binary.LittleEndian.PutUint64(raw[88:], 0x400000)
	binary.LittleEndian.PutUint64(raw[96:], uint64(len(raw)))
	binary.LittleEndian.PutUint64(raw[104:], uint64(len(raw)))
	binary.LittleEndian.PutUint64(raw[112:], 4096)
	copy(raw[120:], "non-executable software-format fixture")
	return raw
}
func inputFixture(arch string) (mediaboot.HelperSet, Input, Input) {
	raw := fixtureELF(arch)
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	recipe := mediaboot.HelperSet{Schema: mediaboot.HelperSetSchema, Arch: arch, PreflightMediaID: strings.Repeat("2", 32), SSHHelperMediaID: strings.Repeat("3", 32), PreflightSHA256: hash, SSHHelperSHA256: hash}
	return recipe, Input{MediaID: recipe.PreflightMediaID, SHA256: hash, Size: int64(len(raw)), File: bytes.NewReader(raw)}, Input{MediaID: recipe.SSHHelperMediaID, SHA256: hash, Size: int64(len(raw)), File: bytes.NewReader(raw)}
}

type archivedFile struct {
	mode uint64
	data []byte
}

func readNewc(t *testing.T, raw []byte) map[string]archivedFile {
	t.Helper()
	files := map[string]archivedFile{}
	offset := 0
	for {
		offset = (offset + 3) &^ 3
		if len(raw)-offset < 110 {
			t.Fatal("truncated newc")
		}
		header := raw[offset : offset+110]
		if string(header[:6]) != "070701" {
			t.Fatal("wrong newc magic")
		}
		field := func(i int) uint64 {
			v, err := strconv.ParseUint(string(header[6+i*8:14+i*8]), 16, 32)
			if err != nil {
				t.Fatal(err)
			}
			return v
		}
		size, namesize := int(field(6)), int(field(11))
		if field(2) != 0 || field(3) != 0 || field(4) != 1 || field(5) != 0 || field(12) != 0 {
			t.Fatal("nondeterministic metadata")
		}
		offset += 110
		if namesize < 1 || namesize > 256 || len(raw)-offset < namesize || raw[offset+namesize-1] != 0 {
			t.Fatal("invalid path")
		}
		name := string(raw[offset : offset+namesize-1])
		offset = (offset + namesize + 3) &^ 3
		if name == "TRAILER!!!" {
			if size != 0 {
				t.Fatal("trailer data")
			}
			break
		}
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") || len(raw)-offset < size {
			t.Fatal("unsafe/truncated archive")
		}
		if _, ok := files[name]; ok {
			t.Fatal("duplicate entry")
		}
		files[name] = archivedFile{field(1), append([]byte{}, raw[offset:offset+size]...)}
		offset += size
	}
	return files
}
func TestOverlayDeterministicFormatAndPinnedComponents(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			recipe, p, s := inputFixture(arch)
			var out bytes.Buffer
			result, err := WriteOverlay(context.Background(), &out, recipe, []byte(`{"fixture":"signed elsewhere"}`), helperJSON(recipe), p, s)
			if err != nil {
				t.Fatal(err)
			}
			var again bytes.Buffer
			if _, err = WriteOverlay(context.Background(), &again, recipe, []byte(`{"fixture":"signed elsewhere"}`), helperJSON(recipe), p, s); err != nil || !bytes.Equal(out.Bytes(), again.Bytes()) {
				t.Fatal("overlay not reproducible", err)
			}
			sum := sha256.Sum256(out.Bytes())
			if result.Size != int64(out.Len()) || result.SHA256 != hex.EncodeToString(sum[:]) {
				t.Fatal("output metadata not exact")
			}
			decompress, err := gzip.NewReader(bytes.NewReader(out.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			raw, err := io.ReadAll(decompress)
			if err != nil {
				t.Fatal(err)
			}
			decompress.Close()
			files := readNewc(t, raw)
			if len(files) != 10 {
				t.Fatalf("unexpected contents %d", len(files))
			}
			for _, path := range []string{PreflightPath, SSHHelperPath} {
				if files[path].mode != 0100555 || !bytes.Equal(files[path].data, fixtureELF(arch)) {
					t.Fatal("helper bytes/mode changed")
				}
			}
			for _, path := range []string{"init", "etc/passwd", "etc/shadow", "dev/sda"} {
				if _, ok := files[path]; ok {
					t.Fatal("unowned/destructive entry")
				}
			}
			var metadata Metadata
			if json.Unmarshal(files["usr/lib/keel/installer-components.json"].data, &metadata) != nil || metadata.Arch != arch || metadata.Helpers.PreflightSHA256 != recipe.PreflightSHA256 {
				t.Fatal("component identity missing")
			}
		})
	}
}
func TestOverlayRejectsWrongArchHashDynamicAndMalformedBinary(t *testing.T) {
	for name, edit := range map[string]func(*mediaboot.HelperSet, *Input, *Input){
		"wrong_arch": func(r *mediaboot.HelperSet, p, _ *Input) {
			raw := fixtureELF("arm64")
			sum := sha256.Sum256(raw)
			p.SHA256 = hex.EncodeToString(sum[:])
			r.PreflightSHA256 = p.SHA256
			p.File = bytes.NewReader(raw)
		},
		"wrong_hash": func(r *mediaboot.HelperSet, p, _ *Input) {
			r.PreflightSHA256 = strings.Repeat("a", 64)
			p.SHA256 = r.PreflightSHA256
		},
		"changed_media": func(_ *mediaboot.HelperSet, p, _ *Input) { p.MediaID = strings.Repeat("f", 32) },
		"wrong_size":    func(_ *mediaboot.HelperSet, p, _ *Input) { p.Size-- },
		"dynamic_interpreter": func(r *mediaboot.HelperSet, p, _ *Input) {
			raw := fixtureELF("amd64")
			binary.LittleEndian.PutUint32(raw[64:], 3)
			sum := sha256.Sum256(raw)
			p.SHA256 = hex.EncodeToString(sum[:])
			r.PreflightSHA256 = p.SHA256
			p.File = bytes.NewReader(raw)
		},
		"non_executable_entry": func(r *mediaboot.HelperSet, p, _ *Input) {
			raw := fixtureELF("amd64")
			binary.LittleEndian.PutUint64(raw[24:], 0)
			sum := sha256.Sum256(raw)
			p.SHA256 = hex.EncodeToString(sum[:])
			r.PreflightSHA256 = p.SHA256
			p.File = bytes.NewReader(raw)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r, p, s := inputFixture("amd64")
			edit(&r, &p, &s)
			var out bytes.Buffer
			if _, err := WriteOverlay(context.Background(), &out, r, []byte(`{}`), helperJSON(r), p, s); err == nil || out.Len() != 0 {
				t.Fatal("invalid input accepted or partially emitted")
			}
		})
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full fixture") }
func TestOverlayCancellationAndWriteFailure(t *testing.T) {
	r, p, s := inputFixture("amd64")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := WriteOverlay(ctx, io.Discard, r, []byte(`{}`), helperJSON(r), p, s); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := WriteOverlay(context.Background(), failingWriter{}, r, []byte(`{}`), helperJSON(r), p, s); err == nil {
		t.Fatal("writer failure swallowed")
	}
}

type swappingBinary struct {
	*bytes.Reader
	original []byte
	seeks    int
}

func (s *swappingBinary) Seek(offset int64, whence int) (int64, error) {
	if offset == 0 && whence == io.SeekStart {
		s.seeks++
		if s.seeks == 2 {
			changed := append([]byte{}, s.original...)
			changed[len(changed)-1] ^= 1
			s.Reader = bytes.NewReader(changed)
		}
	}
	return s.Reader.Seek(offset, whence)
}
func TestOverlayDetectsChangedInputDuringArchiveCopy(t *testing.T) {
	r, p, s := inputFixture("amd64")
	raw := fixtureELF("amd64")
	p.File = &swappingBinary{Reader: bytes.NewReader(raw), original: raw}
	var out bytes.Buffer
	if _, err := WriteOverlay(context.Background(), &out, r, []byte(`{}`), helperJSON(r), p, s); !errors.Is(err, ErrInvalid) {
		t.Fatalf("concurrent replacement accepted: %v", err)
	}
}

func helperJSON(helpers mediaboot.HelperSet) []byte { raw, _ := json.Marshal(helpers); return raw }

func TestReadOverlayChecksExactFileSetAndBytes(t *testing.T) {
	r, p, s := inputFixture("amd64")
	var out bytes.Buffer
	if _, err := WriteOverlay(context.Background(), &out, r, []byte(`{"fixture":"not a signature"}`), helperJSON(r), p, s); err != nil {
		t.Fatal(err)
	}
	parsed, err := ReadOverlay(context.Background(), bytes.NewReader(out.Bytes()))
	if err != nil || parsed.Metadata.Helpers != r {
		t.Fatalf("%+v %v", parsed, err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(gz)
	gz.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"path_traversal", func(raw []byte) []byte {
			return bytes.Replace(raw, []byte(PreflightPath), []byte("../"+PreflightPath[3:]), 1)
		}},
		{"changed_binary", func(raw []byte) []byte {
			index := bytes.Index(raw, fixtureELF("amd64"))
			if index < 0 {
				t.Fatal("fixture missing")
			}
			raw[index+127] ^= 1
			return raw
		}},
		{"extra_cpio_data", func(raw []byte) []byte { return append(raw, []byte("unexpected")...) }},
		{"wrong_owner", func(raw []byte) []byte { copy(raw[22:30], []byte("00000001")); return raw }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changed := tc.change(append([]byte{}, raw...))
			var archive bytes.Buffer
			compressed := gzip.NewWriter(&archive)
			compressed.Write(changed)
			compressed.Close()
			if _, err := ReadOverlay(context.Background(), bytes.NewReader(archive.Bytes())); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
	for _, raw := range [][]byte{out.Bytes()[:out.Len()-1], append(append([]byte{}, out.Bytes()...), 1), append(append([]byte{}, out.Bytes()...), out.Bytes()...)} {
		if _, err := ReadOverlay(context.Background(), bytes.NewReader(raw)); err == nil {
			t.Fatal("accepted truncated/trailing/concatenated archive")
		}
	}
}
