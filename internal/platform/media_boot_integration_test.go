package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/mediamanifest"
)

func TestOSBootMediaComposition(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	objects := &testMediaObjects{}
	s.MediaObjects = objects
	const password = "test-only-os-boot-password"
	for _, name := range []string{"os-owner", "os-reviewer", "os-outsider"} {
		if _, err := s.CreateUser(ctx, name, name, password, true, true); err != nil {
			t.Fatal(err)
		}
	}
	_, owner := testLogin(t, s, "os-owner", password, "198.51.100.170")
	_, reviewer := testLogin(t, s, "os-reviewer", password, "198.51.100.171")
	project, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "OS boot", Slug: "os-boot"}, "os-boot-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Other OS", Slug: "other-os-boot"}, "other-os-boot-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	upload := func(scope *ProjectSummary, name, typ, arch string, deps map[string]any) *MediaItemSummary {
		t.Helper()
		item, e := s.IngestMedia(ctx, owner, scope, RegisterMediaInput{Type: typ, Name: name, Version: "v1", Arch: arch, Deps: deps}, bytes.NewBufferString(name+" actual bytes"), "test")
		if e != nil {
			t.Fatal(e)
		}
		for _, state := range []string{"verified", "approved"} {
			item, e = s.TransitionMedia(ctx, reviewer, scope, item.ID, state, "test")
			if e != nil {
				t.Fatal(e)
			}
		}
		return item
	}
	kernel := upload(nil, "kernel", "os", "amd64", nil)
	initrd := upload(project, "initrd", "os", "amd64", nil)
	template := upload(project, "cloud-template", "config", "noarch", nil)
	profile := mediaboot.Profile{Schema: mediaboot.Schema, Installer: "cloud-init", KernelMediaID: kernel.ID, InitrdMediaID: initrd.ID, TemplateMediaID: template.ID}
	refs := []MediaArtifactDependency{{MediaID: kernel.ID, SHA256: kernel.Sha256}, {MediaID: initrd.ID, SHA256: initrd.Sha256}, {MediaID: template.ID, SHA256: template.Sha256}}
	deps := func(p mediaboot.Profile, refs []MediaArtifactDependency) map[string]any {
		return map[string]any{"os_boot": p, "artifacts": refs}
	}
	root := upload(project, "rootfs", "os", "amd64", deps(profile, refs))
	driver := upload(project, "approved-driver-dependency", "driver", "amd64", map[string]any{"compatibility": map[string]any{"source": "fixture-report", "version": "matrix-1", "report_ref": "fixture:report", "requires": map[string][]string{"cpu.arch": {"amd64"}}}})
	driverRefs := append(append([]MediaArtifactDependency(nil), refs...), MediaArtifactDependency{MediaID: driver.ID, SHA256: driver.Sha256})
	driverRoot := upload(project, "rootfs-with-driver-reference", "os", "amd64", deps(profile, driverRefs))
	// Selection hints must remain a metadata read, without artifact streaming.
	s.MediaObjects = nil
	hints, e := s.ProvisionOSDependencies(ctx, owner, project, driverRoot.ID)
	s.MediaObjects = objects
	if e != nil || len(hints.Drivers) != 1 || hints.Drivers[0].MediaID != driver.ID || hints.Drivers[0].Version != driver.Version || hints.Drivers[0].SHA256 != driver.Sha256 || hints.Drivers[0].Compatibility == nil || hints.Drivers[0].Compatibility.ReportRef != "fixture:report" {
		t.Fatal("driver selection metadata", e)
	}
	if _, e = s.ProvisionOSDependencies(ctx, owner, other, driverRoot.ID); e == nil {
		t.Fatal("driver hints crossed project")
	}
	empty, e := s.ProvisionOSDependencies(ctx, owner, project, root.ID)
	if e != nil || empty.Drivers == nil || len(empty.Drivers) != 0 {
		t.Fatal("missing driver references inferred", e)
	}
	if _, e = s.TransitionMedia(ctx, reviewer, project, driver.ID, "retired", "test"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ProvisionOSDependencies(ctx, owner, project, driverRoot.ID); e == nil {
		t.Fatal("retired driver hint retained")
	}
	plan, err := s.ResolveMediaBootPlan(ctx, owner, project, root.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	if plan.RootID != root.ID || len(plan.Artifacts) != 4 || plan.Artifacts[0].MediaID != root.ID || plan.Artifacts[1].MediaID != kernel.ID || plan.Artifacts[2].MediaID != initrd.ID || plan.Artifacts[3].MediaID != template.ID || plan.Artifacts[1].ScopeKey != "global" {
		t.Fatalf("wrong fixed roles: %+v", plan)
	}
	if _, err = s.ResolveMediaBootPlan(ctx, owner, other, root.ID, "test"); err == nil {
		t.Fatal("cross-project OS root accepted")
	}
	if _, err = s.ResolveMediaBootPlan(ctx, owner, nil, root.ID, "test"); err == nil {
		t.Fatal("project OS root accepted globally")
	}
	if _, err = s.ResolveMediaBootPlan(ctx, owner, project, kernel.ID, "test"); err == nil {
		t.Fatal("plain OS artifact treated as a complete installer")
	}
	// A current session without project membership cannot borrow the owner's
	// project DTO. Admin bypass is explicitly disabled in the real user record.
	if _, err = s.DB.ExecContext(ctx, "UPDATE users SET is_admin=false WHERE username='os-outsider'"); err != nil {
		t.Fatal(err)
	}
	_, outsider := testLogin(t, s, "os-outsider", password, "198.51.100.172")
	if _, err = s.ResolveMediaBootPlan(ctx, outsider, project, root.ID, "test"); err == nil {
		t.Fatal("nonmember read OS composition")
	}
	for name, mutation := range map[string]func(*RegisterMediaInput){
		"unbound-boot-recipe": func(in *RegisterMediaInput) {
			p := profile
			p.Recipe = &mediaboot.Recipe{Schema: mediaboot.RecipeSchema, Protocol: mediaboot.InitrdProtocol, RootFSFormat: "squashfs", UEFIMediaID: strings.Repeat("a", 32), PreflightMediaID: strings.Repeat("b", 32), SSHHelperMediaID: strings.Repeat("c", 32), PreflightSHA256: strings.Repeat("d", 64), SSHHelperSHA256: strings.Repeat("e", 64)}
			in.Deps = deps(p, refs)
		},
		"wrong-root-kind":     func(in *RegisterMediaInput) { in.Type = "tool" },
		"generic-root-arch":   func(in *RegisterMediaInput) { in.Arch = "noarch" },
		"wrong-root-arch":     func(in *RegisterMediaInput) { in.Arch = "arm64" },
		"missing-direct-role": func(in *RegisterMediaInput) { in.Deps = deps(profile, refs[:2]) },
		"duplicate-role":      func(in *RegisterMediaInput) { p := profile; p.InitrdMediaID = p.KernelMediaID; in.Deps = deps(p, refs) },
		"wrong-template-type": func(in *RegisterMediaInput) {
			p := profile
			p.TemplateMediaID = kernel.ID
			p.KernelMediaID = template.ID
			in.Deps = deps(p, refs)
		},
	} {
		t.Run(name, func(t *testing.T) {
			in := RegisterMediaInput{Type: "os", Name: name, Version: "v1", Arch: "amd64", Deps: deps(profile, refs)}
			mutation(&in)
			if _, e := s.IngestMedia(ctx, owner, project, in, bytes.NewBufferString(name), "test"); e == nil {
				t.Fatal("invalid boot composition registered")
			}
		})
	}
	private, public := filepath.Join(t.TempDir(), "signer.pem"), filepath.Join(t.TempDir(), "trusted.pem")
	if _, err = mediamanifest.GenerateKeyFiles(private, public); err != nil {
		t.Fatal(err)
	}
	s.MediaManifestSigner, err = mediamanifest.LoadSigner(private)
	if err != nil {
		t.Fatal(err)
	}
	trusted, err := mediamanifest.LoadPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := s.ExportMediaManifest(ctx, owner, project, root.ID, "test")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := mediamanifest.Verify(raw, trusted)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, artifact := range verified.Payload.Artifacts {
		if artifact.MediaID == root.ID {
			found = artifact.OSBoot != nil && *artifact.OSBoot == profile
		}
	}
	if !found {
		t.Fatal("signed offline manifest lost OS boot roles")
	}
	// Later dependency lifecycle changes cannot retain a stale successful plan.
	if _, err = s.TransitionMedia(ctx, reviewer, project, initrd.ID, "retired", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ResolveMediaBootPlan(ctx, owner, project, root.ID, "test"); err == nil {
		t.Fatal("retired pinned initrd remained bootable")
	}
}
