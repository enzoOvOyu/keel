package mediaboot

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

const HelperSetSchema = "keel.installer-helper-set/v1"

// HelperSet is approved BEFORE an initrd or OS recipe exists. Its standalone
// config artifact directly depends on the two approved binaries; this avoids a
// circular requirement to approve a finished image before producing its inputs.
type HelperSet struct {
	Schema           string `json:"schema"`
	Arch             string `json:"arch"`
	PreflightMediaID string `json:"preflight_media_id"`
	SSHHelperMediaID string `json:"ssh_helper_media_id"`
	PreflightSHA256  string `json:"preflight_sha256"`
	SSHHelperSHA256  string `json:"ssh_helper_sha256"`
}

func (s HelperSet) Validate() error {
	if s.Schema != HelperSetSchema || (s.Arch != "amd64" && s.Arch != "arm64") || !id.MatchString(s.PreflightMediaID) || !id.MatchString(s.SSHHelperMediaID) || s.PreflightMediaID == s.SSHHelperMediaID || !digest.MatchString(s.PreflightSHA256) || !digest.MatchString(s.SSHHelperSHA256) {
		return errors.New("invalid independent installer helper set")
	}
	return nil
}
func DecodeHelperSet(raw []byte) (HelperSet, error) {
	var out HelperSet
	invalid := errors.New("invalid bounded canonical installer helper-set JSON")
	if len(raw) > 64<<10 || !utf8.Valid(raw) {
		return out, invalid
	}
	allowed := map[string]bool{"schema": true, "arch": true, "preflight_media_id": true, "ssh_helper_media_id": true, "preflight_sha256": true, "ssh_helper_sha256": true}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return out, invalid
	}
	seen := map[string]bool{}
	for d.More() {
		token, err = d.Token()
		if err != nil {
			return out, invalid
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || seen[key] {
			return out, invalid
		}
		seen[key] = true
		var value string
		if d.Decode(&value) != nil {
			return out, invalid
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') {
		return out, invalid
	}
	if _, err = d.Token(); err != io.EOF {
		return out, invalid
	}
	if json.Unmarshal(raw, &out) != nil || out.Validate() != nil {
		return out, invalid
	}
	return out, nil
}
