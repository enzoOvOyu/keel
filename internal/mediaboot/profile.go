// Package mediaboot defines immutable OS boot roles within an approved media DAG.
package mediaboot

import (
	"errors"
	"regexp"
)

const Schema = "keel.os-boot/v1"

type Profile struct {
	Schema          string  `json:"schema"`
	Recipe          *Recipe `json:"recipe,omitempty"`
	Installer       string  `json:"installer"`
	KernelMediaID   string  `json:"kernel_media_id"`
	InitrdMediaID   string  `json:"initrd_media_id"`
	TemplateMediaID string  `json:"template_media_id"`
}

// Node is the subset of immutable media metadata needed to validate boot roles.
// Approved state and actual bytes are checked separately by the live registry.
type Node struct {
	ID, Type, Arch, SHA256 string
}

var id = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (p *Profile) Validate() error {
	if p == nil {
		return nil
	}
	if p.Schema != Schema || p.Installer != "cloud-init" || !id.MatchString(p.KernelMediaID) || !id.MatchString(p.InitrdMediaID) || !id.MatchString(p.TemplateMediaID) || p.KernelMediaID == p.InitrdMediaID || p.KernelMediaID == p.TemplateMediaID || p.InitrdMediaID == p.TemplateMediaID {
		return errors.New("OS boot profile requires schema keel.os-boot/v1, cloud-init and three distinct pinned role IDs")
	}
	if p.Recipe != nil {
		return p.Recipe.Validate()
	}
	return nil
}

// ValidateRoles requires every role to be an explicit direct dependency. The
// parent is the rootfs; it cannot be substituted by a transitive or latest item.
func (p *Profile) ValidateRoles(root Node, direct map[string]Node) error {
	if p == nil {
		return nil
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if root.Type != "os" || (root.Arch != "amd64" && root.Arch != "arm64") {
		return errors.New("OS rootfs requires os type and a concrete CPU architecture")
	}
	for _, role := range []struct{ id, typ string }{{p.KernelMediaID, "os"}, {p.InitrdMediaID, "os"}, {p.TemplateMediaID, "config"}} {
		node, ok := direct[role.id]
		if !ok || node.ID != role.id || node.ID == root.ID || node.Type != role.typ || (node.Arch != root.Arch && !(role.typ == "config" && node.Arch == "noarch")) {
			return errors.New("OS boot roles require direct pinned OS kernel/initrd of the rootfs architecture and a compatible config template")
		}
	}
	if p.Recipe != nil {
		roles := []struct{ id, arch string }{{p.Recipe.UEFIMediaID, root.Arch}, {p.Recipe.PreflightMediaID, root.Arch}, {p.Recipe.SSHHelperMediaID, root.Arch}}
		if direct[p.Recipe.PreflightMediaID].SHA256 != p.Recipe.PreflightSHA256 || direct[p.Recipe.SSHHelperMediaID].SHA256 != p.Recipe.SSHHelperSHA256 {
			return errors.New("boot recipe helper digests must match their exact direct dependencies")
		}
		seen := map[string]bool{}
		if p.Recipe.LegacyMediaID != "" {
			if root.Arch != "amd64" {
				return errors.New("legacy PXE recipe is supported only for amd64")
			}
			roles = append(roles, struct{ id, arch string }{p.Recipe.LegacyMediaID, "amd64"})
		}
		for _, role := range roles {
			node, ok := direct[role.id]
			if seen[role.id] || !ok || node.ID != role.id || node.Type != "os" || node.Arch != role.arch || node.ID == root.ID || node.ID == p.KernelMediaID || node.ID == p.InitrdMediaID || node.ID == p.TemplateMediaID {
				return errors.New("boot firmware requires a distinct direct pinned OS artifact of the rootfs architecture")
			}
			seen[role.id] = true
		}
	}
	return nil
}
