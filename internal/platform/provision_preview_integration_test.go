package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/credentialvault"
	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

func provisionFixture(t *testing.T) (*Store, *AuthSession, *AuthSession, *ProjectSummary, string, *ProvisionTarget, *MediaItemSummary, *MediaItemSummary, *SSHDomainKey, ProvisionPreviewInput) {
	t.Helper()
	s := integrationStore(t)
	ctx := context.Background()
	s.MediaObjects = &testMediaObjects{}
	const password = "test-only-provision-preview-password"
	for _, name := range []string{"provision-owner", "provision-reviewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, true, name == "provision-owner"); err != nil {
			t.Fatal(err)
		}
	}
	_, owner := testLogin(t, s, "provision-owner", password, "198.51.100.180")
	_, reviewer := testLogin(t, s, "provision-reviewer", password, "198.51.100.181")
	project, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Provision preview", Slug: "provision-preview"}, "provision-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateDeviceModel(ctx, owner, project, CreateDeviceModelInput{Name: "Preview server", Kind: "server"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	version, err := s.PublishModelVersion(ctx, owner, project, model.ID, PublishModelVersionInput{HwSpec: map[string]any{"cpu_arch": "amd64"}, Slots: []ModelSlot{{SlotNo: 1, PcieSlot: "PCIe-1", ExpectNicModel: "Fixture-NIC", PanelPort: "p1", DefaultRole: "management"}}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CommitBomImport(ctx, owner, project, BomImportInput{FileVersion: "provision-v1", Machines: []BomMachineRow{{SerialNumber: "PREVIEW-SERVER-1", Hostname: "preview-node-01", ModelName: model.Name, Ports: []BomPortRow{{MacAddress: "aa:bb:cc:dd:ee:81", PortName: "p1", Rail: 0}}}}}, "test"); err != nil {
		t.Fatal(err)
	}
	var asset string
	if err = s.DB.QueryRowContext(ctx, "SELECT id FROM project_assets WHERE project_id=$1", project.ID).Scan(&asset); err != nil {
		t.Fatal(err)
	}
	target, err := s.GetProvisionTarget(ctx, owner, project, asset)
	if err != nil {
		t.Fatal(err)
	}
	// Read preparation must not wait for the unrelated last-admin mutex.
	blocker, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback()
	if e = lockKey(ctx, blocker, "keel-admin-initialization"); e != nil {
		t.Fatal(e)
	}
	readContext, cancelRead := context.WithTimeout(ctx, time.Second)
	_, e = s.GetProvisionTarget(readContext, owner, project, asset)
	cancelRead()
	blocker.Rollback()
	if e != nil {
		t.Fatalf("unrelated admin mutex blocked target read: %v", e)
	}
	if target.ModelVersionID != version.ID || target.Arch != "amd64" || len(target.Ports) != 1 || target.Ports[0].MAC != "aa:bb:cc:dd:ee:81" {
		t.Fatalf("wrong BOM identity: %+v", target)
	}
	upload := func(name, typ, arch, payload string, deps map[string]any) *MediaItemSummary {
		t.Helper()
		item, e := s.IngestMedia(ctx, owner, project, RegisterMediaInput{Type: typ, Name: name, Version: "v1", Arch: arch, Deps: deps}, strings.NewReader(payload), "test")
		if e != nil {
			t.Fatal(e)
		}
		for _, state := range []string{"verified", "approved"} {
			item, e = s.TransitionMedia(ctx, reviewer, project, item.ID, state, "test")
			if e != nil {
				t.Fatal(e)
			}
		}
		return item
	}
	kernel := upload("preview-kernel", "os", "amd64", "kernel bytes", nil)
	initrd := upload("preview-initrd", "os", "amd64", "initrd bytes", nil)
	template := upload("preview-template", "config", "noarch", "#cloud-config\nlocale: en_US.UTF-8\npackage_update: false\n", nil)
	profile := mediaboot.Profile{Schema: mediaboot.Schema, Installer: "cloud-init", KernelMediaID: kernel.ID, InitrdMediaID: initrd.ID, TemplateMediaID: template.ID}
	refs := []MediaArtifactDependency{{MediaID: kernel.ID, SHA256: kernel.Sha256}, {MediaID: initrd.ID, SHA256: initrd.Sha256}, {MediaID: template.ID, SHA256: template.Sha256}}
	root := upload("preview-rootfs", "os", "amd64", "rootfs bytes", map[string]any{"os_boot": profile, "artifacts": refs})
	master := filepath.Join(t.TempDir(), "master")
	if err = credentialvault.GenerateMaster(master); err != nil {
		t.Fatal(err)
	}
	vault, err := credentialvault.Load(master)
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.CreateSSHDomainKey(ctx, vault, credentialvault.Identity{ProjectID: project.ID, Ref: "preview-project-key", Version: "v1", ExpiresAt: time.Now().Add(2 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	input := ProvisionPreviewInput{AssetVersion: target.AssetVersion, BootPortID: target.Ports[0].ID, MediaID: root.ID, DomainKeyID: key.ID}
	return s, owner, reviewer, project, asset, target, root, kernel, key, input
}

func TestProvisionPreviewUsesCurrentBOMAndApprovedOS(t *testing.T) {
	s, owner, _, project, asset, target, root, kernel, key, input := provisionFixture(t)
	ctx := context.Background()
	preview, err := s.PreviewProvisionSeed(ctx, owner, project, asset, input, "test")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Target.Hostname != target.Hostname || preview.BootPort.MAC != target.Ports[0].MAC || preview.DomainKey.ID != key.ID || preview.SnapshotSHA256 == "" || !strings.Contains(preview.Seed.UserData, "restrict,command=") || !strings.Contains(preview.Seed.UserData, strings.TrimSpace(key.PublicKey)) {
		t.Fatalf("incorrect seed: %+v", preview)
	}
	again, err := s.PreviewProvisionSeed(ctx, owner, project, asset, input, "test")
	if err != nil || again.SnapshotSHA256 != preview.SnapshotSHA256 {
		t.Fatalf("same pinned preview changed: %v", err)
	}
	raw, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("PRIVATE KEY")) || bytes.Contains(raw, []byte("ciphertext")) || bytes.Contains(raw, []byte("callback_token")) {
		t.Fatal("preview exposed credentials")
	}
	var state, trust string
	var executions int
	if err = s.DB.QueryRowContext(ctx, "SELECT a.state,m.ssh_trust FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1", asset).Scan(&state, &trust); err != nil {
		t.Fatal(err)
	}
	if state != target.State || trust == "established" {
		t.Fatal("preview changed installation or trust state")
	}
	if err = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_executions").Scan(&executions); err != nil {
		t.Fatal(err)
	}
	if executions != 0 {
		t.Fatal("preview created execution")
	}
	for name, mutate := range map[string]func(*ProvisionPreviewInput){"stale-version": func(i *ProvisionPreviewInput) { i.AssetVersion++ }, "foreign-port": func(i *ProvisionPreviewInput) { i.BootPortID = RandomID() }, "foreign-key": func(i *ProvisionPreviewInput) { i.DomainKeyID = RandomID() }, "incomplete-image": func(i *ProvisionPreviewInput) { i.MediaID = kernel.ID }} {
		t.Run(name, func(t *testing.T) {
			copy := input
			mutate(&copy)
			if _, e := s.PreviewProvisionSeed(ctx, owner, project, asset, copy, "test"); e == nil {
				t.Fatal("invalid provisioning selection accepted")
			}
		})
	}
	filtered, err := s.ListMediaFiltered(ctx, owner, project, Pagination{Page: 1, PageSize: 20}, MediaListFilter{Type: "os", State: "approved", StoredOnly: true, BootOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || filtered.Items[0].ID != root.ID {
		t.Fatal("OS selector included component-only media")
	}
	// Version/key lifecycle is rechecked on every read; an old successful preview
	// cannot keep an installation key or BOM binding alive.
	if _, err = s.RevokeSSHDomainKey(ctx, project.ID, key.ID, key.StateVersion, "test revoke"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PreviewProvisionSeed(ctx, owner, project, asset, input, "test"); err == nil {
		t.Fatal("revoked key accepted after successful preview")
	}
}
