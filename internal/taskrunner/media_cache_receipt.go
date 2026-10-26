package taskrunner

import (
	"bytes"
	"encoding/json"
	"io"
)

func validateCacheObservation(o Observation) error {
	if o.State != "success" {
		switch o.Output["reason"] {
		case "cache_directory_unavailable", "cache_index_unavailable", "cache_index_busy", "cache_snapshot_changed", "cache_cursor_invalid", "cache_content_mismatch", "cache_query_interrupted":
			return nil
		default:
			return ErrRejected
		}
	}
	if o.Output["cache_version"] != mediaCacheVersion || o.Output["cache_directory"] != MediaCacheDirectory || o.Output["page_content_verified"] != true || o.Output["reason"] != "cache_page_verified" {
		return ErrRejected
	}
	digest, ok := o.Output["manifest_sha256"].(string)
	if !ok || !scriptDigestPattern.MatchString(digest) || o.Command.CacheManifestSHA256 != "" && digest != o.Command.CacheManifestSHA256 {
		return ErrRejected
	}
	total, valid := scriptOutputInteger(o.Output["registered_count"])
	offset, offsetOK := scriptOutputInteger(o.Output["offset"])
	page, pageOK := scriptOutputInteger(o.Output["page_size"])
	if !valid || !offsetOK || !pageOK || total < 0 || total > mediaCacheEntries || offset < 0 || offset > total || page != mediaCachePageSize || o.Command.CacheCursor == "" && offset != 0 || o.Command.CacheCursor != "" && offset == 0 {
		return ErrRejected
	}
	raw, err := json.Marshal(o.Output["items"])
	if err != nil || len(raw) > 12288 {
		return ErrRejected
	}
	var entries []CacheInventoryEntry
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&entries) != nil || d.Decode(new(any)) != io.EOF || entries == nil {
		return ErrRejected
	}
	expected := total - offset
	if expected > mediaCachePageSize {
		expected = mediaCachePageSize
	}
	if int64(len(entries)) != expected {
		return ErrRejected
	}
	previous := o.Command.CacheCursor
	for _, entry := range entries {
		if !scriptDigestPattern.MatchString(entry.SHA256) || entry.SHA256 <= previous || entry.Size < 1 || entry.Size > MaxScriptBytes || entry.StoredAt.IsZero() {
			return ErrRejected
		}
		previous = entry.SHA256
	}
	next, ok := o.Output["next_cursor"].(string)
	if !ok || offset+int64(len(entries)) < total && (len(entries) == 0 || next != previous) || offset+int64(len(entries)) == total && next != "" {
		return ErrRejected
	}
	return nil
}
