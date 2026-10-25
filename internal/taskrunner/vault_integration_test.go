package taskrunner_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/credentialvault"
	"github.com/enzoOvOyu/keel/internal/httpapi"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"golang.org/x/crypto/ssh"
)

func vaultAdmission(t *testing.T) (*trustAdmissionFixture, *platform.SSHDomainKey, string) {
	t.Helper()
	f := newTrustAdmissionFixture(t)
	master := filepath.Join(t.TempDir(), "master")
	if err := credentialvault.GenerateMaster(master); err != nil {
		t.Fatal(err)
	}
	vault, err := credentialvault.Load(master)
	if err != nil {
		t.Fatal(err)
	}
	key, err := f.store.CreateSSHDomainKey(f.ctx, vault, credentialvault.Identity{ProjectID: f.project.ID, Ref: "keel-ctrl-project", Version: "v1", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	public, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	f.ssh = fixtureWithPublicKey(t, public)
	var config map[string][]taskrunner.TargetConfig
	raw, err := os.ReadFile(f.ssh.configFile)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	c := &config["targets"][0]
	c.ProjectID = f.project.ID
	c.MachineID = f.machine
	c.CredentialRef = key.Ref
	c.CredentialVersion = key.Version
	c.CredentialExpiresAt = &key.ExpiresAt
	c.PrivateKeyFile = ""
	c.CredentialProvider = "vault"
	f.pin = c.HostKey
	raw, err = json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(f.ssh.configFile, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err = f.store.ConfigureSSHRunner(f.ssh.configFile, master); err != nil {
		t.Fatal(err)
	}
	f.ssh.runner.Close()
	f.ssh.runner = f.store.SSHRunner
	f.service.SSHRunner = f.store.SSHRunner
	return f, key, master
}

func TestSSHVaultProviderApprovedProofAndPooledRevocation(t *testing.T) {
	f, key, master := vaultAdmission(t)
	r := f.approved()
	x := f.execute(r, f.creator)
	c := f.claim(x.ID)
	if err := f.service.RunClaim(f.ctx, c); err != nil {
		t.Fatal(err)
	}
	trust, source, state := f.status(r.ID)
	if trust != "established" || source != "ssh" || state != "succeeded" || f.ssh.runs.Load() != 1 {
		t.Fatal("encrypted provider did not establish real SSH evidence")
	}
	// Provider reads must not wait for an available business transaction slot.
	f.store.DB.SetMaxOpenConns(1)
	tx, err := f.store.DB.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.ssh.runner.Binding(f.project.ID, f.machine)
	tx.Rollback()
	f.store.DB.SetMaxOpenConns(16)
	if err != nil {
		t.Fatal("credential resolution exhausted business pool", err)
	}
	if _, err = f.store.RevokeSSHDomainKey(f.ctx, f.project.ID, key.ID, 1, "operator retires control-plane credential"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.ssh.runner.Binding(f.project.ID, f.machine); !errors.Is(err, taskrunner.ErrRejected) {
		t.Fatal("pooled revoked binding accepted", err)
	}
	cmd := command()
	cmd.ProjectID = f.project.ID
	cmd.MachineID = f.machine
	for _, action := range []string{"run", "query", "cancel"} {
		if _, err = f.ssh.runner.Do(context.Background(), taskrunner.Request{Action: action, Command: cmd}); !errors.Is(err, taskrunner.ErrRejected) {
			t.Fatal("revoked credential sent target command", action, err)
		}
	}
	if f.ssh.runs.Load() != 1 {
		t.Fatal("revocation replayed work")
	}
	if err = f.store.ConfigureSSHRunner(f.ssh.configFile, master); err == nil {
		t.Fatal("restart accepted revoked version")
	}
	if !f.ssh.runner.HasTarget(f.project.ID, f.machine) {
		t.Fatal("revoked target became eligible for simulation")
	}
	t.Log("PASS encrypted PostgreSQL credential -> pinned SSH -> approved real proof; active pool and restart reject revocation, no command replay")
}

func TestSSHVaultRevocationPreservesUnknownQuarantine(t *testing.T) {
	f, key, _ := vaultAdmission(t)
	r := f.approved()
	x := f.execute(r, f.creator)
	c := f.claim(x.ID)
	f.ssh.drop.Store(true)
	if err := f.service.RunClaim(f.ctx, c); err == nil {
		t.Fatal("lost response became terminal")
	}
	f.ssh.drop.Store(false)
	version := f.forceUnknown(c)
	if _, err := f.store.RevokeSSHDomainKey(f.ctx, f.project.ID, key.ID, 1, "credential revoked while receipt unknown"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.FetchSSHReceipt(f.ctx, f.creator, f.project.ID, x.ID, c.Attempt.ID, version, platform.RandomID(), "vault-test"); err == nil {
		t.Fatal("revoked credential queried old receipt")
	}
	trust, _, state := f.status(r.ID)
	if trust != "unverified" || state != "unknown" || f.ssh.runs.Load() != 1 {
		t.Fatal("unknown changed trust or repeated target command")
	}
	var held int
	if err := f.store.DB.QueryRowContext(f.ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'`, c.Attempt.ID).Scan(&held); err != nil || held != 2 {
		t.Fatal("revocation dropped unknown quarantine", held, err)
	}
	t.Log("PASS revoked encrypted credential cannot query/replay unknown; both authoritative resource leases stay quarantined")
}

func TestSSHVaultPublicMetadataHTTPAndBrowser(t *testing.T) {
	f, key, master := vaultAdmission(t)
	if _, err := f.store.RevokeSSHDomainKey(f.ctx, f.project.ID, key.ID, 1, "retired version for metadata proof"); err != nil {
		t.Fatal(err)
	}
	vault, err := credentialvault.Load(master)
	if err != nil {
		t.Fatal(err)
	}
	identity := key.Identity
	identity.Version = "v2"
	next, err := f.store.CreateSSHDomainKey(f.ctx, vault, identity)
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewUnstartedServer(nil)
	origin := "http://" + api.Listener.Addr().String()
	api.Config.Handler = httpapi.NewWithStore("ssh-domain-test", os.Getenv("KEEL_TEST_SSH_STATIC_DIR"), f.store, platform.Config{Environment: "development", InsecureDevCookie: true, PublicOrigins: []string{origin}})
	api.Start()
	defer api.Close()
	login := func(name string) string {
		t.Helper()
		pre, _, err := f.store.NewPreSession(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		token, _, err := f.store.Login(f.ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: name, Password: f.password, Method: "local"}, "127.0.0.1", "vault-http")
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	read := func(path, token string, want int) []byte {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, origin+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			request.AddCookie(&http.Cookie{Name: "keel_session", Value: token})
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("metadata status wanted %d got %d", want, response.StatusCode)
		}
		return body
	}
	path := "/api/v1/projects/" + f.project.ID + "/ssh-domain-keys"
	read(path, "", 401)
	adminToken := login(f.creator.User.Username)
	raw := read(path, adminToken, 200)
	var page platform.Page[platform.SSHDomainKey]
	if err = json.Unmarshal(raw, &page); err != nil || page.Total != 2 || len(page.Items) != 2 {
		t.Fatal("public page mismatch", err)
	}
	var fields map[string]any
	var envelope struct {
		Items []map[string]any `json:"items"`
	}
	if err = json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	for _, fields = range envelope.Items {
		for field := range fields {
			if field == "ciphertext" || field == "private_key" || field == "private_key_file" {
				t.Fatal("private field exported")
			}
		}
	}
	read(path+"?page=0", adminToken, 400)
	for _, role := range []string{"Admin", "DL", "SRE", "Viewer", "DE"} {
		name := "keyrole" + platform.RandomID()[:10]
		if _, err = f.store.CreateUser(f.ctx, name, name, f.password, false, false); err != nil {
			t.Fatal(err)
		}
		if err = f.store.SetMembership(f.ctx, name, f.project.ID, role, false); err != nil {
			t.Fatal(err)
		}
		token := login(name)
		want := 200
		if role == "Viewer" || role == "DE" {
			want = 403
		}
		read(path, token, want)
		// Revoking membership revokes the existing session as well.
		if err = f.store.SetMembership(f.ctx, name, f.project.ID, role, true); err != nil {
			t.Fatal(err)
		}
		read(path, token, 401)
	}
	if script := os.Getenv("KEEL_TEST_SSH_DOMAIN_BROWSER_SCRIPT"); script != "" {
		file := filepath.Join(t.TempDir(), "browser.json")
		input := map[string]any{"base_url": origin, "project_id": f.project.ID, "username": f.creator.User.Username, "password": f.password, "revoked_id": key.ID, "active_id": next.ID, "public_key": next.PublicKey}
		raw, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(file, raw, 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("node", script)
		cmd.Env = append(os.Environ(), "KEEL_SSH_DOMAIN_BROWSER_CREDENTIALS_FILE="+file)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("domain browser failed: %v\n%s", err, output)
		}
		t.Log(string(output))
	}
	t.Log("PASS actual HTTP public-only metadata, page validation, five project roles and revoked sessions")
}
