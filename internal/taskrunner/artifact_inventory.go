package taskrunner

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
)

func validateArtifactInventoryObservation(o Observation) error {
	if o.State != "success" {
		return validateCacheObservation(o)
	}
	if o.Output["cache_version"] != artifactInventoryVersion || o.Output["cache_directory"] != ArtifactCacheDirectory || o.Output["page_content_verified"] != false || o.Output["reason"] != "artifact_index_observed" {
		return ErrRejected
	}
	digest, ok := o.Output["manifest_sha256"].(string)
	if !ok || !scriptDigestPattern.MatchString(digest) || o.Command.CacheManifestSHA256 != "" && o.Command.CacheManifestSHA256 != digest {
		return ErrRejected
	}
	total, valid := scriptOutputInteger(o.Output["registered_count"])
	offset, offsetOK := scriptOutputInteger(o.Output["offset"])
	page, pageOK := scriptOutputInteger(o.Output["page_size"])
	ttl, ttlOK := scriptOutputInteger(o.Output["ttl_seconds"])
	if !valid || !offsetOK || !pageOK || !ttlOK || total < 0 || total > 64 || offset < 0 || offset > total || page != artifactInventoryPageSize || ttl != 30 || o.Command.CacheCursor == "" && offset != 0 || o.Command.CacheCursor != "" && offset == 0 {
		return ErrRejected
	}
	raw, err := json.Marshal(o.Output["items"])
	if err != nil || len(raw) > 8192 {
		return ErrRejected
	}
	var items []ArtifactInventoryEntry
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&items) != nil || d.Decode(new(any)) != io.EOF || items == nil || int64(len(items)) != min(page, total-offset) {
		return ErrRejected
	}
	previous := o.Command.CacheCursor
	for _, item := range items {
		if !scriptDigestPattern.MatchString(item.SHA256) || item.SHA256 <= previous || item.Size < 1 || item.Size > MaxArtifactBytes || item.CheckpointOffset < 0 || item.CheckpointOffset > item.Size || item.CheckpointOffset%ArtifactChunkBytes != 0 && item.CheckpointOffset != item.Size || item.ModifiedAt.IsZero() {
			return ErrRejected
		}
		if item.State != "partial" && item.State != "paused" && item.State != "verified" || item.State == "verified" && (item.CheckpointOffset != item.Size || item.LastVerifiedAt == nil || item.LastVerifiedAt.IsZero()) || item.State != "verified" && item.LastVerifiedAt != nil {
			return ErrRejected
		}
		previous = item.SHA256
	}
	next, ok := o.Output["next_cursor"].(string)
	if !ok || offset+int64(len(items)) < total && (len(items) == 0 || next != previous) || offset+int64(len(items)) == total && next != "" {
		return ErrRejected
	}
	capturedRaw, err := json.Marshal(o.Output["collected_at"])
	if err != nil {
		return ErrRejected
	}
	var captured time.Time
	if json.Unmarshal(capturedRaw, &captured) != nil || captured.IsZero() || captured.Before(o.StartedAt.Add(-5*time.Second)) || captured.After(o.FinishedAt.Add(5*time.Second)) {
		return ErrRejected
	}
	return nil
}
