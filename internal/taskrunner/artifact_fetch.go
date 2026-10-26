package taskrunner

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
)

// DecodeArtifactRead binds ephemeral transport bytes before control-plane storage.
// Historical receipts deliberately omit chunk bodies.
func DecodeArtifactRead(o Observation, c TaskCommand, offset int64) ([]byte, error) {
	if o.Validate(c) != nil || o.Terminal() || c.Operation != "media.fetch" || offset < 0 || offset >= c.ArtifactSize {
		return nil, ErrRejected
	}
	value, ok := scriptOutputInteger(o.Output["chunk_offset"])
	if !ok || value != offset || o.Output["source_verified"] != true {
		return nil, ErrRejected
	}
	raw, ok := o.Output["chunk_base64"].(string)
	if !ok || len(raw) > 11000 {
		return nil, ErrRejected
	}
	body, err := base64.StdEncoding.Strict().DecodeString(raw)
	if err != nil || int64(len(body)) != min(int64(ArtifactChunkBytes), c.ArtifactSize-offset) {
		return nil, ErrRejected
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != o.Output["chunk_sha256"] {
		return nil, ErrRejected
	}
	checkpoint, ok := scriptOutputInteger(o.Output["checkpoint_offset"])
	if !ok || checkpoint != offset+int64(len(body)) {
		return nil, ErrRejected
	}
	return body, nil
}
