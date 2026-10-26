package provisioninitrd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionseed"
)

type Files struct {
	Manifest, Preflight []byte
	Seed                provisionseed.Seed
}
type FileSource interface {
	Get(context.Context, string) ([]byte, error)
}

// Download performs only bounded public-file GETs. It checks the independently
// supplied manifest hash before fetching the other fixed resource names.
func Download(ctx context.Context, input Input, source FileSource) (*Files, error) {
	if source == nil || !digest.MatchString(input.ManifestSHA256) {
		return nil, ErrUnavailable
	}
	manifest, err := source.Get(ctx, "manifest.json")
	if err != nil || len(manifest) < 1 || len(manifest) > 4<<20 {
		return nil, ErrUnavailable
	}
	sum := sha256.Sum256(manifest)
	if hex.EncodeToString(sum[:]) != input.ManifestSHA256 {
		return nil, ErrUnavailable
	}
	preflight, err := source.Get(ctx, "preflight.json")
	if err != nil {
		return nil, ErrUnavailable
	}
	user, err := source.Get(ctx, "user-data")
	if err != nil {
		return nil, ErrUnavailable
	}
	meta, err := source.Get(ctx, "meta-data")
	if err != nil {
		return nil, ErrUnavailable
	}
	if len(preflight) < 1 || len(preflight) > 64<<10 || len(user) < 1 || len(user) > 512<<10 || len(meta) < 1 || len(meta) > 64<<10 || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	files := &Files{Manifest: manifest, Preflight: preflight, Seed: provisionseed.Seed{UserData: string(user), MetaData: string(meta)}}
	m, _, err := provisionboot.VerifyBootFiles(files.Manifest, input.ManifestSHA256, files.Preflight, files.Seed, time.Now())
	if err != nil || m.GrantID != input.GrantID || m.MAC != input.BootMAC || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	return files, nil
}
