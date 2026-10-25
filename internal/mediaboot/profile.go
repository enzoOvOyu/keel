// Package mediaboot defines immutable OS boot roles within an approved media DAG.
package mediaboot

import (
	"errors"
	"regexp"
)

const Schema = "keel.os-boot/v1"

type Profile struct {
	Schema          string `json:"schema"`
	Installer       string `json:"installer"`
	KernelMediaID   string `json:"kernel_media_id"`
	InitrdMediaID   string `json:"initrd_media_id"`
	TemplateMediaID string `json:"template_media_id"`
}

// Node is the subset of immutable media metadata needed to validate boot roles.
// Approved state and actual bytes are checked separately by the live registry.
type Node struct {
	ID, Type, Arch string
}

var id = regexp.MustCompile(`^[a-f0-9]{32}$`)

func (p *Profile) Validate() error {
	if p == nil {
		return nil
	}
	if p.Schema != Schema || p.Installer != "cloud-init" || !id.MatchString(p.KernelMediaID) || !id.MatchString(p.InitrdMediaID) || !id.MatchString(p.TemplateMediaID) || p.KernelMediaID == p.InitrdMediaID || p.KernelMediaID == p.TemplateMediaID || p.InitrdMediaID == p.TemplateMediaID {
		return errors.New("OS boot profile requires schema keel.os-boot/v1, cloud-init and three distinct pinned role IDs")
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
	return nil
}
