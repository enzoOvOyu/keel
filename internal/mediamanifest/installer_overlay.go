package mediamanifest

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"io"
	"os"
	"path/filepath"

	"github.com/enzoOvOyu/keel/internal/provisionimage"
)

// CreateInstallerOverlay verifies independently trusted approval metadata and
// both exact helper binaries, then creates a private reproducible gzip/newc
// overlay. The independent helper-set config precedes image production, so no
// final OS/initrd recipe is required. No image is booted, and no live
// installation authorization or production registry approval is created.
func CreateInstallerOverlay(ctx context.Context, raw []byte, trusted ed25519.PublicKey, directory, output string) (*provisionimage.Result, error) {
	manifest, err := Verify(raw, trusted)
	if err != nil {
		return nil, err
	}
	nodes := make(map[string]Artifact, len(manifest.Payload.Artifacts))
	for _, a := range manifest.Payload.Artifacts {
		nodes[a.MediaID] = a
	}
	root := nodes[manifest.Payload.RootID]
	if root.Type != "config" || (root.Arch != "amd64" && root.Arch != "arm64") || root.Size < 1 || root.Size > 64<<10 || directory == "" || output == "" || len(root.Dependencies) != 2 {
		return nil, errors.New("an independently approved helper-set config with two direct binaries, artifact directory and new output are required")
	}
	config, info, err := regularBundleInput(filepath.Join(directory, root.MediaID+".bin"))
	if err != nil {
		return nil, identityError(root.MediaID)
	}
	if info.Size() != root.Size {
		config.Close()
		return nil, identityError(root.MediaID)
	}
	configBytes, readErr := io.ReadAll(io.LimitReader(config, root.Size+1))
	config.Close()
	if readErr != nil {
		return nil, identityError(root.MediaID)
	}
	helpers, records, err := installerHelperRecords(manifest, configBytes)
	if err != nil {
		return nil, err
	}
	var inputs []provisionimage.Input
	for _, artifact := range records {
		source, info, err := regularBundleInput(filepath.Join(directory, artifact.MediaID+".bin"))
		if err != nil {
			return nil, identityError(artifact.MediaID)
		}
		defer source.Close()
		if info.Size() != artifact.Size {
			return nil, identityError(artifact.MediaID)
		}
		inputs = append(inputs, provisionimage.Input{MediaID: artifact.MediaID, SHA256: artifact.SHA256, Size: artifact.Size, File: source})
	}
	if err := validateNewBundleOutput(output); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Dir(output), ".keel-installer-overlay-*")
	if err != nil {
		return nil, errors.New("cannot create private installer overlay")
	}
	defer os.Remove(file.Name())
	defer file.Close()
	result, err := provisionimage.WriteOverlay(ctx, file, helpers, raw, configBytes, inputs[0], inputs[1])
	if err != nil {
		return nil, err
	}
	if err = file.Sync(); err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	if err = os.Link(file.Name(), output); err != nil {
		return nil, errors.New("cannot publish installer overlay; existing output is never replaced")
	}
	return result, nil
}

func installerHelperRecords(manifest *Envelope, config []byte) (mediaboot.HelperSet, []Artifact, error) {
	var helpers mediaboot.HelperSet
	nodes := map[string]Artifact{}
	for _, a := range manifest.Payload.Artifacts {
		nodes[a.MediaID] = a
	}
	root := nodes[manifest.Payload.RootID]
	invalid := errors.New("independently signed helper-set config must bind exactly two direct same-architecture OS binaries")
	if root.Type != "config" || (root.Arch != "amd64" && root.Arch != "arm64") || root.Size < 1 || root.Size > 64<<10 || int64(len(config)) != root.Size || len(root.Dependencies) != 2 {
		return helpers, nil, invalid
	}
	sum := sha256.Sum256(config)
	if hex.EncodeToString(sum[:]) != root.SHA256 {
		return helpers, nil, invalid
	}
	helpers, err := mediaboot.DecodeHelperSet(config)
	if err != nil || helpers.Arch != root.Arch {
		return helpers, nil, invalid
	}
	direct := map[string]string{}
	for _, edge := range root.Dependencies {
		direct[edge.MediaID] = edge.SHA256
	}
	if direct[helpers.PreflightMediaID] != helpers.PreflightSHA256 || direct[helpers.SSHHelperMediaID] != helpers.SSHHelperSHA256 {
		return helpers, nil, invalid
	}
	records := []Artifact{}
	for _, id := range []string{helpers.PreflightMediaID, helpers.SSHHelperMediaID} {
		a, ok := nodes[id]
		if !ok || a.Type != "os" || a.Arch != helpers.Arch || a.Size < 64 || a.Size > provisionimage.MaxBinaryBytes {
			return helpers, nil, invalid
		}
		records = append(records, a)
	}
	return helpers, records, nil
}

// VerifyInstallerOverlay performs read-only format/byte/signature verification.
// The embedded public metadata cannot supply its own signing trust root.
func VerifyInstallerOverlay(ctx context.Context, path string, trusted ed25519.PublicKey) (*provisionimage.Result, error) {
	file, info, err := regularBundleInput(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	if info.Size() < 1 || info.Size() > provisionimage.MaxOverlayBytes {
		return nil, errors.New("installer overlay exceeds archive bounds")
	}
	hash := sha256.New()
	reader := &countedOverlayReader{Reader: io.TeeReader(file, hash)}
	contents, err := provisionimage.ReadOverlay(ctx, reader)
	if err != nil {
		return nil, err
	}
	if reader.size != info.Size() {
		return nil, errors.New("installer overlay changed while reading")
	}
	manifest, err := Verify(contents.Manifest, trusted)
	if err != nil {
		return nil, err
	}
	helpers, records, err := installerHelperRecords(manifest, contents.HelperSet)
	if err != nil {
		return nil, err
	}
	if helpers != contents.Metadata.Helpers {
		return nil, errors.New("overlay helper set differs from signed configuration")
	}
	for i, a := range records {
		c := contents.Metadata.Components[i]
		if c.MediaID != a.MediaID || c.SHA256 != a.SHA256 || c.Size != a.Size {
			return nil, identityError(a.MediaID)
		}
	}
	return &provisionimage.Result{Metadata: contents.Metadata, SHA256: hex.EncodeToString(hash.Sum(nil)), Size: reader.size}, nil
}

type countedOverlayReader struct {
	io.Reader
	size int64
}

func (r *countedOverlayReader) Read(raw []byte) (int, error) {
	n, err := r.Reader.Read(raw)
	r.size += int64(n)
	return n, err
}
