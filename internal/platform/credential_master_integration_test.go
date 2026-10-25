package platform

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/credentialvault"
)

func TestCredentialMasterAtomicRewrapAndRecovery(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-master-maintenance-password"
	if _, err := s.CreateUser(ctx, "master-owner", "Owner", password, true, true); err != nil {
		t.Fatal(err)
	}
	_, owner := testLogin(t, s, "master-owner", password, "198.51.100.190")
	project, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Master maintenance", Slug: "master-maintenance"}, "master-maintenance", "test")
	if err != nil {
		t.Fatal(err)
	}
	vault := func() *credentialvault.Vault {
		t.Helper()
		path := filepath.Join(t.TempDir(), "master")
		if e := credentialvault.GenerateMaster(path); e != nil {
			t.Fatal(e)
		}
		v, e := credentialvault.Load(path)
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	previous, next := vault(), vault()
	keys := []*SSHDomainKey{}
	for _, version := range []string{"v1", "v2"} {
		key, e := s.CreateSSHDomainKey(ctx, previous, credentialvault.Identity{ProjectID: project.ID, Ref: "master-project-key", Version: version, ExpiresAt: time.Now().Add(2 * time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		keys = append(keys, key)
	}
	if keys[1], err = s.RevokeSSHDomainKey(ctx, project.ID, keys[1].ID, keys[1].StateVersion, "historical revoked key"); err != nil {
		t.Fatal(err)
	}
	ciphertexts := func() map[string][]byte {
		t.Helper()
		rows, e := s.DB.QueryContext(ctx, "SELECT id,ciphertext FROM ssh_domain_keys ORDER BY id")
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		out := map[string][]byte{}
		for rows.Next() {
			var id string
			var sealed []byte
			if e = rows.Scan(&id, &sealed); e != nil {
				t.Fatal(e)
			}
			out[id] = sealed
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		return out
	}
	before := ciphertexts()
	inspection, err := s.InspectCredentialMaster(ctx, previous)
	if err != nil || !inspection.Verified || inspection.Keys != 2 || inspection.Epoch != 0 {
		t.Fatalf("initial inspection: %+v %v", inspection, err)
	}
	input := CredentialMasterRewrapInput{ID: RandomID(), ExpectedSourceKeyID: previous.KeyID(), ExpectedKeys: 2, Reason: "test storage custody rotation"}
	// Fail at the final audit write, after every ciphertext/generation write, to
	// verify that the real PostgreSQL transaction rolls back the entire change.
	if _, err = s.DB.ExecContext(ctx, `CREATE FUNCTION reject_test_master_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='credential.master.rewrapped' THEN RAISE EXCEPTION 'test final audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_test_master_audit BEFORE INSERT ON audits FOR EACH ROW EXECUTE FUNCTION reject_test_master_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RewrapCredentialMaster(ctx, previous, next, input); err == nil {
		t.Fatal("rotation committed without its audit")
	}
	afterFailure := ciphertexts()
	for id, sealed := range before {
		if !bytes.Equal(sealed, afterFailure[id]) {
			t.Fatal("failed rotation partially changed ciphertext")
		}
	}
	var stateCount, eventCount int
	if err = s.DB.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM credential_master_state),(SELECT count(*) FROM credential_master_rewraps)").Scan(&stateCount, &eventCount); err != nil {
		t.Fatal(err)
	}
	if stateCount != 0 || eventCount != 0 {
		t.Fatal("failed rotation persisted a generation")
	}
	if _, err = s.DB.ExecContext(ctx, "DROP TRIGGER reject_test_master_audit ON audits; DROP FUNCTION reject_test_master_audit()"); err != nil {
		t.Fatal(err)
	}
	result, err := s.RewrapCredentialMaster(ctx, previous, next, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Keys != 2 || result.Epoch != 1 || result.AlreadyCommitted {
		t.Fatalf("wrong rotation result: %+v", result)
	}
	after := ciphertexts()
	for _, key := range keys {
		if bytes.Equal(before[key.ID], after[key.ID]) {
			t.Fatal("ciphertext unchanged")
		}
		if err = next.CheckSealed(key.Identity, after[key.ID]); err != nil {
			t.Fatal(err)
		}
		if err = previous.CheckSealed(key.Identity, after[key.ID]); err == nil {
			t.Fatal("old master still opened rotated row")
		}
	}
	for _, key := range keys {
		stored, e := scanSSHDomainKey(s.DB.QueryRowContext(ctx, "SELECT "+sshDomainColumns+" FROM ssh_domain_keys WHERE id=$1", key.ID))
		if e != nil {
			t.Fatal(e)
		}
		if stored.PublicKey != key.PublicKey || !stored.ExpiresAt.Equal(key.ExpiresAt) || stored.StateVersion != key.StateVersion || (stored.RevokedAt == nil) != (key.RevokedAt == nil) {
			t.Fatal("rotation changed signing identity or revocation")
		}
	}
	if _, _, err = (&credentialvault.Provider{DB: s.DB, Vault: next}).Resolve(ctx, project.ID, keys[1].Ref, keys[1].Version); err == nil {
		t.Fatal("maintenance enabled revoked signing")
	}
	if _, err = s.InspectCredentialMaster(ctx, previous); err == nil {
		t.Fatal("old master reported current custody")
	}
	inspection, err = s.InspectCredentialMaster(ctx, next)
	if err != nil || !inspection.Verified || inspection.Epoch != 1 || inspection.RegisteredKeyID != next.KeyID() {
		t.Fatalf("new inspection: %+v %v", inspection, err)
	}
	retry, err := s.RewrapCredentialMaster(ctx, previous, next, input)
	if err != nil || !retry.AlreadyCommitted {
		t.Fatalf("lost-response retry: %+v %v", retry, err)
	}
	changed := input
	changed.Reason = "different operation"
	if _, err = s.RewrapCredentialMaster(ctx, previous, next, changed); err == nil {
		t.Fatal("rotation id accepted different inputs")
	}
	if _, err = s.CreateSSHDomainKey(ctx, previous, credentialvault.Identity{ProjectID: project.ID, Ref: "wrong-old-master", Version: "v3", ExpiresAt: time.Now().Add(time.Hour)}); err == nil {
		t.Fatal("stale owner created a mixed wrapping generation")
	}
	if _, err = s.CreateSSHDomainKey(ctx, next, credentialvault.Identity{ProjectID: project.ID, Ref: "new-master", Version: "v3", ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.RewrapCredentialMaster(ctx, next, previous, CredentialMasterRewrapInput{ID: RandomID(), ExpectedSourceKeyID: next.KeyID(), ExpectedKeys: 3, Reason: "reuse old master"}); err == nil {
		t.Fatal("previous master was reused")
	}
	if _, err = s.DB.ExecContext(ctx, "UPDATE ssh_domain_keys SET ciphertext=$1 WHERE id=$2", before[keys[0].ID], keys[0].ID); err == nil {
		t.Fatal("ciphertext changed without fresh owner rewrap entry")
	}
	var audits, entries int
	if err = s.DB.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM audits WHERE action='credential.master.rewrapped'),(SELECT count(*) FROM credential_master_rewrap_entries)").Scan(&audits, &entries); err != nil {
		t.Fatal(err)
	}
	if audits != 1 || entries != 2 {
		t.Fatal("retry duplicated maintenance evidence")
	}
	for _, query := range []string{"DELETE FROM credential_master_rewrap_entries", "UPDATE credential_master_rewraps SET reason='edited'", "DELETE FROM credential_master_state"} {
		if _, err = s.DB.ExecContext(ctx, query); err == nil {
			t.Fatal("maintenance history mutation accepted")
		}
	}
	role := "keel_test_master_" + RandomID()[:12]
	if _, err = s.DB.ExecContext(ctx, "CREATE ROLE "+role+"; GRANT USAGE ON SCHEMA public TO "+role+"; GRANT SELECT ON ssh_domain_keys,credential_master_state,credential_master_rewraps,credential_master_rewrap_entries TO "+role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.DB.ExecContext(context.Background(), "DROP OWNED BY "+role+"; DROP ROLE "+role) })
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err = conn.ExecContext(ctx, "SET ROLE "+role); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(ctx, "RESET ROLE")
	for _, query := range []string{"UPDATE ssh_domain_keys SET ciphertext=ciphertext", "DELETE FROM credential_master_state", "INSERT INTO credential_master_rewraps DEFAULT VALUES"} {
		if _, err = conn.ExecContext(ctx, query); err == nil {
			t.Fatal("runtime role mutated owner custody")
		}
	}
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err = requireDatabaseOwner(ctx, tx); err == nil {
		t.Fatal("runtime role was accepted as database owner")
	}
}
