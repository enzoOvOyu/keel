package taskrunner

import (
	"crypto/sha256"
	"encoding/hex"
)

const ArtifactCacheDirectory = "/opt/keel-cache/artifacts"
const MaxArtifactBytes int64 = 1 << 30
const ArtifactChunkBytes = 8192

func validateArtifactCommand(c TaskCommand) error {
	if !idPattern.MatchString(c.ArtifactMediaID) || !validScriptVersion(c.ArtifactObjectVersion) || !scriptDigestPattern.MatchString(c.ArtifactSHA256) || c.ArtifactSize < 1 || c.ArtifactSize > MaxArtifactBytes || c.AuthorizationExpiresAt < 1 {
		return ErrRejected
	}
	return nil
}

func validateArtifactObservation(o Observation) error {
	if o.Output["artifact_sha256"] != o.Command.ArtifactSHA256 || o.Output["artifact_size"] == nil || o.Output["cache_directory"] != ArtifactCacheDirectory || o.Output["writer_stopped"] != true {
		return ErrRejected
	}
	size, ok := scriptOutputInteger(o.Output["artifact_size"])
	if !ok || size != o.Command.ArtifactSize {
		return ErrRejected
	}
	offset, ok := scriptOutputInteger(o.Output["checkpoint_offset"])
	if !ok || offset < 0 || offset > size || offset%ArtifactChunkBytes != 0 && offset != size {
		return ErrRejected
	}
	verified, ok := o.Output["content_verified"].(bool)
	if !ok {
		return ErrRejected
	}
	if o.State == "success" {
		if !verified || offset != size || o.Output["reason"] != "artifact_verified" {
			return ErrRejected
		}
		return nil
	}
	if verified {
		return ErrRejected
	}
	switch o.Output["reason"] {
	case "artifact_checkpointed", "artifact_cache_unavailable", "artifact_transfer_busy", "artifact_cache_rejected", "artifact_digest_mismatch":
		return nil
	}
	return ErrRejected
}

func validateArtifactChunk(r Request) error {
	if r.ChunkOffset < 0 || r.ChunkOffset > r.Command.ArtifactSize || r.ChunkOffset%ArtifactChunkBytes != 0 || len(r.Chunk) < 1 || len(r.Chunk) > ArtifactChunkBytes || r.ChunkOffset+int64(len(r.Chunk)) > r.Command.ArtifactSize || len(r.Chunk) != ArtifactChunkBytes && r.ChunkOffset+int64(len(r.Chunk)) != r.Command.ArtifactSize {
		return ErrRejected
	}
	sum := sha256.Sum256(r.Chunk)
	if hex.EncodeToString(sum[:]) != r.ChunkSHA256 {
		return ErrRejected
	}
	return nil
}
