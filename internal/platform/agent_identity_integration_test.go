package platform

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
)

// This test never resets a schema. Its owner/role/records belong only to the
// dedicated disposable Agent test database; production roles are not touched.
func TestAgentRuntimePermissions(t *testing.T) {
	dsn := os.Getenv("KEEL_TEST_AGENT_DATABASE_URL")
	if dsn == "" {
		t.Skip("KEEL_TEST_AGENT_DATABASE_URL not configured")
	}
	parsed, e := url.Parse(dsn)
	if e != nil || parsed.Hostname() != "127.0.0.1" || parsed.Port() != "15433" || !strings.HasPrefix(parsed.Path, "/keel_test_agent") {
		t.Fatal("refusing non-disposable Agent database")
	}
	ctx := context.Background()
	owner, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer owner.Close()
	if e = owner.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	suffix := RandomID()[:12]
	var hasAdmin bool
	if e = owner.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE is_admin)`).Scan(&hasAdmin); e != nil {
		t.Fatal(e)
	}
	username := "agent-role-" + suffix
	if _, e = owner.CreateUser(ctx, username, username, "agent-test-only-password-2026", true, !hasAdmin); e != nil {
		t.Fatal(e)
	}
	pre, _, e := owner.NewPreSession(ctx)
	if e != nil {
		t.Fatal(e)
	}
	_, admin, e := owner.Login(ctx, pre, CSRFToken(pre), LoginInput{Username: username, Password: "agent-test-only-password-2026", Method: "local"}, "198.51.100.170", "agent-test")
	if e != nil {
		t.Fatal(e)
	}
	project, e := owner.CreateProject(ctx, admin, CreateProjectInput{Name: "Agent role " + suffix, Slug: "agent-role-" + suffix}, "agent-role-"+suffix, "agent-test")
	if e != nil {
		t.Fatal(e)
	}
	machine, serial := RandomID(), "AGENT-ROLE-"+suffix
	if _, e = owner.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,created_at,updated_at)VALUES($1,$2,now(),now())`, machine, serial); e != nil {
		t.Fatal(e)
	}
	if _, e = owner.DB.ExecContext(ctx, `INSERT INTO project_assets(id,project_id,machine_id,hostname,state,created_at,updated_at)VALUES($1,$2,$3,$4,'provisioned',now(),now())`, RandomID(), project.ID, machine, "agent-role-"+suffix); e != nil {
		t.Fatal(e)
	}
	public, private, e := agentpki.NewAuthority(time.Now())
	if e != nil {
		t.Fatal(e)
	}
	directory := t.TempDir()
	if e = os.Chmod(directory, 0700); e != nil {
		t.Fatal(e)
	}
	cp, kp := filepath.Join(directory, "ca.pem"), filepath.Join(directory, "ca-key.pem")
	if e = agentpki.WriteNew(cp, public); e != nil {
		t.Fatal(e)
	}
	if e = agentpki.WriteNew(kp, private); e != nil {
		t.Fatal(e)
	}
	authority, e := agentpki.Load(cp, kp)
	if e != nil {
		t.Fatal(e)
	}
	grant, token, e := owner.IssueAgentEnrollment(ctx, project.ID, machine, authority.ID, "software fixture registration", 5*time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	role := "keel_test_agent_" + suffix
	if _, e = owner.DB.ExecContext(ctx, `CREATE ROLE `+role+`; GRANT USAGE ON SCHEMA public TO `+role+`; GRANT SELECT ON projects,project_assets,machines,agent_enrollments,agent_credentials,agent_connections,agent_certificate_renewals TO `+role+`; GRANT INSERT ON agent_certificate_renewals TO `+role+`; GRANT UPDATE(version) ON projects,project_assets TO `+role+`; GRANT UPDATE(updated_at) ON machines TO `+role+`; GRANT UPDATE(consumed_at,certificate_id) ON agent_enrollments TO `+role+`; GRANT INSERT,UPDATE(state_version) ON agent_credentials TO `+role+`; GRANT INSERT,UPDATE ON agent_connections TO `+role+`; GRANT INSERT ON audits TO `+role); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, err := owner.DB.ExecContext(ctx, `DROP OWNED BY `+role+`; DROP ROLE `+role); err != nil {
			t.Error(err)
		}
	}()
	query := parsed.Query()
	query.Set("role", role)
	parsed.RawQuery = query.Encode()
	db, e := sql.Open("pgx", parsed.String())
	if e != nil {
		t.Fatal("restricted role configuration rejected")
	}
	defer db.Close()
	if e = db.PingContext(ctx); e != nil {
		t.Fatal(e)
	}
	runtime := &Store{DB: db}
	var current string
	if e = runtime.DB.QueryRowContext(ctx, `SELECT current_user`).Scan(&current); e != nil || current != role {
		t.Fatal("restricted role was not active", e)
	}
	var allowed bool
	if e = runtime.DB.QueryRowContext(ctx, `SELECT has_table_privilege(current_user,'agent_enrollments','INSERT') OR has_column_privilege(current_user,'agent_enrollments','revoked_at','UPDATE') OR has_column_privilege(current_user,'agent_credentials','revoked_at','UPDATE')`).Scan(&allowed); e != nil || allowed {
		t.Fatal("runtime has operator-only grants", e)
	}
	_, key, e := ed25519.GenerateKey(rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	der, e := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, key)
	if e != nil {
		t.Fatal(e)
	}
	credential, e := runtime.EnrollAgent(ctx, authority, project.ID, machine, serial, "test-v1", token, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der}))
	if e != nil {
		t.Fatal("restricted runtime could not consume enrollment", e)
	}
	certificate, e := agentpki.Certificate([]byte(credential.CertificatePEM))
	if e != nil {
		t.Fatal(e)
	}
	conn, e := runtime.OpenAgentConnection(ctx, certificate, authority.ID, project.ID, machine, serial, "test-v1", RandomID())
	if e != nil {
		t.Fatal("column UPDATE must permit FOR SHARE without revocation rights", e)
	}
	if e = runtime.AgentHeartbeat(ctx, certificate, authority.ID, conn, 1); e != nil {
		t.Fatal(e)
	}
	session := RandomID()
	if _, e = runtime.OpenAgentTaskSession(ctx, certificate, authority.ID, conn, session); e != nil {
		t.Fatal("restricted worker could not open task stream", e)
	}
	if binding, connection, err := runtime.ActiveAgentTaskBinding(ctx, project.ID, machine); err != nil || connection != conn || binding.ID != credential.ID {
		t.Fatal("restricted worker could not read current task binding", err)
	}
	if _, e = runtime.OpenAgentTaskSession(ctx, certificate, authority.ID, conn, RandomID()); !errors.Is(e, ErrAgentConnectionBusy) {
		t.Fatal("concurrent task stream was not rejected", e)
	}
	if e = runtime.CloseAgentTaskSession(ctx, conn, session); e != nil {
		t.Fatal(e)
	}
	if _, _, e = runtime.ActiveAgentTaskBinding(ctx, project.ID, machine); !errors.Is(e, ErrAgentDenied) {
		t.Fatal("closed task stream remained dispatchable", e)
	}
	if e = runtime.AgentHeartbeat(ctx, certificate, authority.ID, conn, 1); e == nil {
		t.Fatal("duplicate heartbeat accepted")
	}
	if _, _, e = runtime.IssueAgentEnrollment(ctx, project.ID, RandomID(), authority.ID, "forbidden runtime issue", time.Minute); e == nil {
		t.Fatal("runtime minted enrollment")
	}
	for _, sql := range []string{
		`UPDATE agent_credentials SET revoked_at=now(),revocation_reason='forbidden',state_version=state_version+1 WHERE id=$1`,
		`UPDATE agent_credentials SET state_version=state_version+1 WHERE id=$1`,
		`UPDATE agent_credentials SET certificate_pem='replacement' WHERE id=$1`,
		`DELETE FROM agent_credentials WHERE id=$1`,
		`DELETE FROM agent_enrollments WHERE certificate_id=$1`,
		`UPDATE agent_enrollments SET revoked_at=now() WHERE certificate_id=$1`,
		`DELETE FROM agent_connections WHERE credential_id=$1`,
	} {
		if _, e = runtime.DB.ExecContext(ctx, sql, credential.ID); e == nil {
			t.Fatal("runtime mutated protected identity evidence", sql)
		}
	}
	for _, table := range []string{"agent_enrollments", "agent_credentials", "agent_connections"} {
		if _, e = runtime.DB.ExecContext(ctx, `TRUNCATE `+table+` CASCADE`); e == nil {
			t.Fatal("runtime erased evidence", table)
		}
	}
	var stored, audit string
	if e = owner.DB.QueryRowContext(ctx, `SELECT row_to_json(a)::text FROM agent_enrollments a WHERE id=$1`, grant.ID).Scan(&stored); e != nil || strings.Contains(stored, token) {
		t.Fatal("raw token stored", e)
	}
	if e = owner.DB.QueryRowContext(ctx, `SELECT COALESCE(string_agg(metadata::text,''),'') FROM audits WHERE project_id=$1`, project.ID).Scan(&audit); e != nil || strings.Contains(audit, token) || strings.Contains(audit, "PRIVATE KEY") {
		t.Fatal("secret in audit", e)
	}
	if e = owner.RevokeAgentCredential(ctx, project.ID, credential.ID, 1, "fixture retirement"); e != nil {
		t.Fatal(e)
	}
	if e = owner.RevokeAgentCredential(ctx, project.ID, credential.ID, 1, "fixture retirement"); e != nil {
		t.Fatal("exact revoke retry failed", e)
	}
	if e = owner.RevokeAgentCredential(ctx, project.ID, credential.ID, 1, "different reason"); e == nil {
		t.Fatal("conflicting revoke accepted")
	}
	if _, e = runtime.CheckAgentCertificate(ctx, certificate, authority.ID); e == nil {
		t.Fatal("revoked certificate accepted")
	}
	var count int
	if e = owner.DB.QueryRowContext(ctx, `SELECT count(*) FROM audits WHERE target_id=$1 AND action='agent.revoked'`, credential.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("revocation audit not atomic/idempotent", e)
	}
}
