package mediaboot

import (
	"bytes"
	"encoding/json"
	"errors"
)

const RuntimeSetSchema = "keel.installer-runtime-set/v1"
const WriterRuntimeSetSchema = "keel.installer-runtime-set/v2"

type RuntimeComponent struct {
	MediaID string `json:"media_id"`
	SHA256  string `json:"sha256"`
}
type WriterTools struct {
	UnsquashFS RuntimeComponent `json:"unsquashfs"`
	MkfsExt4   RuntimeComponent `json:"mkfs_ext4"`
	MkfsVFAT   RuntimeComponent `json:"mkfs_vfat"`
}

// RuntimeSet is independent of the final initrd/OS recipe. Approve this config
// and its five (v1) or eight (v2 with writer tools) exact inputs first, then
// produce the initrd and approve the
// result for use in an OS recipe. Tools never imply write permission. No input
// refers to that future result.
type RuntimeSet struct {
	Schema    string           `json:"schema"`
	Arch      string           `json:"arch"`
	Init      RuntimeComponent `json:"init"`
	Preflight RuntimeComponent `json:"preflight"`
	SSHHelper RuntimeComponent `json:"ssh_helper"`
	SMART     RuntimeComponent `json:"smartctl"`
	BootCA    RuntimeComponent `json:"boot_ca"`
	Writer    *WriterTools     `json:"writer_tools,omitempty"`
}

func (s RuntimeSet) Components() []RuntimeComponent {
	out := []RuntimeComponent{s.Init, s.Preflight, s.SSHHelper, s.SMART, s.BootCA}
	if s.Writer != nil {
		out = append(out, s.Writer.UnsquashFS, s.Writer.MkfsExt4, s.Writer.MkfsVFAT)
	}
	return out
}
func (s RuntimeSet) Equal(other RuntimeSet) bool {
	a, b := s.Writer, other.Writer
	s.Writer, other.Writer = nil, nil
	return s == other && ((a == nil && b == nil) || (a != nil && b != nil && *a == *b))
}
func (s RuntimeSet) Helpers() HelperSet {
	return HelperSet{Schema: HelperSetSchema, Arch: s.Arch, PreflightMediaID: s.Preflight.MediaID, PreflightSHA256: s.Preflight.SHA256, SSHHelperMediaID: s.SSHHelper.MediaID, SSHHelperSHA256: s.SSHHelper.SHA256}
}
func (s RuntimeSet) Validate() error {
	invalid := errors.New("invalid independent installer runtime set")
	if (s.Schema != RuntimeSetSchema && s.Schema != WriterRuntimeSetSchema) || (s.Schema == RuntimeSetSchema && s.Writer != nil) || (s.Schema == WriterRuntimeSetSchema && s.Writer == nil) || (s.Arch != "amd64" && s.Arch != "arm64") {
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
