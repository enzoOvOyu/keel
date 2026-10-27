package execution

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"github.com/enzoOvOyu/keel/internal/netbaseline"
	"github.com/enzoOvOyu/keel/internal/platform"
)

// P2 milestone cases are authored here but run only in the concentrated batch.
// The collector is explicitly a protocol fixture, never hardware evidence.
type issueVerificationFixture struct {
	queryPending   atomic.Bool
	queryMu        sync.Mutex
	terminal       *fabricboot.CollectorRecollectObservation
	store          *platform.Store
	service        *Service
	root, engineer *Actor
	project, issue string
	switches       []string
	fault          atomic.Bool
	lost           atomic.Bool
	posts          atomic.Int32
}

func issueVerificationKernel(t *testing.T) *issueVerificationFixture {
	t.Helper()
	dsn := os.Getenv("KEEL_TEST_AGENT_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated disposable execution database absent")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Port() != "15433" || !strings.HasPrefix(u.Path, "/keel_test_agent_") {
		t.Fatal("refusing non-disposable execution database")
	}
	ctx := context.Background()
	store, e := platform.Open(ctx, dsn)
	mustAgent(t, e)
	t.Cleanup(func() { store.Close() })
	mustAgent(t, store.Migrate(ctx))
	suffix := platform.RandomID()[:10]
	password := "p2-issue-test-only-long-password"
	var initialized bool
	mustAgent(t, store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM users WHERE is_admin)").Scan(&initialized))
	rootName, engineerName := "issue-root-"+suffix, "issue-ne-"+suffix
	_, e = store.CreateUser(ctx, rootName, rootName, password, true, !initialized)
	mustAgent(t, e)
	_, e = store.CreateUser(ctx, engineerName, engineerName, password, false, false)
	mustAgent(t, e)
	login := func(name string) *Actor {
		t.Helper()
		pre, _, err := store.NewPreSession(ctx)
		mustAgent(t, err)
		_, a, err := store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: name, Password: password, Method: "local"}, "198.51.100.184", "issue-verification")
		mustAgent(t, err)
		return a
	}
	root := login(rootName)
	project, e := store.CreateProject(ctx, root, platform.CreateProjectInput{Name: "Issue verification " + suffix, Slug: "issue-verify-" + suffix}, platform.RandomID(), "test")
	mustAgent(t, e)
	mustAgent(t, store.SetMembership(ctx, engineerName, project.ID, "NE", false))
	f := &issueVerificationFixture{store: store, root: root, engineer: login(engineerName), project: project.ID, service: New(store.DB)}
	f.service.IssueStore = store
	store.FabricIssueVerificationEnabled = true
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/v1/projects/"+project.ID+"/recollections/") {
			f.queryMu.Lock()
			defer f.queryMu.Unlock()
			if f.queryPending.Load() || f.terminal == nil {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(f.terminal)
			return
		}
		if r.Method != "POST" || r.URL.Path != "/v1/projects/"+project.ID+"/recollections" {
			w.WriteHeader(404)
			return
		}
		f.posts.Add(1)
		var request fabricboot.CollectorRecollectRequest
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Validate() != nil {
			w.WriteHeader(400)
			return
		}
		out := fabricboot.CollectorRecollectResponse{Version: request.Version, ProjectID: request.ProjectID, ExecutionID: request.ExecutionID, CommandID: request.CommandID, FencingToken: request.FencingToken, ScopeHash: request.ScopeHash, EvidenceSource: "protocol_fixture"}
		for i, target := range request.Targets {
			up := !f.fault.Load()
			remote := request.Targets[(i+1)%len(request.Targets)]
			sample := fabricmon.Snapshot{Version: "fabric-snapshot/v1", Source: "switch", Provider: "p2-protocol-fixture", Serial: target.Serial, CollectedAt: time.Now().UTC(), Capabilities: map[string]bool{"interfaces": true, "lldp": true}}
			for _, port := range target.Ports {
				sample.Ports = append(sample.Ports, fabricmon.Port{Name: port, LinkUp: &up, LLDPReadable: true, Neighbor: &fabricmon.Neighbor{Serial: remote.Serial, PortID: remote.Ports[0]}})
			}
			out.Devices = append(out.Devices, sample)
		}
		finished := time.Now().UTC()
		f.queryMu.Lock()
		f.terminal = &fabricboot.CollectorRecollectObservation{Version: "fabric-recollect-status/v1", ProjectID: request.ProjectID, ExecutionID: request.ExecutionID, CommandID: request.CommandID, FencingToken: request.FencingToken, ScopeHash: request.ScopeHash, RequestHash: netbaseline.ContentHash(request), EvidenceSource: request.EvidenceSource, State: "completed", FinishedAt: &finished, Result: &out}
		f.queryMu.Unlock()
		if f.lost.Load() {
			w.WriteHeader(503)
			return
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	server.TLS = &tls.Config{ClientAuth: tls.RequireAnyClientCert, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	directory := t.TempDir()
	caFile, certFile, keyFile := filepath.Join(directory, "ca.pem"), filepath.Join(directory, "client.pem"), filepath.Join(directory, "client-key.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.TLS.Certificates[0].Certificate[0]})
	key, e := x509.MarshalPKCS8PrivateKey(server.TLS.Certificates[0].PrivateKey)
	mustAgent(t, e)
	mustAgent(t, os.WriteFile(caFile, certificate, 0600))
	mustAgent(t, os.WriteFile(certFile, certificate, 0600))
	mustAgent(t, os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600))
	store.FabricProbes, e = fabricboot.NewProbeSuite(fabricboot.ProbeConfig{Version: "fabric-probes/v1", Targets: map[string]fabricboot.ProbeTarget{"collector": {URL: server.URL, CAFile: caFile, CertificateFile: certFile, KeyFile: keyFile}}})
	mustAgent(t, e)
	exec := func(q string, args ...any) {
		t.Helper()
		_, err := store.DB.ExecContext(ctx, q, args...)
		mustAgent(t, err)
	}
	pkg, profile := platform.RandomID(), platform.RandomID()
	swA, swB, portA, portB, cable := platform.RandomID(), platform.RandomID(), platform.RandomID(), platform.RandomID(), platform.RandomID()
	f.switches = []string{swA, swB}
	exec(`INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) VALUES($1,$2,$3,'approved',$4,now())`, profile, project.ID, suffix, root.User.ID)
	exec(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,$3,'bom-import/v1',$3,'sha256:'||repeat('a',64),'test','{}',5,'committed',$4,now())`, pkg, project.ID, suffix, root.User.ID)
	for _, device := range []struct{ id, serial string }{{swA, "ISSUE-A-" + suffix}, {swB, "ISSUE-B-" + suffix}} {
		exec(`INSERT INTO bom_items(item_uid,category,serial,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'switch',$2,'design_bom',$3,now(),now())`, device.id, device.serial, project.ID)
		exec(`INSERT INTO switches(id,serial_number,hostname,created_at,updated_at) VALUES($1,$2,$2,now(),now())`, device.id, device.serial)
	}
	for _, port := range []struct{ id, owner string }{{portA, swA}, {portB, swB}} {
		exec(`INSERT INTO bom_items(item_uid,category,parent_uid,port_index,port_name,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'port',$2,'1','swp1','design_bom',$3,now(),now())`, port.id, port.owner, project.ID)
		exec(`INSERT INTO physical_ports(id,switch_id,parent_uid,port_index,port_name,created_at) VALUES($1,$2,$2,'1','swp1',now())`, port.id, port.owner)
	}
	if portA > portB {
		portA, portB = portB, portA
	}
	exec(`INSERT INTO bom_items(item_uid,category,endpoint_a_uid,endpoint_b_uid,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'cable',$2,$3,'design_bom',$4,now(),now())`, cable, portA, portB, project.ID)
	for i, id := range []string{swA, swB, portA, portB, cable} {
		exec(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,$3,$4,'{}',now())`, pkg, id, i+1, project.ID)
	}
	observed := time.Now().UTC().Add(-time.Second)
	issue, e := store.CreateIssue(ctx, root, project.ID, platform.CreateIssueInput{Title: "Fixture link down", Category: "T1", Source: "fabric", Phase: "delivery", Anchors: []platform.IssueAnchor{{Kind: "cable", ID: cable}}, Evidence: platform.IssueEvidence{ObservedAt: observed, SourceVersion: fabricmon.CableComparisonRuleRef, Expected: "Both switch ports up with reciprocal LLDP", Actual: "Protocol fixture link down"}}, platform.RandomID(), "test")
	mustAgent(t, e)
	f.issue = issue.ID
	exec(`INSERT INTO fabric_issue_bindings(issue_id,project_id,cable_uid,category,evidence_origin,created_at) VALUES($1,$2,$3,'T1','protocol_fixture',now())`, issue.ID, project.ID, cable)
	run := platform.RandomID()
	report, _ := json.Marshal(map[string]any{"evidence_source": "protocol_fixture"})
	finding, _ := json.Marshal(fabricmon.Finding{CableUID: cable, A: portA, B: portB, Verdict: "anomaly", IssueType: "T1", Source: "switch", RuleRef: fabricmon.CableComparisonRuleRef})
	exec(`INSERT INTO fabric_collection_runs(id,project_id,package_id,report,source_hash,collected_at,valid_until,created_by) VALUES($1,$2,$3,$4,repeat('a',64),$5,$6,$7)`, run, project.ID, pkg, report, observed, observed.Add(5*time.Minute), root.User.ID)
	exec(`INSERT INTO fabric_issue_observations(id,issue_id,project_id,evidence_origin,run_id,package_id,observation_hash,finding,observed_at,valid_until,recorded_at) VALUES($1,$2,$3,'protocol_fixture',$4,$5,repeat('b',64),$6,$7,$8,now())`, platform.RandomID(), issue.ID, project.ID, run, pkg, finding, observed, observed.Add(5*time.Minute))
	f.repair(t)
	return f
}
func (f *issueVerificationFixture) repair(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	detail, e := f.store.GetIssue(ctx, f.root, f.issue)
	mustAgent(t, e)
	if detail.Issue.State == "open" || detail.Issue.State == "reopened" {
		issue, err := f.store.ActOnIssue(ctx, f.root, f.issue, "assign", platform.IssueActionInput{Version: detail.Issue.Version, Reason: "Assign fixture repair", AssigneeID: f.engineer.User.ID}, "test")
		mustAgent(t, err)
		_, err = f.store.ActOnIssue(ctx, f.engineer, f.issue, "start", platform.IssueActionInput{Version: issue.Version, Reason: "Start fixture repair"}, "test")
		mustAgent(t, err)
	}
	detail, e = f.store.GetIssue(ctx, f.engineer, f.issue)
	mustAgent(t, e)
	_, e = f.store.ActOnIssue(ctx, f.engineer, f.issue, "repair", platform.IssueActionInput{Version: detail.Issue.Version, Reason: "Reseat fixture cable", RepairAction: "reseat"}, "test")
	mustAgent(t, e)
}
func (f *issueVerificationFixture) claim(t *testing.T) *Claim {
	t.Helper()
	ctx := context.Background()
	scope, e := f.store.IssueVerificationScope(ctx, f.engineer, f.issue)
	mustAgent(t, e)
	if !scope.ExecutionAvailable {
		t.Fatalf("fixture scope blocked: %+v", scope.Blockers)
	}
	in := platform.IssueVerificationInput{Version: scope.IssueVersion, ScopeHash: scope.ScopeHash, Reason: "Verify repaired fixture link"}
	key := platform.RandomID()
	x, e := f.service.CreateIssueVerification(ctx, f.engineer, f.project, f.issue, in, key, "test")
	mustAgent(t, e)
	replay, e := f.service.CreateIssueVerification(ctx, f.engineer, f.project, f.issue, in, key, "test")
	mustAgent(t, e)
	if replay.ID != x.ID {
		t.Fatal("idempotency created another verification")
	}
	_, e = f.service.PumpOutbox(ctx, "issue-verification", 1000)
	mustAgent(t, e)
	claim, e := f.service.claimExecution(ctx, x.ID, "issue-verification", time.Minute)
	mustAgent(t, e)
	if claim == nil {
		t.Fatal("verification was not claimable")
	}
	var leases int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND fencing_token=$2 AND state='held' AND mode='exclusive' AND resource_id IN($3,$4)`, claim.Attempt.ID, claim.Attempt.FencingToken, "ssh:switch:"+f.switches[0], "ssh:switch:"+f.switches[1]).Scan(&leases))
	if leases != 2 {
		t.Fatalf("expected both exclusive switch leases, got %d", leases)
	}
	return claim
}
func TestIssueVerificationExecutionEvidenceAndClosurePostgres(t *testing.T) {
	f := issueVerificationKernel(t)
	ctx := context.Background()
	claim := f.claim(t)
	mustAgent(t, f.service.RunClaim(ctx, claim))
	history, e := f.store.IssueVerifications(ctx, f.engineer, f.issue, platform.Pagination{Page: 1, PageSize: 10})
	mustAgent(t, e)
	if len(history.Items) != 1 || history.Items[0].Verdict != "PASS" || !history.Items[0].CanClose || history.Items[0].ExecutionState != "success" {
		t.Fatalf("PASS did not propose valid closure: %+v", history.Items)
	}
	detail, e := f.store.GetIssue(ctx, f.engineer, f.issue)
	mustAgent(t, e)
	closed, e := f.store.ActOnIssue(ctx, f.engineer, f.issue, "close", platform.IssueActionInput{Version: detail.Issue.Version, Reason: "Confirm scoped fixture verification"}, "test")
	mustAgent(t, e)
	if closed.State != "closed" || closed.Source != "fabric" || closed.Evidence.Actual != "Protocol fixture link down" {
		t.Fatalf("closure changed original discovery: %+v", closed)
	}
	if _, e = f.store.DB.ExecContext(ctx, `UPDATE issue_verification_results SET verdict='FAIL' WHERE request_id=$1`, history.Items[0].Request.ID); e == nil {
		t.Fatal("verification result was mutable")
	}
}
func TestIssueVerificationFailureStreakEscalatesOncePostgres(t *testing.T) {
	f := issueVerificationKernel(t)
	ctx := context.Background()
	f.fault.Store(true)
	for i := 0; i < 3; i++ {
		if i > 0 {
			f.repair(t)
		}
		mustAgent(t, f.service.RunClaim(ctx, f.claim(t)))
		detail, e := f.store.GetIssue(ctx, f.root, f.issue)
		mustAgent(t, e)
		if detail.Issue.State != "reopened" {
			t.Fatalf("failed verification did not reopen: %+v", detail.Issue)
		}
		var notices int
		mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT count(*) FROM issue_escalations WHERE issue_id=$1", f.issue).Scan(&notices))
		expected := 0
		if i > 0 {
			expected = 1
		}
		if notices != expected {
			t.Fatalf("failure %d produced %d escalations", i+1, notices)
		}
	}
}
func TestIssueVerificationUnknownNeverRepostsOrUnlocksPostgres(t *testing.T) {
	f := issueVerificationKernel(t)
	ctx := context.Background()
	f.lost.Store(true)
	claim := f.claim(t)
	if f.service.RunClaim(ctx, claim) == nil {
		t.Fatal("lost recollection produced successful completion")
	}
	_, e := f.store.DB.ExecContext(ctx, "UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1", claim.Attempt.ID)
	mustAgent(t, e)
	_, e = f.service.ReapExpired(ctx)
	mustAgent(t, e)
	var pinned, receipts int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'", claim.Attempt.ID).Scan(&pinned))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_runner_receipts WHERE command_id=$1", claim.Attempt.CommandID).Scan(&receipts))
	if pinned != 2 || receipts != 0 || f.posts.Load() != 1 {
		t.Fatalf("unknown replayed or unlocked: leases=%d receipts=%d posts=%d", pinned, receipts, f.posts.Load())
	}
	detail, e := f.store.GetIssue(ctx, f.engineer, f.issue)
	mustAgent(t, e)
	if _, e = f.store.ActOnIssue(ctx, f.engineer, f.issue, "repair", platform.IssueActionInput{Version: detail.Issue.Version, Reason: "Do not bypass active quarantine", RepairAction: "reseat"}, "test"); e == nil {
		t.Fatal("repair bypassed unknown resource quarantine")
	}
	var version int64
	mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT state_version FROM exec_task_attempts WHERE id=$1", claim.Attempt.ID).Scan(&version))
	f.queryPending.Store(true)
	if _, e = f.service.FetchIssueVerificationReceipt(ctx, f.root, f.project, claim.Attempt.ExecutionID, claim.Attempt.ID, version, platform.RandomID(), "test"); e == nil {
		t.Fatal("missing collector proof released quarantine")
	}
	mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'", claim.Attempt.ID).Scan(&pinned))
	if pinned != 2 {
		t.Fatal("query without proof released switch resources")
	}
	f.queryPending.Store(false)
	key := platform.RandomID()
	recovered, e := f.service.FetchIssueVerificationReceipt(ctx, f.root, f.project, claim.Attempt.ExecutionID, claim.Attempt.ID, version, key, "test")
	mustAgent(t, e)
	if recovered.State != "success" || f.posts.Load() != 1 {
		t.Fatalf("recovery redispatched or failed: %+v posts=%d", recovered, f.posts.Load())
	}
	replay, e := f.service.FetchIssueVerificationReceipt(ctx, f.root, f.project, claim.Attempt.ExecutionID, claim.Attempt.ID, version, key, "test")
	mustAgent(t, e)
	if replay.ID != recovered.ID || f.posts.Load() != 1 {
		t.Fatal("replayed recovery changed the execution")
	}
	mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'", claim.Attempt.ID).Scan(&pinned))
	if pinned != 0 {
		t.Fatal("terminal proof did not release quarantined resources")
	}
}

func TestIssueVerificationLocalUndispatchedRecoveryPostgres(t *testing.T) {
	f := issueVerificationKernel(t)
	ctx := context.Background()
	claim := f.claim(t)
	// Crash after Claim and before committing the first collector dispatch.
	_, e := f.store.DB.ExecContext(ctx, "UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1", claim.Attempt.ID)
	mustAgent(t, e)
	_, e = f.service.ReapExpired(ctx)
	mustAgent(t, e)
	var version int64
	mustAgent(t, f.store.DB.QueryRowContext(ctx, "SELECT state_version FROM exec_task_attempts WHERE id=$1", claim.Attempt.ID).Scan(&version))
	x, e := f.service.FetchIssueVerificationReceipt(ctx, f.root, f.project, claim.Attempt.ExecutionID, claim.Attempt.ID, version, platform.RandomID(), "test")
	mustAgent(t, e)
	if x.State != "failed" || f.posts.Load() != 0 {
		t.Fatal("undispatched recovery fabricated a successful sample or contacted the collector")
	}
	history, e := f.store.IssueVerifications(ctx, f.root, f.issue, platform.Pagination{Page: 1, PageSize: 10})
	mustAgent(t, e)
	if len(history.Items) != 1 || history.Items[0].Verdict != "BLOCKED" || history.Items[0].CanClose {
		t.Fatalf("local nonstart became successful verification: %+v", history.Items)
	}
	// The original worker must not POST after the explicit no-dispatch proof.
	if f.service.RunClaim(ctx, claim) == nil || f.posts.Load() != 0 {
		t.Fatal("late worker dispatched an already reconciled command")
	}
}
