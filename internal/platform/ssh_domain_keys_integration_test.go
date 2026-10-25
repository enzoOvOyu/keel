package platform

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/credentialvault"
)

func TestSSHDomainKeyEncryptedLifecycleAndAuthority(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	password := "domain-key-test-only-password"
	if _, err := s.CreateUser(ctx, "key-admin", "Key admin", password, true, true); err != nil {
		t.Fatal(err)
	}
	_, admin := testLogin(t, s, "key-admin", password, "198.51.100.140")
	project, err := s.CreateProject(ctx, admin, CreateProjectInput{Name: "Domain keys", Slug: "domain-keys"}, "domain-keys", "test")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "master")
	if err = credentialvault.GenerateMaster(path); err != nil {
		t.Fatal(err)
	}
	vault, err := credentialvault.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	i := credentialvault.Identity{ProjectID: project.ID, Ref: "keel-ctrl", Version: "v1", ExpiresAt: time.Now().Add(time.Hour)}
	x, err := s.CreateSSHDomainKey(ctx, vault, i)
	if err != nil {
		t.Fatal(err)
	}
	provider := &credentialvault.Provider{DB: s.DB, Vault: vault}
	if _, _, err = provider.Resolve(ctx, project.ID, i.Ref, i.Version); err != nil {
		t.Fatal(err)
	}
	if _, _, err = provider.Resolve(ctx, RandomID(), i.Ref, i.Version); err == nil {
		t.Fatal("cross-project secret resolved")
	}
	if _, err = s.CreateSSHDomainKey(ctx, vault, i); err == nil {
		t.Fatal("version was replaced")
	}
	list, err := s.ListSSHDomainKeys(ctx, project.ID, 50, 0)
	if err != nil || len(list) != 1 {
		t.Fatal("metadata listing failed", err)
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "ciphertext") || strings.Contains(string(raw), "PRIVATE KEY") || strings.Contains(string(raw), path) {
		t.Fatal("private information exported")
	}
	for _, query := range []string{`UPDATE ssh_domain_keys SET credential_version='changed' WHERE id=$1`, `UPDATE ssh_domain_keys SET expires_at=expires_at+interval '1 hour' WHERE id=$1`, `DELETE FROM ssh_domain_keys WHERE id=$1`} {
		if _, err = s.DB.ExecContext(ctx, query, x.ID); err == nil {
			t.Fatal("immutable evidence changed")
		}
	}
	var count atomic.Int32
	var wg sync.WaitGroup
	for n := 0; n < 2; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := s.RevokeSSHDomainKey(ctx, project.ID, x.ID, 1, "routine retirement")
			if e == nil {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 2 {
		t.Fatal("idempotent concurrent revoke failed")
	}
	first, err := s.RevokeSSHDomainKey(ctx, project.ID, x.ID, 1, "routine retirement")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.RevokeSSHDomainKey(ctx, project.ID, x.ID, 1, "routine retirement")
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("exact revocation retry changed response")
	}
	if _, _, err = provider.Resolve(ctx, project.ID, i.Ref, i.Version); err == nil {
		t.Fatal("revoked key resolved")
	}
	if _, err = s.RevokeSSHDomainKey(ctx, project.ID, x.ID, 1, "different reason"); err == nil {
		t.Fatal("conflicting revoke accepted")
	}
	if _, err = s.CreateSSHDomainKey(ctx, vault, i); err == nil {
		t.Fatal("revoked version reused")
	}
	i.Version = "v2"
	next, err := s.CreateSSHDomainKey(ctx, vault, i)
	if err != nil {
		t.Fatal(err)
	}
	if next.PublicKey == x.PublicKey {
		t.Fatal("new version reused old key")
	}
	var auditCount int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audits WHERE target_id=$1 AND action='credential.ssh.revoked'`, x.ID).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal("revocation audit not atomic/idempotent", err)
	}
	var audit string
	if err = s.DB.QueryRowContext(ctx, `SELECT metadata::text FROM audits WHERE target_id=$1 AND action='credential.ssh.created'`, x.ID).Scan(&audit); err != nil || !strings.Contains(audit, "operator_uid") || strings.Contains(audit, "ciphertext") {
		t.Fatal("unsafe or missing operator audit", err)
	}
	// The production runtime grant is read-only, even if the master key is
	// mounted. Prove permission boundaries under an unprivileged SQL role.
	role := "keel_test_keys_" + RandomID()[:10]
	if _, err = s.DB.ExecContext(ctx, `CREATE ROLE `+role+`; GRANT USAGE ON SCHEMA public TO `+role+`; GRANT SELECT ON ssh_domain_keys TO `+role); err != nil {
		t.Fatal(err)
	}
	defer func() { s.DB.ExecContext(ctx, `DROP OWNED BY `+role+`; DROP ROLE `+role) }()
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, `SET ROLE `+role); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, `RESET ROLE`)
	for _, permission := range []string{"INSERT", "UPDATE", "DELETE", "TRUNCATE"} {
		var granted bool
		if err = conn.QueryRowContext(ctx, `SELECT has_table_privilege(current_user,'ssh_domain_keys',$1)`, permission).Scan(&granted); err != nil || granted {
			t.Fatal("runtime key mutation grant", permission, err)
		}
	}
	for _, query := range []string{`DELETE FROM ssh_domain_keys`, `UPDATE ssh_domain_keys SET revoked_at=now()`, `TRUNCATE ssh_domain_keys`} {
		if _, err = conn.ExecContext(ctx, query); err == nil {
			t.Fatal("runtime role mutated key evidence")
		}
	}
	var encrypted []byte
	if err = conn.QueryRowContext(ctx, `SELECT ciphertext FROM ssh_domain_keys WHERE id=$1`, next.ID).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encrypted), "PRIVATE KEY") {
		t.Fatal("plaintext stored")
	}
	wrongPath := filepath.Join(t.TempDir(), "wrong")
	if err = credentialvault.GenerateMaster(wrongPath); err != nil {
		t.Fatal(err)
	}
	wrong, err := credentialvault.Load(wrongPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = wrong.Open(next.Identity, encrypted); err == nil {
		t.Fatal("database alone unlocked secret")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("master mode changed")
	}
}
