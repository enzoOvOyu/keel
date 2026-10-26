package taskrunner_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/netip"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"golang.org/x/crypto/ssh"
)

type provisionVaultFixture struct {
	signer ssh.Signer
	expiry time.Time
	reject bool
}

func (v *provisionVaultFixture) Resolve(_ context.Context, project, ref, version string) (ssh.Signer, time.Time, error) {
	if v.reject || project != projectID || ref != "approved-new-domain" || version != "v2" {
		return nil, time.Time{}, taskrunner.ErrRejected
	}
	return v.signer, v.expiry, nil
}

// These are localhost protocol fixtures, never evidence of a real device boot.
func provisionSSHFixture(t *testing.T) (*sshFixture, *taskrunner.SSH, *provisionVaultFixture, taskrunner.ProvisionSSHPlan) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	f := fixtureWithPublicKey(t, signer.PublicKey())
	raw, err := os.ReadFile(f.configFile)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Targets []taskrunner.TargetConfig `json:"targets"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		t.Fatal("bad fixture config")
	}
	freshPin := cfg.Targets[0].HostKey
	// The configured OLD pin is deliberately different. The installer may use
	// only the independently authenticated first-boot pin for this one handshake.
	_, oldPrivate, _ := ed25519.GenerateKey(rand.Reader)
	oldSigner, _ := ssh.NewSignerFromKey(oldPrivate)
	cfg.Targets[0].HostKey = ssh.FingerprintSHA256(oldSigner.PublicKey())
	raw, _ = json.Marshal(cfg)
	if err = os.WriteFile(f.configFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	vault := &provisionVaultFixture{signer: signer, expiry: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
	runner, err := taskrunner.LoadWithCredentials(f.configFile, vault)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runner.Close)
	hostname, _ := os.Hostname()
	p := taskrunner.ProvisionSSHPlan{BindingSHA256: strings.Repeat("a", 64), Peer: netip.MustParseAddr("127.0.0.1"), Clients: netip.MustParsePrefix("127.0.0.0/8"), HostKey: freshPin, Hostname: hostname, Arch: runtime.GOARCH, Credential: taskrunner.InstallCredential{ID: strings.Repeat("b", 32), Ref: "approved-new-domain", Version: "v2", ExpiresAt: vault.expiry, PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " keel-ctrl-" + projectID + "-domain"}, ExpiresAt: time.Now().Add(20 * time.Second)}
	return f, runner, vault, p
}

func TestProvisionFirstSSHUsesNewAuthenticatedPinAndVaultKey(t *testing.T) {
	f, runner, vault, p := provisionSSHFixture(t)
	c := command()
	original, err := runner.Binding(projectID, machineID)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := runner.VerifyProvisionIdentity(context.Background(), c, p)
	if err != nil || proof.Validate(c, p, time.Now()) != nil {
		t.Fatal("fresh pinned identity failed", err)
	}
	if f.runs.Load() != 1 {
		t.Fatal("identity command replayed")
	}
	o := proof.Observation()
	o.Output["hostname"] = "tampered"
	if proof.Validate(c, p, time.Now()) != nil {
		t.Fatal("consumer mutated sealed proof")
	}
	next, err := runner.Binding(projectID, machineID)
	if err != nil || next != original || next.HostKey == p.HostKey {
		t.Fatal("ordinary endpoint silently promoted")
	}
	if (taskrunner.ProvisionSSHProof{}).Validate(c, p, time.Now()) == nil {
		t.Fatal("manufactured zero proof accepted")
	}
	changed := p
	changed.HostKey = original.HostKey
	if proof.Validate(c, changed, time.Now()) == nil {
		t.Fatal("proof reused for different host pin")
	}
	vault.reject = true
	if _, err = runner.VerifyProvisionIdentity(context.Background(), c, p); !errors.Is(err, taskrunner.ErrRejected) {
		t.Fatal("revoked vault key accepted", err)
	}
	if f.runs.Load() != 1 {
		t.Fatal("revocation reached helper")
	}
}

func TestProvisionFirstSSHRejectsWrongHostPeerAndLostReceipt(t *testing.T) {
	t.Run("wrong_peer_and_host_pin", func(t *testing.T) {
		f, runner, _, p := provisionSSHFixture(t)
		p.Peer = netip.MustParseAddr("127.0.0.2")
		if _, err := runner.VerifyProvisionIdentity(context.Background(), command(), p); !errors.Is(err, taskrunner.ErrRejected) {
			t.Fatal("peer mismatch accepted", err)
		}
		p.Peer = netip.MustParseAddr("127.0.0.1")
		old, _ := runner.Binding(projectID, machineID)
		p.HostKey = old.HostKey
		if _, err := runner.VerifyProvisionIdentity(context.Background(), command(), p); err == nil {
			t.Fatal("old pin connected to fresh host")
		}
		if f.runs.Load() != 0 {
			t.Fatal("wrong identity reached helper")
		}
	})
	t.Run("lost_receipt_no_run_replay", func(t *testing.T) {
		f, runner, _, p := provisionSSHFixture(t)
		f.drop.Store(true)
		if _, err := runner.VerifyProvisionIdentity(context.Background(), command(), p); !errors.Is(err, taskrunner.ErrUnknown) {
			t.Fatal("lost receipt trusted", err)
		}
		if f.runs.Load() != 1 {
			t.Fatal("ambiguous command automatically retried")
		}
		f.drop.Store(false)
		proof, err := runner.QueryProvisionIdentity(context.Background(), command(), p)
		if err != nil || proof.Validate(command(), p, time.Now()) != nil {
			t.Fatal("explicit query did not recover original receipt", err)
		}
		if f.runs.Load() != 1 {
			t.Fatal("query recovery replayed run")
		}
	})
}
