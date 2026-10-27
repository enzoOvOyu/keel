package platform

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
	"github.com/enzoOvOyu/keel/internal/credentialvault"
)

// Written for the P2 concentrated batch, never run automatically on source edits.
func TestDeviceCredentialBatchRotationPinsOldJobsPostgres(t *testing.T) {
	s, root, _, _, project := templateLineageFixture(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := s.DB.ExecContext(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	suffix := RandomID()[:10]
	pkg, profile := RandomID(), RandomID()
	batch := "BMC-batch-" + suffix
	exec(`INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) VALUES($1,$2,$3,'approved',$4,now())`, profile, project.ID, suffix, root.User.ID)
	exec(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,$3,'bom-import/v1',$3,'sha256:'||repeat('a',64),'fixture','{}',0,'committed',$4,now())`, pkg, project.ID, suffix, root.User.ID)
	exec(`INSERT INTO bom_credential_batches(project_id,credential_batch_ref,scope,rotation_policy,vault_path,owner,package_id,updated_at) VALUES($1,$2,'bmc','manual','vault://fixture/bmc','fixture',$3,now())`, project.ID, batch, pkg)
	path := filepath.Join(t.TempDir(), "device-master")
	if e := credentialvault.GenerateMaster(path); e != nil {
		t.Fatal(e)
	}
	vault, e := credentialvault.Load(path)
	if e != nil {
		t.Fatal(e)
	}
	identity := credentialvault.SecretIdentity{ProjectID: project.ID, Ref: batch, Version: "v1", Scope: "bmc", ExpiresAt: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
	first := DevicePassword{Username: "reader", Password: "fixture-password-old"}
	metadata, e := s.ImportDeviceCredential(ctx, vault, identity, first, "", "initial fixture")
	if e != nil {
		t.Fatal(e)
	}
	safe, _ := json.Marshal(metadata)
	if strings.Contains(string(safe), first.Password) || strings.Contains(string(safe), `"username"`) {
		t.Fatal("credential metadata exposed plaintext")
	}
	target := bmc.Target{RequestID: RandomID(), ProjectID: project.ID, AssetID: RandomID(), MachineID: RandomID(), AssetVersion: 1, Serial: "FIXTURE", CredentialBatchRef: batch, CredentialVersion: "v1"}
	provider := bmcCredentialProvider{db: s.DB, vault: vault}
	pinned, e := provider.Resolve(ctx, target)
	if e != nil || pinned.Password != first.Password {
		t.Fatal("first exact version unavailable", e)
	}
	identity.Version = "v2"
	second := DevicePassword{Username: "reader", Password: "fixture-password-new"}
	if _, e = s.ImportDeviceCredential(ctx, vault, identity, second, "incorrect-head", "rotate fixture"); e == nil {
		t.Fatal("rotation ignored expected head")
	}
	if _, e = s.ImportDeviceCredential(ctx, vault, identity, second, "v1", "rotate fixture"); e != nil {
		t.Fatal(e)
	}
	pinned, e = provider.Resolve(ctx, target)
	if e != nil || pinned.Password != first.Password {
		t.Fatal("in-flight job silently switched credential")
	}
	target.CredentialVersion = "v2"
	pinned, e = provider.Resolve(ctx, target)
	if e != nil || pinned.Password != second.Password {
		t.Fatal("new job did not resolve new credential", e)
	}
	if _, e = s.ImportDeviceCredential(ctx, vault, identity, first, "v2", "overwrite fixture"); e == nil {
		t.Fatal("immutable credential version overwritten")
	}
	if e = s.RevokeDeviceCredential(ctx, project.ID, batch, "bmc", "v1", "retire old fixture"); e != nil {
		t.Fatal(e)
	}
	target.CredentialVersion = "v1"
	if _, e = provider.Resolve(ctx, target); e == nil {
		t.Fatal("revoked credential resolved")
	}
	target.CredentialVersion = "v2"
	if _, e = provider.Resolve(ctx, target); e != nil {
		t.Fatal("revoking old version disabled new version", e)
	}
	var state string
	if e = s.DB.QueryRowContext(ctx, `SELECT verification_state FROM device_credential_heads WHERE project_id=$1 AND credential_batch_ref=$2 AND scope='bmc'`, project.ID, batch).Scan(&state); e != nil {
		t.Fatal(e)
	}
	if state != "pending" {
		t.Fatal("rotation falsely asserted full-batch verification")
	}
	if _, e = s.DB.ExecContext(ctx, `UPDATE device_credential_versions SET ciphertext='changed' WHERE project_id=$1 AND credential_batch_ref=$2`, project.ID, batch); e == nil {
		t.Fatal("ciphertext mutable")
	}
}
