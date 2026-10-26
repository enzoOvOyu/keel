package mediaboot

import (
	"encoding/json"
	"strings"
	"testing"
)

func recipeFixture() Plan {
	p := Plan{RootID: strings.Repeat("1", 32), Profile: Profile{Schema: Schema, Installer: "cloud-init", KernelMediaID: strings.Repeat("2", 32), InitrdMediaID: strings.Repeat("3", 32), TemplateMediaID: strings.Repeat("4", 32), Recipe: &Recipe{Schema: RecipeSchema, Protocol: InitrdProtocol, RootFSFormat: "squashfs", UEFIMediaID: strings.Repeat("5", 32), PreflightMediaID: strings.Repeat("6", 32), SSHHelperMediaID: strings.Repeat("7", 32), PreflightSHA256: strings.Repeat("a", 64), SSHHelperSHA256: strings.Repeat("a", 64)}}}
	for i, role := range []string{"rootfs", "kernel", "initrd", "template", "uefi", "preflight", "ssh-helper"} {
		arch := "amd64"
		if role == "template" {
			arch = "noarch"
		}
		p.Artifacts = append(p.Artifacts, Artifact{Role: role, MediaID: strings.Repeat(string(rune('1'+i)), 32), ScopeKey: "global", Name: role, Version: "fixture-v1", SHA256: strings.Repeat("a", 64), Size: 1, Arch: arch, ObjectVersion: "fixture-immutable-v1"})
	}
	return p
}
func TestRecipeExactApprovedRolesAndCompatibility(t *testing.T) {
	if err := recipeFixture().ValidateInstallable(); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*Plan){
		"old_preview_is_not_installation": func(p *Plan) { p.Profile.Recipe = nil; p.Artifacts = p.Artifacts[:4] },
		"wrong_helper_hash":               func(p *Plan) { p.Profile.Recipe.PreflightSHA256 = strings.Repeat("b", 64) },
		"helper_role_alias":               func(p *Plan) { p.Profile.Recipe.PreflightMediaID = p.Profile.KernelMediaID },
		"wrong_helper_arch":               func(p *Plan) { p.Artifacts[5].Arch = "arm64" },
		"wrong_root_arch":                 func(p *Plan) { p.Artifacts[0].Arch = "arm64" },
		"missing_firmware":                func(p *Plan) { p.Artifacts = append(p.Artifacts[:4], p.Artifacts[5:]...) },
		"object_unversioned":              func(p *Plan) { p.Artifacts[0].ObjectVersion = "null" },
		"duplicated_role":                 func(p *Plan) { p.Artifacts[5].Role = "uefi" },
		"unapproved_kernel_protocol":      func(p *Plan) { p.Profile.Recipe.Protocol = "generic-cloud-init" },
	} {
		t.Run(name, func(t *testing.T) {
			p := recipeFixture()
			edit(&p)
			if err := p.ValidateInstallable(); err == nil {
				t.Fatal("accepted unsafe installation composition")
			}
		})
	}
	p := recipeFixture()
	p.Profile.Recipe = nil
	direct := map[string]Node{}
	for _, a := range p.Artifacts {
		typ := "os"
		if a.Role == "template" {
			typ = "config"
		}
		direct[a.MediaID] = Node{ID: a.MediaID, Type: typ, Arch: a.Arch, SHA256: a.SHA256}
	}
	if p.Profile.ValidateRoles(direct[p.RootID], direct) != nil {
		t.Fatal("old preparation profile lost compatibility")
	}
}
func TestRecipeARMAndLegacyAreExplicit(t *testing.T) {
	p := recipeFixture()
	for i := range p.Artifacts {
		if p.Artifacts[i].Arch == "amd64" {
			p.Artifacts[i].Arch = "arm64"
		}
	}
	if err := p.ValidateInstallable(); err != nil {
		t.Fatal(err)
	}
	p.Profile.Recipe.LegacyMediaID = strings.Repeat("8", 32)
	p.Artifacts = append(p.Artifacts, Artifact{Role: "legacy", MediaID: p.Profile.Recipe.LegacyMediaID, ScopeKey: "global", SHA256: strings.Repeat("a", 64), Size: 1, Arch: "arm64", ObjectVersion: "fixture"})
	if err := p.ValidateInstallable(); err == nil {
		t.Fatal("ARM legacy recipe accepted")
	}
}
func TestRecipeJSONPreservesPinnedHashes(t *testing.T) {
	p := recipeFixture()
	raw, _ := json.Marshal(p)
	var copy Plan
	if json.Unmarshal(raw, &copy) != nil || copy.ValidateInstallable() != nil || copy.Profile.Recipe.PreflightSHA256 != p.Profile.Recipe.PreflightSHA256 {
		t.Fatal("recipe lost during JSON roundtrip")
	}
}

func TestTargetBootContractCannotBeImpliedByNetworkFirmware(t *testing.T) {
	p := recipeFixture()
	if p.ValidateWritable() == nil {
		t.Fatal("network boot recipe authorized target-system writing")
	}
	p.Profile.Recipe.Schema = TargetRecipeSchema
	p.Profile.Recipe.TargetBoot = &TargetBoot{Schema: TargetBootSchema, Path: TargetUKIPath, SHA256: strings.Repeat("b", 64), Size: 4096, KernelRelease: "6.12.0-keel", CommandLine: TargetCommandLine}
	if err := p.ValidateWritable(); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Recipe){
		"downgrade_with_target": func(r *Recipe) { r.Schema = RecipeSchema },
		"missing_target":        func(r *Recipe) { r.TargetBoot = nil },
		"target_is_ipxe":        func(r *Recipe) { r.TargetBoot.Path = "usr/lib/keel/ipxe.efi" },
		"command_addition":      func(r *Recipe) { r.TargetBoot.CommandLine += " init=/bin/sh" },
		"remote_root":           func(r *Recipe) { r.TargetBoot.CommandLine = "root=/dev/nfs" },
		"kernel_path":           func(r *Recipe) { r.TargetBoot.KernelRelease = "../other" },
		"unknown_schema":        func(r *Recipe) { r.TargetBoot.Schema = "future" },
		"oversize":              func(r *Recipe) { r.TargetBoot.Size = MaxTargetUKIBytes + 1 },
	} {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(p)
			var copy Plan
			json.Unmarshal(raw, &copy)
			change(copy.Profile.Recipe)
			if copy.ValidateWritable() == nil {
				t.Fatal("unsafe target content contract accepted")
			}
		})
	}
}
