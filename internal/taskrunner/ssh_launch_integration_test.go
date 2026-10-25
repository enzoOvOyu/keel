package taskrunner_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/httpapi"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func TestSSHReadOnlyLaunchBrowser(t *testing.T) {
	script := os.Getenv("KEEL_TEST_SSH_LAUNCH_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("read-only SSH launch browser not configured")
	}
	f, key, _, master := keyInstallAdmission(t)
	request := approvedInstall(t, f, key)
	x := f.execute(request, f.creator)
	if err := f.service.RunClaim(f.ctx, f.claim(x.ID)); err != nil {
		t.Fatal(err)
	}
	var config map[string][]taskrunner.TargetConfig
	raw, err := os.ReadFile(f.ssh.configFile)
	if err != nil || json.Unmarshal(raw, &config) != nil {
		t.Fatal("fixture binding unavailable")
	}
	target := &config["targets"][0]
	target.CredentialProvider = "vault"
	target.PrivateKeyFile = ""
	target.CredentialRef = key.Ref
	target.CredentialVersion = key.Version
	target.CredentialExpiresAt = &key.ExpiresAt
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
	f.ssh.runner = f.store.SSHRunner
	f.service.SSHRunner = f.ssh.runner
	operator, err := f.store.CreateUser(f.ctx, "ssh-de-"+platform.RandomID()[:10], "SSH project DE", f.password, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.SetMembership(f.ctx, operator.Username, f.project.ID, "DE", false); err != nil {
		t.Fatal(err)
	}
	viewer, err := f.store.CreateUser(f.ctx, "ssh-viewer-"+platform.RandomID()[:10], "SSH project viewer", f.password, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.SetMembership(f.ctx, viewer.Username, f.project.ID, "Viewer", false); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	server.Config.Handler = httpapi.NewWithStore("ssh-read-only-launch", os.Getenv("KEEL_TEST_SSH_STATIC_DIR"), f.store, platform.Config{Environment: "development", InsecureDevCookie: true, PublicOrigins: []string{origin}})
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	failed := make(chan error, 1)
	go func() {
		defer close(done)
		ticker := time.NewTicker(25 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, e := f.service.PumpOutbox(ctx, "ssh-launch-browser", 100); e != nil {
					if ctx.Err() == nil {
						failed <- e
					}
					return
				}
				claim, e := f.service.Claim(ctx, "ssh-launch-browser", 3*time.Second)
				if e != nil {
					if ctx.Err() == nil {
						failed <- e
					}
					return
				}
				if claim != nil {
					if e = f.service.RunClaim(ctx, claim); e != nil {
						failed <- e
						return
					}
				}
			}
		}
	}()
	defer func() { cancel(); <-done }()
	file := filepath.Join(t.TempDir(), "browser.json")
	raw, err = json.Marshal(map[string]string{"base_url": origin, "project_id": f.project.ID, "machine_id": f.machine, "username": operator.Username, "password": f.password, "viewer_username": viewer.Username})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, raw, 0600); err != nil {
		t.Fatal(err)
	}
	browserContext, stopBrowser := context.WithTimeout(f.ctx, 2*time.Minute)
	defer stopBrowser()
	command := exec.CommandContext(browserContext, "node", script)
	command.Env = append(os.Environ(), "KEEL_SSH_LAUNCH_BROWSER_CREDENTIALS_FILE="+file)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("SSH launch browser failed: %v\n%s", err, output)
	}
	select {
	case e := <-failed:
		t.Fatal(e)
	default:
	}
	if f.ssh.runs.Load() != 3 {
		t.Fatalf("expected one installation and two actual read-only commands, got %d", f.ssh.runs.Load())
	}
	var count int
	if err = f.store.DB.QueryRowContext(f.ctx, "SELECT count(*) FROM exec_executions WHERE project_id=$1 AND mode='ssh'", f.project.ID).Scan(&count); err != nil || count != 3 {
		t.Fatal("ambiguous UI response duplicated execution", count, err)
	}
	t.Log(string(output))
}
