//go:build linux

package provisioninitrd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
	"github.com/enzoOvOyu/keel/internal/provisionseed"
)

func checksum(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
func prepareFixture(t *testing.T) (Input, *Files, *fixtureRunner, time.Time) {
	t.Helper()
	now := time.Now().UTC()
	one, zero, yes := 1, 0, true
	scope := provisionboot.Scope{ID: strings.Repeat("1", 32), ProjectID: strings.Repeat("2", 32), Interface: "fixture0", Server: mustAddr("192.0.2.1"), Clients: mustPrefix("192.0.2.0/24"), Origin: "https://boot.example.test"}
	g := provisionboot.Grant{ScopeID: scope.ID, ID: strings.Repeat("3", 32), ProjectID: scope.ProjectID, RequestID: strings.Repeat("4", 32), MachineID: strings.Repeat("5", 32), AttemptID: strings.Repeat("6", 32), CommandID: strings.Repeat("7", 32), SnapshotSHA256: strings.Repeat("8", 64), FencingToken: 23, MAC: "02:00:00:00:00:01", Arch: "amd64", ExpiresAt: now.Add(10 * time.Minute)}
	plan := mediaboot.Plan{RootID: strings.Repeat("1", 32), Profile: mediaboot.Profile{Schema: mediaboot.Schema, Installer: "cloud-init", KernelMediaID: strings.Repeat("2", 32), InitrdMediaID: strings.Repeat("3", 32), TemplateMediaID: strings.Repeat("4", 32), Recipe: &mediaboot.Recipe{Schema: mediaboot.RecipeSchema, Protocol: mediaboot.InitrdProtocol, RootFSFormat: "squashfs", UEFIMediaID: strings.Repeat("5", 32), PreflightMediaID: strings.Repeat("6", 32), SSHHelperMediaID: strings.Repeat("7", 32), PreflightSHA256: strings.Repeat("a", 64), SSHHelperSHA256: strings.Repeat("a", 64)}}}
	for index, role := range []string{"rootfs", "kernel", "initrd", "template", "uefi", "preflight", "ssh-helper"} {
		arch := "amd64"
		if role == "template" {
			arch = "noarch"
		}
		plan.Artifacts = append(plan.Artifacts, mediaboot.Artifact{Role: role, MediaID: strings.Repeat(string(rune('1'+index)), 32), ScopeKey: "global", SHA256: strings.Repeat("a", 64), Size: 1, Arch: arch, ObjectVersion: "fixture-version"})
	}
	spec := provisionpreflight.Spec{Schema: provisionpreflight.Schema, ProjectID: g.ProjectID, RequestID: g.RequestID, MachineID: g.MachineID, AttemptID: g.AttemptID, CommandID: g.CommandID, FencingToken: g.FencingToken, SnapshotSHA256: g.SnapshotSHA256, ExpiresAt: g.ExpiresAt, Expected: provisionpreflight.Expectation{Serial: "fixture-server", Arch: g.Arch, BootMAC: g.MAC, NICCount: &one, GPUCount: &zero, MinimumDisks: 1}}
	seed := provisionseed.Seed{UserData: "#cloud-config\nhostname: fixture-server\n", MetaData: "instance-id: keel-" + g.RequestID + "\nlocal-hostname: fixture-server\n"}
	bundle, err := provisionboot.RenderBootBundle(scope, g, plan, spec, seed, now)
	if err != nil {
		t.Fatal(err)
	}
	input := Input{ManifestURL: scope.Origin + "/boot/" + g.ID + "/manifest.json", ManifestSHA256: checksum(bundle.Manifest), GrantID: g.ID, BootMAC: g.MAC, Server: scope.Server, Clients: scope.Clients}
	observation := provisionpreflight.Observation{Source: provisionpreflight.Source, Firmware: "uefi", StartedAt: now.Add(-time.Second), FinishedAt: now, Serial: spec.Expected.Serial, Arch: spec.Expected.Arch, PCI: []provisionpreflight.PCI{{Address: "0000:01:00.0", Class: "0x020000", Identity: "15b3:1021", PhysicalSlot: "slot-1"}}, Ports: []provisionpreflight.Port{{Name: "eth0", MAC: g.MAC, PCI: "0000:01:00.0"}}, Disks: []provisionpreflight.Disk{{Device: "/dev/sda", Healthy: &yes, Reason: "fixture-only"}}}
	report, err := provisionpreflight.Evaluate(spec, observation, now)
	if err != nil || report.Status != "PASS" {
		t.Fatal("invalid software fixture", err)
	}
	raw, _ := json.Marshal(report)
	return input, &Files{Manifest: bundle.Manifest, Preflight: bundle.Preflight, Seed: seed}, &fixtureRunner{report: raw, spec: bundle.Preflight}, now
}

type fixtureRunner struct {
	report, spec       []byte
	checks, runs, exit int
	deny               bool
	afterRun           func()
}

func (r *fixtureRunner) Check(context.Context, mediaboot.Plan, string) error {
	r.checks++
	if r.deny {
		return ErrUnavailable
	}
	return nil
}
func (r *fixtureRunner) Run(_ context.Context, spec []byte, artifact mediaboot.Artifact, arch string) ([]byte, int, error) {
	r.runs++
	if !bytes.Equal(spec, r.spec) || artifact.Role != "preflight" || arch != "amd64" {
		return nil, -1, ErrUnavailable
	}
	if r.afterRun != nil {
		r.afterRun()
	}
	return r.report, r.exit, nil
}

type fixtureFiles struct {
	files map[string][]byte
	calls []string
}

func (f *fixtureFiles) Get(_ context.Context, name string) ([]byte, error) {
	f.calls = append(f.calls, name)
	raw, ok := f.files[name]
	if !ok {
		return nil, ErrUnavailable
	}
	return raw, nil
}
func TestInitrdPreparationOnlyProducesReadOnlyEvidence(t *testing.T) {
	i, files, runner, now := prepareFixture(t)
	source := &fixtureFiles{files: map[string][]byte{"manifest.json": files.Manifest, "preflight.json": files.Preflight, "user-data": []byte(files.Seed.UserData), "meta-data": []byte(files.Seed.MetaData)}}
	downloaded, err := Download(context.Background(), i, source)
	if err != nil || len(source.calls) != 4 {
		t.Fatal("public files failed", err)
	}
	result, err := Prepare(context.Background(), i, downloaded, runner, func() time.Time { return now })
	if err != nil || result.Status != "ready_for_storage_review" || result.InstallationPerformed || result.ReportSHA256 != checksum(runner.report) || runner.checks != 1 || runner.runs != 1 {
		t.Fatalf("unbound preparation: %+v %v", result, err)
	}
}
func TestInitrdStopsBeforeHelperOnBadBootInputs(t *testing.T) {
	for _, which := range []string{"hash", "grant", "mac", "user-data", "helper"} {
		t.Run(which, func(t *testing.T) {
			i, files, runner, now := prepareFixture(t)
			switch which {
			case "hash":
				i.ManifestSHA256 = strings.Repeat("f", 64)
			case "grant":
				i.GrantID = strings.Repeat("f", 32)
			case "mac":
				i.BootMAC = "02:00:00:00:00:02"
			case "user-data":
				files.Seed.UserData += "#changed"
			case "helper":
				runner.deny = true
			}
			if _, err := Prepare(context.Background(), i, files, runner, func() time.Time { return now }); err == nil || runner.runs != 0 {
				t.Fatal("bad input executed preflight")
			}
		})
	}
	i, files, _, _ := prepareFixture(t)
	source := &fixtureFiles{files: map[string][]byte{"manifest.json": append(append([]byte{}, files.Manifest...), byte(' '))}}
	if _, err := Download(context.Background(), i, source); err == nil || len(source.calls) != 1 {
		t.Fatal("download continued after wrong manifest digest")
	}
}
func TestInitrdVerifiesHelperExitAndFreshReport(t *testing.T) {
	for _, which := range []string{"false_exit", "bad_report", "late_expiry", "canceled", "failed_hardware", "blocked_hardware"} {
		t.Run(which, func(t *testing.T) {
			i, files, runner, now := prepareFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			clock := now
			switch which {
			case "false_exit":
				runner.exit = 1
			case "bad_report":
				runner.report = []byte(`{"status":"PASS"}`)
			case "late_expiry":
				runner.afterRun = func() { clock = now.Add(time.Hour) }
			case "canceled":
				runner.afterRun = cancel
			case "failed_hardware", "blocked_hardware":
				var prior provisionpreflight.Report
				json.Unmarshal(runner.report, &prior)
				if which == "failed_hardware" {
					prior.Observation.Serial = "wrong-server"
				} else {
					prior.Observation.Disks[0].Healthy = nil
				}
				report, err := provisionpreflight.Evaluate(prior.Spec, prior.Observation, now)
				if err != nil {
					t.Fatal(err)
				}
				runner.report, _ = json.Marshal(report)
				runner.exit = 1
			}
			result, err := Prepare(ctx, i, files, runner, func() time.Time { return clock })
			if which == "failed_hardware" || which == "blocked_hardware" {
				if err != nil || result.InstallationPerformed || result.Status == "ready_for_storage_review" {
					t.Fatal("non-PASS lost failure evidence", err)
				}
			} else if err == nil {
				t.Fatal("invalid evidence accepted")
			}
		})
	}
}
