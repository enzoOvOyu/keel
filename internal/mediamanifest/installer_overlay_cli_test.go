package mediamanifest

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
)

// Explicit milestone opt-in: runs the real local admin CLI against separately
// built static helper bytes. No helper is executed, no DB/network is used, and
// all manifests/keys/outputs are owned disposable software-test fixtures.
func TestInstallerOverlayRealHelperCLI(t *testing.T) {
	admin := os.Getenv("KEEL_TEST_PROVISION_ADMIN_BIN")
	preflight := os.Getenv("KEEL_TEST_PROVISION_PREFLIGHT_BIN")
	ssh := os.Getenv("KEEL_TEST_PROVISION_SSH_HELPER_BIN")
	if admin == "" || preflight == "" || ssh == "" {
		t.Skip("set all three KEEL_TEST_PROVISION_*_BIN paths for the offline CLI milestone")
	}
	signer, key, payload := fixture(t)
	base := payload.Artifacts[0]
	payload.Artifacts = nil
	directory := t.TempDir()
	arch := runtime.GOARCH
	helpers := mediaboot.HelperSet{Schema: mediaboot.HelperSetSchema, Arch: arch, PreflightMediaID: strings.Repeat("2", 32), SSHHelperMediaID: strings.Repeat("3", 32)}
	for i, source := range []string{preflight, ssh} {
		file, err := os.Open(source)
		if err != nil {
			t.Fatal(err)
		}
		id := []string{helpers.PreflightMediaID, helpers.SSHHelperMediaID}[i]
		output, err := os.OpenFile(filepath.Join(directory, id+".bin"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		hash := sha256.New()
		size, err := io.Copy(io.MultiWriter(output, hash), file)
		file.Close()
		output.Close()
		if err != nil {
			t.Fatal(err)
		}
		digest := hex.EncodeToString(hash.Sum(nil))
		if i == 0 {
			helpers.PreflightSHA256 = digest
		} else {
			helpers.SSHHelperSHA256 = digest
		}
		node := base
		node.MediaID = id
		node.Type = "os"
		node.Arch = arch
		node.Name = []string{"preflight", "ssh-helper"}[i]
		node.URI = "media://os/" + node.Name + "/v1"
		node.SHA256 = digest
		node.Size = size
		payload.Artifacts = append(payload.Artifacts, node)
	}
	config, _ := json.Marshal(helpers)
	hash := sha256.Sum256(config)
	root := base
	root.MediaID = strings.Repeat("4", 32)
	root.Type = "config"
	root.Arch = arch
	root.Name = "independent-helpers"
	root.URI = "media://config/independent-helpers/v1"
	root.SHA256 = hex.EncodeToString(hash[:])
	root.Size = int64(len(config))
	root.Dependencies = []Dependency{}
	for _, node := range payload.Artifacts {
		root.Dependencies = append(root.Dependencies, Dependency{MediaID: node.MediaID, SHA256: node.SHA256})
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
	raw, _ := json.Marshal(signed)
	manifestFile := filepath.Join(directory, "manifest.json")
	if err := os.WriteFile(manifestFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	writePublic := func(key ed25519.PublicKey, name string) string {
		der, err := x509.MarshalPKIXPublicKey(key)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	public := writePublic(key, "trusted.pem")
	var environment []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "KEEL_") {
			environment = append(environment, entry)
		}
	}
	environment = append(environment, "KEEL_DATABASE_URL=must-not-be-opened")
	run := func(arguments ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, admin, arguments...)
		command.Env = environment
		return command.CombinedOutput()
	}
	output := filepath.Join(directory, "helpers.cpio.gz")
	create := []string{"provision-overlay-create", "--manifest-file", manifestFile, "--public-key-file", public, "--artifact-directory", directory, "--output-file", output}
	created, err := run(create...)
	if err != nil {
		t.Fatalf("real create CLI: %v: %s", err, created)
	}
	var result struct {
		Status    string `json:"status"`
		SHA256    string `json:"sha256"`
		Size      int64  `json:"size"`
		Verified  bool   `json:"helper_bytes_verified"`
		Installed bool   `json:"installation_performed"`
	}
	if json.Unmarshal(created, &result) != nil || result.Status != "ok" || !result.Verified || result.Installed || result.Size < 1 {
		t.Fatalf("incorrect create proof %s", created)
	}
	verified, err := run("provision-overlay-verify", "--overlay-file", output, "--public-key-file", public)
	if err != nil {
		t.Fatalf("real verify CLI: %v: %s", err, verified)
	}
	var checked struct {
		SHA256 string `json:"sha256"`
		Size   int64  `json:"size"`
	}
	if json.Unmarshal(verified, &checked) != nil || checked.SHA256 != result.SHA256 || checked.Size != result.Size {
		t.Fatalf("CLI identity mismatch %s", verified)
	}
	if response, err := run(create...); err == nil {
		t.Fatalf("existing output overwritten: %s", response)
	}
	_, other, _ := fixture(t)
	wrong := writePublic(other, "untrusted.pem")
	if response, err := run("provision-overlay-verify", "--overlay-file", output, "--public-key-file", wrong); err == nil {
		t.Fatalf("self-supplied trust accepted: %s", response)
	}
	t.Log("Real admin create/verify used exact static helper binaries; no DB, hardware probe, initrd boot or installation occurred")
}
