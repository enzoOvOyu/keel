package platform

import (
	"context"
	"io"
)

// References come exclusively from durable execution evidence, never HTTP input.
// Captured output corruption must not quarantine the independent media source.
func (s *Store) OpenCapturedArtifact(ctx context.Context, key, version, sha string, size int64) (io.ReadSeekCloser, error) {
	if s == nil || s.MediaObjects == nil {
		return nil, apiError(503, "ARTIFACT_STORAGE_UNAVAILABLE", "Captured output storage unavailable")
	}
	body, err := s.MediaObjects.Open(ctx, key, version)
	if err != nil {
		return nil, apiError(503, "ARTIFACT_OUTPUT_UNAVAILABLE", "Captured output version is unavailable")
	}
	defer body.Close()
	staged, err := stageMedia(body)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil || staged.size != size || staged.sha256 != sha {
		staged.close()
		return nil, apiError(422, "ARTIFACT_OUTPUT_DIGEST_MISMATCH", "Captured output failed full integrity verification")
	}
	if _, err = staged.file.Seek(0, io.SeekStart); err != nil {
		staged.close()
		return nil, err
	}
	return &taskArtifactFile{staged: staged}, nil
}
