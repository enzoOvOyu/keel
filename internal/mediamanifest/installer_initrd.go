package mediamanifest

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
)

func installerRuntimeRecords(manifest *Envelope, config []byte) (mediaboot.RuntimeSet, []Artifact, error) {
	var set mediaboot.RuntimeSet
	invalid := errors.New("independently signed runtime config must bind four static binaries and one public CA file")
	nodes := map[string]Artifact{}
	for _, a := range manifest.Payload.Artifacts {
		nodes[a.MediaID] = a
	}
	root := nodes[manifest.Payload.RootID]
	if root.Type != "config" || (root.Arch != "amd64" && root.Arch != "arm64") || root.Size < 1 || root.Size > 64<<10 || int64(len(config)) != root.Size || len(root.Dependencies) != 5 || len(nodes) != 6 {
		return set, nil, invalid
	}
	hash := sha256.Sum256(config)
	if root.SHA256 != hex.EncodeToString(hash[:]) {
		return set, nil, invalid
	}
	set, err := mediaboot.DecodeRuntimeSet(config)
	if err != nil || set.Arch != root.Arch {
		return set, nil, invalid
	}
	direct := map[string]string{}
	for _, d := range root.Dependencies {
		direct[d.MediaID] = d.SHA256
	}
	var records []Artifact
	for i, component := range set.Components() {
		a, ok := nodes[component.MediaID]
		if !ok || a.SHA256 != component.SHA256 || direct[a.MediaID] != component.SHA256 || len(a.Dependencies) != 0 {
			return set, nil, invalid
		}
		if i < 4 {
			if a.Type != "os" || a.Arch != set.Arch || a.Size < 64 || a.Size > provisionimage.MaxBinaryBytes {
				return set, nil, invalid
			}
		} else if a.Type != "config" || a.Arch != "noarch" || a.Size < 1 || a.Size > 1<<20 {
			return set, nil, invalid
		}
		records = append(records, a)
	}
	return set, records, nil
}

// CreateInstallerInitrd is an offline, acyclic production step. Only after this
// output exists can it be imported, independently approved, and referenced by a
// final OS recipe. It does not boot or attest kernel/driver/hardware behavior.
func CreateInstallerInitrd(ctx context.Context, raw []byte, trusted ed25519.PublicKey, directory, output string) (*provisionimage.InitrdResult, error) {
	manifest, err := Verify(raw, trusted)
	if err != nil {
		return nil, err
	}
	if directory == "" || output == "" {
		return nil, errors.New("artifact directory and new initrd output are required")
	}
	var root Artifact
	for _, a := range manifest.Payload.Artifacts {
		if a.MediaID == manifest.Payload.RootID {
			root = a
		}
	}
	if root.Type != "config" || root.Size < 1 || root.Size > 64<<10 {
		return nil, identityError(root.MediaID)
	}
	config, info, err := regularBundleInput(filepath.Join(directory, root.MediaID+".bin"))
	if err != nil {
		return nil, identityError(root.MediaID)
	}
	if info.Size() != root.Size {
		config.Close()
		return nil, identityError(root.MediaID)
	}
	configBytes, err := io.ReadAll(io.LimitReader(config, root.Size+1))
	config.Close()
	if err != nil {
		return nil, identityError(root.MediaID)
	}
	set, records, err := installerRuntimeRecords(manifest, configBytes)
	if err != nil {
		return nil, err
	}
	var inputs []provisionimage.Input
	for _, a := range records {
		f, info, err := regularBundleInput(filepath.Join(directory, a.MediaID+".bin"))
		if err != nil {
			return nil, identityError(a.MediaID)
		}
		defer f.Close()
		if info.Size() != a.Size {
			return nil, identityError(a.MediaID)
		}
		inputs = append(inputs, provisionimage.Input{MediaID: a.MediaID, SHA256: a.SHA256, Size: a.Size, File: f})
	}
	if err = validateNewBundleOutput(output); err != nil {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(output), ".keel-installer-initrd-*")
	if err != nil {
		return nil, errors.New("cannot create private initrd output")
	}
	defer os.Remove(f.Name())
	defer f.Close()
	result, err := provisionimage.WriteInitrd(ctx, f, set, raw, configBytes, inputs)
	if err != nil {
		return nil, err
	}
	if err = f.Sync(); err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err = os.Link(f.Name(), output); err != nil {
		return nil, errors.New("cannot publish initrd; existing output is never replaced")
	}
	parent, err := os.Open(filepath.Dir(output))
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err = parent.Sync(); err != nil {
		return nil, err
	}
	return result, nil
}

// VerifyInstallerInitrd authenticates the embedded signed input closure with an
// independently supplied key, checks every actual component and returns the
// actual archive digest. It never extracts or executes the archive.
func VerifyInstallerInitrd(ctx context.Context, path string, trusted ed25519.PublicKey) (*provisionimage.InitrdResult, error) {
	f, info, err := regularBundleInput(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if info.Size() < 1 || info.Size() > provisionimage.MaxInitrdBytes {
		return nil, errors.New("initrd exceeds archive bounds")
	}
	return verifyInstallerInitrdReader(ctx, f, info.Size(), trusted)
}

func verifyInstallerInitrdReader(ctx context.Context, f io.Reader, size int64, trusted ed25519.PublicKey) (*provisionimage.InitrdResult, error) {
	if f == nil || size < 1 || size > provisionimage.MaxInitrdBytes || len(trusted) != ed25519.PublicKeySize || ctx.Err() != nil {
		return nil, errors.New("initrd exceeds archive or verification bounds")
	}
	hash := sha256.New()
	reader := &countedOverlayReader{Reader: io.TeeReader(io.LimitReader(f, size+1), hash)}
	contents, err := provisionimage.ReadInitrd(ctx, reader)
	if err != nil {
		return nil, err
	}
	if reader.size != size {
		return nil, errors.New("initrd changed while reading")
	}
	manifest, err := Verify(contents.Manifest, trusted)
	if err != nil {
		return nil, err
	}
	set, records, err := installerRuntimeRecords(manifest, contents.RuntimeSet)
	if err != nil {
		return nil, err
	}
	if set != contents.Metadata.Runtime {
		return nil, errors.New("runtime differs from independently signed config")
	}
	for i, a := range records {
		c := contents.Metadata.Components[i]
		if c.MediaID != a.MediaID || c.SHA256 != a.SHA256 || c.Size != a.Size {
			return nil, identityError(a.MediaID)
		}
	}
	return &provisionimage.InitrdResult{Metadata: contents.Metadata, SHA256: hex.EncodeToString(hash.Sum(nil)), Size: reader.size}, nil
}

// VerifyInstallerInitrdForPlan binds the authenticated actual archive to an
// already frozen final role plan. Merely carrying a keel-initrd recipe is not
// sufficient: its two helper IDs/digests/sizes must be inside these exact bytes.
// The caller still checks current media approval, live lease and image PoC.
func VerifyInstallerInitrdForPlan(ctx context.Context, reader io.Reader, plan mediaboot.Plan, trusted ed25519.PublicKey) (*provisionimage.InitrdResult, error) {
	if plan.ValidateInstallable() != nil {
		return nil, errors.New("installable frozen role plan required")
	}
	roles := map[string]mediaboot.Artifact{}
	for _, a := range plan.Artifacts {
		roles[a.Role] = a
	}
	result, err := verifyInstallerInitrdReader(ctx, reader, roles["initrd"].Size, trusted)
	if err != nil {
		return nil, err
	}
	if result.SHA256 != roles["initrd"].SHA256 || result.Metadata.Arch != roles["initrd"].Arch {
		return nil, errors.New("actual initrd differs from frozen role")
	}
	for _, binding := range []struct {
		role  string
		index int
	}{{"preflight", 1}, {"ssh-helper", 2}} {
		c := result.Metadata.Components[binding.index]
		a := roles[binding.role]
		if c.MediaID != a.MediaID || c.SHA256 != a.SHA256 || c.Size != a.Size || result.Metadata.Arch != a.Arch {
			return nil, errors.New("initrd helper differs from frozen role")
		}
	}
	return result, nil
}
