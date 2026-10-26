package provisionimage

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

// These superblocks and component readers are format fixtures. They are never
// decompressed/executed and do not represent a bootable or accepted OS image.
func rootFSHeaderFixture() ([]byte, mediaboot.Artifact) {
	raw := make([]byte, 4096)
	u16 := func(off int, v uint16) { binary.LittleEndian.PutUint16(raw[off:], v) }
	u32 := func(off int, v uint32) { binary.LittleEndian.PutUint32(raw[off:], v) }
	u64 := func(off int, v uint64) { binary.LittleEndian.PutUint64(raw[off:], v) }
	u32(0, 0x73717368)
	u32(4, 10)
	u32(12, 131072)
	u16(20, 4)
	u16(22, 17)
	u16(26, 1)
	u16(28, 4)
	u64(32, 0)
	u64(40, 512)
	u64(48, 496)
	u64(56, ^uint64(0))
	u64(64, 128)
	u64(72, 256)
	u64(80, ^uint64(0))
	u64(88, ^uint64(0))
	return raw, mediaboot.Artifact{Role: "rootfs", MediaID: strings.Repeat("1", 32), SHA256: rawSHA(raw), Size: int64(len(raw)), Arch: "amd64"}
}

func TestRootFSArchiveRequiresFullHashAndBoundedV4Tables(t *testing.T) {
	raw, artifact := rootFSHeaderFixture()
	got, err := VerifyRootFSArchive(context.Background(), bytes.NewReader(raw), artifact)
	if err != nil || got.BytesUsed != 512 || got.Inodes != 10 || got.Compression != 4 || got.SHA256 != artifact.SHA256 {
		t.Fatalf("rootfs byte checks: %#v %v", got, err)
	}
	for name, change := range map[string]func([]byte, *mediaboot.Artifact){
		"wrong_hash":             func(b []byte, _ *mediaboot.Artifact) { b[300] ^= 1 },
		"appended_payload":       func(b []byte, _ *mediaboot.Artifact) { b[4000] = 1 },
		"magic_only":             func(b []byte, _ *mediaboot.Artifact) { clear(b[4:96]) },
		"big_endian":             func(b []byte, _ *mediaboot.Artifact) { binary.BigEndian.PutUint32(b[:4], 0x73717368) },
		"block_mismatch":         func(b []byte, _ *mediaboot.Artifact) { binary.LittleEndian.PutUint16(b[22:], 16) },
		"unknown_version":        func(b []byte, _ *mediaboot.Artifact) { binary.LittleEndian.PutUint16(b[28:], 5) },
		"table_outside":          func(b []byte, _ *mediaboot.Artifact) { binary.LittleEndian.PutUint64(b[48:], 513) },
		"array_overflow":         func(b []byte, _ *mediaboot.Artifact) { binary.LittleEndian.PutUint64(b[48:], 511) },
		"missing_fragment_table": func(b []byte, _ *mediaboot.Artifact) { binary.LittleEndian.PutUint32(b[16:], 1) },
		"invalid_root_inode":     func(b []byte, _ *mediaboot.Artifact) { binary.LittleEndian.PutUint64(b[32:], 8192) },
		"unbounded_inodes":       func(b []byte, _ *mediaboot.Artifact) { binary.LittleEndian.PutUint32(b[4:], ^uint32(0)) },
	} {
		t.Run(name, func(t *testing.T) {
			b, a := rootFSHeaderFixture()
			change(b, &a)
			if name != "wrong_hash" {
				a.SHA256 = rawSHA(b)
			}
			if _, err := VerifyRootFSArchive(context.Background(), bytes.NewReader(b), a); err == nil {
				t.Fatal("invalid rootfs accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := VerifyRootFSArchive(ctx, bytes.NewReader(raw), artifact); err == nil {
		t.Fatal("canceled archive check accepted")
	}
}

type fixtureRootFiles map[string][]byte

func (s fixtureRootFiles) ReadFile(_ context.Context, path string, _ int64) ([]byte, error) {
	raw, ok := s[path]
	if !ok {
		return nil, errors.New("missing fixture file")
	}
	return bytes.Clone(raw), nil
}

func contentPlanFixture(t *testing.T, arch string) (mediaboot.Plan, []byte, fixtureRootFiles) {
	t.Helper()
	uki, target := targetPEFixture(t, arch)
	helper := fixtureELF(arch)
	p := mediaboot.Plan{RootID: strings.Repeat("1", 32), Profile: mediaboot.Profile{Schema: mediaboot.Schema, Installer: "cloud-init", KernelMediaID: strings.Repeat("2", 32), InitrdMediaID: strings.Repeat("3", 32), TemplateMediaID: strings.Repeat("4", 32), Recipe: &mediaboot.Recipe{Schema: mediaboot.TargetRecipeSchema, Protocol: mediaboot.InitrdProtocol, RootFSFormat: "squashfs", UEFIMediaID: strings.Repeat("5", 32), PreflightMediaID: strings.Repeat("6", 32), SSHHelperMediaID: strings.Repeat("7", 32), PreflightSHA256: rawSHA(helper), SSHHelperSHA256: rawSHA(helper), TargetBoot: target}}}
	for i, role := range []string{"rootfs", "kernel", "initrd", "template", "uefi", "preflight", "ssh-helper"} {
		a := arch
		if role == "template" {
			a = "noarch"
		}
		p.Artifacts = append(p.Artifacts, mediaboot.Artifact{Role: role, MediaID: strings.Repeat(string(rune('1'+i)), 32), ScopeKey: "global", SHA256: rawSHA(helper), Size: int64(len(helper)), Arch: a, ObjectVersion: "fixture-only"})
	}
	files := fixtureRootFiles{"usr/lib/os-release": []byte("ID=keel\nVERSION_ID=1\n"), "usr/lib/modules/6.12.0-keel/modules.dep": {}, SSHHelperPath: helper, "usr/local/libexec/keel-firstboot": helper, "usr/bin/cloud-init": []byte("#!/usr/bin/python3\n# format fixture, never run\n"), NoCloudPolicyPath: []byte(NoCloudPolicy)}
	return p, uki, files
}

func TestRootFSContentsBindsTargetKernelHelperAndLocalDataSource(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			p, uki, files := contentPlanFixture(t, arch)
			got, err := VerifyRootFSContents(context.Background(), p, bytes.NewReader(uki), files)
			if err != nil || got.Target.Arch != arch || got.NoCloudPolicySHA256 != rawSHA([]byte(NoCloudPolicy)) {
				t.Fatalf("rootfs contents: %#v %v", got, err)
			}
		})
	}
	for _, path := range []string{"usr/lib/os-release", "usr/lib/modules/6.12.0-keel/modules.dep", SSHHelperPath, "usr/local/libexec/keel-firstboot", "usr/bin/cloud-init", NoCloudPolicyPath} {
		t.Run("missing_"+path, func(t *testing.T) {
			p, uki, files := contentPlanFixture(t, "amd64")
			delete(files, path)
			if _, err := VerifyRootFSContents(context.Background(), p, bytes.NewReader(uki), files); err == nil {
				t.Fatal("missing required target component accepted")
			}
		})
	}
	for path, replacement := range map[string][]byte{"usr/lib/os-release": []byte("ID=other\n"), SSHHelperPath: fixtureELF("arm64"), NoCloudPolicyPath: []byte("datasource_list: [ Ec2, NoCloud ]\n")} {
		t.Run("wrong_"+path, func(t *testing.T) {
			p, uki, files := contentPlanFixture(t, "amd64")
			files[path] = replacement
			if _, err := VerifyRootFSContents(context.Background(), p, bytes.NewReader(uki), files); err == nil {
				t.Fatal("mismatched target component accepted")
			}
		})
	}
}
