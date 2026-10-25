package platform

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
	"golang.org/x/crypto/ssh"
)

func TestSSHTrustRequestAuthorizationAndEvidenceBoundaries(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "ssh-trust-domain-test-only-password"
	if _, e := s.CreateUser(ctx, "ssh-trust-admin", "Admin", password, true, true); e != nil {
		t.Fatal(e)
	}
	_, admin := testLogin(t, s, "ssh-trust-admin", password, "198.51.100.120")
	project, e := s.CreateProject(ctx, admin, CreateProjectInput{Name: "SSH trust", Slug: "ssh-trust-domain"}, "ssh-trust-project", "test")
	if e != nil {
		t.Fatal(e)
	}
	actors := map[string]*AuthSession{"Admin": admin}
	for _, role := range []string{"DL", "SRE", "Viewer"} {
		name := "ssh-trust-" + strings.ToLower(role)
		if _, e = s.CreateUser(ctx, name, role, password, false, false); e != nil {
			t.Fatal(e)
		}
		if _, e = s.ControlCreateMember(ctx, admin, project.ID, CreateMemberInput{Username: name, Role: role}, "test"); e != nil {
			t.Fatal(e)
		}
		_, actors[role] = testLogin(t, s, name, password, "198.51.100.121")
	}
	machine := RandomID()
	_, e = s.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,created_at,updated_at)VALUES($1,'SSH-TRUST-DOMAIN',now(),now())`, machine)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at)VALUES($1,$2,$3,'ssh-trust-fixture','provisioned',now(),now())`, RandomID(), project.ID, machine)
	if e != nil {
		t.Fatal(e)
	}
	_, private, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := ssh.NewSignerFromKey(private)
	if e != nil {
		t.Fatal(e)
	}
	host := ssh.FingerprintSHA256(signer.PublicKey())
	der, e := x509.MarshalPKCS8PrivateKey(private)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	keyfile := filepath.Join(dir, "credential.pem")
	if e = os.WriteFile(keyfile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); e != nil {
		t.Fatal(e)
	}
	makeRunner := func(expiry time.Time) *taskrunner.SSH {
		t.Helper()
		cfg := map[string]any{"targets": []map[string]any{{"project_id": project.ID, "machine_id": machine, "address": "127.0.0.1:65534", "user": "keel-ctrl", "host_key": host, "credential_ref": "test-domain-ref", "credential_version": "fixture-v1", "credential_expires_at": expiry, "private_key_file": keyfile}}}
		raw, _ := json.Marshal(cfg)
		file := filepath.Join(dir, RandomID()+".json")
		if e = os.WriteFile(file, raw, 0600); e != nil {
			t.Fatal(e)
		}
		runner, e := taskrunner.Load(file)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(runner.Close)
		return runner
	}
	input := CreateSSHTrustRequestInput{MachineID: machine, Action: "trust.bootstrap", ExpectedAssetVersion: 1, HostKey: host, Reason: "Host key checked against independent inventory", ExpiresInSeconds: 900}
	_, e = s.CreateSSHTrustRequest(ctx, admin, project, input, "disabled", "test")
	assertErrorCode(t, e, "SSH_DISABLED")
	s.SSHRunner = makeRunner(time.Now().Add(time.Hour))
	wrong := input
	wrong.HostKey = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	_, e = s.CreateSSHTrustRequest(ctx, admin, project, wrong, "wrong-pin", "test")
	assertErrorCode(t, e, "SSH_TRUST_HOST_KEY_MISMATCH")
	wrong = input
	wrong.ExpectedAssetVersion = 2
	_, e = s.CreateSSHTrustRequest(ctx, admin, project, wrong, "stale", "test")
	assertErrorCode(t, e, "VERSION_CONFLICT")
	_, e = s.CreateSSHTrustRequest(ctx, actors["Viewer"], project, input, "viewer", "test")
	assertErrorCode(t, e, "FORBIDDEN")
	x, e := s.CreateSSHTrustRequest(ctx, actors["SRE"], project, input, "create", "test")
	if e != nil {
		t.Fatal(e)
	}
	if x.State != "pending" || x.StateVersion != 1 || x.CreatedBy != actors["SRE"].User.ID || x.CredentialVersion != "fixture-v1" {
		t.Fatalf("bad request: %+v", x)
	}
	repeat, e := s.CreateSSHTrustRequest(ctx, actors["SRE"], project, input, "create", "test")
	if e != nil || repeat.ID != x.ID {
		t.Fatalf("idempotency failed: %+v %v", repeat, e)
	}
	t.Run("approval_rechecks_requester_revocation_after_wait", func(t *testing.T) {
		pending, err := s.CreateSSHTrustRequest(ctx, actors["SRE"], project, input, "race-requester", "test")
		if err != nil {
			t.Fatal(err)
		}
		blocker, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer blocker.Rollback()
		var pid int
		if err = blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		var id string
		if err = blocker.QueryRowContext(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, actors["SRE"].User.ID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		approvalCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, err := s.ActSSHTrustRequest(approvalCtx, actors["DL"], project, pending.ID, "approve", SSHTrustRequestActionInput{ExpectedVersion: 1}, "race-approval", "test")
			result <- err
		}()
		blocked := false
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)))`, pid).Scan(&blocked); err != nil {
				t.Fatal(err)
			}
			if blocked {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if !blocked {
			t.Fatal("approval did not pin requester authority before writing")
		}
		if _, err = blocker.ExecContext(ctx, `UPDATE memberships SET role='Viewer' WHERE project_id=$1 AND user_id=$2`, project.ID, actors["SRE"].User.ID); err != nil {
			t.Fatal(err)
		}
		if err = blocker.Commit(); err != nil {
			t.Fatal(err)
		}
		err = <-result
		assertErrorCode(t, err, "SSH_TRUST_CREATOR_REVOKED")
		if _, err = s.DB.ExecContext(ctx, `UPDATE memberships SET role='SRE' WHERE project_id=$1 AND user_id=$2`, project.ID, actors["SRE"].User.ID); err != nil {
			t.Fatal(err)
		}
		var state string
		var version int64
		if err = s.DB.QueryRowContext(ctx, `SELECT state,state_version FROM ssh_trust_requests WHERE id=$1`, pending.ID).Scan(&state, &version); err != nil || state != "pending" || version != 1 {
			t.Fatalf("revoked approval changed request: %s %d %v", state, version, err)
		}
	})
	t.Run("concurrent_approve_and_revoke_serialize", func(t *testing.T) {
		pending, err := s.CreateSSHTrustRequest(ctx, actors["SRE"], project, input, "race-actions", "test")
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		type outcome struct {
			state string
			err   error
		}
		results := make(chan outcome, 2)
		for _, action := range []string{"approve", "revoke"} {
			go func(action string) {
				<-start
				reason := ""
				if action == "revoke" {
					reason = "Withdraw before dispatch"
				}
				out, err := s.ActSSHTrustRequest(ctx, actors["DL"], project, pending.ID, action, SSHTrustRequestActionInput{ExpectedVersion: 1, Reason: reason}, "race-"+action, "test")
				state := ""
				if out != nil {
					state = out.State
				}
				results <- outcome{state, err}
			}(action)
		}
		close(start)
		successes := 0
		for range 2 {
			out := <-results
			if out.err == nil {
				successes++
				if out.state != "approved" && out.state != "revoked" {
					t.Fatalf("unexpected terminal state %s", out.state)
				}
			} else {
				assertErrorCode(t, out.err, "VERSION_CONFLICT")
			}
		}
		if successes != 1 {
			t.Fatalf("%d concurrent actions committed", successes)
		}
	})
	wrong = input
	wrong.Reason = "Changed request"
	_, e = s.CreateSSHTrustRequest(ctx, actors["SRE"], project, wrong, "create", "test")
	assertErrorCode(t, e, "IDEMPOTENCY_CONFLICT")
	_, e = s.ActSSHTrustRequest(ctx, actors["SRE"], project, x.ID, "approve", SSHTrustRequestActionInput{ExpectedVersion: 1}, "sre-approve", "test")
	assertErrorCode(t, e, "FORBIDDEN")
	own, e := s.CreateSSHTrustRequest(ctx, admin, project, input, "own", "test")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.ActSSHTrustRequest(ctx, admin, project, own.ID, "approve", SSHTrustRequestActionInput{ExpectedVersion: 1}, "self-approve", "test")
	assertErrorCode(t, e, "SSH_TRUST_INDEPENDENT_APPROVAL_REQUIRED")
	if _, e = s.DB.ExecContext(ctx, `UPDATE memberships SET role='Viewer' WHERE project_id=$1 AND user_id=$2`, project.ID, actors["SRE"].User.ID); e != nil {
		t.Fatal(e)
	}
	_, e = s.ActSSHTrustRequest(ctx, actors["DL"], project, x.ID, "approve", SSHTrustRequestActionInput{ExpectedVersion: 1}, "revoked-requester", "test")
	assertErrorCode(t, e, "SSH_TRUST_CREATOR_REVOKED")
	if _, e = s.DB.ExecContext(ctx, `UPDATE memberships SET role='SRE' WHERE project_id=$1 AND user_id=$2`, project.ID, actors["SRE"].User.ID); e != nil {
		t.Fatal(e)
	}
	approved, e := s.ActSSHTrustRequest(ctx, actors["DL"], project, x.ID, "approve", SSHTrustRequestActionInput{ExpectedVersion: 1}, "approve", "test")
	if e != nil || approved.State != "approved" || approved.StateVersion != 2 || approved.ApprovedBy != actors["DL"].User.ID {
		t.Fatalf("approval: %+v %v", approved, e)
	}
	repeat, e = s.ActSSHTrustRequest(ctx, actors["DL"], project, x.ID, "approve", SSHTrustRequestActionInput{ExpectedVersion: 1}, "approve", "test")
	if e != nil || repeat.ID != x.ID || repeat.StateVersion != 2 {
		t.Fatalf("approval retry: %+v %v", repeat, e)
	}
	_, e = s.ActSSHTrustRequest(ctx, actors["DL"], project, x.ID, "revoke", SSHTrustRequestActionInput{ExpectedVersion: 1, Reason: "Stale"}, "stale-revoke", "test")
	assertErrorCode(t, e, "VERSION_CONFLICT")
	_, e = s.DB.ExecContext(ctx, `UPDATE ssh_trust_requests SET host_key='SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA',state_version=state_version+1 WHERE id=$1`, x.ID)
	if e == nil {
		t.Fatal("frozen host key was mutable")
	}
	revoked, e := s.ActSSHTrustRequest(ctx, admin, project, x.ID, "revoke", SSHTrustRequestActionInput{ExpectedVersion: 2, Reason: "Cancel unused authorization"}, "revoke", "test")
	if e != nil || revoked.State != "revoked" {
		t.Fatalf("revoke: %+v %v", revoked, e)
	}
	var trust string
	if e = s.DB.QueryRowContext(ctx, `SELECT ssh_trust FROM machines WHERE id=$1`, machine).Scan(&trust); e != nil || trust != "unverified" {
		t.Fatalf("approval falsely established trust: %s %v", trust, e)
	}
	list, e := s.ListSSHTrustRequests(ctx, actors["Viewer"], project, Pagination{Page: 1, PageSize: 20})
	if e != nil || list.Total != 4 {
		t.Fatalf("list: %+v %v", list, e)
	}
	raw, _ := json.Marshal(list)
	for _, secret := range []string{keyfile, "127.0.0.1:65534", "PRIVATE KEY"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("request leaked connection or credential material")
		}
	}
	_, e = s.RunTrustAction(ctx, admin, project, TrustActionInput{MachineIDs: []string{machine}, Action: "trust.bootstrap", Fingerprint: host}, "test")
	assertErrorCode(t, e, "SSH_TRUST_REAL_VERIFICATION_REQUIRED")
	s.SSHRunner = nil
	_, e = s.RunTrustAction(ctx, admin, project, TrustActionInput{MachineIDs: []string{machine}, Action: "trust.bootstrap", Fingerprint: host}, "test")
	assertErrorCode(t, e, "SSH_TRUST_REAL_VERIFICATION_REQUIRED")
	expiredID := RandomID()
	_, e = s.DB.ExecContext(ctx, `INSERT INTO ssh_trust_requests(id,project_id,machine_id,action,asset_version,expected_trust,host_key,config_hash,credential_ref,credential_version,credential_expires_at,reason,created_by,created_at,expires_at,state,state_version) SELECT $1,project_id,machine_id,action,asset_version,expected_trust,host_key,config_hash,credential_ref,credential_version,credential_expires_at,reason,created_by,created_at-interval '20 minutes',expires_at-interval '20 minutes','pending',1 FROM ssh_trust_requests WHERE id=$2`, expiredID, own.ID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.ActSSHTrustRequest(ctx, actors["DL"], project, expiredID, "approve", SSHTrustRequestActionInput{ExpectedVersion: 1}, "expired-approval", "test")
	assertErrorCode(t, e, "SSH_TRUST_EXPIRED")
	// Unconfigured simulation keeps a separate provenance. Configuring SSH later
	// cannot turn this old success label into host evidence.
	simulated := RandomID()
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,created_at,updated_at)VALUES($1,'SSH-SIM-SOURCE',now(),now())`, simulated); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at)VALUES($1,$2,$3,'sim-source-fixture','provisioned',now(),now())`, RandomID(), project.ID, simulated); e != nil {
		t.Fatal(e)
	}
	mock, e := s.RunTrustAction(ctx, admin, project, TrustActionInput{MachineIDs: []string{simulated}, Action: "trust.bootstrap", Fingerprint: host}, "test")
	if e != nil || len(mock) != 1 || mock[0].Source != "sim" {
		t.Fatalf("mock provenance: %+v %v", mock, e)
	}
	assets, e := s.ListProjectAssets(ctx, actors["Viewer"], project, Pagination{Page: 1, PageSize: 20})
	if e != nil {
		t.Fatal(e)
	}
	for _, asset := range assets.Items {
		if asset.MachineID == simulated && (asset.SshTrust != "established" || asset.SshTrustSource != "sim") {
			t.Fatalf("false real source: %+v", asset)
		}
	}
	s.SSHRunner = makeRunner(time.Now().Add(10 * time.Minute))
	_, e = s.CreateSSHTrustRequest(ctx, admin, project, input, "credential-short", "test")
	assertErrorCode(t, e, "SSH_TRUST_CREDENTIAL_INVALID")
	s.SSHRunner = makeRunner(time.Now().Add(time.Hour))
	for _, state := range []string{"revoked", "established"} {
		if _, e = s.DB.ExecContext(ctx, `UPDATE machines SET ssh_trust=$2 WHERE id=$1`, machine, state); e != nil {
			t.Fatal(e)
		}
		_, e = s.CreateSSHTrustRequest(ctx, admin, project, input, "state-"+state, "test")
		assertErrorCode(t, e, "SSH_TRUST_TARGET_CHANGED")
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE machines SET ssh_trust='broken' WHERE id=$1`, machine); e != nil {
		t.Fatal(e)
	}
	repair := input
	repair.Action = "trust.repair"
	if _, e = s.CreateSSHTrustRequest(ctx, admin, project, repair, "repair", "test"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE machines SET ssh_trust_source='ssh' WHERE id=$1`, simulated); e != nil {
		t.Fatal(e)
	}
	_, e = s.RunTrustAction(ctx, admin, project, TrustActionInput{MachineIDs: []string{simulated}, Action: "trust.revoke"}, "test")
	assertErrorCode(t, e, "SSH_TRUST_REAL_VERIFICATION_REQUIRED")
	for _, source := range []string{"legacy", "sim"} {
		if _, e = s.DB.ExecContext(ctx, `UPDATE machines SET ssh_trust='established',ssh_trust_source=$2 WHERE id=$1`, machine, source); e != nil {
			t.Fatal(e)
		}
		verification, err := s.CreateSSHTrustRequest(ctx, admin, project, repair, "migrate-"+source, "test")
		if err != nil || verification.ExpectedTrust != "established" {
			t.Fatalf("legacy/sim migration: %+v %v", verification, err)
		}
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE machines SET ssh_trust_source='ssh' WHERE id=$1`, machine); e != nil {
		t.Fatal(e)
	}
	_, e = s.CreateSSHTrustRequest(ctx, admin, project, repair, "real-already-established", "test")
	assertErrorCode(t, e, "SSH_TRUST_TARGET_CHANGED")
	if _, e = s.DB.ExecContext(ctx, `UPDATE projects SET state='delivered' WHERE id=$1`, project.ID); e != nil {
		t.Fatal(e)
	}
	_, e = s.CreateSSHTrustRequest(ctx, admin, project, repair, "delivered", "test")
	assertErrorCode(t, e, "SSH_TRUST_PROJECT_CLOSED")
	t.Log("PASS independent approval/idempotency/immutable binding, no premature trust projection, current role/version/credential/state/closed-project gates and historical real-target mock denial")
}
