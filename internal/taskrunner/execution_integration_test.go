package taskrunner_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/httpapi"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// The caller supplies its own newly-created disposable database. No schema
// reset, production credential inheritance, external targets or hardware writes.
func TestSSHExecutionPostgresProtocolAndHTTP(t *testing.T) {
	dsn := os.Getenv("KEEL_TEST_SSH_DATABASE_URL")
	if dsn == "" {
		t.Skip("KEEL_TEST_SSH_DATABASE_URL absent; actual PostgreSQL/SSH integration not run")
	}
	u, e := url.Parse(dsn)
	if e != nil || !strings.HasPrefix(u.Path, "/keel_test_ssh") || u.Hostname() != "127.0.0.1" || u.Port() != "15433" {
		t.Fatal("explicit isolated SSH test database required")
	}
	ctx := context.Background()
	store, e := platform.Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { store.Close() })
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	password := "isolated-ssh-" + platform.RandomID()
	name := "ssh" + platform.RandomID()[:8]
	// Other SSH fixtures may have initialized the shared disposable database.
	// Own a distinct administrator without depending on test registration order.
	var initialized bool
	if e = store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE is_admin)").Scan(&initialized); e != nil {
		t.Fatal(e)
	}
	if _, e = store.CreateUser(ctx, name, name, password, true, !initialized); e != nil {
		t.Fatal(e)
	}
	login := func(user string) (string, *platform.AuthSession) {
		t.Helper()
		pre, _, e := store.NewPreSession(ctx)
		if e != nil {
			t.Fatal(e)
		}
		token, a, e := store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: user, Password: password, Method: "local"}, "127.0.0.1", "ssh-test")
		if e != nil {
			t.Fatal(e)
		}
		return token, a
	}
	token, actor := login(name)
	project, e := store.CreateProject(ctx, actor, platform.CreateProjectInput{Name: "SSH protocol test", Slug: "ssh-" + platform.RandomID()[:8]}, platform.RandomID(), "ssh-test")
	if e != nil {
		t.Fatal(e)
	}
	f := fixture(t)
	var cfg map[string][]taskrunner.TargetConfig
	b, _ := os.ReadFile(f.configFile)
	json.Unmarshal(b, &cfg)
	cfg["targets"][0].ProjectID = project.ID
	b, _ = json.Marshal(cfg)
	os.WriteFile(f.configFile, b, 0600)
	f.runner.Close()
	f.runner, e = taskrunner.Load(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	store.SSHRunner = f.runner
	// Seed the external device identity/trust fixture; this is explicitly a
	// software protocol target, not a successful PXE or real trust bootstrap.
	if _, e = store.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,ssh_trust,ssh_trust_source,host_key_fingerprint,created_at,updated_at) VALUES($1,$2,'established','ssh',$3,now(),now())`, machineID, "ssh-fixture-"+platform.RandomID()[:8], cfg["targets"][0].HostKey); e != nil {
		t.Fatal(e)
	}
	if _, e = store.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at) VALUES($1,$2,$3,'ssh-protocol-target','provisioned',now(),now())`, platform.RandomID(), project.ID, machineID); e != nil {
		t.Fatal(e)
	}
	s := execution.New(store.DB)
	s.SSHRunner = f.runner
	api := httptest.NewUnstartedServer(nil)
	origin := "http://" + api.Listener.Addr().String()
	api.Config.Handler = httpapi.NewWithStore("ssh-integration", os.Getenv("KEEL_TEST_SSH_STATIC_DIR"), store, platform.Config{Environment: "development", InsecureDevCookie: true, PublicOrigins: []string{origin}})
	api.Start()
	defer api.Close()
	post := func(path string, input any, cookie, csrf string, want int, keys ...string) []byte {
		t.Helper()
		body, _ := json.Marshal(input)
		r, _ := http.NewRequest("POST", api.URL+path, bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		idem := platform.RandomID()
		if len(keys) > 0 {
			idem = keys[0]
		}
		r.Header.Set("Idempotency-Key", idem)
		if cookie != "" {
			r.AddCookie(&http.Cookie{Name: "keel_session", Value: cookie})
		}
		if csrf != "" {
			r.Header.Set("X-CSRF-Token", csrf)
		}
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		var out bytes.Buffer
		out.ReadFrom(res.Body)
		if res.StatusCode != want {
			t.Fatalf("HTTP %s wanted %d got %d: %s", path, want, res.StatusCode, out.String())
		}
		return out.Bytes()
	}
	path := "/api/v1/projects/" + project.ID + "/ssh-executions"
	input := execution.SSHCreateInput{MachineIDs: []string{machineID}, Operation: "host.identity"}
	post(path, input, "", "", 401)
	post(path, input, token, "", 403)
	post(path, map[string]any{"machine_ids": []string{machineID}, "operation": "host.identity", "command": "id"}, token, platform.CSRFToken(token), 400)
	post(path, execution.SSHCreateInput{MachineIDs: []string{machineID}, Operation: "sh"}, token, platform.CSRFToken(token), 400)
	viewerName := "viewer" + platform.RandomID()[:8]
	if _, e = store.CreateUser(ctx, viewerName, viewerName, password, false, false); e != nil {
		t.Fatal(e)
	}
	if e = store.SetMembership(ctx, viewerName, project.ID, "Viewer", false); e != nil {
		t.Fatal(e)
	}
	viewerToken, _ := login(viewerName)
	post(path, input, viewerToken, platform.CSRFToken(viewerToken), 403)
	outsiderName := "other" + platform.RandomID()[:8]
	if _, e = store.CreateUser(ctx, outsiderName, outsiderName, password, false, false); e != nil {
		t.Fatal(e)
	}
	outsiderToken, _ := login(outsiderName)
	post(path, input, outsiderToken, platform.CSRFToken(outsiderToken), 404)
	store.SSHRunner = nil
	post(path, input, token, platform.CSRFToken(token), 503)
	store.SSHRunner = f.runner
	response := post(path, input, token, platform.CSRFToken(token), 201)
	x := &execution.Execution{}
	if json.Unmarshal(response, x) != nil {
		t.Fatal("execution response")
	}
	if bytes.Contains(response, []byte("private_key")) || bytes.Contains(response, []byte(f.listener.Addr().String())) {
		t.Fatal("snapshot leaked endpoint/credentials")
	}
	claim := func() *execution.Claim {
		t.Helper()
		if _, e := s.PumpOutbox(ctx, "ssh-test", 100); e != nil {
			t.Fatal(e)
		}
		c, e := s.Claim(ctx, "ssh-test", time.Second)
		if e != nil {
			t.Fatal(e)
		}
		if c == nil {
			t.Fatal("expected claim")
		}
		return c
	}
	get := func(id string) *execution.Execution {
		t.Helper()
		d, e := s.Get(ctx, project.ID, id)
		if e != nil {
			t.Fatal(e)
		}
		return &d.Execution
	}
	c := claim()
	if c.Target.Mode != "ssh" {
		t.Fatal("incorrect target mode")
	}
	if e = s.RunClaim(ctx, c); e != nil {
		t.Fatal(e)
	}
	if get(x.ID).State != "success" {
		t.Fatal("SSH result not settled")
	}
	if script := os.Getenv("KEEL_TEST_SSH_BROWSER_SCRIPT"); script != "" {
		privateDir := t.TempDir()
		if e = os.Chmod(privateDir, 0700); e != nil {
			t.Fatal(e)
		}
		credentials := filepath.Join(privateDir, "credentials.json")
		data, _ := json.Marshal(map[string]string{"base_url": api.URL, "username": name, "password": password, "project_id": project.ID, "execution_id": x.ID})
		if e = os.WriteFile(credentials, data, 0600); e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command("node", script)
		cmd.Env = append(os.Environ(), "KEEL_SSH_BROWSER_CREDENTIALS_FILE="+credentials)
		out, e := cmd.CombinedOutput()
		os.Remove(credentials)
		if e != nil {
			t.Fatalf("browser: %s %v", out, e)
		}
		t.Log(strings.TrimSpace(string(out)))
	}
	before := f.runs.Load()
	if e = s.RunClaim(ctx, c); e == nil {
		t.Fatal("completed claim accepted as new run")
	}
	if f.runs.Load() != before {
		t.Fatal("stale claim reached SSH")
	}
	create := func(op string, wait int) *execution.Execution {
		t.Helper()
		x, e := s.CreateSSH(ctx, actor, project.ID, execution.SSHCreateInput{MachineIDs: []string{machineID}, Operation: op, WaitMS: wait, TimeoutMS: 15000}, platform.RandomID(), "ssh-test")
		if e != nil {
			t.Fatal(e)
		}
		return x
	}
	idemKey := platform.RandomID()
	idem1, e := s.CreateSSH(ctx, actor, project.ID, input, idemKey, "ssh-test")
	if e != nil {
		t.Fatal(e)
	}
	idem2, e := s.CreateSSH(ctx, actor, project.ID, input, idemKey, "ssh-test")
	if e != nil || idem1.ID != idem2.ID {
		t.Fatal("duplicate submission created new execution", e)
	}
	c = claim()
	if c.Attempt.ExecutionID != idem1.ID {
		t.Fatal("idempotent execution lost")
	}
	if e = s.RunClaim(ctx, c); e != nil {
		t.Fatal(e)
	}
	// One operation lost its response: query recovers, with exactly one run.
	x = create("host.identity", 0)
	c = claim()
	f.drop.Store(true)
	if e = s.RunClaim(ctx, c); e == nil {
		t.Fatal("lost receipt reported settled")
	}
	f.drop.Store(false)
	if _, e = store.DB.ExecContext(ctx, `UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1`, c.Attempt.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReapExpired(ctx); e != nil {
		t.Fatal(e)
	}
	if get(x.ID).State != "reconciling" {
		t.Fatal("unknown not quarantined")
	}
	var version int64
	if e = store.DB.QueryRowContext(ctx, `SELECT state_version FROM exec_task_attempts WHERE id=$1`, c.Attempt.ID).Scan(&version); e != nil {
		t.Fatal(e)
	}
	before = f.runs.Load()
	recoverPath := "/api/v1/projects/" + project.ID + "/executions/" + x.ID + "/attempts/" + c.Attempt.ID + "/ssh-receipt"
	recoverKey := platform.RandomID()
	post(recoverPath, map[string]any{"expected_version": version}, token, platform.CSRFToken(token), 200, recoverKey)
	post(recoverPath, map[string]any{"expected_version": version}, token, platform.CSRFToken(token), 200, recoverKey)
	post(recoverPath, map[string]any{"expected_version": version + 1}, token, platform.CSRFToken(token), 409, recoverKey)
	if get(x.ID).State != "success" || f.runs.Load() != before {
		t.Fatal("reconciliation reran operation")
	}
	// Shared read leases still conflict with an exclusive machine lease.
	x = create("host.wait", 10000)
	c = claim()
	if _, e = store.DB.ExecContext(ctx, `UPDATE exec_resource_leases SET mode='exclusive' WHERE attempt_id=$1`, c.Attempt.ID); e != nil {
		t.Fatal(e)
	}
	other := create("host.identity", 0)
	s.PumpOutbox(ctx, "ssh-test", 100)
	if blocked, e := s.Claim(ctx, "ssh-test", time.Second); e != nil || blocked != nil {
		t.Fatal("exclusive machine lease bypassed", e)
	}
	done := make(chan error, 1)
	go func() { done <- s.RunClaim(ctx, c) }()
	deadline := time.Now().Add(3 * time.Second)
	for f.runs.Load() == before {
		if time.Now().After(deadline) {
			t.Fatal("SSH wait not running")
		}
		time.Sleep(10 * time.Millisecond)
	}
	current := get(x.ID)
	if _, e = s.Control(ctx, actor, project.ID, x.ID, execution.ControlInput{Action: "cancel", ExpectedVersion: current.StateVersion}, platform.RandomID(), "ssh-test"); e != nil {
		t.Fatal(e)
	}
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	if get(x.ID).State != "killed" {
		t.Fatal("cancel lacked actual terminal receipt")
	}
	c = claim()
	if c.Attempt.ExecutionID != other.ID {
		t.Fatal("conflicting task lost")
	}
	if e = s.RunClaim(ctx, c); e != nil {
		t.Fatal(e)
	}
	// Accepted jobs survive logout but do not survive removal of execution role.
	deName := "de" + platform.RandomID()[:8]
	if _, e = store.CreateUser(ctx, deName, deName, password, false, false); e != nil {
		t.Fatal(e)
	}
	if e = store.SetMembership(ctx, deName, project.ID, "DE", false); e != nil {
		t.Fatal(e)
	}
	_, deActor := login(deName)
	deJob, e := s.CreateSSH(ctx, deActor, project.ID, input, platform.RandomID(), "ssh-test")
	if e != nil {
		t.Fatal(e)
	}
	if e = store.SetMembership(ctx, deName, project.ID, "Viewer", false); e != nil {
		t.Fatal(e)
	}
	s.PumpOutbox(ctx, "ssh-test", 100)
	before = f.runs.Load()
	if blocked, e := s.Claim(ctx, "ssh-test", time.Second); e != nil || blocked != nil {
		t.Fatal("revoked submitter claimed", e)
	}
	if get(deJob.ID).State != "paused" || f.runs.Load() != before {
		t.Fatal("revoked submitter reached SSH")
	}
	// Current trust is checked again after admission, before any dispatch.
	x = create("host.identity", 0)
	if _, e = store.DB.ExecContext(ctx, `UPDATE machines SET ssh_trust='revoked' WHERE id=$1`, machineID); e != nil {
		t.Fatal(e)
	}
	s.PumpOutbox(ctx, "ssh-test", 100)
	before = f.runs.Load()
	if c, e := s.Claim(ctx, "ssh-test", time.Second); e != nil || c != nil {
		t.Fatal("revoked target claimed", e)
	}
	if get(x.ID).State != "paused" || f.runs.Load() != before {
		t.Fatal("revoked target reached transport")
	}
	post(path, input, token, platform.CSRFToken(token), 409)
	t.Log("actual PostgreSQL/outbox/lease + SSH protocol + HTTP identity, cancellation, disconnect/recovery, stale claims, resource conflicts and revoked trust passed; external identity is a fixture, hardware acceptance not run")
}
