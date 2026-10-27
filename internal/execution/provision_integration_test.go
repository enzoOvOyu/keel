package execution

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/credentialvault"
	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/platform"
)

// Only the object transport is substituted. PostgreSQL, auth, approvals,
// execution leases, hashing and receipt reconciliation remain the real code.
type provisionObjects struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (o *provisionObjects) Put(ctx context.Context, key string, r io.Reader, size int64) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.data == nil {
		o.data = map[string][]byte{}
	}
	o.data[key] = raw
	return "v1", nil
}
func (o *provisionObjects) Open(ctx context.Context, key, version string) (io.ReadCloser, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	raw, ok := o.data[key]
	if !ok || version != "v1" {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(append([]byte{}, raw...))), nil
}

type provisionKernelFixture struct {
	*agentKernelFixture
	reviewer *Actor
	request  *platform.ProvisionRequest
	asset    string
	before   platform.ProvisionTarget
}

func provisionKernel(t *testing.T) *provisionKernelFixture {
	return provisionKernelWithRecipe(t, false)
}

func provisionKernelWithRecipe(t *testing.T, installable bool) *provisionKernelFixture {
	return provisionKernelWithImage(t, installable, false)
}
func provisionKernelWithImage(t *testing.T, installable, signedImage bool, writer ...bool) *provisionKernelFixture {
	t.Helper()
	f := agentKernel(t)
	ctx := context.Background()
	s := f.store
	s.ProvisionPrepareEnabled = true
	s.MediaObjects = &provisionObjects{}
	f.service.ProvisionStore = s
	suffix := platform.RandomID()[:10]
	password := "provision-kernel-test-only-long-password"
	username := "prep-review-" + suffix
	_, err := s.CreateUser(ctx, username, username, password, true, false)
	mustAgent(t, err)
	pre, _, err := s.NewPreSession(ctx)
	mustAgent(t, err)
	_, reviewer, err := s.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: username, Password: password, Method: "local"}, "198.51.100.184", "provision-kernel")
	mustAgent(t, err)
	project, err := s.GetProject(ctx, f.actor, f.project)
	mustAgent(t, err)
	model, err := s.CreateDeviceModel(ctx, f.actor, project, platform.CreateDeviceModelInput{Name: "Prep " + suffix, Kind: "server"}, "test")
	mustAgent(t, err)
	hardware := map[string]any{"cpu_arch": "amd64"}
	var bom map[string]any
	if signedImage {
		hardware["gpu_count"] = 0
		bom = map[string]any{"nic_count": 1}
	}
	_, err = s.PublishModelVersion(ctx, f.actor, project, model.ID, platform.PublishModelVersionInput{HwSpec: hardware, BomExpect: bom, Slots: []platform.ModelSlot{{SlotNo: 1, PcieSlot: "PCIe-1", ExpectNicModel: "Fixture-NIC", PanelPort: "p1", DefaultRole: "management"}}}, "test")
	mustAgent(t, err)
	var serial string
	mustAgent(t, s.DB.QueryRowContext(ctx, "SELECT serial_number FROM machines WHERE id=$1", f.machine).Scan(&serial))
	// Import updates the existing stable machine rather than inventing a separate resource identity.
	_, err = s.CommitBomImport(ctx, f.actor, project, platform.BomImportInput{FileVersion: "prep-" + suffix, Machines: []platform.BomMachineRow{{SerialNumber: serial, Hostname: "prep-" + suffix, ModelName: model.Name, Ports: []platform.BomPortRow{{MacAddress: "02:" + suffix[0:2] + ":" + suffix[2:4] + ":" + suffix[4:6] + ":" + suffix[6:8] + ":" + suffix[8:10], PortName: "p1", Rail: 0}}}}}, "test")
	mustAgent(t, err)
	// BOM changes the asset version; an identity enrolled against the old
	// version must remain invalid. Enroll this fixture against the current BOM.
	mustAgent(t, s.RevokeAgentCredential(ctx, f.project, f.credential.ID, f.credential.StateVersion, "replace pre-BOM software fixture identity"))
	f.enroll(t)
	var asset string
	mustAgent(t, s.DB.QueryRowContext(ctx, "SELECT id FROM project_assets WHERE project_id=$1 AND machine_id=$2", f.project, f.machine).Scan(&asset))
	target, err := s.GetProvisionTarget(ctx, f.actor, project, asset)
	mustAgent(t, err)
	upload := func(name, typ, arch, payload string, deps map[string]any) *platform.MediaItemSummary {
		t.Helper()
		m, e := s.IngestMedia(ctx, f.actor, project, platform.RegisterMediaInput{Name: name + suffix, Type: typ, Arch: arch, Version: "v1", Deps: deps}, strings.NewReader(payload), "test")
		mustAgent(t, e)
		for _, state := range []string{"verified", "approved"} {
			m, e = s.TransitionMedia(ctx, reviewer, project, m.ID, state, "test")
			mustAgent(t, e)
		}
		return m
	}
	kernel := upload("kernel", "os", "amd64", "fixture kernel", nil)
	var initrd *platform.MediaItemSummary
	if !signedImage {
		initrd = upload("initrd", "os", "amd64", "fixture initrd", nil)
	}
	template := upload("template", "config", "noarch", "#cloud-config\nlocale: en_US.UTF-8\n", nil)
	var signedPreflight, signedSSH *platform.MediaItemSummary
	if signedImage {
		initrd, signedPreflight, signedSSH = signedProvisionImageFixture(t, s, f.actor, project, upload, writer...)
	}
	profile := mediaboot.Profile{Schema: mediaboot.Schema, Installer: "cloud-init", KernelMediaID: kernel.ID, InitrdMediaID: initrd.ID, TemplateMediaID: template.ID}
	refs := []platform.MediaArtifactDependency{{MediaID: kernel.ID, SHA256: kernel.Sha256}, {MediaID: initrd.ID, SHA256: initrd.Sha256}, {MediaID: template.ID, SHA256: template.Sha256}}
	if installable {
		uefi := upload("uefi", "os", "amd64", "non-executed fixture UEFI", nil)
		preflight, ssh := signedPreflight, signedSSH
		if !signedImage {
			preflight = upload("preflight", "os", "amd64", "non-executed fixture preflight", nil)
			ssh = upload("ssh-helper", "os", "amd64", "non-executed fixture SSH helper", nil)
		}
		profile.Recipe = &mediaboot.Recipe{Schema: mediaboot.RecipeSchema, Protocol: mediaboot.InitrdProtocol, RootFSFormat: "squashfs", UEFIMediaID: uefi.ID, PreflightMediaID: preflight.ID, SSHHelperMediaID: ssh.ID, PreflightSHA256: preflight.Sha256, SSHHelperSHA256: ssh.Sha256}
		if len(writer) > 0 && writer[0] {
			profile.Recipe.Schema = mediaboot.TargetRecipeSchema
			profile.Recipe.TargetBoot = &mediaboot.TargetBoot{Schema: mediaboot.TargetBootSchema, Path: mediaboot.TargetUKIPath, SHA256: strings.Repeat("a", 64), Size: 4096, KernelRelease: "fixture-kernel", CommandLine: mediaboot.TargetCommandLine}
		}
		for _, media := range []*platform.MediaItemSummary{uefi, preflight, ssh} {
			refs = append(refs, platform.MediaArtifactDependency{MediaID: media.ID, SHA256: media.Sha256})
		}
	}
	root := upload("rootfs", "os", "amd64", "fixture rootfs", map[string]any{"os_boot": profile, "artifacts": refs})
	master := filepath.Join(t.TempDir(), "master")
	mustAgent(t, credentialvault.GenerateMaster(master))
	vault, err := credentialvault.Load(master)
	mustAgent(t, err)
	key, err := s.CreateSSHDomainKey(ctx, vault, credentialvault.Identity{ProjectID: f.project, Ref: "prep-key", Version: "v1", ExpiresAt: time.Now().Add(2 * time.Hour)})
	mustAgent(t, err)
	request, err := s.CreateProvisionRequest(ctx, f.actor, project, asset, platform.CreateProvisionRequestInput{ProvisionPreviewInput: platform.ProvisionPreviewInput{AssetVersion: target.AssetVersion, BootPortID: target.Ports[0].ID, MediaID: root.ID, DomainKeyID: key.ID}, Reason: "isolated source verification", Reinstall: true, ConfirmSerial: serial}, platform.RandomID(), "test")
	mustAgent(t, err)
	request, err = s.ChangeProvisionRequest(ctx, reviewer, f.project, request.ID, "approve", platform.ProvisionRequestActionInput{ExpectedVersion: request.StateVersion, Reason: "independent installation source review"}, platform.RandomID(), "test")
	mustAgent(t, err)
	return &provisionKernelFixture{agentKernelFixture: f, reviewer: reviewer, request: request, asset: asset, before: *target}
}
func (f *provisionKernelFixture) prepare(t *testing.T) *Execution {
	t.Helper()
	x, err := f.service.CreateProvisionPreparation(context.Background(), f.actor, f.project, f.request.ID, f.request.StateVersion, platform.RandomID(), "test")
	mustAgent(t, err)
	return x
}
func TestProvisionExecutionKernelPostgres(t *testing.T) {
	ctx := context.Background()
	t.Run("exclusive_machine_lease_and_actual_sources", func(t *testing.T) {
		f := provisionKernel(t)
		agent := f.create(t)
		agentClaim := f.claim(t, agent.ID)
		if agentClaim == nil {
			t.Fatal("agent claim absent")
		}
		prep := f.prepare(t)
		if claim := f.claim(t, prep.ID); claim != nil {
			t.Fatal("preparation overlapped shared Agent machine lease")
		}
		mustAgent(t, f.service.RunClaim(ctx, agentClaim))
		claim := f.claim(t, prep.ID)
		if claim == nil {
			t.Fatal("preparation never acquired released lease")
		}
		var resource, mode string
		mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT resource_id,mode FROM exec_resource_leases WHERE attempt_id=$1 AND state='held'", claim.Attempt.ID).Scan(&resource, &mode))
		if resource != "device:machine:"+f.machine || mode != "exclusive" {
			t.Fatal("wrong shared hardware lease", resource, mode)
		}
		mustAgent(t, f.service.RunClaim(ctx, claim))
		result, err := f.store.ReadProvisionRequest(ctx, f.actor, f.project, f.request.ID)
		mustAgent(t, err)
		if result.State != "prepared" {
			t.Fatal("sources were not verified", result.State)
		}
		var state, trust string
		mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT a.state,m.ssh_trust FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1", f.asset).Scan(&state, &trust))
		if state != f.before.State || trust == "established" {
			t.Fatal("preparation fabricated installation or trust")
		}
	})
	t.Run("pending_cancel_and_single_consumption", func(t *testing.T) {
		f := provisionKernel(t)
		prep := f.prepare(t)
		if _, err := f.service.CreateProvisionPreparation(ctx, f.actor, f.project, f.request.ID, f.request.StateVersion, platform.RandomID(), "test"); err == nil {
			t.Fatal("approval consumed twice")
		}
		_, err := f.service.Control(ctx, f.actor, f.project, prep.ID, ControlInput{Action: "cancel", ExpectedVersion: prep.StateVersion}, platform.RandomID(), "test")
		mustAgent(t, err)
		request, err := f.store.ReadProvisionRequest(ctx, f.actor, f.project, f.request.ID)
		mustAgent(t, err)
		if request.State != "canceled" {
			t.Fatal("never-started request stuck preparing")
		}
	})
	t.Run("receipt_binding_and_unknown_retains_lock", func(t *testing.T) {
		f := provisionKernel(t)
		prep := f.prepare(t)
		claim := f.claim(t, prep.ID)
		if claim == nil {
			t.Fatal("claim absent")
		}
		now := time.Now().UTC()
		receipt := Receipt{CommandID: claim.Attempt.CommandID, AttemptID: claim.Attempt.ID, FencingToken: claim.Attempt.FencingToken, Result: "success", StartedAt: now, FinishedAt: now, Output: map[string]any{"schema": "keel.provision-preparation-proof/v1", "request_id": platform.RandomID(), "snapshot_sha256": f.request.SnapshotSHA256, "installation_performed": false}}
		if err := f.service.RecordReceipt(ctx, receipt); err == nil {
			t.Fatal("forged request binding persisted")
		}
		receipt.Output["request_id"] = f.request.ID
		mustAgent(t, f.service.RecordReceipt(ctx, receipt))
		_, err := f.store.DB.ExecContext(ctx, "UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1", claim.Attempt.ID)
		mustAgent(t, err)
		_, err = f.service.ReapExpired(ctx)
		mustAgent(t, err)
		var pinned int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'", claim.Attempt.ID).Scan(&pinned))
		if pinned != 1 {
			t.Fatal("unknown released machine")
		}
		if err = f.service.Complete(ctx, claim); err == nil {
			t.Fatal("late completion bypassed reconciliation")
		}
	})
}
