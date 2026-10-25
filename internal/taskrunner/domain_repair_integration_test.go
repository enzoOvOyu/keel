package taskrunner_test

import (
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// Start with actual installation/new-key proof, then explicitly inject a broken
// trust state. No seed or copied credential label substitutes for key proof.
func TestSSHInstalledDomainReadOnlyRepairBoundaries(t *testing.T) {
	for _, outcome := range []string{"success", "rebound_before_dispatch", "revoked_after_remote_proof"} {
		t.Run(outcome, func(t *testing.T) {
			f, key, _, master := keyInstallAdmission(t)
			installed := approvedInstall(t, f, key)
			x := f.execute(installed, f.creator)
			if err := f.service.RunClaim(f.ctx, f.claim(x.ID)); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.DB.ExecContext(f.ctx, "UPDATE machines SET ssh_trust='broken' WHERE id=$1", f.machine); err != nil {
				t.Fatal(err)
			}
			var config map[string][]taskrunner.TargetConfig
			raw, err := os.ReadFile(f.ssh.configFile)
			if err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(raw, &config); err != nil {
				t.Fatal(err)
			}
			target := &config["targets"][0]
			temporaryFile := target.PrivateKeyFile
			target.CredentialRef, target.CredentialVersion, target.CredentialExpiresAt = key.Ref, key.Version, &key.ExpiresAt
			configure := func(vault bool) {
				t.Helper()
				target.CredentialProvider, target.PrivateKeyFile = "", temporaryFile
				if vault {
					target.CredentialProvider, target.PrivateKeyFile = "vault", ""
				}
				data, e := json.Marshal(config)
				if e != nil {
					t.Fatal(e)
				}
				if e = os.WriteFile(f.ssh.configFile, data, 0600); e != nil {
					t.Fatal(e)
				}
				if e = f.store.ConfigureSSHRunner(f.ssh.configFile, master); e != nil {
					t.Fatal(e)
				}
				f.ssh.runner, f.service.SSHRunner = f.store.SSHRunner, f.store.SSHRunner
			}
			requireBindingDenial := func(e error) {
				t.Helper()
				var problem *platform.Error
				if !errors.As(e, &problem) || problem.Code != "SSH_DOMAIN_BINDING_REQUIRED" {
					t.Fatalf("expected domain binding refusal, got %v", e)
				}
			}
			create := func() (*platform.SSHTrustRequest, error) {
				return f.store.CreateSSHTrustRequest(f.ctx, f.creator, f.project, platform.CreateSSHTrustRequestInput{MachineID: f.machine, Action: "trust.repair", ExpectedAssetVersion: 1, HostKey: f.pin, Reason: "repair after explicit broken-trust fault using the previously proven domain key", ExpiresInSeconds: 900}, platform.RandomID(), "domain-repair")
			}
			configure(false)
			_, err = create()
			requireBindingDenial(err)
			configure(true)
			request, err := create()
			if err != nil {
				t.Fatal(err)
			}
			configure(false)
			_, err = f.store.ActSSHTrustRequest(f.ctx, f.approver, f.project, request.ID, "approve", platform.SSHTrustRequestActionInput{ExpectedVersion: request.StateVersion}, platform.RandomID(), "domain-repair")
			requireBindingDenial(err)
			configure(true)
			request, err = f.store.ActSSHTrustRequest(f.ctx, f.approver, f.project, request.ID, "approve", platform.SSHTrustRequestActionInput{ExpectedVersion: request.StateVersion}, platform.RandomID(), "domain-repair")
			if err != nil {
				t.Fatal(err)
			}
			configure(false)
			_, err = f.service.CreateSSHTrustVerification(f.ctx, f.creator, f.project.ID, request.ID, request.StateVersion, platform.RandomID(), "domain-repair")
			if err == nil {
				t.Fatal("temporary credential admitted repair execution")
			}
			configure(true)
			x = f.execute(request, f.creator)
			claim := f.claim(x.ID)
			if f.ssh.runs.Load() != 1 {
				t.Fatal("rejected repair admission dispatched SSH")
			}
			switch outcome {
			case "rebound_before_dispatch":
				configure(false)
			case "revoked_after_remote_proof":
				f.service.SSHRunner = trustProofBarrierRunner{SSH: f.ssh.runner, after: func(taskrunner.Request) error {
					_, e := f.store.RevokeSSHDomainKey(f.ctx, f.project.ID, key.ID, 1, "revoke repair key after actual terminal host proof")
					return e
				}}
			}
			err = f.service.RunClaim(f.ctx, claim)
			if outcome == "rebound_before_dispatch" {
				if err == nil || f.ssh.runs.Load() != 1 {
					t.Fatal("changed repair binding dispatched work", err)
				}
				// The rejected claim has no stop receipt. Exercise the actual
				// expiry path and retain quarantine instead of leaving a running
				// fixture claim for subsequent tests to discover by timing.
				f.forceUnknown(claim)
			} else if err != nil {
				t.Fatal(err)
			}
			trust, source, state := f.status(request.ID)
			if outcome == "success" {
				if trust != "established" || source != "ssh" || state != "succeeded" || f.ssh.runs.Load() != 2 {
					t.Fatal("exact proven domain repair failed", trust, source, state)
				}
			} else if trust != "broken" {
				t.Fatal("rejected or revoked repair established trust", trust, state)
			}
			if outcome == "revoked_after_remote_proof" && (state != "failed" || f.ssh.runs.Load() != 2) {
				t.Fatal("post-proof revocation fabricated a successful repair", state)
			}
			var identity taskrunner.InstallCredential
			if err = f.store.DB.QueryRowContext(f.ctx, "SELECT ssh_domain_credential FROM machines WHERE id=$1", f.machine).Scan(&raw); err != nil || json.Unmarshal(raw, &identity) != nil || identity.ID != key.ID || identity.PublicKey != key.PublicKey {
				t.Fatal("read-only repair replaced or cleared the proven domain identity", err)
			}
			t.Log("PASS real installation proof; create/approve/execute reject temporary-label spoofing;", outcome, "preserves exact domain identity")
		})
	}
}
