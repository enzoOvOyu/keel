package taskrunner_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"golang.org/x/crypto/ssh"
)

// Localhost restricted-helper protocol fixture, never a hardware boot proof.
func TestProvisionKeyRepairFreshDomainHandshake(t *testing.T) {
	f := fixture(t)
	keysDir := filepath.Join(t.TempDir(), "ssh")
	if err := os.Mkdir(keysDir, 0700); err != nil {
		t.Fatal(err)
	}
	keysFile := filepath.Join(keysDir, "authorized_keys")
	if err := os.WriteFile(keysFile, []byte("# customer-owned entries are preserved\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.installedKeysFile = keysFile
	f.mu.Unlock()
	var cfg struct {
		Targets []taskrunner.TargetConfig `json:"targets"`
	}
	raw, err := os.ReadFile(f.configFile)
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal("fixture configuration")
	}
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	cfg.Targets[0].CredentialVersion = "bootstrap-v1"
	cfg.Targets[0].CredentialExpiresAt = &expiry
	raw, _ = json.Marshal(cfg)
	if err = os.WriteFile(f.configFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	vault := &provisionVaultFixture{signer: signer, expiry: expiry}
	runner, err := taskrunner.LoadWithCredentials(f.configFile, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	hostname, _ := os.Hostname()
	plan := taskrunner.ProvisionSSHPlan{BindingSHA256: strings.Repeat("a", 64), Peer: netip.MustParseAddr("127.0.0.1"), Clients: netip.MustParsePrefix("127.0.0.0/8"), HostKey: cfg.Targets[0].HostKey, Hostname: hostname, Arch: runtime.GOARCH, Credential: taskrunner.InstallCredential{ID: strings.Repeat("b", 32), Ref: "approved-new-domain", Version: "v2", ExpiresAt: expiry, PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " keel-ctrl-" + projectID + "-domain"}, ExpiresAt: time.Now().Add(20 * time.Second)}
	bootstrap, err := runner.ProvisionRepairBootstrap(projectID, machineID, plan)
	if err != nil {
		t.Fatal(err)
	}
	c := command()
	c.Operation = "trust.install"
	c.PublicKey = plan.Credential.PublicKey
	c.AuthorizationExpiresAt = plan.ExpiresAt.UnixMicro()
	proof, err := runner.RepairProvisionKey(context.Background(), c, plan, bootstrap, "run")
	if err != nil || proof.Validate(c, plan, time.Now()) != nil {
		t.Fatal("fresh-key receipt proof", err)
	}
	if f.runs.Load() != 1 || f.installedAuth.Load() == 0 {
		t.Fatal("missing single run or new-key handshake")
	}
	copied := proof.Observation()
	copied.Output["key_installed"] = false
	if proof.Validate(c, plan, time.Now()) != nil {
		t.Fatal("public observation mutated opaque proof")
	}
	before := f.runs.Load()
	recovered, err := runner.RepairProvisionKey(context.Background(), c, plan, bootstrap, "query")
	if err != nil || recovered.Validate(c, plan, time.Now()) != nil || f.runs.Load() != before {
		t.Fatal("query replayed mutation", err)
	}
	wrong := plan
	wrong.Credential.Version = "v3"
	if proof.Validate(c, wrong, time.Now()) == nil {
		t.Fatal("changed credential accepted")
	}
}

func TestProvisionKeyRepairRejectsImplicitBootstrap(t *testing.T) {
	_, runner, _, plan := provisionSSHFixtureWithTarget(t, true)
	if _, err := runner.ProvisionRepairBootstrap(projectID, machineID, plan); err == nil {
		t.Fatal("coordinates fabricated bootstrap authority")
	}
	c := command()
	c.Operation = "trust.install"
	c.PublicKey = plan.Credential.PublicKey
	c.AuthorizationExpiresAt = plan.ExpiresAt.UnixMicro()
	if (taskrunner.ProvisionKeyRepairProof{}).Validate(c, plan, time.Now()) == nil {
		t.Fatal("zero proof accepted")
	}
	if _, err := runner.RepairProvisionKey(context.Background(), c, plan, taskrunner.ProvisionBootstrap{}, "retry"); err == nil {
		t.Fatal("implicit mutation retry accepted")
	}
}
