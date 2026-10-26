package taskrunner_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func writeProvisionOnlyConfig(t *testing.T, targets ...taskrunner.TargetConfig) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"targets": targets})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "targets.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestProvisionOnlyAllowlistRequiresNoOldCredentialAndRejectsAmbiguity(t *testing.T) {
	base := taskrunner.TargetConfig{ProvisionOnly: true, ProjectID: projectID, MachineID: machineID, Address: "192.0.2.10:22", User: "keel-ctrl"}
	// A configured but currently unavailable vault must not prevent startup of
	// a coordinates-only entry. No credential is selected until approved work.
	unavailable := &provisionVaultFixture{reject: true}
	file := writeProvisionOnlyConfig(t, base)
	runner, err := taskrunner.LoadWithCredentials(file, unavailable)
	if err != nil {
		t.Fatal("initial target required a fabricated previous credential", err)
	}
	runner.Close()
	if _, err = taskrunner.Load(file); !errors.Is(err, taskrunner.ErrRejected) {
		t.Fatal("provision target without vault provider accepted", err)
	}
	cases := map[string]func(*taskrunner.TargetConfig){
		"old_pin":           func(c *taskrunner.TargetConfig) { c.HostKey = "SHA256:previous" },
		"file_key":          func(c *taskrunner.TargetConfig) { c.PrivateKeyFile = "/absent/old-key" },
		"key_ref":           func(c *taskrunner.TargetConfig) { c.CredentialRef = "old" },
		"key_version":       func(c *taskrunner.TargetConfig) { c.CredentialVersion = "v1" },
		"key_expiry":        func(c *taskrunner.TargetConfig) { until := time.Now().Add(time.Hour); c.CredentialExpiresAt = &until },
		"provider":          func(c *taskrunner.TargetConfig) { c.CredentialProvider = "vault" },
		"dns":               func(c *taskrunner.TargetConfig) { c.Address = "device.example:22" },
		"root":              func(c *taskrunner.TargetConfig) { c.User = "root" },
		"port":              func(c *taskrunner.TargetConfig) { c.Address = "192.0.2.10:0" },
		"implicit_ordinary": func(c *taskrunner.TargetConfig) { c.ProvisionOnly = false },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := base
			change(&cfg)
			r, err := taskrunner.LoadWithCredentials(writeProvisionOnlyConfig(t, cfg), unavailable)
			if r != nil {
				r.Close()
			}
			if !errors.Is(err, taskrunner.ErrRejected) {
				t.Fatal("ambiguous or unbounded target admitted", err)
			}
		})
	}
	if r, err := taskrunner.LoadWithCredentials(writeProvisionOnlyConfig(t, base, base), unavailable); !errors.Is(err, taskrunner.ErrRejected) {
		if r != nil {
			r.Close()
		}
		t.Fatal("duplicate initial endpoint admitted", err)
	}
}

func TestProvisionOnlyFirstSSHNeverPromotesOrdinaryEndpoint(t *testing.T) {
	f, runner, vault, p := provisionSSHFixtureWithTarget(t, true)
	c := command()
	rejectOrdinary := func() {
		t.Helper()
		if !runner.HasTarget(projectID, machineID) {
			t.Fatal("real target disappeared, permitting a mock fallback")
		}
		if _, err := runner.Binding(projectID, machineID); !errors.Is(err, taskrunner.ErrRejected) {
			t.Fatal("ordinary binding admitted", err)
		}
		if _, err := runner.CredentialMetadata(projectID, machineID); !errors.Is(err, taskrunner.ErrRejected) {
			t.Fatal("ordinary metadata admitted", err)
		}
		if err := runner.CheckActiveCredential(context.Background(), projectID, machineID, p.Credential); !errors.Is(err, taskrunner.ErrRejected) {
			t.Fatal("ordinary credential admitted", err)
		}
		if err := runner.CheckInstallCredential(projectID, machineID, p.Credential); !errors.Is(err, taskrunner.ErrRejected) {
			t.Fatal("old-key install endpoint admitted", err)
		}
		for _, action := range []string{"run", "query", "cancel"} {
			if _, err := runner.Do(context.Background(), taskrunner.Request{Action: action, Command: c}); !errors.Is(err, taskrunner.ErrRejected) {
				t.Fatal("ordinary action admitted", action, err)
			}
		}
	}
	rejectOrdinary()
	proof, err := runner.VerifyProvisionIdentity(context.Background(), c, p)
	if err != nil || proof.Validate(c, p, time.Now()) != nil {
		t.Fatal("first handshake without old pin failed", err)
	}
	rejectOrdinary()
	recovered, err := runner.QueryProvisionIdentity(context.Background(), c, p)
	if err != nil || recovered.Validate(c, p, time.Now()) != nil {
		t.Fatal("explicit original receipt query failed", err)
	}
	if f.runs.Load() != 1 {
		t.Fatal("ordinary or recovery action replayed work")
	}
	rejectOrdinary()
	vault.reject = true
	if _, err = runner.QueryProvisionIdentity(context.Background(), c, p); !errors.Is(err, taskrunner.ErrRejected) {
		t.Fatal("revocation did not block fresh recovery", err)
	}
	vault.reject = false
	wrong := p
	wrong.Peer = wrong.Peer.Next()
	if _, err = runner.VerifyProvisionIdentity(context.Background(), c, wrong); !errors.Is(err, taskrunner.ErrRejected) {
		t.Fatal("report changed allowlisted peer", err)
	}
	wrong = p
	wrong.HostKey = "SHA256:invalid"
	if _, err = runner.VerifyProvisionIdentity(context.Background(), c, wrong); !errors.Is(err, taskrunner.ErrRejected) {
		t.Fatal("untrusted pin admitted", err)
	}
	runner.Close()
	if _, err = runner.QueryProvisionIdentity(context.Background(), c, p); !errors.Is(err, taskrunner.ErrRejected) {
		t.Fatal("closed runner admitted provision handshake", err)
	}
}
