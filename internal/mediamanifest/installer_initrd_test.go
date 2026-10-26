package mediamanifest

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionimage"
)

// All ELF bytes are non-executed format fixtures, not accepted runtime tools.
func signedInitrdFixture(t *testing.T) ([]byte, ed25519.PublicKey, string) {
	return signedInitrdFixtureMode(t, false)
}
func signedInitrdFixtureMode(t *testing.T, writer bool) ([]byte, ed25519.PublicKey, string) {
	t.Helper()
	signer, key, payload := fixture(t)
	base := payload.Artifacts[0]
	payload.Artifacts = nil
	set := mediaboot.RuntimeSet{Schema: mediaboot.RuntimeSetSchema, Arch: "amd64"}
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &private.PublicKey, private)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	components := []*mediaboot.RuntimeComponent{&set.Init, &set.Preflight, &set.SSHHelper, &set.SMART, &set.BootCA}
	if writer {
		set.Schema = mediaboot.WriterRuntimeSetSchema
		set.Writer = &mediaboot.WriterTools{}
		components = append(components, &set.Writer.UnsquashFS, &set.Writer.MkfsExt4, &set.Writer.MkfsVFAT)
	}
	for i, c := range components {
		data := append(overlayExecutableFixture(), byte(i))
		if i == 4 {
			data = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
		}
		sum := sha256.Sum256(data)
		c.MediaID = strings.Repeat(string(rune('1'+i)), 32)
		c.SHA256 = hex.EncodeToString(sum[:])
		a := base
		a.MediaID = c.MediaID
		a.SHA256 = c.SHA256
		a.Size = int64(len(data))
		a.Type = "os"
		if i >= 5 {
			a.Type = "tool"
		}
		a.Arch = "amd64"
		if i == 4 {
			a.Type = "config"
			a.Arch = "noarch"
		}
		a.Name = "runtime-input-" + string(rune('1'+i))
		a.URI = "media://" + a.Type + "/" + a.Name + "/v1"
		payload.Artifacts = append(payload.Artifacts, a)
		if err = os.WriteFile(filepath.Join(directory, a.MediaID+".bin"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	config, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(config)
	root := base
	root.MediaID = strings.Repeat("6", 32)
	if writer {
		root.MediaID = strings.Repeat("9", 32)
	}
	root.Type = "config"
	root.Arch = "amd64"
	root.Name = "independent-runtime-set"
	root.URI = "media://config/" + root.Name + "/v1"
	root.Size = int64(len(config))
	root.SHA256 = hex.EncodeToString(sum[:])
	root.Dependencies = []Dependency{}
	for _, a := range payload.Artifacts {
		root.Dependencies = append(root.Dependencies, Dependency{MediaID: a.MediaID, SHA256: a.SHA256})
	}
	payload.Artifacts = append(payload.Artifacts, root)
	payload.RootID = root.MediaID
	if err = os.WriteFile(filepath.Join(directory, root.MediaID+".bin"), config, 0600); err != nil {
		t.Fatal(err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		t.Fatal(err)
	}
	return raw, key, directory
}

func runtimeBootPlan(result *provisionimage.InitrdResult) mediaboot.Plan {
	set := result.Metadata.Runtime
	ids := map[string]string{"rootfs": strings.Repeat("a", 32), "kernel": strings.Repeat("b", 32), "initrd": strings.Repeat("c", 32), "template": strings.Repeat("d", 32), "uefi": strings.Repeat("e", 32), "preflight": set.Preflight.MediaID, "ssh-helper": set.SSHHelper.MediaID}
	p := mediaboot.Plan{RootID: ids["rootfs"], Profile: mediaboot.Profile{Schema: mediaboot.Schema, Installer: "cloud-init", KernelMediaID: ids["kernel"], InitrdMediaID: ids["initrd"], TemplateMediaID: ids["template"], Recipe: &mediaboot.Recipe{Schema: mediaboot.RecipeSchema, Protocol: mediaboot.InitrdProtocol, RootFSFormat: "squashfs", UEFIMediaID: ids["uefi"], PreflightMediaID: set.Preflight.MediaID, PreflightSHA256: set.Preflight.SHA256, SSHHelperMediaID: set.SSHHelper.MediaID, SSHHelperSHA256: set.SSHHelper.SHA256}}}
	for _, role := range []string{"rootfs", "kernel", "initrd", "template", "uefi", "preflight", "ssh-helper"} {
		a := mediaboot.Artifact{Role: role, MediaID: ids[role], ScopeKey: "global", Arch: set.Arch, SHA256: strings.Repeat("c", 64), Size: 1, ObjectVersion: "exact-object-v1"}
		if role == "template" {
			a.Arch = "noarch"
		}
		if role == "initrd" {
			a.SHA256 = result.SHA256
			a.Size = result.Size
		}
		for _, c := range result.Metadata.Components {
			if c.MediaID == a.MediaID {
				a.SHA256 = c.SHA256
				a.Size = c.Size
			}
		}
		p.Artifacts = append(p.Artifacts, a)
	}
	return p
}

func TestInstallerInitrdBindsActualArchiveToFrozenRolePlan(t *testing.T) {
	raw, key, directory := signedInitrdFixture(t)
	output := filepath.Join(t.TempDir(), "runtime.cpio.gz")
	result, err := CreateInstallerInitrd(context.Background(), raw, key, directory, output)
	if err != nil {
		t.Fatal(err)
	}
	archive, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	plan := runtimeBootPlan(result)
	if _, err = VerifyInstallerInitrdForPlan(context.Background(), bytes.NewReader(archive), plan, key); err != nil {
		t.Fatal("same frozen role plan", err)
	}
	// Keep the plan intrinsically consistent while changing the helper approval:
	// the actual archive must still reject a different valid-looking recipe.
	for i := range plan.Artifacts {
		if plan.Artifacts[i].Role == "preflight" {
			plan.Artifacts[i].SHA256 = strings.Repeat("d", 64)
		}
	}
	plan.Profile.Recipe.PreflightSHA256 = strings.Repeat("d", 64)
	if plan.ValidateInstallable() != nil {
		t.Fatal("mismatch fixture must remain a valid plan")
	}
	if _, err = VerifyInstallerInitrdForPlan(context.Background(), bytes.NewReader(archive), plan, key); err == nil {
		t.Fatal("different pinned helper accepted")
	}
	plan = runtimeBootPlan(result)
	for i := range plan.Artifacts {
		if plan.Artifacts[i].Role == "initrd" {
			plan.Artifacts[i].SHA256 = strings.Repeat("e", 64)
		}
	}
	if _, err = VerifyInstallerInitrdForPlan(context.Background(), bytes.NewReader(archive), plan, key); err == nil {
		t.Fatal("different outer archive digest accepted")
	}
}

func TestInstallerInitrdIndependentApprovedClosureAndPublication(t *testing.T) {
	raw, key, directory := signedInitrdFixture(t)
	output := filepath.Join(t.TempDir(), "runtime.cpio.gz")
	result, err := CreateInstallerInitrd(context.Background(), raw, key, directory, output)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyInstallerInitrd(context.Background(), output, key)
	if err != nil || verified.SHA256 != result.SHA256 || verified.Size != result.Size || len(verified.Metadata.Components) != 5 {
		t.Fatal("signed runtime roundtrip", err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("output must be private", err)
	}
	if _, err = CreateInstallerInitrd(context.Background(), raw, key, directory, output); err == nil {
		t.Fatal("overwrote existing initrd")
	}
	_, wrongKey, _ := signedInitrdFixture(t)
	if _, err = VerifyInstallerInitrd(context.Background(), output, wrongKey); err == nil {
		t.Fatal("embedded key cannot replace independent trust root")
	}
	if err = os.WriteFile(filepath.Join(directory, strings.Repeat("1", 32)+".bin"), []byte("changed init"), 0600); err != nil {
		t.Fatal(err)
	}
	badOutput := filepath.Join(t.TempDir(), "changed.cpio.gz")
	if _, err = CreateInstallerInitrd(context.Background(), raw, key, directory, badOutput); err == nil {
		t.Fatal("changed approved input")
	}
	if _, err = os.Stat(badOutput); !os.IsNotExist(err) {
		t.Fatal("failed build published output")
	}
}
func TestInstallerInitrdRejectsCanceledAndUntrustedBuild(t *testing.T) {
	raw, key, directory := signedInitrdFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	output := filepath.Join(t.TempDir(), "canceled.cpio.gz")
	if _, err := CreateInstallerInitrd(ctx, raw, key, directory, output); err == nil {
		t.Fatal("canceled image build")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatal("canceled output published")
	}
	_, other, _ := signedInitrdFixture(t)
	if _, err := CreateInstallerInitrd(context.Background(), raw, other, directory, output); err == nil {
		t.Fatal("untrusted signing key")
	}
}

func TestInstallerInitrdWriterToolsIndependentClosure(t *testing.T) {
	raw, key, directory := signedInitrdFixtureMode(t, true)
	output := filepath.Join(t.TempDir(), "writer-inputs.cpio.gz")
	result, err := CreateInstallerInitrd(context.Background(), raw, key, directory, output)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyInstallerInitrd(context.Background(), output, key)
	if err != nil || result.Metadata.Schema != provisionimage.WriterInitrdSchema || verified.Metadata.Runtime.Writer == nil || len(verified.Metadata.Components) != 8 || !verified.Metadata.Runtime.Equal(result.Metadata.Runtime) {
		t.Fatal("writer tool inputs not independently bound", err)
	}
	archive, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = VerifyInstallerInitrdForPlan(context.Background(), bytes.NewReader(archive), runtimeBootPlan(result), key); err != nil {
		t.Fatal("final role plan did not retain actual approved inputs", err)
	}
	// Changing a tool after independent approval must leave no published archive.
	id := result.Metadata.Runtime.Writer.MkfsExt4.MediaID
	if err = os.WriteFile(filepath.Join(directory, id+".bin"), []byte("changed tool bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	changed := filepath.Join(t.TempDir(), "changed-tool.cpio.gz")
	if _, err = CreateInstallerInitrd(context.Background(), raw, key, directory, changed); err == nil {
		t.Fatal("changed tool accepted")
	}
	if _, err = os.Stat(changed); !os.IsNotExist(err) {
		t.Fatal("failed writer-input build published")
	}
}
