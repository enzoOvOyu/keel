package taskrunner_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func loadConfigured(t *testing.T, f *sshFixture, change func(*taskrunner.TargetConfig)) (*taskrunner.SSH, error) {
	t.Helper()
	b, e := os.ReadFile(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	var cfg map[string][]taskrunner.TargetConfig
	if e = json.Unmarshal(b, &cfg); e != nil {
		t.Fatal(e)
	}
	change(&cfg["targets"][0])
	b, e = json.Marshal(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(f.configFile, b, 0600); e != nil {
		t.Fatal(e)
	}
	return taskrunner.Load(f.configFile)
}

func TestSSHCredentialVersionAndExpirationBindSnapshots(t *testing.T) {
	f := fixture(t)
	original, e := f.runner.Binding(projectID, machineID)
	if e != nil {
		t.Fatal(e)
	}
	expiry := time.Now().Add(time.Minute).UTC()
	first, e := loadConfigured(t, f, func(c *taskrunner.TargetConfig) { c.CredentialVersion = "v1"; c.CredentialExpiresAt = &expiry })
	if e != nil {
		t.Fatal(e)
	}
	defer first.Close()
	binding, e := first.Binding(projectID, machineID)
	if e != nil || binding.ConfigHash == original.ConfigHash {
		t.Fatal("credential metadata was not bound", e)
	}
	metadata, e := first.CredentialMetadata(projectID, machineID)
	if e != nil || metadata.Ref != "test-project-rail-key" || metadata.Version != "v1" || metadata.ExpiresAt == nil || !metadata.ExpiresAt.Equal(expiry.Truncate(time.Microsecond)) || metadata.ExpiresAt.Location() != time.UTC {
		t.Fatal("metadata mismatch", e)
	}
	// Changing the returned metadata must not mutate operator configuration.
	*metadata.ExpiresAt = time.Now().Add(-time.Hour)
	if _, e = first.Binding(projectID, machineID); e != nil {
		t.Fatal("metadata mutated internal expiry", e)
	}
	raw, _ := json.Marshal(metadata)
	if strings.Contains(string(raw), "private_key") || strings.Contains(string(raw), f.listener.Addr().String()) {
		t.Fatal("secret or coordinates exposed")
	}
	second, e := loadConfigured(t, f, func(c *taskrunner.TargetConfig) { c.CredentialVersion = "v2" })
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	changed, e := second.Binding(projectID, machineID)
	if e != nil || changed.ConfigHash == binding.ConfigHash {
		t.Fatal("version rotation did not change binding", e)
	}
	third, e := loadConfigured(t, f, func(c *taskrunner.TargetConfig) { next := expiry.Add(time.Minute); c.CredentialExpiresAt = &next })
	if e != nil {
		t.Fatal(e)
	}
	defer third.Close()
	next, e := third.Binding(projectID, machineID)
	if e != nil || next.ConfigHash == changed.ConfigHash {
		t.Fatal("expiry rotation did not change binding", e)
	}
	// Sub-microsecond serialization differences must not invalidate the same
	// instant after it is frozen in a timestamptz authorization record.
	fourth, e := loadConfigured(t, f, func(c *taskrunner.TargetConfig) {
		normalized := c.CredentialExpiresAt.UTC().Truncate(time.Microsecond)
		c.CredentialExpiresAt = &normalized
	})
	if e != nil {
		t.Fatal(e)
	}
	defer fourth.Close()
	equivalent, e := fourth.Binding(projectID, machineID)
	if e != nil || equivalent != next {
		t.Fatal("normalized credential expiry changed snapshot binding", e)
	}
	first.Close()
	if _, e = first.CredentialMetadata(projectID, machineID); !errors.Is(e, taskrunner.ErrRejected) {
		t.Fatal("closed runner returned metadata", e)
	}
	if !first.HasTarget(projectID, machineID) || first.HasTarget(machineID, projectID) || (*taskrunner.SSH)(nil).HasTarget(projectID, machineID) {
		t.Fatal("configured target identity changed with connection lifecycle")
	}
}

func TestSSHExpiredCredentialDropsPooledConnectionAndRejectsActions(t *testing.T) {
	f := fixture(t)
	expiry := time.Now().Add(300 * time.Millisecond)
	r, e := loadConfigured(t, f, func(c *taskrunner.TargetConfig) { c.CredentialVersion = "v1"; c.CredentialExpiresAt = &expiry })
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	c := command()
	c.Operation = "trust.verify"
	o, e := r.Do(context.Background(), taskrunner.Request{Action: "run", Command: c})
	if e != nil || o.State != "success" || o.Command.Operation != "trust.verify" || o.Output["hostname"] == "" {
		t.Fatal("restricted verification failed", e)
	}
	before := f.runs.Load()
	time.Sleep(time.Until(expiry) + 10*time.Millisecond)
	if _, e = r.Binding(projectID, machineID); !errors.Is(e, taskrunner.ErrRejected) {
		t.Fatal("expired binding accepted", e)
	}
	if _, e = r.CredentialMetadata(projectID, machineID); !errors.Is(e, taskrunner.ErrRejected) {
		t.Fatal("expired metadata accepted", e)
	}
	if !r.HasTarget(projectID, machineID) {
		t.Fatal("expired real target became eligible for mock trust actions")
	}
	for _, action := range []string{"run", "query", "cancel"} {
		if _, e = r.Do(context.Background(), taskrunner.Request{Action: action, Command: c}); !errors.Is(e, taskrunner.ErrRejected) {
			t.Fatal("expired action accepted", action, e)
		}
	}
	if f.runs.Load() != before {
		t.Fatal("expired credentials reached helper")
	}
	deadline := time.Now().Add(time.Second)
	for f.closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if f.closed.Load() == 0 {
		t.Fatal("expired pooled SSH connection remained open")
	}
}

func TestSSHCredentialExpiryBoundsInFlightRequest(t *testing.T) {
	f := fixture(t)
	expiry := time.Now().Add(250 * time.Millisecond)
	r, e := loadConfigured(t, f, func(c *taskrunner.TargetConfig) { c.CredentialVersion = "v1"; c.CredentialExpiresAt = &expiry })
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	c := command()
	c.Operation = "host.wait"
	c.WaitMS = 2000
	c.TimeoutMS = 2500
	started := time.Now()
	if _, e = r.Do(context.Background(), taskrunner.Request{Action: "run", Command: c}); !errors.Is(e, taskrunner.ErrUnknown) {
		t.Fatal("expiry fabricated terminal result", e)
	}
	if time.Since(started) > time.Second {
		t.Fatal("in-flight request outlived credential deadline")
	}
	if _, e = r.Do(context.Background(), taskrunner.Request{Action: "query", Command: c}); !errors.Is(e, taskrunner.ErrRejected) {
		t.Fatal("expired credential queried target", e)
	}
}

func TestSSHCredentialConfigurationRejectsInvalidMetadata(t *testing.T) {
	for _, version := range []string{" space", "a/b", "a\nsecret", strings.Repeat("x", 65)} {
		t.Run("version-"+strings.ReplaceAll(version, "/", "_"), func(t *testing.T) {
			f := fixture(t)
			r, e := loadConfigured(t, f, func(c *taskrunner.TargetConfig) { c.CredentialVersion = version })
			if e == nil {
				r.Close()
				t.Fatal("invalid credential version accepted")
			}
		})
	}
	for _, expiry := range []time.Time{{}, time.Now().Add(-time.Second)} {
		f := fixture(t)
		r, e := loadConfigured(t, f, func(c *taskrunner.TargetConfig) { c.CredentialVersion = "v1"; c.CredentialExpiresAt = &expiry })
		if e == nil {
			r.Close()
			t.Fatal("invalid expiry accepted")
		}
	}
	f := fixture(t)
	r, e := loadConfigured(t, f, func(c *taskrunner.TargetConfig) {
		expiry := time.Now().Add(time.Minute)
		c.CredentialExpiresAt = &expiry
	})
	if e == nil {
		r.Close()
		t.Fatal("expiring credential missing version accepted")
	}
}

func TestWrapperTrustVerificationIsReadOnlyAndBounded(t *testing.T) {
	f := fixture(t)
	c := command()
	c.Operation = "trust.verify"
	c.WaitMS = 1
	if c.Validate() == nil {
		t.Fatal("verification accepted wait parameter")
	}
	c.WaitMS = 0
	if _, e := f.runner.Do(context.Background(), taskrunner.Request{Action: "run", Command: c}); e != nil {
		t.Fatal(e)
	}
	for _, op := range []string{"trust.bootstrap", "trust.repair", "trust.install", "trust.revoke", "trust.verify;id"} {
		c.Operation = op
		if c.Validate() == nil {
			t.Fatal("mutating trust operation accepted by helper", op)
		}
	}
}

func TestSSHHostKeyFingerprintIsCanonical(t *testing.T) {
	valid := "SHA256:" + strings.Repeat("A", 43)
	if !taskrunner.IsValidHostKey(valid) {
		t.Fatal("canonical SHA256 pin rejected")
	}
	for _, pin := range []string{"", "sha256:" + strings.Repeat("A", 43), valid + "=", "SHA256:" + strings.Repeat("A", 42) + "B", "SHA256:" + strings.Repeat("_", 43), valid + "\n"} {
		if taskrunner.IsValidHostKey(pin) {
			t.Fatal("noncanonical host-key fingerprint accepted")
		}
	}
}

func TestSSHCredentialExpiryDoesNotEvictAnotherTarget(t *testing.T) {
	f := fixture(t)
	b, e := os.ReadFile(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	var cfg map[string][]taskrunner.TargetConfig
	if e = json.Unmarshal(b, &cfg); e != nil {
		t.Fatal(e)
	}
	expiry := time.Now().Add(400 * time.Millisecond)
	cfg["targets"][0].CredentialVersion = "temporary-v1"
	cfg["targets"][0].CredentialExpiresAt = &expiry
	other := cfg["targets"][0]
	other.MachineID = "55555555555555555555555555555555"
	other.CredentialVersion = "domain-v1"
	other.CredentialExpiresAt = nil
	cfg["targets"] = append(cfg["targets"], other)
	b, _ = json.Marshal(cfg)
	if e = os.WriteFile(f.configFile, b, 0600); e != nil {
		t.Fatal(e)
	}
	r, e := taskrunner.Load(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	first := command()
	if _, e = r.Do(context.Background(), taskrunner.Request{Action: "run", Command: first}); e != nil {
		t.Fatal(e)
	}
	second := command()
	second.MachineID = other.MachineID
	second.CommandID = "66666666666666666666666666666666"
	if _, e = r.Do(context.Background(), taskrunner.Request{Action: "run", Command: second}); e != nil {
		t.Fatal(e)
	}
	f.mu.Lock()
	before := len(f.connections)
	f.mu.Unlock()
	time.Sleep(time.Until(expiry) + 10*time.Millisecond)
	if _, e = r.Binding(projectID, machineID); !errors.Is(e, taskrunner.ErrRejected) {
		t.Fatal("expired target remained usable", e)
	}
	if _, e = r.Do(context.Background(), taskrunner.Request{Action: "query", Command: second}); e != nil {
		t.Fatal("unrelated live target evicted", e)
	}
	f.mu.Lock()
	after := len(f.connections)
	f.mu.Unlock()
	if before != 2 || after != before {
		t.Fatal("unrelated target connection was replaced", before, after)
	}
}
