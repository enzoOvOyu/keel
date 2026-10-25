package taskrunner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/credentialvault"
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/httpapi"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type installProofBarrierRunner struct {
	*taskrunner.SSH
	after func()
}

func (r installProofBarrierRunner) QueryInstalledCredential(ctx context.Context, request taskrunner.Request, key taskrunner.InstallCredential) (taskrunner.Observation, error) {
	o, err := r.SSH.QueryInstalledCredential(ctx, request, key)
	if err == nil {
		r.after()
	}
	return o, err
}

func keyInstallAdmission(t *testing.T) (*trustAdmissionFixture, *platform.SSHDomainKey, string) {
	t.Helper()
	f := newTrustAdmissionFixture(t)
	dir := filepath.Join(f.ssh.root, "keys")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "authorized_keys")
	if err := os.WriteFile(path, []byte("# customer-owned data must remain\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f.ssh.mu.Lock()
	f.ssh.installedKeysFile = path
	f.ssh.mu.Unlock()
	master := filepath.Join(t.TempDir(), "master")
	if err := credentialvault.GenerateMaster(master); err != nil {
		t.Fatal(err)
	}
	vault, err := credentialvault.Load(master)
	if err != nil {
		t.Fatal(err)
	}
	key, err := f.store.CreateSSHDomainKey(f.ctx, vault, credentialvault.Identity{ProjectID: f.project.ID, Ref: "keel-ctrl-install", Version: "v1", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.ConfigureSSHRunner(f.ssh.configFile, master); err != nil {
		t.Fatal(err)
	}
	f.ssh.runner = f.store.SSHRunner
	f.service.SSHRunner = f.store.SSHRunner
	return f, key, path
}

func approvedInstall(t *testing.T, f *trustAdmissionFixture, key *platform.SSHDomainKey) *platform.SSHTrustRequest {
	t.Helper()
	r, err := f.store.CreateSSHTrustRequest(f.ctx, f.creator, f.project, platform.CreateSSHTrustRequestInput{MachineID: f.machine, Action: "trust.bootstrap", ExpectedAssetVersion: 1, HostKey: f.pin, Reason: "independent target pin and frozen domain key approved for restricted installation", ExpiresInSeconds: 900, InstallKeyID: key.ID}, platform.RandomID(), "key-install")
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.store.ActSSHTrustRequest(f.ctx, f.approver, f.project, r.ID, "approve", platform.SSHTrustRequestActionInput{ExpectedVersion: r.StateVersion}, platform.RandomID(), "key-install")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSSHKeyInstallRequiresFreshDomainKeyHandshake(t *testing.T) {
	f, key, path := keyInstallAdmission(t)
	r := approvedInstall(t, f, key)
	x := f.execute(r, f.creator)
	claim := f.claim(x.ID)
	if err := f.service.RunClaim(f.ctx, claim); err != nil {
		t.Fatal(err)
	}
	trust, source, state := f.status(r.ID)
	if trust != "established" || source != "ssh" || state != "succeeded" || f.ssh.runs.Load() != 1 || f.ssh.installedAuth.Load() < 1 {
		t.Fatal("installation lacks a single run plus fresh new-key SSH proof", trust, source, state, f.ssh.runs.Load(), f.ssh.installedAuth.Load())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(raw, []byte("# customer-owned data must remain\n")) || strings.Count(string(raw), strings.TrimSpace(key.PublicKey)) != 1 {
		t.Fatal("installation modified customer bytes or duplicated key")
	}
	var installed bool
	if err = f.store.DB.QueryRowContext(f.ctx, `SELECT (detail->>'key_installation')::boolean FROM trust_logs WHERE machine_id=$1 ORDER BY created_at DESC LIMIT 1`, f.machine).Scan(&installed); err != nil || !installed {
		t.Fatal("control-plane install evidence missing", err)
	}
	if _, err = f.store.DB.ExecContext(f.ctx, `UPDATE ssh_trust_requests SET install_key_snapshot='null',state_version=state_version+1 WHERE id=$1`, r.ID); err == nil {
		t.Fatal("approved key snapshot changed")
	}
	t.Log("PASS actual PG + SSH: one restricted key append, customer bytes retained, freshly authenticated new key queries same durable receipt before trust")
}

func TestSSHKeyInstallUnknownCannotReplayOrClaimTrustBeforeNewKeyProof(t *testing.T) {
	f, key, _ := keyInstallAdmission(t)
	r := approvedInstall(t, f, key)
	x := f.execute(r, f.creator)
	claim := f.claim(x.ID)
	f.ssh.rejectInstalled.Store(true)
	if err := f.service.RunClaim(f.ctx, claim); err == nil {
		t.Fatal("target rejected new key but install claimed success")
	}
	version := f.forceUnknown(claim)
	trust, _, state := f.status(r.ID)
	if trust != "unverified" || state != "unknown" || f.ssh.runs.Load() != 1 {
		t.Fatal("unproven new key promoted trust or replayed install")
	}
	if _, err := f.service.Reconcile(f.ctx, f.creator, f.project.ID, x.ID, claim.Attempt.ID, execution.ReconcileInput{ExpectedVersion: version, Result: "success", Evidence: "operator claims public key file exists"}, platform.RandomID(), "key-install"); err == nil {
		t.Fatal("operator assertion bypassed fresh key handshake")
	}
	f.ssh.rejectInstalled.Store(false)
	if _, err := f.service.FetchSSHReceipt(f.ctx, f.creator, f.project.ID, x.ID, claim.Attempt.ID, version, platform.RandomID(), "key-install"); err != nil {
		t.Fatal(err)
	}
	trust, source, state := f.status(r.ID)
	if trust != "established" || source != "ssh" || state != "succeeded" || f.ssh.runs.Load() != 1 {
		t.Fatal("receipt recovery failed or reran installation")
	}
	t.Log("PASS successful append with new-key rejection stays unknown/quarantined; actual fresh new-key query recovers once, without second run")
}

func TestSSHKeyInstallRevocationBeforeDispatchBlocksMutation(t *testing.T) {
	f, key, path := keyInstallAdmission(t)
	r := approvedInstall(t, f, key)
	x := f.execute(r, f.creator)
	claim := f.claim(x.ID)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.RevokeSSHDomainKey(f.ctx, f.project.ID, key.ID, 1, "withdraw approved install key"); err != nil {
		t.Fatal(err)
	}
	if err = f.service.RunClaim(f.ctx, claim); err == nil {
		t.Fatal("revoked install key reached transport")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) || f.ssh.runs.Load() != 0 {
		t.Fatal("revocation failed to block target mutation")
	}
	f.forceUnknown(claim)
}

func TestSSHKeyInstallRevocationAfterFreshProofCannotEstablishTrust(t *testing.T) {
	f, key, _ := keyInstallAdmission(t)
	r := approvedInstall(t, f, key)
	x := f.execute(r, f.creator)
	claim := f.claim(x.ID)
	var revokedErr error
	f.service.SSHRunner = installProofBarrierRunner{SSH: f.ssh.runner, after: func() {
		_, revokedErr = f.store.RevokeSSHDomainKey(f.ctx, f.project.ID, key.ID, 1, "revoke at final projection boundary")
	}}
	if err := f.service.RunClaim(f.ctx, claim); err != nil {
		t.Fatal(err)
	}
	if revokedErr != nil {
		t.Fatal(revokedErr)
	}
	trust, _, state := f.status(r.ID)
	if trust != "unverified" || state != "failed" || f.ssh.runs.Load() != 1 || f.ssh.installedAuth.Load() < 1 {
		t.Fatal("stale fresh proof bypassed current domain credential authority", trust, state)
	}
	t.Log("PASS actual new-key proof followed by domain-key revocation fails final trust projection without fabricating target rollback")
}

func TestSSHKeyInstallBrowserApprovalAndExecution(t *testing.T) {
	if os.Getenv("KEEL_TEST_SSH_INSTALL_BROWSER_SCRIPT") == "" {
		t.Skip("install browser script not configured")
	}
	f, key, _ := keyInstallAdmission(t)
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	server.Config.Handler = httpapi.NewWithStore("ssh-key-install", os.Getenv("KEEL_TEST_SSH_STATIC_DIR"), f.store, platform.Config{Environment: "development", InsecureDevCookie: true, PublicOrigins: []string{origin}})
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	fail := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := f.service.PumpOutbox(ctx, "install-browser", 100); err != nil {
					if ctx.Err() == nil {
						fail <- err
					}
					return
				}
				claim, err := f.service.Claim(ctx, "install-browser", 3*time.Second)
				if err != nil {
					if ctx.Err() == nil {
						fail <- err
					}
					return
				}
				if claim != nil {
					if err = f.service.RunClaim(ctx, claim); err != nil {
						fail <- err
						return
					}
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()
	file := filepath.Join(t.TempDir(), "browser.json")
	raw, err := json.Marshal(map[string]string{"base_url": origin, "project_id": f.project.ID, "machine_id": f.machine, "host_key": f.pin, "username": f.creator.User.Username, "password": f.password, "approver_username": f.approver.User.Username, "key_id": key.ID, "public_key": key.PublicKey})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("node", os.Getenv("KEEL_TEST_SSH_INSTALL_BROWSER_SCRIPT"))
	cmd.Env = append(os.Environ(), "KEEL_SSH_INSTALL_BROWSER_CREDENTIALS_FILE="+file)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("key install browser: %v\n%s", err, output)
	}
	select {
	case err := <-fail:
		t.Fatal(err)
	default:
	}
	if f.ssh.runs.Load() != 1 || f.ssh.installedAuth.Load() < 1 {
		t.Fatal("browser did not complete one install and fresh new-key proof")
	}
	t.Log(string(output))
}
