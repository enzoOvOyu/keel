package taskrunner_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

type trustAdmissionFixture struct {
	t                      *testing.T
	ctx                    context.Context
	store                  *platform.Store
	service                *execution.Service
	ssh                    *sshFixture
	project                *platform.ProjectSummary
	creator, approver      *platform.AuthSession
	machine, pin, password string
}

func newTrustAdmissionFixture(t *testing.T) *trustAdmissionFixture {
	t.Helper()
	dsn := os.Getenv("KEEL_TEST_SSH_DATABASE_URL")
	if dsn == "" {
		t.Skip("KEEL_TEST_SSH_DATABASE_URL absent; actual trust admission PostgreSQL/SSH proof not run")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Port() != "15433" || !strings.HasPrefix(u.Path, "/keel_test_ssh") {
		t.Fatal("explicit disposable SSH database required")
	}
	f := &trustAdmissionFixture{t: t, ctx: context.Background(), password: "trust-admission-" + platform.RandomID(), machine: platform.RandomID()}
	f.store, e = platform.Open(f.ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { f.store.Close() })
	if e = f.store.Migrate(f.ctx); e != nil {
		t.Fatal(e)
	}
	f.creator = f.newAdmin("requester")
	f.approver = f.newAdmin("reviewer")
	f.project, e = f.store.CreateProject(f.ctx, f.creator, platform.CreateProjectInput{Name: "Trust admission isolation", Slug: "trust-admit-" + platform.RandomID()[:10]}, platform.RandomID(), "trust-admission")
	if e != nil {
		t.Fatal(e)
	}
	f.ssh = fixture(t)
	var cfg map[string][]taskrunner.TargetConfig
	raw, e := os.ReadFile(f.ssh.configFile)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &cfg); e != nil {
		t.Fatal(e)
	}
	target := &cfg["targets"][0]
	target.ProjectID = f.project.ID
	target.MachineID = f.machine
	target.CredentialVersion = "admission-v1"
	expiry := time.Now().UTC().Add(45 * time.Minute).Truncate(time.Microsecond)
	target.CredentialExpiresAt = &expiry
	f.pin = target.HostKey
	raw, e = json.Marshal(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(f.ssh.configFile, raw, 0600); e != nil {
		t.Fatal(e)
	}
	f.ssh.runner.Close()
	f.ssh.runner, e = taskrunner.Load(f.ssh.configFile)
	if e != nil {
		t.Fatal(e)
	}
	f.store.SSHRunner = f.ssh.runner
	if _, e = f.store.DB.ExecContext(f.ctx, `INSERT INTO machines(id,serial_number,ssh_trust,created_at,updated_at)VALUES($1,$2,'unverified',now(),now())`, f.machine, "trust-admission-"+f.machine); e != nil {
		t.Fatal(e)
	}
	if _, e = f.store.DB.ExecContext(f.ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at)VALUES($1,$2,$3,$4,'provisioned',now(),now())`, platform.RandomID(), f.project.ID, f.machine, "admission-"+f.machine[:8]); e != nil {
		t.Fatal(e)
	}
	f.service = execution.New(f.store.DB)
	f.service.SSHRunner = f.ssh.runner
	return f
}
func (f *trustAdmissionFixture) login(name string) *platform.AuthSession {
	f.t.Helper()
	pre, _, e := f.store.NewPreSession(f.ctx)
	if e != nil {
		f.t.Fatal(e)
	}
	_, a, e := f.store.Login(f.ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: name, Password: f.password, Method: "local"}, "127.0.0.1", "trust-admission")
	if e != nil {
		f.t.Fatal(e)
	}
	return a
}
func (f *trustAdmissionFixture) newAdmin(prefix string) *platform.AuthSession {
	f.t.Helper()
	name := prefix + platform.RandomID()[:10]
	if _, e := f.store.CreateUser(f.ctx, name, name, f.password, true, false); e != nil {
		f.t.Fatal(e)
	}
	return f.login(name)
}
func (f *trustAdmissionFixture) approved() *platform.SSHTrustRequest {
	f.t.Helper()
	r, e := f.store.CreateSSHTrustRequest(f.ctx, f.creator, f.project, platform.CreateSSHTrustRequestInput{MachineID: f.machine, Action: "trust.bootstrap", ExpectedAssetVersion: 1, HostKey: f.pin, Reason: "independent fixture host key reviewed before restricted connection", ExpiresInSeconds: 900}, platform.RandomID(), "trust-admission")
	if e != nil {
		f.t.Fatal(e)
	}
	r, e = f.store.ActSSHTrustRequest(f.ctx, f.approver, f.project, r.ID, "approve", platform.SSHTrustRequestActionInput{ExpectedVersion: r.StateVersion}, platform.RandomID(), "trust-admission")
	if e != nil {
		f.t.Fatal(e)
	}
	return r
}
func (f *trustAdmissionFixture) execute(r *platform.SSHTrustRequest, a *platform.AuthSession) *execution.Execution {
	f.t.Helper()
	x, e := f.service.CreateSSHTrustVerification(f.ctx, a, f.project.ID, r.ID, r.StateVersion, platform.RandomID(), "trust-admission")
	if e != nil {
		f.t.Fatal(e)
	}
	return x
}
func (f *trustAdmissionFixture) claim(id string) *execution.Claim {
	f.t.Helper()
	if _, e := f.service.PumpOutbox(f.ctx, "trust-admission-"+f.project.ID, 1000); e != nil {
		f.t.Fatal(e)
	}
	c, e := f.service.Claim(f.ctx, "trust-admission-"+f.project.ID, 2*time.Second)
	if e != nil || c == nil || c.Attempt.ExecutionID != id {
		f.t.Fatalf("expected own claim %s, got %#v: %v", id, c, e)
	}
	return c
}
func (f *trustAdmissionFixture) status(request string) (string, string, string) {
	f.t.Helper()
	var trust, source, state string
	if e := f.store.DB.QueryRowContext(f.ctx, `SELECT m.ssh_trust,m.ssh_trust_source,r.state FROM machines m JOIN ssh_trust_requests r ON r.machine_id=m.id WHERE m.id=$1 AND r.id=$2`, f.machine, request).Scan(&trust, &source, &state); e != nil {
		f.t.Fatal(e)
	}
	return trust, source, state
}
func (f *trustAdmissionFixture) forceUnknown(c *execution.Claim) int64 {
	f.t.Helper()
	// Explicit control-plane lease-expiry fault; no target receipt or trust data
	// is manufactured, and immutable approval snapshots remain untouched.
	if _, e := f.store.DB.ExecContext(f.ctx, `UPDATE exec_task_attempts SET lease_until=now()-interval '1 second' WHERE id=$1`, c.Attempt.ID); e != nil {
		f.t.Fatal(e)
	}
	if _, e := f.service.ReapExpired(f.ctx); e != nil {
		f.t.Fatal(e)
	}
	var state string
	var version int64
	if e := f.store.DB.QueryRowContext(f.ctx, `SELECT state,state_version FROM exec_task_attempts WHERE id=$1`, c.Attempt.ID).Scan(&state, &version); e != nil {
		f.t.Fatal(e)
	}
	if state != "unknown" {
		f.t.Fatal("expired attempt lost unknown quarantine", state)
	}
	var held int
	if e := f.store.DB.QueryRowContext(f.ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'`, c.Attempt.ID).Scan(&held); e != nil || held != 2 {
		f.t.Fatal("unknown did not quarantine both device and SSH leases", held, e)
	}
	return version
}
func requireAdmissionStatus(t *testing.T, e error, status int) {
	t.Helper()
	var api *platform.Error
	if !errors.As(e, &api) || api.Status != status {
		t.Fatalf("expected API status%d, got %v", status, e)
	}
}

func TestSSHTrustAdmissionConcurrentCredentialConsumption(t *testing.T) {
	f := newTrustAdmissionFixture(t)
	grants := []*platform.SSHTrustRequest{f.approved(), f.approved()}
	type result struct {
		x *execution.Execution
		e error
	}
	start := make(chan struct{})
	done := make(chan result, 2)
	for _, grant := range grants {
		go func(r *platform.SSHTrustRequest) {
			<-start
			x, e := f.service.CreateSSHTrustVerification(f.ctx, f.creator, f.project.ID, r.ID, r.StateVersion, platform.RandomID(), "trust-admission-concurrent")
			done <- result{x, e}
		}(grant)
	}
	close(start)
	var accepted *execution.Execution
	for range grants {
		r := <-done
		if r.e == nil {
			if accepted != nil {
				t.Fatal("same credential version consumed twice")
			}
			accepted = r.x
		} else {
			requireAdmissionStatus(t, r.e, 409)
		}
	}
	if accepted == nil {
		t.Fatal("neither independent authorization was admitted")
	}
	var count int
	if e := f.store.DB.QueryRowContext(f.ctx, `SELECT count(*) FROM exec_executions WHERE project_id=$1`, f.project.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("concurrent consumption left partial/duplicate executions", count, e)
	}
	c := f.claim(accepted.ID)
	if e := f.service.RunClaim(f.ctx, c); e != nil {
		t.Fatal(e)
	}
	if f.ssh.runs.Load() != 1 {
		t.Fatal("concurrent authorization replayed target work", f.ssh.runs.Load())
	}
	t.Log("PASS actual PostgreSQL concurrent independent grants: exactly one execution and one SSH run; second credential-version consumption rejected409")
}

func TestSSHTrustAdmissionThirdExecutorDemotionBlocksDispatch(t *testing.T) {
	for _, phase := range []string{"before_claim", "before_transport"} {
		t.Run(phase, func(t *testing.T) {
			f := newTrustAdmissionFixture(t)
			name := "executor" + platform.RandomID()[:10]
			u, e := f.store.CreateUser(f.ctx, name, name, f.password, false, false)
			if e != nil {
				t.Fatal(e)
			}
			member, e := f.store.ControlCreateMember(f.ctx, f.creator, f.project.ID, platform.CreateMemberInput{Username: name, Role: "SRE"}, "trust-admission")
			if e != nil {
				t.Fatal(e)
			}
			executor := f.login(name)
			r := f.approved()
			x := f.execute(r, executor)
			var claim *execution.Claim
			if phase == "before_transport" {
				claim = f.claim(x.ID)
			}
			if _, e = f.store.ControlChangeMember(f.ctx, f.creator, f.project.ID, u.ID, platform.UpdateMemberInput{Role: "DE", Version: member.Version}, false, "trust-admission-revoke"); e != nil {
				t.Fatal(e)
			}
			if phase == "before_claim" {
				if _, e = f.service.PumpOutbox(f.ctx, "trust-demotion", 1000); e != nil {
					t.Fatal(e)
				}
				if got, e := f.service.Claim(f.ctx, "trust-demotion", time.Second); e != nil || got != nil {
					t.Fatalf("demoted trust executor claimed: %#v %v", got, e)
				}
				detail, e := f.service.Get(f.ctx, f.project.ID, x.ID)
				if e != nil || detail.Execution.State != "paused" {
					t.Fatal("demoted executor did not pause dispatch", e)
				}
			} else {
				if e = f.service.RunClaim(f.ctx, claim); e == nil {
					t.Fatal("demoted third executor reached transport")
				}
				f.forceUnknown(claim)
			}
			if f.ssh.runs.Load() != 0 {
				t.Fatal("authority revocation reached SSH helper")
			}
			trust, _, _ := f.status(r.ID)
			if trust != "unverified" {
				t.Fatal("role downgrade established trust", trust)
			}
			t.Log("PASS third-party SRE executor demoted to DE is blocked at " + phase + "; creator and approver remained authorized")
		})
	}
}

func TestSSHTrustAdmissionUnknownRequiresOriginalTerminalProof(t *testing.T) {
	f := newTrustAdmissionFixture(t)
	r := f.approved()
	x := f.execute(r, f.creator)
	c := f.claim(x.ID)
	f.ssh.drop.Store(true)
	if e := f.service.RunClaim(f.ctx, c); e == nil {
		t.Fatal("lost terminal response reported trust success")
	}
	f.ssh.drop.Store(false)
	version := f.forceUnknown(c)
	trust, _, state := f.status(r.ID)
	if trust != "unverified" || state != "unknown" || f.ssh.runs.Load() != 1 {
		t.Fatal("unknown fabricated trust or replayed run", trust, state, f.ssh.runs.Load())
	}
	_, e := f.service.Reconcile(f.ctx, f.creator, f.project.ID, x.ID, c.Attempt.ID, execution.ReconcileInput{ExpectedVersion: version, Result: "success", Evidence: "operator assertion without a recorded target receipt"}, platform.RandomID(), "trust-admission")
	requireAdmissionStatus(t, e, 409)
	key := platform.RandomID()
	recovered, e := f.service.FetchSSHReceipt(f.ctx, f.creator, f.project.ID, x.ID, c.Attempt.ID, version, key, "trust-admission")
	if e != nil {
		t.Fatal(e)
	}
	if recovered.State != "success" {
		t.Fatal("original target receipt did not recover execution", recovered.State)
	}
	if _, e = f.service.FetchSSHReceipt(f.ctx, f.creator, f.project.ID, x.ID, c.Attempt.ID, version, key, "trust-admission"); e != nil {
		t.Fatal("recovery idempotent retry failed", e)
	}
	trust, source, state := f.status(r.ID)
	if trust != "established" || source != "ssh" || state != "succeeded" || f.ssh.runs.Load() != 1 {
		t.Fatal("recovery lacked verified trust projection or executed again", trust, source, state, f.ssh.runs.Load())
	}
	t.Log("PASS actual SSH dropped terminal response -> PostgreSQL unknown/reconciling -> original durable query receipt -> authorized atomic trust recovery, with exactly one run")
}

// Expiry fault is injected only after a real SSH run lost its response. The
// transport's natural deadline/connection shutdown is independently exercised
// with real short-lived credentials in credential_test.go. No immutable DB
// authorization, timestamp or proof is edited to accelerate this boundary.
type expiredTrustAdmissionRunner struct{ delegate *taskrunner.SSH }

func (r expiredTrustAdmissionRunner) Binding(string, string) (taskrunner.Binding, error) {
	return taskrunner.Binding{}, taskrunner.ErrRejected
}
func (r expiredTrustAdmissionRunner) Do(context.Context, taskrunner.Request) (taskrunner.Observation, error) {
	return taskrunner.Observation{}, taskrunner.ErrRejected
}
func (r expiredTrustAdmissionRunner) CredentialMetadata(string, string) (taskrunner.CredentialMetadata, error) {
	return taskrunner.CredentialMetadata{}, taskrunner.ErrRejected
}

func TestSSHTrustAdmissionExpiredOrRotatedBindingPreservesUnknown(t *testing.T) {
	for _, fault := range []string{"expired_credential_fault", "rotated_credential_version"} {
		t.Run(fault, func(t *testing.T) {
			f := newTrustAdmissionFixture(t)
			r := f.approved()
			x := f.execute(r, f.creator)
			c := f.claim(x.ID)
			f.ssh.drop.Store(true)
			if e := f.service.RunClaim(f.ctx, c); e == nil {
				t.Fatal("missing receipt settled execution")
			}
			f.ssh.drop.Store(false)
			version := f.forceUnknown(c)
			if fault == "expired_credential_fault" {
				f.service.SSHRunner = expiredTrustAdmissionRunner{f.ssh.runner}
			} else {
				raw, e := os.ReadFile(f.ssh.configFile)
				if e != nil {
					t.Fatal(e)
				}
				var cfg map[string][]taskrunner.TargetConfig
				if e = json.Unmarshal(raw, &cfg); e != nil {
					t.Fatal(e)
				}
				cfg["targets"][0].CredentialVersion = "admission-v2"
				raw, _ = json.Marshal(cfg)
				if e = os.WriteFile(f.ssh.configFile, raw, 0600); e != nil {
					t.Fatal(e)
				}
				rotated, e := taskrunner.Load(f.ssh.configFile)
				if e != nil {
					t.Fatal(e)
				}
				t.Cleanup(rotated.Close)
				f.service.SSHRunner = rotated
			}
			_, e := f.service.FetchSSHReceipt(f.ctx, f.creator, f.project.ID, x.ID, c.Attempt.ID, version, platform.RandomID(), "trust-admission")
			requireAdmissionStatus(t, e, 409)
			trust, _, state := f.status(r.ID)
			if trust != "unverified" || state != "unknown" || f.ssh.runs.Load() != 1 {
				t.Fatal("expired/rotated binding changed trust or repeated command", trust, state, f.ssh.runs.Load())
			}
			var reconciling int
			if e = f.store.DB.QueryRowContext(f.ctx, `SELECT count(*) FROM exec_resource_leases WHERE attempt_id=$1 AND state='reconciling'`, c.Attempt.ID).Scan(&reconciling); e != nil || reconciling != 2 {
				t.Fatal("expiry/rotation released quarantined device or SSH lease", reconciling, e)
			}
			t.Log("PASS " + fault + ": original SSH run is durable, but unavailable frozen authorization keeps trust unverified and leases reconciling; no automatic repair or replay")
		})
	}
}
