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

type trustProofBarrierRunner struct {
	*taskrunner.SSH
	after func(taskrunner.Request) error
}

func (r trustProofBarrierRunner) Do(ctx context.Context, request taskrunner.Request) (taskrunner.Observation, error) {
	observation, e := r.SSH.Do(ctx, request)
	if e == nil && request.Action == "run" && r.after != nil {
		e = r.after(request)
	}
	return observation, e
}

func TestSSHTrustApprovalExecutionAndProof(t *testing.T) {
	dsn := os.Getenv("KEEL_TEST_SSH_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL not configured")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Port() != "15433" || !strings.HasPrefix(u.Path, "/keel_test_ssh") {
		t.Fatal("disposable SSH test DB required")
	}
	ctx := context.Background()
	store, e := platform.Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if e = store.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	password := "trust-test-" + platform.RandomID()
	login := func(prefix string) (string, string, *platform.AuthSession) {
		t.Helper()
		name := prefix + platform.RandomID()[:10]
		if _, e := store.CreateUser(ctx, name, name, password, true, false); e != nil {
			t.Fatal(e)
		}
		pre, _, e := store.NewPreSession(ctx)
		if e != nil {
			t.Fatal(e)
		}
		token, a, e := store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: name, Password: password, Method: "local"}, "127.0.0.1", "trust-test")
		if e != nil {
			t.Fatal(e)
		}
		return name, token, a
	}
	creatorName, token, actor := login("trust")
	approverName, approverToken, approver := login("approver")
	project, e := store.CreateProject(ctx, actor, platform.CreateProjectInput{Name: "Trust verification", Slug: "trust-" + platform.RandomID()[:8]}, platform.RandomID(), "trust-test")
	if e != nil {
		t.Fatal(e)
	}
	f := fixture(t)
	var cfg map[string][]taskrunner.TargetConfig
	b, _ := os.ReadFile(f.configFile)
	if e = json.Unmarshal(b, &cfg); e != nil {
		t.Fatal(e)
	}
	machine, browserMachine, failedMachine, unknownMachine := platform.RandomID(), platform.RandomID(), platform.RandomID(), platform.RandomID()
	expiry := time.Now().UTC().Add(45 * time.Minute).Truncate(time.Microsecond)
	cfg["targets"][0].ProjectID = project.ID
	cfg["targets"][0].MachineID = machine
	cfg["targets"][0].CredentialVersion = "v1"
	cfg["targets"][0].CredentialExpiresAt = &expiry
	for _, id := range []string{browserMachine, failedMachine, unknownMachine} {
		other := cfg["targets"][0]
		other.MachineID = id
		cfg["targets"] = append(cfg["targets"], other)
	}
	b, _ = json.Marshal(cfg)
	if e = os.WriteFile(f.configFile, b, 0600); e != nil {
		t.Fatal(e)
	}
	f.runner.Close()
	f.runner, e = taskrunner.Load(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	store.SSHRunner = f.runner
	for _, m := range []string{machine, browserMachine, failedMachine, unknownMachine} {
		if _, e = store.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,ssh_trust,created_at,updated_at)VALUES($1,$2,'unverified',now(),now())`, m, "trust-"+m); e != nil {
			t.Fatal(e)
		}
		if _, e = store.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at)VALUES($1,$2,$3,$4,'provisioned',now(),now())`, platform.RandomID(), project.ID, m, "trust-"+m[:8]); e != nil {
			t.Fatal(e)
		}
	}
	s := execution.New(store.DB)
	s.SSHRunner = f.runner
	server := httptest.NewUnstartedServer(nil)
	origin := "http://" + server.Listener.Addr().String()
	server.Config.Handler = httpapi.NewWithStore("trust-test", os.Getenv("KEEL_TEST_SSH_STATIC_DIR"), store, platform.Config{Environment: "development", InsecureDevCookie: true, PublicOrigins: []string{origin}})
	server.Start()
	defer server.Close()
	post := func(path string, input any, cookie, csrf string, want int, key string) []byte {
		t.Helper()
		data, _ := json.Marshal(input)
		r, _ := http.NewRequest("POST", server.URL+path, bytes.NewReader(data))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		if key == "" {
			key = platform.RandomID()
		}
		r.Header.Set("Idempotency-Key", key)
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
			t.Fatalf("POST %s want%d got%d: %s", path, want, res.StatusCode, out.String())
		}
		return out.Bytes()
	}
	path := "/api/v1/projects/" + project.ID + "/ssh-trust-requests"
	csrf := platform.CSRFToken(token)
	approveCSRF := platform.CSRFToken(approverToken)
	input := platform.CreateSSHTrustRequestInput{MachineID: machine, Action: "trust.bootstrap", ExpectedAssetVersion: 1, HostKey: cfg["targets"][0].HostKey, Reason: "independently checked fixture host identity", ExpiresInSeconds: 900}
	post(path, input, "", "", 401, "")
	post(path, input, token, "", 403, "")
	createKey := platform.RandomID()
	raw := post(path, input, token, csrf, 201, createKey)
	var request platform.SSHTrustRequest
	json.Unmarshal(raw, &request)
	var duplicate platform.SSHTrustRequest
	json.Unmarshal(post(path, input, token, csrf, 201, createKey), &duplicate)
	if duplicate.ID != request.ID {
		t.Fatal("request idempotency failed")
	}
	if bytes.Contains(raw, []byte("private_key")) || bytes.Contains(raw, []byte(f.listener.Addr().String())) {
		t.Fatal("credential or endpoint leaked")
	}
	post(path+"/"+request.ID+"/approve", platform.SSHTrustRequestActionInput{ExpectedVersion: 1}, token, csrf, 403, "")
	post(path+"/"+request.ID+"/execute", map[string]int{"expected_version": 1}, token, csrf, 409, "")
	json.Unmarshal(post(path+"/"+request.ID+"/approve", platform.SSHTrustRequestActionInput{ExpectedVersion: 1}, approverToken, approveCSRF, 200, ""), &request)
	post("/api/v1/projects/"+project.ID+"/trust/actions", platform.TrustActionInput{MachineIDs: []string{machine}, Action: "trust.bootstrap", Fingerprint: input.HostKey}, token, csrf, 409, "")
	post("/api/v1/projects/"+project.ID+"/ssh-executions", execution.SSHCreateInput{MachineIDs: []string{machine}, Operation: "trust.verify"}, token, csrf, 400, "")
	executeKey := platform.RandomID()
	execRaw := post(path+"/"+request.ID+"/execute", map[string]int64{"expected_version": request.StateVersion}, token, csrf, 201, executeKey)
	var x execution.Execution
	json.Unmarshal(execRaw, &x)
	var same execution.Execution
	json.Unmarshal(post(path+"/"+request.ID+"/execute", map[string]int64{"expected_version": request.StateVersion}, token, csrf, 201, executeKey), &same)
	if same.ID != x.ID {
		t.Fatal("execute replay created new job")
	}
	post(path+"/"+request.ID+"/execute", map[string]int64{"expected_version": request.StateVersion}, token, csrf, 409, "")
	if len(x.Snapshot.Targets) != 1 || x.Snapshot.Targets[0].SSHTrustRequestID != request.ID {
		t.Fatal("trust approval snapshot missing")
	}
	if _, e = s.PumpOutbox(ctx, "trust-test", 100); e != nil {
		t.Fatal(e)
	}
	claim, e := s.Claim(ctx, "trust-test", 2*time.Second)
	if e != nil || claim == nil || claim.Attempt.ExecutionID != x.ID {
		t.Fatalf("claim: %v %#v", e, claim)
	}
	if claim.Task.Type != "trust.verify" {
		t.Fatal("unsafe trust operation")
	}
	if e = s.RunClaim(ctx, claim); e != nil {
		t.Fatal(e)
	}
	var state, pin, requestState string
	if e = store.DB.QueryRowContext(ctx, "SELECT ssh_trust,host_key_fingerprint FROM machines WHERE id=$1", machine).Scan(&state, &pin); e != nil {
		t.Fatal(e)
	}
	if state != "established" || pin != input.HostKey {
		t.Fatalf("proof did not establish trusted host: %s", state)
	}
	if e = store.DB.QueryRowContext(ctx, "SELECT state FROM ssh_trust_requests WHERE id=$1", request.ID).Scan(&requestState); e != nil || requestState != "succeeded" {
		t.Fatal("request not succeeded", e, requestState)
	}
	var count int
	if e = store.DB.QueryRowContext(ctx, "SELECT count(*) FROM trust_logs WHERE machine_id=$1 AND detail->>'source'='ssh'", machine).Scan(&count); e != nil || count != 1 {
		t.Fatal("actual trust audit missing", e, count)
	}
	t.Log("PASS independent approval -> existing Execution/outbox/SSH -> durable proof -> atomic trust projection; mock bypass and unsafe endpoint rejected")

	prepareVerification := func(id string) (*platform.SSHTrustRequest, *execution.Execution, *execution.Claim) {
		t.Helper()
		in := input
		in.MachineID = id
		request, e := store.CreateSSHTrustRequest(ctx, actor, project, in, platform.RandomID(), "trust-test")
		if e != nil {
			t.Fatal(e)
		}
		request, e = store.ActSSHTrustRequest(ctx, approver, project, request.ID, "approve", platform.SSHTrustRequestActionInput{ExpectedVersion: request.StateVersion}, platform.RandomID(), "trust-test")
		if e != nil {
			t.Fatal(e)
		}
		x, e := s.CreateSSHTrustVerification(ctx, actor, project.ID, request.ID, request.StateVersion, platform.RandomID(), "trust-test")
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.PumpOutbox(ctx, "trust-test", 100); e != nil {
			t.Fatal(e)
		}
		c, e := s.Claim(ctx, "trust-test", 2*time.Second)
		if e != nil || c == nil || c.Attempt.ExecutionID != x.ID {
			t.Fatalf("verification claim: %v %#v", e, c)
		}
		return request, x, c
	}
	failedRequest, failedExecution, failedClaim := prepareVerification(failedMachine)
	s.SSHRunner = trustProofBarrierRunner{SSH: f.runner, after: func(request taskrunner.Request) error {
		if request.Command.MachineID == failedMachine {
			_, e := store.DB.ExecContext(ctx, "UPDATE project_assets SET version=version+1 WHERE project_id=$1 AND machine_id=$2", project.ID, failedMachine)
			return e
		}
		return nil
	}}
	if e = s.RunClaim(ctx, failedClaim); e != nil {
		t.Fatal(e)
	}
	s.SSHRunner = f.runner
	if e = store.DB.QueryRowContext(ctx, "SELECT state FROM ssh_trust_requests WHERE id=$1", failedRequest.ID).Scan(&requestState); e != nil || requestState != "failed" {
		t.Fatal("changed asset was incorrectly trusted", requestState, e)
	}
	if e = store.DB.QueryRowContext(ctx, "SELECT ssh_trust FROM machines WHERE id=$1", failedMachine).Scan(&state); e != nil || state != "unverified" {
		t.Fatal("failed proof established trust", state, e)
	}
	unknownRequest, unknownExecution, unknownClaim := prepareVerification(unknownMachine)
	f.drop.Store(true)
	e = s.RunClaim(ctx, unknownClaim)
	f.drop.Store(false)
	if e == nil {
		t.Fatal("lost trust response was reported complete")
	}
	if _, e = store.DB.ExecContext(ctx, "UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1", unknownClaim.Attempt.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ReapExpired(ctx); e != nil {
		t.Fatal(e)
	}
	if e = store.DB.QueryRowContext(ctx, "SELECT state FROM ssh_trust_requests WHERE id=$1", unknownRequest.ID).Scan(&requestState); e != nil || requestState != "unknown" {
		t.Fatal("missing unknown authorization", requestState, e)
	}
	if e = store.DB.QueryRowContext(ctx, "SELECT ssh_trust FROM machines WHERE id=$1", unknownMachine).Scan(&state); e != nil || state != "unverified" {
		t.Fatal("unknown proof established trust", state, e)
	}
	t.Log("PASS changed asset after real SSH proof refuses trust; dropped proof stays unknown with unverified machine")
	input.MachineID = browserMachine
	browserRequest, e := store.CreateSSHTrustRequest(ctx, actor, project, input, platform.RandomID(), "trust-test")
	if e != nil {
		t.Fatal(e)
	}
	if script := os.Getenv("KEEL_TEST_SSH_TRUST_BROWSER_SCRIPT"); script != "" {
		dir := t.TempDir()
		os.Chmod(dir, 0700)
		credentials := filepath.Join(dir, "credentials.json")
		data, _ := json.Marshal(map[string]string{"base_url": server.URL, "username": creatorName, "password": password, "approver_username": approverName, "approver_password": password, "project_id": project.ID, "request_id": browserRequest.ID, "machine_id": browserMachine, "host_key": input.HostKey, "success_request_id": request.ID, "execution_id": x.ID, "failed_request_id": failedRequest.ID, "failed_execution_id": failedExecution.ID, "unknown_request_id": unknownRequest.ID, "unknown_execution_id": unknownExecution.ID})
		if e = os.WriteFile(credentials, data, 0600); e != nil {
			t.Fatal(e)
		}
		cmd := exec.Command("node", script)
		cmd.Env = append(os.Environ(), "KEEL_SSH_TRUST_BROWSER_CREDENTIALS_FILE="+credentials)
		out, e := cmd.CombinedOutput()
		os.Remove(credentials)
		if e != nil {
			t.Fatalf("trust browser: %s %v", out, e)
		}
		t.Log(strings.TrimSpace(string(out)))
	}
	var recoveryVersion int64
	if e = store.DB.QueryRowContext(ctx, "SELECT state_version FROM exec_task_attempts WHERE id=$1", unknownClaim.Attempt.ID).Scan(&recoveryVersion); e != nil {
		t.Fatal(e)
	}
	beforeRecovery := f.runs.Load()
	recovered, e := s.FetchSSHReceipt(ctx, actor, project.ID, unknownExecution.ID, unknownClaim.Attempt.ID, recoveryVersion, platform.RandomID(), "trust-test")
	if e != nil || recovered.State != "success" {
		t.Fatal("actual unknown trust proof did not recover", e)
	}
	if f.runs.Load() != beforeRecovery {
		t.Fatal("trust recovery replayed command")
	}
	if e = store.DB.QueryRowContext(ctx, "SELECT ssh_trust FROM machines WHERE id=$1", unknownMachine).Scan(&state); e != nil || state != "established" {
		t.Fatal("recovered proof did not establish trust", state, e)
	}
	t.Log("PASS unknown trust receipt recovery reuses original command, establishes only proven trust and never reruns")
}
