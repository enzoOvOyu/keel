package agentidentity_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentclient"
	"github.com/enzoOvOyu/keel/internal/agentidentity"
	"github.com/enzoOvOyu/keel/internal/agentpki"
	pb "github.com/enzoOvOyu/keel/internal/agentproto"
	"github.com/enzoOvOyu/keel/internal/agentrunner"
	"github.com/enzoOvOyu/keel/internal/platform"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

type identityFixture struct {
	actor                     *platform.AuthSession
	broker                    *agentrunner.Broker
	config                    agentidentity.Config
	store                     *platform.Store
	authority                 *agentpki.Authority
	project, endpoint, caPath string
	roots                     *x509.CertPool
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func privateDirectory(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	requireNoError(t, os.Chmod(dir, 0700))
	return dir
}

// These are isolated software identities, not PXE, installation or hardware evidence.
func newIdentityFixture(t *testing.T, tasks ...bool) *identityFixture {
	t.Helper()
	dsn := os.Getenv("KEEL_TEST_AGENT_DATABASE_URL")
	if dsn == "" {
		t.Skip("KEEL_TEST_AGENT_DATABASE_URL absent; actual PostgreSQL/mTLS verification skipped")
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" || u.Port() != "15433" || !strings.HasPrefix(u.Path, "/keel_test_agent_") {
		t.Fatal("refusing Agent integration outside dedicated loopback test database")
	}
	ctx := context.Background()
	s, err := platform.Open(ctx, dsn)
	requireNoError(t, err)
	t.Cleanup(func() { s.Close() })
	requireNoError(t, s.Migrate(ctx))
	var exists bool
	requireNoError(t, s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE is_admin)`).Scan(&exists))
	username := "agent-" + platform.RandomID()[:12]
	password := "agent-integration-only-long-password"
	_, err = s.CreateUser(ctx, username, "Agent fixture", password, true, !exists)
	requireNoError(t, err)
	pre, _, err := s.NewPreSession(ctx)
	requireNoError(t, err)
	_, admin, err := s.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: username, Password: password, Method: "local"}, "198.51.100.154", "agent-fixture")
	requireNoError(t, err)
	project, err := s.CreateProject(ctx, admin, platform.CreateProjectInput{Name: "Agent software fixture", Slug: username}, platform.RandomID(), "agent-fixture")
	requireNoError(t, err)
	dir := privateDirectory(t)
	ca, caKey, err := agentpki.NewAuthority(time.Now())
	requireNoError(t, err)
	caPath, keyPath := filepath.Join(dir, "ca.pem"), filepath.Join(dir, "ca-key.pem")
	requireNoError(t, agentpki.WriteNew(caPath, ca))
	requireNoError(t, agentpki.WriteNew(keyPath, caKey))
	authority, err := agentpki.Load(caPath, keyPath)
	requireNoError(t, err)
	block, _ := pem.Decode(caKey)
	caSigner, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	requireNoError(t, err)
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	requireNoError(t, err)
	leaf := &x509.Certificate{SerialNumber: big.NewInt(123), Subject: pkix.Name{CommonName: "loopback fixture"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, leaf, authority.Certificate, pub, caSigner)
	requireNoError(t, err)
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	requireNoError(t, err)
	serverCert, serverKey := filepath.Join(dir, "server.pem"), filepath.Join(dir, "server-key.pem")
	requireNoError(t, agentpki.WriteNew(serverCert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	requireNoError(t, agentpki.WriteNew(serverKey, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})))
	config := agentidentity.Config{ServerCertificate: serverCert, ServerKey: serverKey, CACertificate: caPath, CAKey: keyPath}
	config.RenewalEnabled = len(tasks) > 1 && tasks[1]
	config.NetworkInventoryEnabled = len(tasks) > 2 && tasks[2]
	s.AgentNetworkInventoryEnabled = config.NetworkInventoryEnabled
	var broker *agentrunner.Broker
	if len(tasks) > 0 && tasks[0] {
		config.TasksEnabled = true
		broker = agentrunner.New(s)
	}
	server, err := agentidentity.NewWithTasks(s, config, broker)
	requireNoError(t, err)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	requireNoError(t, err)
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); <-done })
	roots := x509.NewCertPool()
	roots.AddCert(authority.Certificate)
	return &identityFixture{actor: admin, broker: broker, config: config, store: s, authority: authority, project: project.ID, endpoint: listener.Addr().String(), caPath: caPath, roots: roots}
}

func (f *identityFixture) target(t *testing.T) (machine, serial string) {
	t.Helper()
	machine, serial = platform.RandomID(), "SOFTWARE-"+platform.RandomID()
	_, err := f.store.DB.Exec(`INSERT INTO machines(id,serial_number,created_at,updated_at)VALUES($1,$2,now(),now())`, machine, serial)
	requireNoError(t, err)
	_, err = f.store.DB.Exec(`INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at)VALUES($1,$2,$3,$4,'provisioned',now(),now())`, platform.RandomID(), f.project, machine, "fixture-"+machine)
	requireNoError(t, err)
	return
}

func (f *identityFixture) grant(t *testing.T, machine string) (*platform.AgentEnrollment, string) {
	t.Helper()
	g, token, err := f.store.IssueAgentEnrollment(context.Background(), f.project, machine, f.authority.ID, "isolated software verification", 5*time.Minute)
	requireNoError(t, err)
	return g, token
}

func request(t *testing.T) (csr, key []byte) {
	t.Helper()
	_, signer, err := ed25519.GenerateKey(rand.Reader)
	requireNoError(t, err)
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{Subject: pkix.Name{CommonName: "untrusted CSR identity"}, DNSNames: []string{"forged.example"}}, signer)
	requireNoError(t, err)
	private, err := x509.MarshalPKCS8PrivateKey(signer)
	requireNoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private})
}

func (f *identityFixture) client(t *testing.T, pair *tls.Certificate) pb.AgentIdentityClient {
	t.Helper()
	config := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: f.roots, ServerName: "127.0.0.1"}
	if pair != nil {
		config.Certificates = []tls.Certificate{*pair}
	}
	conn, err := grpc.NewClient("passthrough:///"+f.endpoint, grpc.WithTransportCredentials(credentials.NewTLS(config)), grpc.WithNoProxy(), grpc.WithDisableRetry())
	requireNoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return pb.NewAgentIdentityClient(conn)
}

func (f *identityFixture) enroll(t *testing.T, machine, serial string) (*platform.AgentCredential, tls.Certificate) {
	t.Helper()
	_, token := f.grant(t, machine)
	csr, key := request(t)
	credential, err := f.store.EnrollAgent(context.Background(), f.authority, f.project, machine, serial, "integration-v1", token, csr)
	requireNoError(t, err)
	pair, err := tls.X509KeyPair([]byte(credential.CertificatePEM), key)
	requireNoError(t, err)
	return credential, pair
}

func requireDenied(t *testing.T, err error) {
	t.Helper()
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got %v", err)
	}
}

func TestAgentIdentityPostgresMTLSLifecycle(t *testing.T) {
	f := newIdentityFixture(t)
	ctx := context.Background()
	t.Run("token_binding_replay_and_public_identity", func(t *testing.T) {
		machine, serial := f.target(t)
		grant, token := f.grant(t, machine)
		csr, key := request(t)
		client := f.client(t, nil)
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		for _, in := range []*pb.EnrollRequest{
			{ProjectId: platform.RandomID(), MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", EnrollmentToken: token, CsrPem: csr},
			{ProjectId: f.project, MachineId: machine, SerialNumber: "WRONG", AgentVersion: "integration-v1", EnrollmentToken: token, CsrPem: csr},
			{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", EnrollmentToken: token, CsrPem: []byte("invalid CSR")},
		} {
			_, err := client.Enroll(work, in)
			requireDenied(t, err)
		}
		in := &pb.EnrollRequest{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", EnrollmentToken: token, CsrPem: csr}
		out, err := client.Enroll(work, in)
		requireNoError(t, err)
		_, err = client.Enroll(work, in)
		requireDenied(t, err)
		cert, err := agentpki.Certificate(out.CertificatePem)
		requireNoError(t, err)
		if len(cert.URIs) != 1 || cert.URIs[0].String() != agentpki.URI(f.project, machine, out.CredentialId).String() || len(cert.DNSNames) != 0 || cert.Subject.CommonName != out.CredentialId {
			t.Fatal("CSR identity escaped server binding")
		}
		_, err = tls.X509KeyPair(out.CertificatePem, key)
		requireNoError(t, err)
		var hash, consumed string
		requireNoError(t, f.store.DB.QueryRow(`SELECT token_hash,certificate_id FROM agent_enrollments WHERE id=$1`, grant.ID).Scan(&hash, &consumed))
		if hash == token || consumed != out.CredentialId {
			t.Fatal("secret storage or consumption mismatch")
		}
		var audits string
		requireNoError(t, f.store.DB.QueryRow(`SELECT coalesce(jsonb_agg(metadata)::text,'[]') FROM audits WHERE project_id=$1`, f.project).Scan(&audits))
		if strings.Contains(audits, token) || strings.Contains(audits, "PRIVATE KEY") {
			t.Fatal("secret reached audit")
		}
	})
	t.Run("concurrent_single_consumption", func(t *testing.T) {
		machine, serial := f.target(t)
		_, token := f.grant(t, machine)
		csr, _ := request(t)
		var successes atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if _, err := f.store.EnrollAgent(ctx, f.authority, f.project, machine, serial, "integration-v1", token, csr); err == nil {
					successes.Add(1)
				}
			}()
		}
		wg.Wait()
		if successes.Load() != 1 {
			t.Fatalf("issued %d identities for one token", successes.Load())
		}
	})
	t.Run("cancel_expiry_CA_and_asset_version", func(t *testing.T) {
		machine, serial := f.target(t)
		grant, token := f.grant(t, machine)
		csr, _ := request(t)
		requireNoError(t, f.store.CancelAgentEnrollment(ctx, f.project, grant.ID, "cancel fixture"))
		if _, err := f.store.EnrollAgent(ctx, f.authority, f.project, machine, serial, "integration-v1", token, csr); err == nil {
			t.Fatal("cancelled grant accepted")
		}
		_, token = f.grant(t, machine)
		_, err := f.store.DB.Exec(`UPDATE project_assets SET version=version+1 WHERE project_id=$1 AND machine_id=$2`, f.project, machine)
		requireNoError(t, err)
		if _, err = f.store.EnrollAgent(ctx, f.authority, f.project, machine, serial, "integration-v1", token, csr); err == nil {
			t.Fatal("stale asset grant accepted")
		}
		expired := platform.RandomID() + platform.RandomID()
		_, err = f.store.DB.Exec(`INSERT INTO agent_enrollments(id,project_id,machine_id,asset_version,serial_number,authority_id,token_hash,created_at,expires_at,reason)VALUES($1,$2,$3,2,$4,$5,$6,now()-interval '2 minutes',now()-interval '1 minute','expired fixture')`, platform.RandomID(), f.project, machine, serial, f.authority.ID, agentpki.Hash([]byte("keel-agent-enrollment-v1:"+expired)))
		requireNoError(t, err)
		if _, err = f.store.EnrollAgent(ctx, f.authority, f.project, machine, serial, "integration-v1", expired, csr); err == nil {
			t.Fatal("expired grant accepted")
		}
		_, token = f.grant(t, machine)
		fake := *f.authority
		fake.ID = strings.Repeat("0", 64)
		if _, err = f.store.EnrollAgent(ctx, &fake, f.project, machine, serial, "integration-v1", token, csr); err == nil {
			t.Fatal("different authority consumed grant")
		}
		credential, err := f.store.EnrollAgent(ctx, f.authority, f.project, machine, serial, "integration-v1", token, csr)
		requireNoError(t, err)
		cert, err := agentpki.Certificate([]byte(credential.CertificatePEM))
		requireNoError(t, err)
		_, err = f.store.DB.Exec(`UPDATE project_assets SET version=version+1 WHERE project_id=$1 AND machine_id=$2`, f.project, machine)
		requireNoError(t, err)
		if _, err = f.store.CheckAgentCertificate(ctx, cert, f.authority.ID); err == nil {
			t.Fatal("changed asset retained identity")
		}
	})
	t.Run("anonymous_and_unregistered_mTLS_denied", func(t *testing.T) {
		work, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		stream, err := f.client(t, nil).Heartbeats(work)
		requireNoError(t, err)
		_ = stream.Send(&pb.Heartbeat{Sequence: 1})
		_, err = stream.Recv()
		requireDenied(t, err)
		machine, serial := f.target(t)
		csr, key := request(t)
		public, _, err := f.authority.Issue(csr, f.project, machine, platform.RandomID(), time.Now())
		requireNoError(t, err)
		pair, err := tls.X509KeyPair(public, key)
		requireNoError(t, err)
		stream, err = f.client(t, &pair).Heartbeats(work)
		requireNoError(t, err)
		requireNoError(t, stream.Send(&pb.Heartbeat{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", BootId: platform.RandomID(), Sequence: 1}))
		_, err = stream.Recv()
		requireDenied(t, err)
	})
	t.Run("heartbeat_sequence_and_live_revocation", func(t *testing.T) {
		machine, serial := f.target(t)
		credential, pair := f.enroll(t, machine, serial)
		work, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		stream, err := f.client(t, &pair).Heartbeats(work)
		requireNoError(t, err)
		in := &pb.Heartbeat{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", BootId: platform.RandomID(), Sequence: 1}
		requireNoError(t, stream.Send(in))
		ack, err := stream.Recv()
		requireNoError(t, err)
		if ack.Sequence != 1 || len(ack.ConnectionId) != 32 {
			t.Fatal("invalid heartbeat acknowledgment")
		}
		competing, err := f.client(t, &pair).Heartbeats(work)
		requireNoError(t, err)
		requireNoError(t, competing.Send(in))
		_, err = competing.Recv()
		if status.Code(err) != codes.ResourceExhausted {
			t.Fatalf("active connection was not preserved: %v", err)
		}
		in.Sequence = 2
		requireNoError(t, stream.Send(in))
		ack, err = stream.Recv()
		requireNoError(t, err)
		var sequence int64
		requireNoError(t, f.store.DB.QueryRow(`SELECT sequence FROM agent_connections WHERE id=$1`, ack.ConnectionId).Scan(&sequence))
		if sequence != 2 {
			t.Fatal("heartbeat not persisted")
		}
		// Revocation must terminate an established stream without another client message.
		requireNoError(t, f.store.RevokeAgentCredential(ctx, f.project, credential.ID, 1, "fixture retirement"))
		_, err = stream.Recv()
		requireDenied(t, err)
		requireNoError(t, f.store.RevokeAgentCredential(ctx, f.project, credential.ID, 1, "fixture retirement"))
		if f.store.RevokeAgentCredential(ctx, f.project, credential.ID, 1, "different reason") == nil {
			t.Fatal("conflicting revocation succeeded")
		}
		var closed bool
		requireNoError(t, f.store.DB.QueryRow(`SELECT disconnected_at IS NOT NULL FROM agent_connections WHERE id=$1`, ack.ConnectionId).Scan(&closed))
		if !closed {
			t.Fatal("revoked connection remains active")
		}
		var count int
		requireNoError(t, f.store.DB.QueryRow(`SELECT count(*) FROM audits WHERE action='agent.revoked' AND target_id=$1`, credential.ID).Scan(&count))
		if count != 1 {
			t.Fatal("revocation retry duplicated audit")
		}
		in.Sequence = 1
		stream, err = f.client(t, &pair).Heartbeats(work)
		requireNoError(t, err)
		_ = stream.Send(in)
		_, err = stream.Recv()
		requireDenied(t, err)
	})
	t.Run("duplicate_sequence_and_identity_mutation_denied", func(t *testing.T) {
		for _, mutate := range []bool{false, true} {
			machine, serial := f.target(t)
			_, pair := f.enroll(t, machine, serial)
			work, cancel := context.WithTimeout(ctx, 5*time.Second)
			stream, err := f.client(t, &pair).Heartbeats(work)
			requireNoError(t, err)
			in := &pb.Heartbeat{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", BootId: platform.RandomID(), Sequence: 1}
			requireNoError(t, stream.Send(in))
			_, err = stream.Recv()
			requireNoError(t, err)
			if mutate {
				in.Sequence = 2
				in.MachineId = platform.RandomID()
			}
			requireNoError(t, stream.Send(in))
			_, err = stream.Recv()
			requireDenied(t, err)
			cancel()
		}
	})
	t.Run("database_lock_timeout_is_recoverable", func(t *testing.T) {
		machine, serial := f.target(t)
		credential, pair := f.enroll(t, machine, serial)
		work, cancel := context.WithTimeout(ctx, 12*time.Second)
		defer cancel()
		stream, err := f.client(t, &pair).Heartbeats(work)
		requireNoError(t, err)
		requireNoError(t, stream.Send(&pb.Heartbeat{ProjectId: f.project, MachineId: machine, SerialNumber: serial, AgentVersion: "integration-v1", BootId: platform.RandomID(), Sequence: 1}))
		_, err = stream.Recv()
		requireNoError(t, err)
		// A real owner row lock blocks the periodic certificate FOR SHARE check.
		// Its bounded timeout must close the stream without invalidating identity.
		tx, err := f.store.DB.BeginTx(work, nil)
		requireNoError(t, err)
		defer tx.Rollback()
		var lockedID string
		requireNoError(t, tx.QueryRowContext(work, `SELECT id FROM agent_credentials WHERE id=$1 FOR UPDATE`, credential.ID).Scan(&lockedID))
		_, err = stream.Recv()
		if status.Code(err) != codes.Unavailable {
			t.Fatalf("temporary database lock permanently rejected identity: %v", err)
		}
		requireNoError(t, tx.Rollback())
		cert, err := agentpki.Certificate([]byte(credential.CertificatePEM))
		requireNoError(t, err)
		current, err := f.store.CheckAgentCertificate(work, cert, f.authority.ID)
		requireNoError(t, err)
		if current.ID != lockedID || current.RevokedAt != nil {
			t.Fatal("temporary lock changed the registered identity")
		}
	})
	t.Run("real_agent_client_enrollment_and_liveness", func(t *testing.T) {
		machine, serial := f.target(t)
		_, token := f.grant(t, machine)
		dir := privateDirectory(t)
		tokenPath := filepath.Join(dir, "enrollment-token")
		requireNoError(t, agentpki.WriteNew(tokenPath, []byte(token)))
		config := agentclient.Config{Endpoint: f.endpoint, ServerName: "127.0.0.1", ServerCA: f.caPath, AgentCA: f.caPath, ProjectID: f.project, MachineID: machine, Serial: serial, StateDirectory: dir, TokenFile: tokenPath}
		identity, err := agentclient.Enroll(ctx, config, "integration-v1")
		requireNoError(t, err)
		if _, err = agentclient.Enroll(ctx, config, "integration-v1"); err == nil {
			t.Fatal("existing local identity overwritten")
		}
		key, err := agentpki.ReadFile(filepath.Join(dir, "agent-key.pem"), 8192, true)
		requireNoError(t, err)
		if !strings.Contains(string(key), "PRIVATE KEY") {
			t.Fatal("private key not retained locally")
		}
		metadata, err := agentpki.ReadFile(filepath.Join(dir, "identity.json"), 16384, true)
		requireNoError(t, err)
		var disk agentclient.Identity
		requireNoError(t, json.Unmarshal(metadata, &disk))
		if disk.CredentialID != identity.CredentialID || strings.Contains(string(metadata), token) || strings.Contains(string(metadata), "PRIVATE KEY") {
			t.Fatal("invalid public identity metadata")
		}
		work, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		var acknowledged bool
		err = agentclient.Stream(work, config, "integration-v1", func(ack *pb.HeartbeatAck) { acknowledged = ack.Sequence == 1; cancel() })
		if err == nil || !acknowledged {
			t.Fatal("real client heartbeat/cancellation failed", err)
		}
	})
	t.Run("operator_and_agent_CLI_end_to_end", func(t *testing.T) {
		adminBin, agentBin := os.Getenv("KEEL_TEST_AGENT_ADMIN_BIN"), os.Getenv("KEEL_TEST_AGENT_AGENT_BIN")
		if adminBin == "" || agentBin == "" {
			t.Skip("isolated CLI binaries absent; run scripts/check-agent-integration.py")
		}
		environment := make([]string, 0, len(os.Environ()))
		for _, item := range os.Environ() {
			if !strings.HasPrefix(item, "KEEL_") {
				environment = append(environment, item)
			}
		}
		environment = append(environment, "KEEL_DATABASE_URL="+os.Getenv("KEEL_TEST_AGENT_DATABASE_URL"))
		run := func(binary string, args ...string) ([]byte, error) {
			work, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			command := exec.CommandContext(work, binary, args...)
			command.Env = environment
			return command.CombinedOutput()
		}
		dir := privateDirectory(t)
		newCA := filepath.Join(dir, "new-authority")
		out, err := run(adminBin, "agent-ca-init", "--directory", newCA)
		requireNoError(t, err)
		if bytes.Contains(out, []byte("PRIVATE KEY")) {
			t.Fatal("CA CLI exposed key")
		}
		originalCA, err := agentpki.ReadFile(filepath.Join(newCA, "ca.pem"), 16384, false)
		requireNoError(t, err)
		if _, err = run(adminBin, "agent-ca-init", "--directory", newCA); err == nil {
			t.Fatal("existing CA directory replaced")
		}
		preservedCA, err := agentpki.ReadFile(filepath.Join(newCA, "ca.pem"), 16384, false)
		requireNoError(t, err)
		if !bytes.Equal(originalCA, preservedCA) {
			t.Fatal("existing CA changed")
		}
		machine, serial := f.target(t)
		tokenPath := filepath.Join(dir, "token")
		createArgs := []string{"agent-enrollment-create", "--project", f.project, "--machine", machine, "--ca-certificate", f.caPath, "--reason", "software CLI fixture", "--token-output", tokenPath}
		out, err = run(adminBin, createArgs...)
		requireNoError(t, err)
		var grant platform.AgentEnrollment
		requireNoError(t, json.Unmarshal(out, &grant))
		token, err := agentpki.ReadFile(tokenPath, 256, true)
		requireNoError(t, err)
		if len(grant.ID) != 32 || bytes.Contains(out, bytes.TrimSpace(token)) || bytes.Contains(out, []byte("PRIVATE KEY")) {
			t.Fatal("invalid CLI grant or secret exposed")
		}
		state := filepath.Join(dir, "state")
		requireNoError(t, os.Mkdir(state, 0700))
		config := agentclient.Config{Endpoint: f.endpoint, ServerName: "127.0.0.1", ServerCA: f.caPath, AgentCA: f.caPath, ProjectID: f.project, MachineID: machine, Serial: serial, StateDirectory: state, TokenFile: tokenPath}
		raw, err := json.Marshal(config)
		requireNoError(t, err)
		configPath := filepath.Join(dir, "config.json")
		requireNoError(t, agentpki.WriteNew(configPath, raw))
		out, err = run(agentBin, "enroll", "--config", configPath)
		requireNoError(t, err)
		var enrolled struct {
			CredentialID string `json:"credential_id"`
		}
		requireNoError(t, json.Unmarshal(out, &enrolled))
		if len(enrolled.CredentialID) != 32 || bytes.Contains(out, bytes.TrimSpace(token)) || bytes.Contains(out, []byte("PRIVATE KEY")) {
			t.Fatal("CLI enrollment identity or output invalid")
		}
		if _, err = run(agentBin, "enroll", "--config", configPath); err == nil {
			t.Fatal("CLI re-enrolled existing identity")
		}
		command := exec.Command(agentBin, "run", "--config", configPath)
		command.Env = environment
		var processOutput bytes.Buffer
		command.Stdout, command.Stderr = &processOutput, &processOutput
		requireNoError(t, command.Start())
		defer func() {
			if command.ProcessState == nil {
				_ = command.Process.Kill()
				_ = command.Wait()
			}
		}()
		deadline := time.Now().Add(5 * time.Second)
		for {
			var live bool
			requireNoError(t, f.store.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM agent_connections WHERE credential_id=$1 AND sequence>=1 AND disconnected_at IS NULL)`, enrolled.CredentialID).Scan(&live))
			if live {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("Agent CLI produced no durable heartbeat")
			}
			time.Sleep(10 * time.Millisecond)
		}
		requireNoError(t, command.Process.Signal(syscall.SIGTERM))
		requireNoError(t, command.Wait())
		if bytes.Contains(processOutput.Bytes(), bytes.TrimSpace(token)) || bytes.Contains(processOutput.Bytes(), []byte("PRIVATE KEY")) {
			t.Fatal("Agent run exposed identity secrets")
		}
		_, err = run(adminBin, "agent-revoke", "--project", f.project, "--id", enrolled.CredentialID, "--expected-version", "1", "--reason", "CLI fixture retirement")
		requireNoError(t, err)
		if _, err = run(agentBin, "run", "--config", configPath); err == nil {
			t.Fatal("revoked Agent CLI remained usable")
		}
		// A failed protected handoff must preserve the existing file and cancel its new grant.
		other, _ := f.target(t)
		createArgs[4] = other
		if _, err = run(adminBin, createArgs...); err == nil {
			t.Fatal("existing token file overwritten")
		}
		preservedToken, err := agentpki.ReadFile(tokenPath, 256, true)
		requireNoError(t, err)
		if !bytes.Equal(token, preservedToken) {
			t.Fatal("existing token changed")
		}
		var cancelled bool
		requireNoError(t, f.store.DB.QueryRow(`SELECT EXISTS(SELECT 1 FROM agent_enrollments WHERE machine_id=$1 AND revoked_at IS NOT NULL AND consumed_at IS NULL)`, other).Scan(&cancelled))
		if !cancelled {
			t.Fatal("failed token handoff left a usable grant")
		}
	})
}
