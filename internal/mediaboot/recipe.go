package mediaboot

import (
	"errors"
	"regexp"
)

const RecipeSchema = "keel.boot-recipe/v1"
const InitrdProtocol = "keel-initrd/v1"

var digest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Recipe is a reviewed installation-environment contract, not proof that an
// arbitrary kernel/initrd implements it. The complete pinned image still needs
// boot/driver/preflight-before-write acceptance before any real installation.
// The protocol has no user-supplied kernel command line or executable template.
type Recipe struct {
	Schema           string      `json:"schema"`
	Protocol         string      `json:"protocol"`
	RootFSFormat     string      `json:"rootfs_format"`
	UEFIMediaID      string      `json:"uefi_media_id"`
	LegacyMediaID    string      `json:"legacy_media_id,omitempty"`
	PreflightMediaID string      `json:"preflight_media_id"`
	SSHHelperMediaID string      `json:"ssh_helper_media_id"`
	PreflightSHA256  string      `json:"preflight_sha256"`
	SSHHelperSHA256  string      `json:"ssh_helper_sha256"`
	TargetBoot       *TargetBoot `json:"target_boot,omitempty"`
}

func (r *Recipe) Validate() error {
	if r == nil {
		return errors.New("an explicit approved boot recipe is required")
	}
	if !id.MatchString(r.PreflightMediaID) || !id.MatchString(r.SSHHelperMediaID) || r.PreflightMediaID == r.SSHHelperMediaID {
		return errors.New("distinct pinned preflight and SSH helper artifacts are required")
	}
	if (r.Schema != RecipeSchema && r.Schema != TargetRecipeSchema) || (r.Schema == RecipeSchema && r.TargetBoot != nil) || (r.Schema == TargetRecipeSchema && r.TargetBoot.Validate() != nil) || r.Protocol != InitrdProtocol || r.RootFSFormat != "squashfs" || !id.MatchString(r.UEFIMediaID) || (r.LegacyMediaID != "" && (!id.MatchString(r.LegacyMediaID) || r.LegacyMediaID == r.UEFIMediaID)) || !digest.MatchString(r.PreflightSHA256) || !digest.MatchString(r.SSHHelperSHA256) {
		return errors.New("invalid pinned keel-initrd boot recipe")
	}
	return nil
}
func (p *Profile) ValidateInstallable(root Node, direct map[string]Node) error {
	if p == nil {
		return errors.New("OS boot profile required")
	}
	if err := p.ValidateRoles(root, direct); err != nil {
		return err
	}
	return p.Recipe.Validate()
}
