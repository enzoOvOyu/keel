package provisionboot

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
	"github.com/enzoOvOyu/keel/internal/provisionseed"
)

// VerifyBootFiles verifies public files fetched by an accepted installation
// environment. expectedManifestSHA256 MUST come from an independently trusted
// boot path (the controlled script/kernel arguments), never from these files.
// This is integrity verification, not a live callback/install permit: current
// approval, network ownership, attempt/fence and exclusive lease stay server-side.
// It performs no network, filesystem, command execution or installation.
func VerifyBootFiles(manifestRaw []byte, expectedManifestSHA256 string, preflightRaw []byte, seed provisionseed.Seed, now time.Time) (*BootManifest, *provisionpreflight.Spec, error) {
	if len(manifestRaw) < 1 || len(manifestRaw) > 4<<20 || !utf8.Valid(manifestRaw) || !sha256ID.MatchString(expectedManifestSHA256) || bytesDigest(manifestRaw) != expectedManifestSHA256 {
		return nil, nil, ErrDenied
	}
	var manifest BootManifest
	if json.Unmarshal(manifestRaw, &manifest) != nil {
		return nil, nil, ErrDenied
	}
	// The producer emits canonical JSON. Exact re-encoding also rejects ignored
	// unknown/case-aliased/duplicate fields or appended documents in the envelope.
	canonical, err := json.Marshal(manifest)
	if err != nil || !bytes.Equal(canonical, manifestRaw) || manifest.Schema != BootManifestSchema {
		return nil, nil, ErrDenied
	}
	if !manifest.ExpiresAt.After(now) || manifest.FencingToken < 1 || !unicastMAC(manifest.MAC) || (manifest.Arch != "amd64" && manifest.Arch != "arm64") {
		return nil, nil, ErrDenied
	}
	for _, id := range []string{manifest.ScopeID, manifest.GrantID, manifest.ProjectID, manifest.RequestID, manifest.MachineID, manifest.AttemptID, manifest.CommandID} {
		if !opaqueID.MatchString(id) {
			return nil, nil, ErrDenied
		}
	}
	for _, hash := range []string{manifest.SnapshotSHA256, manifest.PreflightSHA256, manifest.UserDataSHA256, manifest.MetaDataSHA256} {
		if !sha256ID.MatchString(hash) {
			return nil, nil, ErrDenied
		}
	}
	if manifest.Media.ValidateInstallable() != nil {
		return nil, nil, ErrDenied
	}
	for _, a := range manifest.Media.Artifacts {
		if (a.ScopeKey != "global" && a.ScopeKey != "project:"+manifest.ProjectID) || (a.Role == "rootfs" && a.Arch != manifest.Arch) {
			return nil, nil, ErrDenied
		}
	}
	if len(preflightRaw) > 64<<10 || bytesDigest(preflightRaw) != manifest.PreflightSHA256 || len(seed.UserData) < 1 || len(seed.UserData) > 512<<10 || len(seed.MetaData) < 1 || len(seed.MetaData) > 64<<10 || bytesDigest([]byte(seed.UserData)) != manifest.UserDataSHA256 || bytesDigest([]byte(seed.MetaData)) != manifest.MetaDataSHA256 || !strings.HasPrefix(seed.UserData, "#cloud-config\n") {
		return nil, nil, ErrDenied
	}
	spec, err := provisionpreflight.DecodeSpec(preflightRaw)
	if err != nil || spec.Validate(now) != nil {
		return nil, nil, ErrDenied
	}
	if spec.ProjectID != manifest.ProjectID || spec.RequestID != manifest.RequestID || spec.MachineID != manifest.MachineID || spec.AttemptID != manifest.AttemptID || spec.CommandID != manifest.CommandID || spec.FencingToken != manifest.FencingToken || spec.SnapshotSHA256 != manifest.SnapshotSHA256 || !spec.ExpiresAt.Equal(manifest.ExpiresAt) || spec.Expected.BootMAC != manifest.MAC || spec.Expected.Arch != manifest.Arch {
		return nil, nil, ErrDenied
	}
	if !validSeedMetadata(seed.MetaData, manifest.RequestID) {
		return nil, nil, ErrDenied
	}
	return &manifest, &spec, nil
}
