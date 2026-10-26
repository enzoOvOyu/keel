package mediaboot

import "errors"

func invalidPlan() error { return errors.New("invalid immutable installation boot-role plan") }

// Artifact contains exact approved object metadata. It is not a download grant.
type Artifact struct {
	Role          string `json:"role"`
	MediaID       string `json:"media_id"`
	ScopeKey      string `json:"scope_key"`
	Name          string `json:"name"`
	Version       string `json:"version"`
	SHA256        string `json:"sha256"`
	Size          int64  `json:"size"`
	Arch          string `json:"arch"`
	ObjectVersion string `json:"object_version"`
}
type Plan struct {
	RootID    string     `json:"root_id"`
	Profile   Profile    `json:"profile"`
	Artifacts []Artifact `json:"artifacts"`
}

// ValidateInstallable requires exact one-per-role bindings. It does not replace
// live approval/revocation or full actual object-byte verification by Authority.
func (p Plan) ValidateInstallable() error {
	if !id.MatchString(p.RootID) || len(p.Artifacts) < 7 || len(p.Artifacts) > 8 {
		return invalidPlan()
	}
	byRole := map[string]Artifact{}
	byID := map[string]Node{}
	for _, a := range p.Artifacts {
		if !id.MatchString(a.MediaID) || !digest.MatchString(a.SHA256) || a.Size < 1 || a.ObjectVersion == "" || a.ObjectVersion == "null" || len(a.ObjectVersion) > 4096 || byRole[a.Role].MediaID != "" || byID[a.MediaID].ID != "" {
			return invalidPlan()
		}
		if a.ScopeKey != "global" && (len(a.ScopeKey) != 40 || a.ScopeKey[:8] != "project:" || !id.MatchString(a.ScopeKey[8:])) {
			return invalidPlan()
		}
		typ := "os"
		if a.Role == "template" {
			typ = "config"
		}
		switch a.Role {
		case "rootfs", "kernel", "initrd", "template", "uefi", "legacy", "preflight", "ssh-helper":
		default:
			return invalidPlan()
		}
		limit := int64(1 << 30)
		switch a.Role {
		case "rootfs":
			limit = 64 << 30
		case "template":
			limit = 64 << 10
		case "uefi", "legacy":
			limit = 16 << 20
		case "preflight", "ssh-helper":
			limit = 64 << 20
		}
		if a.Size > limit {
			return invalidPlan()
		}
		byRole[a.Role] = a
		byID[a.MediaID] = Node{ID: a.MediaID, Type: typ, Arch: a.Arch, SHA256: a.SHA256}
	}
	root := byRole["rootfs"]
	if root.MediaID != p.RootID {
		return invalidPlan()
	}
	if err := p.Profile.ValidateInstallable(byID[p.RootID], byID); err != nil {
		return err
	}
	for _, role := range []struct{ role, id string }{{"kernel", p.Profile.KernelMediaID}, {"initrd", p.Profile.InitrdMediaID}, {"template", p.Profile.TemplateMediaID}, {"uefi", p.Profile.Recipe.UEFIMediaID}, {"preflight", p.Profile.Recipe.PreflightMediaID}, {"ssh-helper", p.Profile.Recipe.SSHHelperMediaID}} {
		if byRole[role.role].MediaID != role.id {
			return invalidPlan()
		}
	}
	if byRole["preflight"].SHA256 != p.Profile.Recipe.PreflightSHA256 || byRole["ssh-helper"].SHA256 != p.Profile.Recipe.SSHHelperSHA256 {
		return invalidPlan()
	}
	if p.Profile.Recipe.LegacyMediaID != "" {
		if byRole["legacy"].MediaID != p.Profile.Recipe.LegacyMediaID {
			return invalidPlan()
		}
	} else if byRole["legacy"].MediaID != "" {
		return invalidPlan()
	}
	return nil
}
