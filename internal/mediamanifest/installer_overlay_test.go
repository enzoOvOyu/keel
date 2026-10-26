package mediamanifest

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

// Software ELF format fixture only. It is never executed or called a real helper.
func overlayExecutableFixture() []byte {
	raw := make([]byte, 128)
	copy(raw, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	binary.LittleEndian.PutUint16(raw[16:], 2)
	binary.LittleEndian.PutUint16(raw[18:], 62)
	binary.LittleEndian.PutUint32(raw[20:], 1)
	binary.LittleEndian.PutUint64(raw[24:], 0x400078)
	binary.LittleEndian.PutUint64(raw[32:], 64)
	binary.LittleEndian.PutUint16(raw[52:], 64)
	binary.LittleEndian.PutUint16(raw[54:], 56)
	binary.LittleEndian.PutUint16(raw[56:], 1)
	binary.LittleEndian.PutUint32(raw[64:], 1)
	binary.LittleEndian.PutUint32(raw[68:], 5)
	binary.LittleEndian.PutUint64(raw[80:], 0x400000)
	binary.LittleEndian.PutUint64(raw[96:], uint64(len(raw)))
	binary.LittleEndian.PutUint64(raw[104:], uint64(len(raw)))
	return raw
}
func signedOverlayFixture(t *testing.T) ([]byte, ed25519.PublicKey, string) {
	t.Helper()
	signer, key, payload := fixture(t)
	base := payload.Artifacts[0]
	payload.Artifacts = nil
	binaryBytes := overlayExecutableFixture()
	sum := sha256.Sum256(binaryBytes)
	digest := hex.EncodeToString(sum[:])
	directory := t.TempDir()
	helpers := mediaboot.HelperSet{Schema: mediaboot.HelperSetSchema, Arch: "amd64", PreflightMediaID: strings.Repeat("2", 32), SSHHelperMediaID: strings.Repeat("3", 32), PreflightSHA256: digest, SSHHelperSHA256: digest}
	for i, name := range []string{"preflight", "ssh-helper"} {
		node := base
		node.MediaID = []string{helpers.PreflightMediaID, helpers.SSHHelperMediaID}[i]
		node.Type = "os"
		node.Arch = "amd64"
		node.Name = name
		node.SHA256 = digest
		node.Size = int64(len(binaryBytes))
		node.URI = "media://os/" + name + "/v1"
		payload.Artifacts = append(payload.Artifacts, node)
		if err := os.WriteFile(filepath.Join(directory, node.MediaID+".bin"), binaryBytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	config, _ := json.Marshal(helpers)
	configHash := sha256.Sum256(config)
	root := base
	root.MediaID = strings.Repeat("4", 32)
	root.Type = "config"
	root.Arch = "amd64"
	root.Name = "independent-installer-helpers"
	root.URI = "media://config/" + root.Name + "/v1"
	root.Size = int64(len(config))
	root.SHA256 = hex.EncodeToString(configHash[:])
	root.Dependencies = []Dependency{}
	for _, child := range payload.Artifacts {
		root.Dependencies = append(root.Dependencies, Dependency{MediaID: child.MediaID, SHA256: child.SHA256})
	}
	payload.Artifacts = append(payload.Artifacts, root)
	payload.RootID = root.MediaID
	if err := os.WriteFile(filepath.Join(directory, root.MediaID+".bin"), config, 0600); err != nil {
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
func TestInstallerOverlayTrustedMetadataAndExactPrivateOutput(t *testing.T) {
	raw, key, directory := signedOverlayFixture(t)
	output := filepath.Join(t.TempDir(), "helpers.cpio.gz")
	result, err := CreateInstallerOverlay(context.Background(), raw, key, directory, output)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := VerifyInstallerOverlay(context.Background(), output, key)
	if err != nil || verified.SHA256 != result.SHA256 || verified.Size != result.Size {
		t.Fatal("trusted overlay did not verify", err)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	if result.SHA256 != hex.EncodeToString(sum[:]) || result.Size != int64(len(content)) {
		t.Fatal("incorrect output identity")
	}
	info, _ := os.Stat(output)
	if info.Mode().Perm() != 0600 {
		t.Fatal("output not private")
	}
	output2 := filepath.Join(filepath.Dir(output), "same.cpio.gz")
	if _, err := CreateInstallerOverlay(context.Background(), raw, key, directory, output2); err != nil {
		t.Fatal(err)
	}
	content2, _ := os.ReadFile(output2)
	if !bytes.Equal(content, content2) {
		t.Fatal("same signed inputs produced different overlay")
	}
	before := append([]byte{}, content...)
	if _, err := CreateInstallerOverlay(context.Background(), raw, key, directory, output); err == nil {
		t.Fatal("replaced existing output")
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(before, after) {
		t.Fatal("existing output changed")
	}
}
func TestInstallerOverlayRejectsCorruptSymlinkAndUntrustedInputs(t *testing.T) {
	for _, scenario := range []string{"corrupt-config", "corrupt-helper", "symlink-helper", "wrong-key", "symlink-output", "writable-output-directory"} {
		t.Run(scenario, func(t *testing.T) {
			raw, key, directory := signedOverlayFixture(t)
			parent := t.TempDir()
			output := filepath.Join(parent, "out.cpio.gz")
			helper := filepath.Join(directory, strings.Repeat("2", 32)+".bin")
			switch scenario {
			case "corrupt-config":
				os.WriteFile(filepath.Join(directory, strings.Repeat("4", 32)+".bin"), []byte("bad config"), 0600)
			case "corrupt-helper":
				os.WriteFile(helper, []byte("bad bytes"), 0600)
			case "symlink-helper":
				target := filepath.Join(directory, "target.bin")
				os.Rename(helper, target)
				os.Symlink(target, helper)
			case "wrong-key":
				_, key, _ = fixture(t)
			case "symlink-output":
				target := filepath.Join(parent, "preserved")
				os.WriteFile(target, []byte("preserve"), 0600)
				os.Symlink(target, output)
			case "writable-output-directory":
				os.Chmod(parent, 0770)
				defer os.Chmod(parent, 0700)
			}
			if _, err := CreateInstallerOverlay(context.Background(), raw, key, directory, output); err == nil {
				t.Fatal("unsafe output accepted")
			}
			if scenario != "symlink-output" {
				if _, err := os.Lstat(output); !os.IsNotExist(err) {
					t.Fatal("partial output published")
				}
			} else {
				value, _ := os.ReadFile(output)
				if string(value) != "preserve" {
					t.Fatal("symlink target overwritten")
				}
			}
			entries, _ := filepath.Glob(filepath.Join(parent, ".keel-installer-overlay-*"))
			if len(entries) != 0 {
				t.Fatal("private staging file leaked")
			}
		})
	}
}

func TestInstallerOverlayVerificationRequiresIndependentTrust(t *testing.T) {
	raw, key, directory := signedOverlayFixture(t)
	output := filepath.Join(t.TempDir(), "out.cpio.gz")
	if _, err := CreateInstallerOverlay(context.Background(), raw, key, directory, output); err != nil {
		t.Fatal(err)
	}
	_, other, _ := fixture(t)
	if _, err := VerifyInstallerOverlay(context.Background(), output, other); err == nil {
		t.Fatal("overlay supplied its own signing authority")
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	for _, changed := range [][]byte{content[:len(content)-1], append(append([]byte{}, content...), content...)} {
		path := filepath.Join(t.TempDir(), "bad.cpio.gz")
		os.WriteFile(path, changed, 0600)
		if _, err := VerifyInstallerOverlay(context.Background(), path, key); err == nil {
			t.Fatal("accepted truncated/concatenated overlay")
		}
	}
}
