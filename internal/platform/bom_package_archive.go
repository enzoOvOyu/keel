package platform

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	maxBOMArchiveBytes  = 512 << 20
	maxBOMExpandedBytes = 1024 << 20
	maxBOMEntries       = 10000
)

// ReadBOMPackageArchive accepts a .zip or .tar.gz package with either a
// one enclosing root directory. It rejects links,
// special entries, traversal, duplicate names and expansion bombs before the
// package validator sees any file. The upload layer must enforce the compressed
// size limit while streaming the request body to temporary storage.
func ReadBOMPackageArchive(name string, data []byte) (map[string][]byte, error) {
	if len(data) == 0 || len(data) > maxBOMArchiveBytes {
		return nil, errors.New("BOM archive size outside allowed range")
	}
	raw := map[string][]byte{}
	var expanded int64
	add := func(file string, size int64, reader io.Reader) error {
		file = strings.TrimPrefix(file, "./")
		if !packagePath(file) || strings.HasSuffix(file, "/") {
			return fmt.Errorf("unsafe BOM archive path %q", file)
		}
		if _, exists := raw[file]; exists {
			return fmt.Errorf("duplicate BOM archive path %q", file)
		}
		if len(raw) >= maxBOMEntries || size < 0 || size > maxBOMExpandedBytes || expanded+size > maxBOMExpandedBytes {
			return errors.New("BOM archive expansion limit exceeded")
		}
		b, e := io.ReadAll(io.LimitReader(reader, size+1))
		if e != nil {
			return e
		}
		if int64(len(b)) != size {
			return fmt.Errorf("BOM archive size mismatch for %q", file)
		}
		raw[file] = b
		expanded += size
		return nil
	}
	switch {
	case strings.HasSuffix(strings.ToLower(name), ".zip"):
		zr, e := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if e != nil {
			return nil, e
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			if !f.FileInfo().Mode().IsRegular() {
				return nil, fmt.Errorf("nonregular BOM archive entry %q", f.Name)
			}
			if f.UncompressedSize64 > maxBOMExpandedBytes {
				return nil, errors.New("BOM archive entry too large")
			}
			r, e := f.Open()
			if e != nil {
				return nil, e
			}
			e = add(f.Name, int64(f.UncompressedSize64), r)
			closeErr := r.Close()
			if e != nil {
				return nil, e
			}
			if closeErr != nil {
				return nil, closeErr
			}
		}
	case strings.HasSuffix(strings.ToLower(name), ".tar.gz"):
		gz, e := gzip.NewReader(bytes.NewReader(data))
		if e != nil {
			return nil, e
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			h, e := tr.Next()
			if errors.Is(e, io.EOF) {
				break
			}
			if e != nil {
				return nil, e
			}
			if h.Typeflag == tar.TypeDir {
				continue
			}
			if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
				return nil, fmt.Errorf("nonregular BOM archive entry %q", h.Name)
			}
			if e = add(h.Name, h.Size, tr); e != nil {
				return nil, e
			}
		}
	default:
		return nil, errors.New("BOM package must be .zip or .tar.gz")
	}
	var prefix string
	for file := range raw {
		slash := strings.IndexByte(file, '/')
		if slash < 1 {
			return nil, errors.New("BOM archive has no single root directory")
		}
		candidate := file[:slash+1]
		if prefix == "" {
			prefix = candidate
		} else if prefix != candidate {
			return nil, errors.New("BOM archive has multiple root directories")
		}
	}
	if _, ok := raw[prefix+"manifest.json"]; !ok {
		return nil, errors.New("BOM archive missing manifest.json")
	}
	normalized := make(map[string][]byte, len(raw))
	for file, data := range raw {
		relative := strings.TrimPrefix(file, prefix)
		if !packagePath(relative) {
			return nil, fmt.Errorf("unsafe BOM package path %q", relative)
		}
		if _, duplicate := normalized[relative]; duplicate {
			return nil, fmt.Errorf("duplicate BOM package path %q", relative)
		}
		normalized[relative] = data
	}
	return normalized, nil
}
