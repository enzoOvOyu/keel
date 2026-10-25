package agentidentity_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentclient"
	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/httpapi"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type agentHTTPClient struct {
	base, csrf string
	client     *http.Client
}

func (c *agentHTTPClient) request(t *testing.T, method, path string, body any, key string, status int) []byte {
	t.Helper()
	var payload []byte
	if body != nil {
		var e error
		payload, e = json.Marshal(body)
		requireNoError(t, e)
	}
	req, e := http.NewRequest(method, c.base+path, bytes.NewReader(payload))
	requireNoError(t, e)
	req.Header.Set("Origin", c.base)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", c.csrf)
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, e := c.client.Do(req)
	requireNoError(t, e)
	defer response.Body.Close()
	data, e := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	requireNoError(t, e)
	if response.StatusCode != status {
		// These endpoints return only public records and structured errors.
		t.Fatalf("%s %s: status %d, wanted %d: %s", method, path, response.StatusCode, status, data)
	}
	if strings.HasPrefix(path, "/api/") && response.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("private Agent API response is cacheable")
	}
	return data
}

func loginAgentHTTP(t *testing.T, base, username, password string) *agentHTTPClient {
	t.Helper()
	jar, e := cookiejar.New(nil)
	requireNoError(t, e)
	client := &agentHTTPClient{base: base, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	var pre struct {
		CSRF string `json:"csrf_token"`
	}
	requireNoError(t, json.Unmarshal(client.request(t, "GET", "/api/v1/auth/csrf", nil, "", 200), &pre))
	client.csrf = pre.CSRF
	var session platform.SessionResponse
	requireNoError(t, json.Unmarshal(client.request(t, "POST", "/api/v1/auth/login", platform.LoginInput{Username: username, Password: password, Method: "local"}, "", 200), &session))
	client.csrf = session.CSRFToken
	return client
}

func agentHTTPServer(t *testing.T, f *identityFixture, static string) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()
	server.Config.Handler = httpapi.NewWithStore("agent-http-test", static, f.store, platform.Config{Environment: "development", InsecureDevCookie: true, PublicOrigins: []string{base}})
	server.Start()
	t.Cleanup(server.Close)
	return server
}

func enrollAgentHTTP(t *testing.T, f *identityFixture) (agentclient.Config, *agentclient.Identity) {
	t.Helper()
	machine, serial := f.target(t)
	_, token := f.grant(t, machine)
	dir := privateDirectory(t)
	file := filepath.Join(dir, "token")
	requireNoError(t, agentpki.WriteNew(file, []byte(token)))
	config := agentclient.Config{Endpoint: f.endpoint, ServerName: "127.0.0.1", ServerCA: f.caPath, AgentCA: f.caPath, ProjectID: f.project, MachineID: machine, Serial: serial, StateDirectory: dir, TokenFile: file, TasksEnabled: true}
	identity, e := agentclient.Enroll(context.Background(), config, "http-fixture-v1")
	requireNoError(t, e)
	return config, &identity
}

func startAgentHTTP(t *testing.T, f *identityFixture, config agentclient.Config) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agentclient.Stream(ctx, config, "http-fixture-v1", nil) }()
	waitTaskBinding(t, f, config.MachineID)
	return stop, done
}

func TestAgentHTTPAdmissionPermissionsAndDurableRecovery(t *testing.T) {
	f := newIdentityFixture(t, true)
	ctx := context.Background()
	config, _ := enrollAgentHTTP(t, f)
	server := agentHTTPServer(t, f, "")
	admin := loginAgentHTTP(t, server.URL, f.actor.User.Username, "agent-integration-only-long-password")
	base := "/api/v1/projects/" + f.project
	input := execution.SSHCreateInput{MachineIDs: []string{config.MachineID}, Operation: "host.identity", TimeoutMS: 5000}
	var listing struct {
		Enabled bool `json:"enabled"`
		Total   int  `json:"total"`
		Page    int  `json:"page"`
		Items   []struct {
			Machine    string `json:"machine_id"`
			Credential string `json:"credential_id"`
			Connected  bool   `json:"connected"`
		} `json:"items"`
	}
	list := func(path string) []byte {
		t.Helper()
		data := admin.request(t, "GET", path, nil, "", 200)
		requireNoError(t, json.Unmarshal(data, &listing))
		for _, forbidden := range []string{"certificate_pem", "PRIVATE KEY", "enrollment_token", "authority_id", "certificate_hash"} {
			if bytes.Contains(data, []byte(forbidden)) {
				t.Fatal("target metadata exposed identity material", forbidden)
			}
		}
		return data
	}
	list(base + "/agent-targets")
	if listing.Enabled || listing.Total != 0 || len(listing.Items) != 0 {
		t.Fatal("default-disabled API exposed enabled task targets")
	}
	data := admin.request(t, "POST", base+"/agent-executions", input, platform.RandomID(), 503)
	if !bytes.Contains(data, []byte("AGENT_DISABLED")) {
		t.Fatal("disabled task admission was not explicit")
	}
	anonymous := &agentHTTPClient{base: server.URL, client: server.Client()}
	anonymous.request(t, "GET", base+"/agent-targets", nil, "", 401)
	anonymous.request(t, "POST", base+"/agent-executions", input, platform.RandomID(), 401)
	// Configuration is immutable for a running API process. Restart the test
	// HTTP server before opting in rather than racing an in-flight handler.
	server.Close()
	f.store.AgentTasksEnabled = true
	server = agentHTTPServer(t, f, "")
	admin = loginAgentHTTP(t, server.URL, f.actor.User.Username, "agent-integration-only-long-password")
	list(base + "/agent-targets")
	if !listing.Enabled || listing.Total != 1 || len(listing.Items) != 1 || listing.Items[0].Machine != config.MachineID || listing.Items[0].Connected {
		t.Fatal("registered offline task target metadata is incorrect")
	}
	noCSRF := *admin
	noCSRF.csrf = ""
	noCSRF.request(t, "POST", base+"/agent-executions", input, platform.RandomID(), 403)
	admin.request(t, "POST", base+"/agent-executions", map[string]any{"machine_ids": input.MachineIDs, "operation": "host.identity", "shell": "id"}, platform.RandomID(), 400)
	bad := input
	bad.Operation = "shell"
	admin.request(t, "POST", base+"/agent-executions", bad, platform.RandomID(), 400)
	bad = input
	bad.MachineIDs = []string{config.MachineID, config.MachineID}
	admin.request(t, "POST", base+"/agent-executions", bad, platform.RandomID(), 400)
	admin.request(t, "GET", base+"/agent-targets?page_size=101", nil, "", 400)
	admin.request(t, "GET", base+"/agent-targets?page=0", nil, "", 400)
	admin.request(t, "POST", base+"/agent-executions", input, platform.RandomID(), 503)
	var viewer *agentHTTPClient
	for _, role := range []string{"Admin", "DE", "DL", "SRE", "Viewer", "NE", "NDE", "NH"} {
		username := "http-" + platform.RandomID()[:12]
		_, e := f.store.CreateUser(ctx, username, username, "agent-http-role-only-password", false, false)
		requireNoError(t, e)
		requireNoError(t, f.store.SetMembership(ctx, username, f.project, role, false))
		client := loginAgentHTTP(t, server.URL, username, "agent-http-role-only-password")
		client.request(t, "GET", base+"/agent-targets", nil, "", 200)
		status := 403
		if role == "Admin" || role == "DE" || role == "DL" || role == "SRE" {
			status = 503
		}
		client.request(t, "POST", base+"/agent-executions", input, platform.RandomID(), status)
		if role == "Viewer" {
			viewer = client
		}
	}
	other, e := f.store.CreateProject(ctx, f.actor, platform.CreateProjectInput{Name: "Other Agent HTTP project", Slug: "other-" + platform.RandomID()[:12]}, platform.RandomID(), "agent-http")
	requireNoError(t, e)
	viewer.request(t, "GET", "/api/v1/projects/"+other.ID+"/agent-targets", nil, "", 404)
	viewer.request(t, "POST", "/api/v1/projects/"+other.ID+"/agent-executions", input, platform.RandomID(), 404)
	foreign := input
	foreign.MachineIDs = []string{platform.RandomID()}
	admin.request(t, "POST", base+"/agent-executions", foreign, platform.RandomID(), 409)
	second, serial := f.target(t)
	_, _ = f.enroll(t, second, serial)
	list(base + "/agent-targets?page_size=1&page=1")
	if listing.Total != 2 || len(listing.Items) != 1 || listing.Page != 1 {
		t.Fatal("first target page is not bounded")
	}
	first := listing.Items[0].Machine
	list(base + "/agent-targets?page_size=1&page=2")
	if listing.Total != 2 || len(listing.Items) != 1 || listing.Items[0].Machine == first || listing.Page != 2 {
		t.Fatal("target pagination duplicated identities")
	}
	_, e = f.store.DB.Exec(`UPDATE project_assets SET version=version+1 WHERE project_id=$1 AND machine_id=$2`, f.project, second)
	requireNoError(t, e)
	list(base + "/agent-targets")
	if listing.Total != 1 || listing.Items[0].Machine != config.MachineID {
		t.Fatal("asset-version mismatch remained eligible")
	}
	stop, done := startAgentHTTP(t, f, config)
	defer func() {
		stop()
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("Agent HTTP fixture stream did not stop")
			}
		}
	}()
	list(base + "/agent-targets")
	if len(listing.Items) != 1 || !listing.Items[0].Connected {
		t.Fatal("authenticated live task target was not visible")
	}
	credentialID := listing.Items[0].Credential
	key := platform.RandomID()
	var x execution.Execution
	created := admin.request(t, "POST", base+"/agent-executions", input, key, 201)
	requireNoError(t, json.Unmarshal(created, &x))
	if x.Mode != "agent" || len(x.Snapshot.Targets) != 1 || x.Snapshot.Targets[0].Agent == nil {
		t.Fatal("HTTP admission did not freeze the registered Agent binding")
	}
	if replay := admin.request(t, "POST", base+"/agent-executions", input, key, 201); !bytes.Equal(created, replay) {
		t.Fatal("HTTP Agent create retry produced a different execution")
	}
	bad = input
	bad.Operation = "host.wait"
	bad.WaitMS = 1
	admin.request(t, "POST", base+"/agent-executions", bad, key, 409)
	s := execution.New(f.store.DB)
	s.AgentRunner = f.broker
	claim := claimAgent(t, s, x.ID)
	requireNoError(t, s.RunClaim(ctx, claim))
	var detail execution.Detail
	requireNoError(t, json.Unmarshal(admin.request(t, "GET", base+"/executions/"+x.ID, nil, "", 200), &detail))
	if detail.Execution.State != "success" || len(detail.Attempts) != 1 || detail.Attempts[0].State != "success" {
		t.Fatal("real Agent HTTP execution did not finish")
	}
	viewer.request(t, "GET", base+"/executions/"+x.ID, nil, "", 200)
	var raw []byte
	requireNoError(t, f.store.DB.QueryRow(`SELECT payload FROM exec_runner_receipts WHERE command_id=$1`, claim.Attempt.CommandID).Scan(&raw))
	var receipt execution.Receipt
	requireNoError(t, json.Unmarshal(raw, &receipt))
	hostname, _ := os.Hostname()
	if receipt.Output["transport"] != "agent" || receipt.Output["hardware_evidence"] != false || receipt.Output["observation"].(map[string]any)["hostname"] != hostname {
		t.Fatal("HTTP result lacks actual bounded host observation")
	}

	// A lost response remains quarantined until the live worker queries the
	// original command and an authorized browser operator consumes its proof.
	input.Operation, input.TimeoutMS, input.WaitMS = "host.wait", 10000, 8000
	requireNoError(t, json.Unmarshal(admin.request(t, "POST", base+"/agent-executions", input, platform.RandomID(), 201), &x))
	claim = claimAgent(t, s, x.ID)
	running := make(chan error, 1)
	go func() { running <- s.RunClaim(ctx, claim) }()
	path := filepath.Join(config.StateDirectory, "tasks", claim.Attempt.CommandID, "receipt.json")
	deadline := time.Now().Add(5 * time.Second)
	for {
		local, err := os.ReadFile(path)
		var observation taskrunner.Observation
		if err == nil && json.Unmarshal(local, &observation) == nil && observation.State == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Agent did not persist running before lost response")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	select {
	case <-done:
		done = nil
	case <-time.After(5 * time.Second):
		t.Fatal("lost task stream did not stop")
	}
	select {
	case e = <-running:
		if e == nil {
			t.Fatal("lost task response was falsely successful")
		}
	case <-time.After(7 * time.Second):
		t.Fatal("worker did not bound the lost response")
	}
	_, e = f.store.DB.Exec(`UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1`, claim.Attempt.ID)
	requireNoError(t, e)
	_, e = s.ReapExpired(ctx)
	requireNoError(t, e)
	var attempt execution.Attempt
	requireNoError(t, json.Unmarshal(admin.request(t, "GET", base+"/executions/"+x.ID, nil, "", 200), &detail))
	if len(detail.Attempts) != 1 || detail.Attempts[0].State != "unknown" {
		t.Fatal("lost Agent HTTP response bypassed unknown isolation")
	}
	attempt = detail.Attempts[0]
	reconcilePath := base + "/executions/" + x.ID + "/attempts/" + attempt.ID + "/reconcile"
	reconcile := execution.ReconcileInput{ExpectedVersion: attempt.StateVersion, Result: "cancelled", Evidence: "Agent HTTP explicit durable receipt recovery"}
	viewer.request(t, "POST", reconcilePath, reconcile, platform.RandomID(), 403)
	proofRequired := admin.request(t, "POST", reconcilePath, reconcile, platform.RandomID(), 409)
	if !bytes.Contains(proofRequired, []byte("RECEIPT_REQUIRED")) {
		t.Fatal("missing Agent receipt did not keep isolation")
	}
	time.Sleep(1100 * time.Millisecond) // existing heartbeat reconnect anti-thrash
	stop, done = startAgentHTTP(t, f, config)
	count, e := s.RefreshAgentReceipts(ctx, 4)
	requireNoError(t, e)
	if count != 1 {
		t.Fatal("live worker failed to recover original Agent proof", count)
	}
	requireNoError(t, f.store.DB.QueryRow(`SELECT payload FROM exec_runner_receipts WHERE command_id=$1`, claim.Attempt.CommandID).Scan(&raw))
	requireNoError(t, json.Unmarshal(raw, &receipt))
	reconcile.Result = receipt.Result
	admin.request(t, "POST", reconcilePath, reconcile, platform.RandomID(), 200)
	requireNoError(t, json.Unmarshal(admin.request(t, "GET", base+"/executions/"+x.ID, nil, "", 200), &detail))
	if detail.Attempts[0].State != receipt.Result || detail.Attempts[0].State == "unknown" {
		t.Fatal("HTTP reconciliation did not consume the real Agent proof")
	}
	requireNoError(t, f.store.RevokeAgentCredential(ctx, f.project, credentialID, 1, "isolated HTTP Agent revocation"))
	list(base + "/agent-targets")
	if listing.Total != 0 || len(listing.Items) != 0 {
		t.Fatal("revoked Agent remained selectable")
	}
	admin.request(t, "POST", base+"/agent-executions", input, platform.RandomID(), 409)
}

func TestAgentReadOnlyLaunchBrowser(t *testing.T) {
	static, script := os.Getenv("KEEL_TEST_AGENT_STATIC_DIR"), os.Getenv("KEEL_TEST_AGENT_BROWSER_SCRIPT")
	if static == "" || script == "" {
		t.Skip("Agent real browser fixture not configured")
	}
	f := newIdentityFixture(t, true)
	f.store.AgentTasksEnabled = true
	config, _ := enrollAgentHTTP(t, f)
	stop, done := startAgentHTTP(t, f, config)
	t.Cleanup(func() {
		stop()
		if done == nil {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("browser Agent did not stop")
		}
	})
	s := execution.New(f.store.DB)
	s.AgentRunner = f.broker
	background := context.Background()
	unknown, e := s.CreateAgent(background, f.actor, f.project, execution.SSHCreateInput{MachineIDs: []string{config.MachineID}, Operation: "host.wait", TimeoutMS: 10000, WaitMS: 8000}, platform.RandomID(), "agent-browser-recovery")
	requireNoError(t, e)
	claim := claimAgent(t, s, unknown.ID)
	running := make(chan error, 1)
	go func() { running <- s.RunClaim(background, claim) }()
	path := filepath.Join(config.StateDirectory, "tasks", claim.Attempt.CommandID, "receipt.json")
	deadline := time.Now().Add(5 * time.Second)
	for {
		local, err := os.ReadFile(path)
		var observation taskrunner.Observation
		if err == nil && json.Unmarshal(local, &observation) == nil && observation.State == "running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("browser recovery fixture did not persist running")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	select {
	case <-done:
		done = nil
	case <-time.After(5 * time.Second):
		t.Fatal("browser fixture lost stream did not stop")
	}
	select {
	case err := <-running:
		if err == nil {
			t.Fatal("browser recovery fixture falsely completed lost response")
		}
	case <-time.After(7 * time.Second):
		t.Fatal("browser recovery fixture worker exceeded bound")
	}
	_, e = f.store.DB.Exec(`UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1`, claim.Attempt.ID)
	requireNoError(t, e)
	_, e = s.ReapExpired(background)
	requireNoError(t, e)
	time.Sleep(1100 * time.Millisecond)
	stop, done = startAgentHTTP(t, f, config)
	count, e := s.RefreshAgentReceipts(background, 4)
	requireNoError(t, e)
	if count != 1 {
		t.Fatal("browser recovery fixture did not fetch original durable proof", count)
	}
	var state string
	var held int
	requireNoError(t, f.store.DB.QueryRow(`SELECT state FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&state))
	requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'`, claim.Attempt.ID).Scan(&held))
	if state != "unknown" || held == 0 {
		t.Fatal("browser recovery fixture did not retain unknown and isolated resources")
	}
	server := agentHTTPServer(t, f, static)
	private := privateDirectory(t)
	credentials := filepath.Join(private, "browser.json")
	viewer := "agent-browser-viewer-" + platform.RandomID()[:10]
	_, e = f.store.CreateUser(background, viewer, "Agent browser viewer", "agent-integration-only-long-password", false, false)
	requireNoError(t, e)
	requireNoError(t, f.store.SetMembership(background, viewer, f.project, "Viewer", false))
	evidence := os.Getenv("KEEL_TEST_AGENT_BROWSER_EVIDENCE_DIR")
	if evidence == "" {
		evidence = private
	}
	data, e := json.Marshal(map[string]string{"base_url": server.URL, "username": f.actor.User.Username, "viewer_username": viewer, "password": "agent-integration-only-long-password", "project_id": f.project, "machine_id": config.MachineID, "serial_number": config.Serial, "evidence_directory": evidence, "unknown_execution_id": unknown.ID, "unknown_attempt_id": claim.Attempt.ID})
	requireNoError(t, e)
	requireNoError(t, agentpki.WriteNew(credentials, data))
	ctx, cancel := context.WithCancel(context.Background())
	workerDone := make(chan error, 1)
	go func() {
		for {
			if ctx.Err() != nil {
				workerDone <- nil
				return
			}
			if _, err := s.PumpOutbox(ctx, "agent-browser-worker", 100); err != nil {
				workerDone <- err
				return
			}
			claim, err := s.Claim(ctx, "agent-browser-worker", 3*time.Second)
			if err != nil {
				workerDone <- err
				return
			}
			if claim != nil {
				if err = s.RunClaim(ctx, claim); err != nil {
					workerDone <- err
					return
				}
			}
			select {
			case <-ctx.Done():
				workerDone <- nil
				return
			case <-time.After(25 * time.Millisecond):
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-workerDone:
			if err != nil && !strings.Contains(err.Error(), "context canceled") {
				t.Error(err)
			}
		case <-time.After(10 * time.Second):
			t.Error("browser worker did not stop")
		}
	})
	browserCtx, browserCancel := context.WithTimeout(ctx, 90*time.Second)
	defer browserCancel()
	command := exec.CommandContext(browserCtx, "node", script, credentials)
	output, e := command.CombinedOutput()
	if e != nil {
		t.Fatalf("Agent browser failed: %v\n%s", e, output)
	}
	requireNoError(t, f.store.DB.QueryRow(`SELECT state FROM exec_task_attempts WHERE id=$1`, claim.Attempt.ID).Scan(&state))
	requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state IN('held','reconciling')`, claim.Attempt.ID).Scan(&held))
	var attempts int
	requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM exec_task_attempts WHERE execution_id=$1 AND command_id=$2`, unknown.ID, claim.Attempt.CommandID).Scan(&attempts))
	if state == "unknown" || held != 0 || attempts != 1 {
		t.Fatal("browser did not explicitly reconcile original proof and release quarantine without replay", state, held, attempts)
	}
	t.Logf("actual Agent browser passed: %s", output)
}
