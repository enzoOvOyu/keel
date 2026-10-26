package provisionboot

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
	"github.com/enzoOvOyu/keel/internal/provisionseed"
)

func renderFixture() (Scope, Grant, mediaboot.Plan, provisionpreflight.Spec, provisionseed.Seed, time.Time) {
	scope, f := fixture()
	g := f.grant
	now := time.Now()
	one := 1
	p := mediaboot.Plan{RootID: strings.Repeat("1", 32), Profile: mediaboot.Profile{Schema: mediaboot.Schema, Installer: "cloud-init", KernelMediaID: strings.Repeat("2", 32), InitrdMediaID: strings.Repeat("3", 32), TemplateMediaID: strings.Repeat("4", 32), Recipe: &mediaboot.Recipe{Schema: mediaboot.RecipeSchema, Protocol: mediaboot.InitrdProtocol, RootFSFormat: "squashfs", UEFIMediaID: strings.Repeat("5", 32), PreflightMediaID: strings.Repeat("6", 32), SSHHelperMediaID: strings.Repeat("7", 32), PreflightSHA256: strings.Repeat("a", 64), SSHHelperSHA256: strings.Repeat("a", 64)}}}
	for i, role := range []string{"rootfs", "kernel", "initrd", "template", "uefi", "preflight", "ssh-helper"} {
		arch := "amd64"
		if role == "template" {
			arch = "noarch"
		}
		p.Artifacts = append(p.Artifacts, mediaboot.Artifact{Role: role, MediaID: strings.Repeat(string(rune('1'+i)), 32), ScopeKey: "global", SHA256: strings.Repeat("a", 64), Size: 1, Arch: arch, ObjectVersion: "fixture-version"})
	}
	s := provisionpreflight.Spec{Schema: provisionpreflight.Schema, ProjectID: g.ProjectID, RequestID: g.RequestID, MachineID: g.MachineID, AttemptID: g.AttemptID, CommandID: g.CommandID, FencingToken: g.FencingToken, SnapshotSHA256: g.SnapshotSHA256, ExpiresAt: g.ExpiresAt, Expected: provisionpreflight.Expectation{Serial: "fixture-server", Arch: g.Arch, BootMAC: g.MAC, NICCount: &one, MinimumDisks: 1}}
	seed := provisionseed.Seed{UserData: "#cloud-config\nhostname: fixture-server\n", MetaData: "instance-id: keel-" + g.RequestID + "\nlocal-hostname: fixture-server\n"}
	return scope, g, p, s, seed, now
}
func TestBootBundleBindsFrozenImagesAndPreflight(t *testing.T) {
	scope, g, p, s, seed, now := renderFixture()
	b, err := RenderBootBundle(scope, g, p, s, seed, now)
	if err != nil {
		t.Fatal(err)
	}
	again, err := RenderBootBundle(scope, g, p, s, seed, now)
	if err != nil || !bytes.Equal(b.IPXE, again.IPXE) || !bytes.Equal(b.Manifest, again.Manifest) {
		t.Fatal("render not deterministic")
	}
	var m BootManifest
	if json.Unmarshal(b.Manifest, &m) != nil || m.RequestID != g.RequestID || m.FencingToken != g.FencingToken || m.PreflightSHA256 != bytesDigest(b.Preflight) || m.UserDataSHA256 != bytesDigest([]byte(seed.UserData)) {
		t.Fatalf("unbound manifest %+v", m)
	}
	script := string(b.IPXE)
	for _, required := range []string{"#!ipxe\n", "iseq ${net0/mac} " + g.MAC, "ifconf --configurator dhcp --timeout 15000 net0", "initrd=initrd.magic", "keel.manifest_sha256=" + bytesDigest(b.Manifest), "BOOTIF=01-02-aa-bb-cc-dd-ee", "ip=off", "keel.server_ipv4=192.0.2.1", "keel.client_cidr=192.0.2.0/24", "exit 1"} {
		if !strings.Contains(script, required) {
			t.Fatalf("missing %s", required)
		}
	}
	for _, forbidden := range []string{"autoinstall", "init=/bin/sh", "callback_token=", "shell\n", "sanboot", "ip=dhcp"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("uncontrolled installation argument %s", forbidden)
		}
	}
}
func TestBootBundleRejectsCrossRequestOrUnboundPreparation(t *testing.T) {
	for name, edit := range map[string]func(*Scope, *Grant, *mediaboot.Plan, *provisionpreflight.Spec, *provisionseed.Seed){
		"changed_fence": func(_ *Scope, _ *Grant, _ *mediaboot.Plan, s *provisionpreflight.Spec, _ *provisionseed.Seed) {
			s.FencingToken++
		},
		"changed_command": func(_ *Scope, _ *Grant, _ *mediaboot.Plan, s *provisionpreflight.Spec, _ *provisionseed.Seed) {
			s.CommandID = strings.Repeat("9", 32)
		},
		"changed_expectation_MAC": func(_ *Scope, _ *Grant, _ *mediaboot.Plan, s *provisionpreflight.Spec, _ *provisionseed.Seed) {
			s.Expected.BootMAC = "02:00:00:00:00:01"
		},
		"foreign_project_media": func(_ *Scope, _ *Grant, p *mediaboot.Plan, _ *provisionpreflight.Spec, _ *provisionseed.Seed) {
			p.Artifacts[0].ScopeKey = "project:" + strings.Repeat("f", 32)
		},
		"missing_recipe": func(_ *Scope, _ *Grant, p *mediaboot.Plan, _ *provisionpreflight.Spec, _ *provisionseed.Seed) {
			p.Profile.Recipe = nil
		},
		"old_seed_identity": func(_ *Scope, _ *Grant, _ *mediaboot.Plan, _ *provisionpreflight.Spec, s *provisionseed.Seed) {
			s.MetaData = "instance-id: keel-old\nlocal-hostname: fixture-server\n"
		},
		"multiple_seed_documents": func(_ *Scope, _ *Grant, _ *mediaboot.Plan, _ *provisionpreflight.Spec, s *provisionseed.Seed) {
			s.MetaData += "---\ninstance-id: another\n"
		},
		"script_origin_injection": func(s *Scope, _ *Grant, _ *mediaboot.Plan, _ *provisionpreflight.Spec, _ *provisionseed.Seed) {
			s.Origin = "https://boot.example.test/$bad"
		},
	} {
		t.Run(name, func(t *testing.T) {
			scope, g, p, s, seed, now := renderFixture()
			edit(&scope, &g, &p, &s, &seed)
			if _, err := RenderBootBundle(scope, g, p, s, seed, now); err == nil {
				t.Fatal("accepted unbound/injectable boot bundle")
			}
		})
	}
}
