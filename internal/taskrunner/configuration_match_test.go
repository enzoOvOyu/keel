package taskrunner_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"golang.org/x/crypto/ssh"
)

func TestSSHConfigurationMatchDoesNotResolveOrPromote(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	expiry := time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)
	vault := &provisionVaultFixture{signer: signer, expiry: expiry}
	pin := ssh.FingerprintSHA256(signer.PublicKey())
	key := taskrunner.InstallCredential{ID: strings.Repeat("b", 32), Ref: "approved-new-domain", Version: "v2", ExpiresAt: expiry, PublicKey: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))) + " keel-ctrl-" + projectID + "-domain"}
	cfg := taskrunner.TargetConfig{ProjectID: projectID, MachineID: machineID, Address: "192.0.2.10:22", User: "keel-ctrl", HostKey: pin, CredentialProvider: "vault", CredentialRef: key.Ref, CredentialVersion: key.Version, CredentialExpiresAt: &expiry}
	runner, err := taskrunner.LoadWithCredentials(writeProvisionOnlyConfig(t, cfg), vault)
	if err != nil {
		t.Fatal(err)
	}
	defer runner.Close()
	// Matching public configuration is distinct from runtime vault availability.
	vault.reject = true
	if !runner.MatchesDomainConfiguration(projectID, machineID, pin, key) {
		t.Fatal("public matching fetched a secret")
	}
	if runner.CheckActiveCredential(context.Background(), projectID, machineID, key) == nil {
		t.Fatal("runtime credential check bypassed current vault authority")
	}
	if runner.MatchesDomainConfiguration(projectID, strings.Repeat("0", 32), pin, key) || runner.MatchesDomainConfiguration(projectID, machineID, "SHA256:other", key) {
		t.Fatal("wrong target or pin matched")
	}
	for _, change := range []func(*taskrunner.InstallCredential){
		func(k *taskrunner.InstallCredential) { k.Ref = "other" },
		func(k *taskrunner.InstallCredential) { k.Version = "v3" },
		func(k *taskrunner.InstallCredential) { k.ExpiresAt = k.ExpiresAt.Add(time.Second) },
		func(k *taskrunner.InstallCredential) { k.PublicKey = "invalid" },
	} {
		bad := key
		change(&bad)
		if runner.MatchesDomainConfiguration(projectID, machineID, pin, bad) {
			t.Fatal("different domain credential matched")
		}
	}
	runner.Close()
	if runner.MatchesDomainConfiguration(projectID, machineID, pin, key) {
		t.Fatal("closed runner matched")
	}
	initial := taskrunner.TargetConfig{ProvisionOnly: true, ProjectID: projectID, MachineID: machineID, Address: cfg.Address, User: cfg.User}
	pending, err := taskrunner.LoadWithCredentials(writeProvisionOnlyConfig(t, initial), vault)
	if err != nil {
		t.Fatal(err)
	}
	defer pending.Close()
	if pending.MatchesDomainConfiguration(projectID, machineID, pin, key) {
		t.Fatal("provision-only coordinates promoted")
	}
	var absent *taskrunner.SSH
	if absent.MatchesDomainConfiguration(projectID, machineID, pin, key) {
		t.Fatal("absent runner matched")
	}
}
