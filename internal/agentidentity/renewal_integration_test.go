package agentidentity_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentclient"
	"github.com/enzoOvOyu/keel/internal/agentpki"
	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/enzoOvOyu/keel/internal/platform"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Only the disposable owner fixture creates a shortened initial leaf. This
// exercises real wall-clock TLS renewal without changing production clocks or
// waiting 45 minutes. It is not an enrollment or hardware acceptance claim.
func renewalIdentity(t *testing.T, f *identityFixture, ttl time.Duration) (agentclient.Config, pb.AgentIdentityClient, *x509.Certificate, []byte) {
	t.Helper()
	machine, serial := f.target(t)
	grant, _ := f.grant(t, machine)
	csr, key := request(t)
	public, e := agentpki.CSR(csr)
	requireNoError(t, e)
	raw, e := agentpki.ReadFile(filepath.Join(filepath.Dir(f.caPath), "ca-key.pem"), 16384, true)
	requireNoError(t, e)
	block, _ := pem.Decode(raw)
	ca, e := x509.ParsePKCS8PrivateKey(block.Bytes)
	requireNoError(t, e)
	now := time.Now().UTC().Truncate(time.Second)
	id := platform.RandomID()
	template := &x509.Certificate{SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: id}, NotBefore: now.Add(-time.Second), NotAfter: now.Add(ttl), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	template.URIs = append(template.URIs, agentpki.URI(f.project, machine, id))
	der, e := x509.CreateCertificate(rand.Reader, template, f.authority.Certificate, public, ca)
	requireNoError(t, e)
	cert, e := x509.ParseCertificate(der)
	requireNoError(t, e)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	tx, e := f.store.DB.BeginTx(context.Background(), nil)
	requireNoError(t, e)
	defer tx.Rollback()
	_, e = tx.Exec(`INSERT INTO agent_credentials(id,enrollment_id,project_id,machine_id,asset_version,serial_number,authority_id,certificate_pem,certificate_hash,agent_version,created_at,expires_at)VALUES($1,$2,$3,$4,1,$5,$6,$7,$8,'renewal-fixture-v1',$9,$10)`, id, grant.ID, f.project, machine, serial, f.authority.ID, string(certPEM), agentpki.Hash(der), now, cert.NotAfter)
	requireNoError(t, e)
	_, e = tx.Exec(`UPDATE agent_enrollments SET consumed_at=now(),certificate_id=$2 WHERE id=$1`, grant.ID, id)
	requireNoError(t, e)
	requireNoError(t, tx.Commit())
	dir := privateDirectory(t)
	identity := agentclient.Identity{CredentialID: id, ProjectID: f.project, MachineID: machine, Serial: serial, Version: "renewal-fixture-v1", Certificate: string(certPEM)}
	data, e := json.Marshal(identity)
	requireNoError(t, e)
	requireNoError(t, agentpki.WriteNew(filepath.Join(dir, "agent-key.pem"), key))
	requireNoError(t, agentpki.WriteNew(filepath.Join(dir, "identity.json"), data))
	pair, e := tls.X509KeyPair(certPEM, key)
	requireNoError(t, e)
	config := agentclient.Config{Endpoint: f.endpoint, ServerName: "127.0.0.1", ServerCA: f.caPath, AgentCA: f.caPath, ProjectID: f.project, MachineID: machine, Serial: serial, StateDirectory: dir, RenewalEnabled: true, TasksEnabled: f.config.TasksEnabled}
	return config, f.client(t, &pair), cert, csr
}

func TestAgentRenewalMTLSAndStableExecutionIdentity(t *testing.T) {
	f := newIdentityFixture(t, true, true)
	ctx := context.Background()
	config, client, original, csr := renewalIdentity(t, f, 5*time.Second)
	if _, e := f.client(t, nil).Renew(ctx, &pb.RenewRequest{CsrPem: csr}); status.Code(e) != codes.PermissionDenied {
		t.Fatal("anonymous renewal admitted", e)
	}
	wrong, _ := request(t)
	if _, e := client.Renew(ctx, &pb.RenewRequest{CsrPem: wrong}); status.Code(e) != codes.PermissionDenied {
		t.Fatal("renewal replaced registered key", e)
	}
	credential, e := f.store.CheckAgentCertificate(ctx, original, f.authority.ID)
	requireNoError(t, e)
	anchor := platform.AgentCertificateBinding(credential)
	start := func() (context.CancelFunc, <-chan error) {
		work, stop := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() { done <- agentclient.Stream(work, config, "renewal-fixture-v1", nil) }()
		waitTaskBinding(t, f, config.MachineID)
		return stop, done
	}
	stop, done := start()
	defer func() {
		stop()
		if done != nil {
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Error("renewal task stream did not stop")
			}
		}
	}()
	s := execution.New(f.store.DB)
	s.AgentRunner = f.broker
	// Admit before renewal. The queued snapshot must still match after reconnect.
	x, e := s.CreateAgent(ctx, f.actor, f.project, execution.SSHCreateInput{MachineIDs: []string{config.MachineID}, Operation: "host.identity"}, platform.RandomID(), "renewal-test")
	requireNoError(t, e)
	var before string
	requireNoError(t, f.store.DB.QueryRow(`SELECT id FROM agent_connections WHERE credential_id=$1 AND disconnected_at IS NULL`, credential.ID).Scan(&before))
	var wg sync.WaitGroup
	responses := make(chan *pb.EnrollResponse, 4)
	failures := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := client.Renew(ctx, &pb.RenewRequest{CsrPem: csr})
			if err != nil {
				failures <- err
			} else {
				responses <- out
			}
		}()
	}
	wg.Wait()
	close(responses)
	close(failures)
	for err := range failures {
		t.Fatal("concurrent renewal failed", err)
	}
	var certificate []byte
	for out := range responses {
		if out.GetCredentialId() != credential.ID {
			t.Fatal("credential changed during renewal")
		}
		if certificate != nil && !bytes.Equal(certificate, out.GetCertificatePem()) {
			t.Fatal("renewal replay returned a new certificate")
		}
		certificate = out.GetCertificatePem()
	}
	if len(certificate) == 0 {
		t.Fatal("renewal returned no certificate")
	}
	leaf, e := agentpki.Certificate(certificate)
	requireNoError(t, e)
	current, e := f.store.CheckAgentCertificate(ctx, leaf, f.authority.ID)
	requireNoError(t, e)
	if platform.AgentCertificateBinding(current) != anchor || !current.ExpiresAt.After(original.NotAfter) {
		t.Fatal("renewal changed frozen execution identity or failed to extend validity")
	}
	if _, e = f.store.OpenAgentTaskSession(ctx, leaf, f.authority.ID, before, platform.RandomID()); e == nil {
		t.Fatal("renewed leaf borrowed old certificate's heartbeat connection")
	}
	if _, e = client.Renew(ctx, &pb.RenewRequest{CsrPem: wrong}); status.Code(e) != codes.PermissionDenied {
		t.Fatal("idempotent replay bypassed key check", e)
	}
	changed, e := agentclient.RenewIfDue(ctx, config, "renewal-fixture-v1")
	requireNoError(t, e)
	if !changed {
		t.Fatal("client did not persist previously issued successor")
	}
	changed, e = agentclient.RenewIfDue(ctx, config, "renewal-fixture-v1")
	requireNoError(t, e)
	if changed {
		t.Fatal("fresh certificate renewed again")
	}
	stop()
	select {
	case <-done:
		done = nil
	case <-time.After(5 * time.Second):
		t.Fatal("old transport did not stop")
	}
	time.Sleep(1100 * time.Millisecond)
	stop, done = start()
	// Let the enrollment leaf expire for real, then dispatch the snapshot that
	// was admitted before renewal. The stable key pin and renewed validity must
	// preserve both task authority and original command/receipt identity.
	if wait := time.Until(original.NotAfter) + 20*time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	if _, err := f.store.CheckAgentCertificate(ctx, original, f.authority.ID); err == nil {
		t.Fatal("expired predecessor remained usable")
	}
	claim := claimAgent(t, s, x.ID)
	requireNoError(t, s.RunClaim(ctx, claim))
	detail, e := s.Get(ctx, f.project, x.ID)
	requireNoError(t, e)
	if detail.Execution.State != "success" || detail.Attempts[0].Receipt == nil {
		t.Fatal("queued pre-renewal task lost its actual receipt")
	}
	var renewals, audits int
	requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM agent_certificate_renewals WHERE credential_id=$1`, credential.ID).Scan(&renewals))
	requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM audits WHERE action='agent.certificate_renewed' AND target_id=$1`, credential.ID).Scan(&audits))
	if renewals != 1 || audits != 1 {
		t.Fatal("renewal retries duplicated immutable records", renewals, audits)
	}
	summary, e := f.store.AgentCredentialStatus(ctx, f.project, credential.ID)
	requireNoError(t, e)
	if summary.State != "active" || summary.RenewalsTotal != 1 || summary.OriginalCertificateHash != anchor.ConfigHash || len(summary.RecentConnections) == 0 || !summary.RecentConnections[0].TasksConnected {
		t.Fatal("owner recovery status lost renewal or live stream provenance")
	}
	if _, e = f.store.AgentCredentialStatus(ctx, platform.RandomID(), credential.ID); e == nil {
		t.Fatal("credential status escaped explicit project scope")
	}
	if binary := os.Getenv("KEEL_TEST_AGENT_ADMIN_BIN"); binary != "" {
		command := exec.CommandContext(ctx, binary, "agent-credential-status", "--project", f.project, "--id", credential.ID)
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "KEEL_") {
				command.Env = append(command.Env, entry)
			}
		}
		command.Env = append(command.Env, "KEEL_DATABASE_URL="+os.Getenv("KEEL_TEST_AGENT_DATABASE_URL"))
		data, err := command.Output()
		requireNoError(t, err)
		var status platform.AgentCredentialSummary
		requireNoError(t, json.Unmarshal(data, &status))
		if status.State != "active" || status.ID != credential.ID || bytes.Contains(data, []byte("certificate_pem")) || bytes.Contains(data, []byte("PRIVATE KEY")) {
			t.Fatal("owner status is incorrect or exposed identity material")
		}
	}
	for _, query := range []string{`UPDATE agent_certificate_renewals SET expires_at=expires_at+interval '1 minute' WHERE credential_id=$1`, `DELETE FROM agent_certificate_renewals WHERE credential_id=$1`, `UPDATE agent_connections SET certificate_hash=repeat('a',64) WHERE credential_id=$1`} {
		if _, e = f.store.DB.Exec(query, credential.ID); e == nil {
			t.Fatal("renewal or connection identity evidence was mutable")
		}
	}
	requireNoError(t, f.store.RevokeAgentCredential(ctx, f.project, credential.ID, 1, "renewal fixture complete"))
	summary, e = f.store.AgentCredentialStatus(ctx, f.project, credential.ID)
	requireNoError(t, e)
	if summary.State != "revoked" || summary.StateVersion != 2 {
		t.Fatal("owner status failed to expose whole-credential revocation")
	}
	if _, e = f.store.CheckAgentCertificate(ctx, leaf, f.authority.ID); e == nil {
		t.Fatal("revocation did not invalidate renewed leaf")
	}
	// The old TLS stream now also has an expired leaf. Exercise post-revocation
	// authorization with the still-unexpired successor instead.
	if _, e = f.store.RenewAgentCertificate(ctx, f.authority, leaf, csr); e == nil {
		t.Fatal("revoked credential replayed renewal")
	}
}

func TestAgentRenewalDefaultsAndEligibility(t *testing.T) {
	ctx := context.Background()
	disabled := newIdentityFixture(t)
	_, client, _, csr := renewalIdentity(t, disabled, 10*time.Minute)
	if _, e := client.Renew(ctx, &pb.RenewRequest{CsrPem: csr}); status.Code(e) != codes.Unimplemented {
		t.Fatal("renewal enabled by default", e)
	}
	f := newIdentityFixture(t, false, true)
	config, client, leaf, csr := renewalIdentity(t, f, 50*time.Minute)
	if _, e := client.Renew(ctx, &pb.RenewRequest{CsrPem: csr}); status.Code(e) != codes.PermissionDenied {
		t.Fatal("early renewal admitted", e)
	}
	if changed, e := agentclient.RenewIfDue(ctx, config, "renewal-fixture-v1"); e != nil || changed {
		t.Fatal("client did not skip fresh certificate", e)
	}
	_, e := f.store.DB.Exec(`UPDATE project_assets SET version=version+1 WHERE project_id=$1 AND machine_id=$2`, config.ProjectID, config.MachineID)
	requireNoError(t, e)
	if _, e = f.store.CheckAgentCertificate(ctx, leaf, f.authority.ID); e == nil {
		t.Fatal("asset mismatch remained valid")
	}
	if _, e = client.Renew(ctx, &pb.RenewRequest{CsrPem: csr}); status.Code(e) != codes.PermissionDenied {
		t.Fatal("asset mismatch renewed", e)
	}
	// Expired local identity never falls back to an enrollment token or new key.
	config, _, _, _ = renewalIdentity(t, f, time.Second)
	time.Sleep(1100 * time.Millisecond)
	keyBefore, e := os.ReadFile(filepath.Join(config.StateDirectory, "agent-key.pem"))
	requireNoError(t, e)
	if _, e = agentclient.RenewIfDue(ctx, config, "renewal-fixture-v1"); e == nil {
		t.Fatal("expired local certificate renewed")
	}
	keyAfter, e := os.ReadFile(filepath.Join(config.StateDirectory, "agent-key.pem"))
	requireNoError(t, e)
	if !bytes.Equal(keyBefore, keyAfter) {
		t.Fatal("expired identity silently replaced its private key")
	}
}

func TestAgentRenewalLostLocalCommit(t *testing.T) {
	f := newIdentityFixture(t, false, true)
	config, _, leaf, _ := renewalIdentity(t, f, 10*time.Minute)
	path := filepath.Join(config.StateDirectory, "identity.json")
	original, e := os.ReadFile(path)
	requireNoError(t, e)
	key, e := os.ReadFile(filepath.Join(config.StateDirectory, "agent-key.pem"))
	requireNoError(t, e)
	unlock, e := agentpki.LockIdentity(config.StateDirectory)
	requireNoError(t, e)
	unlock()
	requireNoError(t, os.Chmod(config.StateDirectory, 0500))
	t.Cleanup(func() { os.Chmod(config.StateDirectory, 0700) })
	if _, e = agentclient.RenewIfDue(context.Background(), config, "renewal-fixture-v1"); e == nil {
		t.Fatal("failed local identity write reported success")
	}
	stored, e := os.ReadFile(path)
	requireNoError(t, e)
	if !bytes.Equal(stored, original) {
		t.Fatal("failed write corrupted the old durable identity")
	}
	var issued string
	var count int
	requireNoError(t, f.store.DB.QueryRow(`SELECT certificate_pem FROM agent_certificate_renewals WHERE predecessor_hash=$1`, agentpki.Hash(leaf.Raw)).Scan(&issued))
	requireNoError(t, os.Chmod(config.StateDirectory, 0700))
	changed, e := agentclient.RenewIfDue(context.Background(), config, "renewal-fixture-v1")
	requireNoError(t, e)
	if !changed {
		t.Fatal("client failed to recover issued successor")
	}
	stored, e = os.ReadFile(path)
	requireNoError(t, e)
	var identity agentclient.Identity
	requireNoError(t, json.Unmarshal(stored, &identity))
	if identity.Certificate != issued {
		t.Fatal("lost local commit minted another successor")
	}
	requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM agent_certificate_renewals WHERE predecessor_hash=$1`, agentpki.Hash(leaf.Raw)).Scan(&count))
	after, e := os.ReadFile(filepath.Join(config.StateDirectory, "agent-key.pem"))
	requireNoError(t, e)
	if count != 1 || !bytes.Equal(key, after) {
		t.Fatal("local recovery replaced key or duplicated issuance")
	}
}

func TestAgentRenewalRuntimeRoleAndRevocationRace(t *testing.T) {
	f := newIdentityFixture(t, false, true)
	_, _, leaf, csr := renewalIdentity(t, f, 10*time.Minute)
	ctx := context.Background()
	role := "keel_test_renew_" + platform.RandomID()[:12]
	_, e := f.store.DB.Exec(`CREATE ROLE ` + role + `; GRANT USAGE ON SCHEMA public TO ` + role + `; GRANT SELECT ON projects,project_assets,machines,agent_credentials,agent_certificate_renewals TO ` + role + `; GRANT UPDATE(version) ON projects,project_assets TO ` + role + `; GRANT UPDATE(updated_at) ON machines TO ` + role + `; GRANT UPDATE(state_version) ON agent_credentials TO ` + role + `; GRANT INSERT ON agent_certificate_renewals,audits TO ` + role)
	requireNoError(t, e)
	u, e := url.Parse(os.Getenv("KEEL_TEST_AGENT_DATABASE_URL"))
	requireNoError(t, e)
	query := u.Query()
	query.Set("role", role)
	u.RawQuery = query.Encode()
	db, e := sql.Open("pgx", u.String())
	requireNoError(t, e)
	t.Cleanup(func() { db.Close(); f.store.DB.Exec(`DROP OWNED BY ` + role + `; DROP ROLE ` + role) })
	runtime := &platform.Store{DB: db}
	var actual string
	requireNoError(t, db.QueryRow(`SELECT current_user`).Scan(&actual))
	if actual != role {
		t.Fatal("restricted renewal role was not active")
	}
	renewed, e := runtime.RenewAgentCertificate(ctx, f.authority, leaf, csr)
	requireNoError(t, e)
	for _, statement := range []string{`UPDATE agent_certificate_renewals SET certificate_pem='replaced' WHERE credential_id=$1`, `DELETE FROM agent_certificate_renewals WHERE credential_id=$1`, `UPDATE agent_credentials SET revoked_at=now(),revocation_reason='forbidden',state_version=state_version+1 WHERE id=$1`} {
		if _, e = db.Exec(statement, renewed.CredentialID); e == nil {
			t.Fatal("runtime modified owner-only or immutable renewal evidence")
		}
	}
	// Hold owner revocation first; an already authenticated concurrent renewer
	// must recheck the credential after the lock and cannot append a successor.
	_, _, other, otherCSR := renewalIdentity(t, f, 10*time.Minute)
	credential, e := f.store.CheckAgentCertificate(ctx, other, f.authority.ID)
	requireNoError(t, e)
	tx, e := f.store.DB.BeginTx(ctx, nil)
	requireNoError(t, e)
	defer tx.Rollback()
	var locked string
	requireNoError(t, tx.QueryRow(`SELECT id FROM agent_credentials WHERE id=$1 FOR UPDATE`, credential.ID).Scan(&locked))
	result := make(chan error, 1)
	go func() { _, err := runtime.RenewAgentCertificate(ctx, f.authority, other, otherCSR); result <- err }()
	_, e = tx.Exec(`UPDATE agent_credentials SET revoked_at=now(),revocation_reason='renewal race',state_version=state_version+1 WHERE id=$1`, credential.ID)
	requireNoError(t, e)
	requireNoError(t, tx.Commit())
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("revocation lost to stale renewal")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("renewal did not finish after owner revocation")
	}
	var count int
	requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM agent_certificate_renewals WHERE credential_id=$1`, credential.ID).Scan(&count))
	if count != 0 {
		t.Fatal("revoked credential acquired a renewal")
	}
}
