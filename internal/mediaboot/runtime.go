package mediaboot

import (
	"bytes"
	"encoding/json"
	"errors"
)

const RuntimeSetSchema = "keel.installer-runtime-set/v1"

type RuntimeComponent struct {
	MediaID string `json:"media_id"`
	SHA256  string `json:"sha256"`
}

// RuntimeSet is independent of the final initrd/OS recipe. Approve this config
// and its five exact direct inputs first, produce the initrd, then approve the
// result for use in an OS recipe. No input refers to that future result.
type RuntimeSet struct {
	Schema    string           `json:"schema"`
	Arch      string           `json:"arch"`
	Init      RuntimeComponent `json:"init"`
	Preflight RuntimeComponent `json:"preflight"`
	SSHHelper RuntimeComponent `json:"ssh_helper"`
	SMART     RuntimeComponent `json:"smartctl"`
	BootCA    RuntimeComponent `json:"boot_ca"`
}

func (s RuntimeSet) Components() []RuntimeComponent {
	return []RuntimeComponent{s.Init, s.Preflight, s.SSHHelper, s.SMART, s.BootCA}
}
func (s RuntimeSet) Helpers() HelperSet {
	return HelperSet{Schema: HelperSetSchema, Arch: s.Arch, PreflightMediaID: s.Preflight.MediaID, PreflightSHA256: s.Preflight.SHA256, SSHHelperMediaID: s.SSHHelper.MediaID, SSHHelperSHA256: s.SSHHelper.SHA256}
}
func (s RuntimeSet) Validate() error {
	invalid := errors.New("invalid independent installer runtime set")
	if s.Schema != RuntimeSetSchema || (s.Arch != "amd64" && s.Arch != "arm64") {
		return invalid
	}
	seen := map[string]bool{}
	for _, component := range s.Components() {
		if !id.MatchString(component.MediaID) || !digest.MatchString(component.SHA256) || seen[component.MediaID] {
			return invalid
		}
		seen[component.MediaID] = true
	}
	return nil
}

// Runtime configs use canonical JSON, including field order and escaping. This
// rejects unknown, duplicate and case-aliased keys at every nesting level.
func DecodeRuntimeSet(raw []byte) (RuntimeSet, error) {
	var out RuntimeSet
	invalid := errors.New("invalid bounded canonical installer runtime-set JSON")
	if len(raw) == 0 || len(raw) > 64<<10 || json.Unmarshal(raw, &out) != nil || out.Validate() != nil {
		return RuntimeSet{}, invalid
	}
	canonical, err := json.Marshal(out)
	if err != nil || !bytes.Equal(raw, canonical) {
		return RuntimeSet{}, invalid
	}
	return out, nil
}
