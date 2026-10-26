package mediamanifest

import (
	"archive/tar"
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const bundleManifestName = "manifest.json"

// bundleSize includes fixed USTAR headers, block padding and both end markers.
// Manifest validation bounds the sum to 129 artifacts of at most 1 GiB each.
func bundleSize(manifestBytes int, payload Payload) int64 {
	size := int64(1024 + 512 + ((manifestBytes+511)/512)*512)
	for _, node := range payload.Artifacts {
		size += 512 + ((node.Size+511)/512)*512
	}
	return size
}
func bundleHeader(name string, size int64) *tar.Header {
	return &tar.Header{Name: name, Mode: 0600, Size: size, Typeflag: tar.TypeReg, ModTime: time.Unix(0, 0), Format: tar.FormatUSTAR}
}
func regularBundleInput(path string) (*os.File, os.FileInfo, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, errors.New("cannot open regular bundle input")
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, errors.New("bundle inputs must be regular files")
	}
	return file, info, nil
}

// CreateBundle verifies independently trusted metadata, then hashes exactly the
// bytes written into a portable USTAR archive. Failure never publishes a partial
// output; an existing output (including a symlink) is never replaced.
func CreateBundle(raw []byte, trusted ed25519.PublicKey, directory, output string) (*Envelope, error) {
	manifest, err := Verify(raw, trusted)
	if err != nil {
		return nil, err
	}
	if directory == "" || output == "" {
		return nil, errors.New("artifact directory and new bundle output are required")
	}
	if err := validateNewBundleOutput(output); err != nil {
		return nil, err
	}
	// Temporary output is on the destination filesystem; link publishes it only
	// after all byte checks, archive closure and fsync, without replacing anything.
	file, err := os.CreateTemp(filepath.Dir(output), ".keel-media-bundle-*")
	if err != nil {
		return nil, errors.New("cannot create private bundle staging file")
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	defer file.Close()
	archive := tar.NewWriter(file)
	if err = archive.WriteHeader(bundleHeader(bundleManifestName, int64(len(raw)))); err == nil {
		_, err = archive.Write(raw)
	}
	if err != nil {
		return nil, errors.New("cannot write bundle manifest")
	}
	for _, node := range manifest.Payload.Artifacts {
		source, info, openErr := regularBundleInput(filepath.Join(directory, node.MediaID+".bin"))
		if openErr != nil {
			return nil, identityError(node.MediaID)
		}
		if info.Size() != node.Size {
			source.Close()
			return nil, identityError(node.MediaID)
		}
		err = archive.WriteHeader(bundleHeader(node.MediaID+".bin", node.Size))
		if err != nil {
			source.Close()
			return nil, errors.New("cannot write bundle artifact header")
		}
		hash := sha256.New()
		size, readErr := io.Copy(io.MultiWriter(archive, hash), io.LimitReader(source, node.Size+1))
		source.Close()
		if readErr != nil || size != node.Size || hex.EncodeToString(hash.Sum(nil)) != node.SHA256 {
			return nil, identityError(node.MediaID)
		}
	}
	if err = archive.Close(); err != nil {
		return nil, errors.New("cannot finish bundle archive")
	}
	if err = file.Sync(); err != nil {
		return nil, errors.New("cannot persist bundle archive")
	}
	if err = file.Close(); err != nil {
		return nil, errors.New("cannot close bundle archive")
	}
	if err = os.Link(temporary, output); err != nil {
		return nil, errors.New("cannot publish bundle; existing output is never replaced")
	}
	return manifest, nil
}

// Read exactly one physical USTAR header before asking archive/tar to decode it.
// A generic tar.Reader transparently skips PAX/GNU headers; those must never
// consume arbitrary hidden metadata before the first trusted manifest is read.
func readBundleHeader(reader io.Reader) (*tar.Header, error) {
	var block [512]byte
	if _, err := io.ReadFull(reader, block[:]); err != nil {
		return nil, errors.New("missing bundle header")
	}
	if block[156] != tar.TypeReg || string(block[257:263]) != "ustar\x00" || string(block[263:265]) != "00" {
		return nil, errors.New("only physical regular USTAR entries are accepted")
	}
	header, err := tar.NewReader(bytes.NewReader(block[:])).Next()
	if err != nil || header.Format != tar.FormatUSTAR || header.Linkname != "" || header.Mode != 0600 || header.Uid != 0 || header.Gid != 0 {
		return nil, errors.New("invalid bundle header or metadata")
	}
	return header, nil
}
func readBundlePadding(reader io.Reader, size int64) error {
	padding := (512 - size%512) % 512
	var block [512]byte
	if _, err := io.ReadFull(reader, block[:padding]); err != nil {
		return errors.New("incomplete bundle padding")
	}
	for _, value := range block[:padding] {
		if value != 0 {
			return errors.New("nonzero bundle padding")
		}
	}
	return nil
}

// VerifyBundle reads only the signed manifest and its exact complete fixed file
// set. It never extracts paths, follows archive links, downloads or installs.
// The trusted key must be independently provisioned, never taken from a bundle.
func VerifyBundle(path string, trusted ed25519.PublicKey) (*Envelope, error) {
	file, info, err := regularBundleInput(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	header, err := readBundleHeader(file)
	if err != nil || header.Name != bundleManifestName || header.Size <= 0 || header.Size > MaxBytes {
		return nil, errors.New("bundle must start with a bounded regular manifest.json")
	}
	raw, err := io.ReadAll(io.LimitReader(file, header.Size))
	if err != nil || int64(len(raw)) != header.Size {
		return nil, errors.New("cannot read complete bundle manifest")
	}
	if err = readBundlePadding(file, header.Size); err != nil {
		return nil, err
	}
	manifest, err := Verify(raw, trusted)
	if err != nil {
		return nil, err
	}
	if info.Size() != bundleSize(len(raw), manifest.Payload) {
		return nil, errors.New("bundle length does not match the signed file set and end markers")
	}
	for _, node := range manifest.Payload.Artifacts {
		header, err := readBundleHeader(file)
		if err != nil {
			return nil, err
		}
		if header.Name != node.MediaID+".bin" || header.Size != node.Size {
			return nil, errors.New("unexpected bundle file, order or signed size")
		}
		hash := sha256.New()
		size, err := io.Copy(hash, io.LimitReader(file, node.Size))
		if err != nil || size != node.Size || hex.EncodeToString(hash.Sum(nil)) != node.SHA256 {
			return nil, identityError(node.MediaID)
		}
		if err = readBundlePadding(file, node.Size); err != nil {
			return nil, err
		}
	}
	var end [1024]byte
	if _, err = io.ReadFull(file, end[:]); err != nil || end != [1024]byte{} {
		return nil, errors.New("invalid bundle end markers")
	}
	var extra [1]byte
	if count, err := file.Read(extra[:]); count != 0 || err != io.EOF {
		return nil, errors.New("bundle contains trailing data")
	}
	// Reject changes to the regular input's length while it was being verified.
	current, err := file.Stat()
	if err != nil || current.Size() != info.Size() {
		return nil, errors.New("bundle changed during verification")
	}
	return manifest, nil
}

// Shared publication boundary for offline media and installer helper archives.
func validateNewBundleOutput(output string) error {
	// An untrusted writable parent could replace the staging pathname between
	// byte verification and publication. Use an operator-owned protected directory.
	parentInfo, parentErr := os.Lstat(filepath.Dir(output))
	if parentErr != nil || !parentInfo.IsDir() || parentInfo.Mode().Perm()&0022 != 0 {
		return errors.New("bundle output directory must be protected from group/other writes")
	}
	owner, ok := parentInfo.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return errors.New("bundle output directory must belong to the current operator")
	}
	if _, err := os.Lstat(output); err == nil || !os.IsNotExist(err) {
		return errors.New("cannot publish bundle; existing output is never replaced")
	}
	return nil
}
