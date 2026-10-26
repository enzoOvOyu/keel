package platform

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

func TestProvisionRolloutExplicitCanonicalSelection(t *testing.T) {
	a, b := strings.Repeat("a", 32), strings.Repeat("b", 32)
	input := []string{b, a}
	out, err := CanonicalProvisionRollout("validated", input)
	if err != nil || out[0] != a || input[0] != b {
		t.Fatal("selection not canonical or caller mutated", err)
	}
	for _, bad := range []struct {
		mode string
		ids  []string
	}{
		{"", nil}, {"auto", nil}, {"canary", []string{a}}, {"validated", nil}, {"validated", []string{a, a}}, {"validated", []string{a, b, strings.Repeat("c", 32)}}, {"validated", []string{"latest"}},
	} {
		if _, e := CanonicalProvisionRollout(bad.mode, bad.ids); e == nil {
			t.Fatal("ambiguous or unbounded selection accepted", bad)
		}
	}
	for _, canaries := range [][]ProvisionCanary{
		{}, {{InstallationID: a, Verified: false}}, {{InstallationID: a, Verified: true}, {InstallationID: b, Verified: false}},
	} {
		if matchProvisionCanaries(canaries, []string{a}) == nil {
			t.Fatal("missing/failed canary hidden by selection")
		}
	}
	if matchProvisionCanaries([]ProvisionCanary{{InstallationID: b, Verified: true}, {InstallationID: a, Verified: true}}, out) != nil {
		t.Fatal("complete verified set refused")
	}
	if matchProvisionCanaries([]ProvisionCanary{{InstallationID: b, Verified: true}}, []string{a}) == nil {
		t.Fatal("old proof substituted for current canary")
	}
}

func rolloutRequestFixture() *ProvisionRequest {
	id := func(c string) string { return strings.Repeat(c, 32) }
	plan := mediaboot.Plan{RootID: id("1"), Profile: mediaboot.Profile{Schema: mediaboot.Schema, Installer: "cloud-init", KernelMediaID: id("2"), InitrdMediaID: id("3"), TemplateMediaID: id("4"), Recipe: &mediaboot.Recipe{Schema: mediaboot.RecipeSchema, Protocol: mediaboot.InitrdProtocol, RootFSFormat: "squashfs", UEFIMediaID: id("5"), PreflightMediaID: id("6"), SSHHelperMediaID: id("7"), PreflightSHA256: strings.Repeat("a", 64), SSHHelperSHA256: strings.Repeat("a", 64)}}}
	for i, role := range []string{"rootfs", "kernel", "initrd", "template", "uefi", "preflight", "ssh-helper"} {
		arch := "amd64"
		if role == "template" {
			arch = "noarch"
		}
		plan.Artifacts = append(plan.Artifacts, mediaboot.Artifact{Role: role, MediaID: id(string(rune('1' + i))), ScopeKey: "global", Name: role, Version: "v1", SHA256: strings.Repeat("a", 64), Size: 1, Arch: arch, ObjectVersion: "immutable-v1"})
	}
	x := &ProvisionRequest{ProjectID: id("a"), MachineID: id("b"), AssetID: id("c")}
	x.Snapshot = ProvisionPreview{Target: ProvisionTarget{ProjectID: x.ProjectID, MachineID: x.MachineID, AssetID: x.AssetID, ModelVersionID: id("d"), ModelContentHash: strings.Repeat("e", 64), Arch: "amd64", Serial: "NODE-1", Hostname: "node-1"}, Media: plan}
	rehashRolloutRequest(x)
	return x
}
func rehashRolloutRequest(x *ProvisionRequest) {
	x.Snapshot.SnapshotSHA256 = ""
	raw, _ := json.Marshal(x.Snapshot)
	x.SnapshotSHA256 = digest(string(raw))
	x.Snapshot.SnapshotSHA256 = x.SnapshotSHA256
}

func TestProvisionRolloutIdentityBindsWholeOSAndModel(t *testing.T) {
	original := rolloutRequestFixture()
	want, err := provisionRolloutIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	reordered := rolloutRequestFixture()
	for i, j := 0, len(reordered.Snapshot.Media.Artifacts)-1; i < j; i, j = i+1, j-1 {
		reordered.Snapshot.Media.Artifacts[i], reordered.Snapshot.Media.Artifacts[j] = reordered.Snapshot.Media.Artifacts[j], reordered.Snapshot.Media.Artifacts[i]
	}
	rehashRolloutRequest(reordered)
	if got, e := provisionRolloutIdentity(reordered); e != nil || got != want {
		t.Fatal("artifact ordering changed OS identity", e)
	}
	other := rolloutRequestFixture()
	other.MachineID = strings.Repeat("f", 32)
	other.AssetID = strings.Repeat("9", 32)
	other.Snapshot.Target.MachineID = other.MachineID
	other.Snapshot.Target.AssetID = other.AssetID
	other.Snapshot.Target.Hostname = "node-2"
	other.Snapshot.Target.Serial = "NODE-2"
	rehashRolloutRequest(other)
	if got, e := provisionRolloutIdentity(other); e != nil || got != want {
		t.Fatal("per-node identity prevented shared canary proof", e)
	}
	for _, change := range []func(*ProvisionRequest){
		func(x *ProvisionRequest) { x.Snapshot.Target.ModelVersionID = strings.Repeat("8", 32) },
		func(x *ProvisionRequest) { x.Snapshot.Target.ModelContentHash = strings.Repeat("f", 64) },
		func(x *ProvisionRequest) { x.Snapshot.Media.Artifacts[0].SHA256 = strings.Repeat("b", 64) },
		func(x *ProvisionRequest) { x.Snapshot.Media.Artifacts[0].ObjectVersion = "different-object-version" },
		func(x *ProvisionRequest) {
			x.ProjectID = strings.Repeat("0", 32)
			x.Snapshot.Target.ProjectID = x.ProjectID
		},
	} {
		changed := rolloutRequestFixture()
		change(changed)
		rehashRolloutRequest(changed)
		if got, e := provisionRolloutIdentity(changed); e != nil || got == want {
			t.Fatal("source/model/project change reused canary", e)
		}
	}
}
