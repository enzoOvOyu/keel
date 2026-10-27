package execution

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/bmc"
	"github.com/enzoOvOyu/keel/internal/platform"
)

type bmcTestObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (o *bmcTestObjects) Put(_ context.Context, key string, r io.Reader, _ int64) (string, error) {
	raw, e := io.ReadAll(r)
	if e != nil {
		return "", e
	}
	version := platform.RandomID()
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.objects == nil {
		o.objects = map[string][]byte{}
	}
	o.objects[key+":"+version] = raw
	return version, nil
}
func (o *bmcTestObjects) Open(_ context.Context, key, version string) (io.ReadCloser, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	raw, ok := o.objects[key+":"+version]
	if !ok {
		return nil, platform.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(append([]byte(nil), raw...))), nil
}
func bmcExecutionFixture(t *testing.T) (*agentKernelFixture, string) {
	t.Helper()
	f := agentKernel(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		_, e := f.store.DB.ExecContext(ctx, q, args...)
		mustAgent(t, e)
	}
	suffix := platform.RandomID()[:10]
	pkg, profile := platform.RandomID(), platform.RandomID()
	batch := "bmc-" + suffix
	exec(`INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) VALUES($1,$2,$3,'approved',$4,now())`, profile, f.project, suffix, f.actor.User.ID)
	exec(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,$3,'bom-import/v1',$3,'sha256:'||repeat('a',64),'fixture','{}',1,'committed',$4,now())`, pkg, f.project, suffix, f.actor.User.ID)
	var serial string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT serial_number FROM machines WHERE id=$1`, f.machine).Scan(&serial))
	exec(`INSERT INTO bom_items(item_uid,category,serial,source_of_record,first_project_id,credential_batch_ref,created_at,updated_at) VALUES($1,'server',$2,'design_bom',$3,$4,now(),now())`, f.machine, serial, f.project, batch)
	raw, _ := json.Marshal(map[string]any{"values": map[string]string{"serial_number": serial, "credential_batch_ref": batch}})
	exec(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,1,$3,$4,now())`, pkg, f.machine, f.project, raw)
	exec(`INSERT INTO bom_credential_batches(project_id,credential_batch_ref,scope,rotation_policy,vault_path,owner,package_id,updated_at) VALUES($1,$2,'bmc','manual','vault://fixture/bmc','fixture',$3,now())`, f.project, batch, pkg)
	config := bmc.SuiteConfig{Schema: "keel.bmc-inventory/v1", TimeoutSeconds: 5, Targets: []bmc.TargetConfig{{ProjectID: f.project, MachineID: f.machine, Origin: "sim", CredentialBatchRef: batch}}}
	raw, _ = json.Marshal(config)
	path := filepath.Join(t.TempDir(), "bmc.json")
	mustAgent(t, os.WriteFile(path, raw, 0600))
	mustAgent(t, f.store.ConfigureBMCInventory(path, ""))
	f.store.BMCInventoryEnabled = true
	f.store.MediaObjects = &bmcTestObjects{}
	f.service.BMCStore = f.store
	var asset string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT id FROM project_assets WHERE project_id=$1 AND machine_id=$2`, f.project, f.machine).Scan(&asset))
	return f, asset
}
func claimBMC(t *testing.T, f *agentKernelFixture, id string) *Claim {
	t.Helper()
	_, e := f.service.PumpOutbox(context.Background(), "bmc-fixture", 1000)
	mustAgent(t, e)
	claim, e := f.service.claimExecution(context.Background(), id, "bmc-fixture", 10*time.Second)
	mustAgent(t, e)
	return claim
}

// Authored for the P2 milestone; no automatic execution during source increments.
func TestBMCSharedExecutionEvidenceAndScopePostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	input := platform.BMCCollectionInput{Level: "L1", ExpectedAssetVersion: 1, Reason: "Inventory fixture"}
	key := platform.RandomID()
	x, e := f.service.CreateBMCCollection(ctx, f.actor, f.project, asset, input, key, "bmc")
	mustAgent(t, e)
	replay, e := f.service.CreateBMCCollection(ctx, f.actor, f.project, asset, input, key, "bmc")
	mustAgent(t, e)
	if replay.ID != x.ID || x.Mode != "bmc" {
		t.Fatal("BMC create lost idempotency or mode")
	}
	c := claimBMC(t, f, x.ID)
	if c == nil {
		t.Fatal("BMC not claimable")
	}
	var common, channel string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT mode FROM exec_resource_leases WHERE attempt_id=$1 AND resource_id=$2 AND state='held'`, c.Attempt.ID, "device:machine:"+f.machine).Scan(&common))
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT mode FROM exec_resource_leases WHERE attempt_id=$1 AND resource_id=$2 AND state='held'`, c.Attempt.ID, "bmc:machine:"+f.machine).Scan(&channel))
	if common != "shared" || channel != "exclusive" {
		t.Fatal("BMC machine/channel conflict rules differ")
	}
	mustAgent(t, f.service.RunClaim(ctx, c))
	detail, e := f.service.Get(ctx, f.project, x.ID)
	mustAgent(t, e)
	if detail.Execution.State != "success" {
		t.Fatal("sim inventory not settled")
	}
	page, e := f.store.ListBMCCollections(ctx, f.actor, &platform.ProjectSummary{ID: f.project}, asset, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	if page.Total != 1 || page.Items[0].Origin != "sim" || page.Items[0].ResourceCount != 5 {
		t.Fatal("lost raw-backed sim history", page)
	}
	record, e := f.store.GetBMCCollection(ctx, f.actor, &platform.ProjectSummary{ID: f.project}, asset, page.Items[0].ID)
	mustAgent(t, e)
	if record.Report.Validate() != nil {
		t.Fatal("JSONB roundtrip invalidated evidence")
	}
	raw, e := f.store.OpenBMCRaw(ctx, f.actor, &platform.ProjectSummary{ID: f.project}, asset, record.ID, "/redfish/v1/")
	mustAgent(t, e)
	body, e := io.ReadAll(raw)
	raw.Close()
	mustAgent(t, e)
	if !bytes.Contains(body, []byte("RedfishVersion")) {
		t.Fatal("raw object unavailable")
	}
	if _, e = f.store.GetBMCCollection(ctx, f.actor, &platform.ProjectSummary{ID: platform.RandomID()}, asset, record.ID); e == nil {
		t.Fatal("admin read bypassed actual project scope")
	}
	for _, query := range []string{`UPDATE bmc_collection_records SET report='{}' WHERE id=$1`, `DELETE FROM bmc_collection_resources WHERE collection_id=$1`} {
		if _, e = f.store.DB.ExecContext(ctx, query, record.ID); e == nil {
			t.Fatal("immutable collection could be changed")
		}
	}
	input.Level = "L3"
	partial, e := f.service.CreateBMCCollection(ctx, f.actor, f.project, asset, input, platform.RandomID(), "bmc")
	mustAgent(t, e)
	mustAgent(t, f.service.RunClaim(ctx, claimBMC(t, f, partial.ID)))
	detail, e = f.service.Get(ctx, f.project, partial.ID)
	mustAgent(t, e)
	if detail.Execution.State != "failed" {
		t.Fatal("unknown hardware capability passed as full inventory")
	}
}
func TestBMCSharedExecutionBindingFenceAndReceiptPostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	input := platform.BMCCollectionInput{Level: "L1", ExpectedAssetVersion: 1, Reason: "Fenced inventory"}
	create := func() *Execution {
		t.Helper()
		x, e := f.service.CreateBMCCollection(ctx, f.actor, f.project, asset, input, platform.RandomID(), "bmc")
		mustAgent(t, e)
		return x
	}
	first, second := create(), create()
	c := claimBMC(t, f, first.ID)
	if c == nil {
		t.Fatal("first BMC request missing")
	}
	if claimBMC(t, f, second.ID) != nil {
		t.Fatal("concurrent BMC reads escaped channel lease")
	}
	forged := Receipt{CommandID: c.Attempt.CommandID, AttemptID: c.Attempt.ID, FencingToken: c.Attempt.FencingToken, Result: "success", StartedAt: time.Now(), FinishedAt: time.Now(), Output: map[string]any{"collection_id": first.Parameters["bmc_collection_id"], "stopped": true, "hardware_evidence": true}}
	if e := f.service.RecordReceipt(ctx, forged); e == nil {
		t.Fatal("accepted hardware success without retained evidence")
	}
	report, e := f.store.RunBMCCollection(ctx, first.Parameters["bmc_collection_id"].(string), first.ID, c.Attempt.ID, c.Attempt.CommandID, c.Attempt.FencingToken)
	mustAgent(t, e)
	if e = f.store.RecordBMCCollection(ctx, first.ID, c.Attempt.FencingToken+1, report); e == nil {
		t.Fatal("accepted mismatched fencing token")
	}
	mustAgent(t, f.store.RecordBMCCollection(ctx, first.ID, c.Attempt.FencingToken, report))
	mustAgent(t, f.service.RunClaim(ctx, c))
	next := claimBMC(t, f, second.ID)
	if next == nil {
		t.Fatal("BMC channel not released after real receipt")
	}
	_, e = f.store.DB.ExecContext(ctx, `UPDATE project_assets SET version=version+1 WHERE id=$1`, asset)
	mustAgent(t, e)
	mustAgent(t, f.service.RunClaim(ctx, next))
	detail, e := f.service.Get(ctx, f.project, second.ID)
	mustAgent(t, e)
	if detail.Execution.State != "failed" {
		t.Fatal("stale asset collection was reported successful")
	}
	if _, e = f.service.CreateBMCCollection(ctx, f.actor, f.project, asset, input, platform.RandomID(), "bmc"); e == nil {
		t.Fatal("accepted stale asset version")
	}
}
func TestBMCResourceModesKeepSharedMachineAndExclusiveChannel(t *testing.T) {
	if hostResourceLeaseMode("bmc", "device:machine:asset", "shared") != "shared" || hostResourceLeaseMode("bmc", "bmc:machine:asset", "shared") != "exclusive" {
		t.Fatal("BMC reader conflicts with read-only machine work or overlaps its own channel")
	}
}
func TestBMCFallbackPersistsScopedRawEvidenceOnSharedExecutionPostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	var batch, serial string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT b.credential_batch_ref,m.serial_number FROM bom_credential_batches b JOIN machines m ON m.id=$2 WHERE b.project_id=$1 AND b.scope='bmc'`, f.project, f.machine).Scan(&batch, &serial))
	area := []byte{1, 0, 0}
	for _, value := range []string{"Fixture", "Fixture System", "PART-001", "v1", serial, "fixture-tag", "fixture-file"} {
		area = append(area, 0xc0|byte(len(value)))
		area = append(area, []byte(value)...)
	}
	area = append(area, 0xc1)
	for (len(area)+1)%8 != 0 {
		area = append(area, 0)
	}
	area = append(area, 0)
	area[1] = byte(len(area) / 8)
	var checksum byte
	for _, v := range area {
		checksum += v
	}
	area[len(area)-1] -= checksum
	header := []byte{1, 0, 0, 0, 1, 0, 0, 254}
	fru := append(header, area...)
	dir := t.TempDir()
	primaryPath := filepath.Join(dir, "redfish.json")
	fallbackPath := filepath.Join(dir, "ipmi.json")
	suitePath := filepath.Join(dir, "suite.json")
	primary, _ := json.Marshal(map[string]any{"schema": "keel.redfish-fixture/v1", "documents": map[string]any{}, "failures": map[string]bmc.Failure{"/redfish/v1/": {Class: "timeout", Code: "request_timeout", Retryable: true}}})
	mustAgent(t, os.WriteFile(primaryPath, primary, 0600))
	fallback, _ := json.Marshal(map[string]any{"schema": bmc.IPMIParserVersion, "replies": map[string]string{"fru-0": hex.EncodeToString(fru)}})
	mustAgent(t, os.WriteFile(fallbackPath, fallback, 0600))
	config := bmc.SuiteConfig{Schema: "keel.bmc-inventory/v1", TimeoutSeconds: 5, Targets: []bmc.TargetConfig{{ProjectID: f.project, MachineID: f.machine, Origin: "sim", CredentialBatchRef: batch, RedfishFixtureFile: primaryPath, IPMI: &bmc.IPMIConfig{FixtureFile: fallbackPath}}}}
	raw, _ := json.Marshal(config)
	mustAgent(t, os.WriteFile(suitePath, raw, 0600))
	mustAgent(t, f.store.ConfigureBMCInventory(suitePath, ""))
	x, e := f.service.CreateBMCCollection(ctx, f.actor, f.project, asset, platform.BMCCollectionInput{Level: "L3", ExpectedAssetVersion: 1, Reason: "Fallback fixture"}, platform.RandomID(), "fixture")
	mustAgent(t, e)
	claim := claimBMC(t, f, x.ID)
	if claim == nil {
		t.Fatal("fallback execution was not claimable")
	}
	mustAgent(t, f.service.RunClaim(ctx, claim))
	page, e := f.store.ListBMCCollections(ctx, f.actor, &platform.ProjectSummary{ID: f.project}, asset, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	if page.Total != 1 || page.Items[0].State != "partial" || !page.Items[0].IdentityMatched {
		t.Fatal("fallback outcome invented complete Redfish capabilities")
	}
	record := page.Items[0]
	manifest, e := f.store.GetBMCCollectionManifest(ctx, f.actor, &platform.ProjectSummary{ID: f.project}, asset, record.ID)
	mustAgent(t, e)
	if manifest.Fallback == nil || manifest.Fallback.Reason != "redfish_timeout" || manifest.Fallback.ResourceCount != 1 {
		t.Fatal("fallback reason missing from manifest")
	}
	path := "/ipmi/v2/inventory/fru-0"
	resource, e := f.store.GetBMCResource(ctx, f.actor, &platform.ProjectSummary{ID: f.project}, asset, record.ID, path)
	mustAgent(t, e)
	if resource.Type != "#IPMI.Inventory.v1" {
		t.Fatal("IPMI provenance lost")
	}
	body, e := f.store.OpenBMCRaw(ctx, f.actor, &platform.ProjectSummary{ID: f.project}, asset, record.ID, path)
	mustAgent(t, e)
	retained, e := io.ReadAll(body)
	body.Close()
	mustAgent(t, e)
	if bmc.CanonicalDigest(retained) != resource.DataSHA256 {
		t.Fatal("retained fallback bytes differ from JSONB")
	}
	if _, e = f.store.OpenBMCRaw(ctx, f.actor, &platform.ProjectSummary{ID: f.project}, platform.RandomID(), record.ID, path); e == nil {
		t.Fatal("cross-asset fallback raw exposed")
	}
}
