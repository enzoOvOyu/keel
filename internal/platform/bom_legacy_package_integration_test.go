package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestLegacyBOMAtomicBatchIdentity(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-legacy-bom-password"
	if _, err := s.CreateUser(ctx, "legacy-bom-admin", "Legacy BOM Admin", password, true, true); err != nil {
		t.Fatal(err)
	}
	_, actor := testLogin(t, s, "legacy-bom-admin", password, "198.51.100.96")
	project, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Legacy BOM", Slug: "p1"}, "new-legacy-bom-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateDeviceModel(ctx, actor, project, CreateDeviceModelInput{Name: "server-v1", Kind: "server"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	serverVersion, err := s.PublishModelVersion(ctx, actor, project, model.ID, PublishModelVersionInput{HwSpec: map[string]any{"name": "server-v1"}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	batch := "BOM-BATCH-P1-20261020-1"
	data := []byte("SN,hostname,model_name,MAC,port_name,rail\nSN-1,node-1,server-v1,aa:bb:cc:dd:ee:01,p1,0\nSN-1,node-1,server-v1,aa:bb:cc:dd:ee:02,p2,1\n")
	preview, err := s.PreviewLegacyBOM(ctx, actor, project, batch, data)
	if err != nil || !preview.CanCommit {
		t.Fatalf("preview: %+v, %v", preview, err)
	}
	var before int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM bom_items`).Scan(&before); err != nil || before != 0 {
		t.Fatalf("preview wrote material: %d %v", before, err)
	}
	first, err := s.CommitLegacyBOM(ctx, actor, project, batch, data, preview.PreviewToken, "test")
	if err != nil {
		t.Fatal(err)
	}
	if first.ItemCount != 5 {
		t.Fatalf("wanted server+2 NIC+2 port, got %d", first.ItemCount)
	}
	var serverID, nicID, portID string
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='server' AND serial='SN-1'`).Scan(&serverID); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='nic' AND serial='LEGACY-MAC-aabbccddee01'`).Scan(&nicID); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='port' AND parent_uid=$1 AND port_index='p1'`, nicID).Scan(&portID); err != nil {
		t.Fatal(err)
	}
	var machineID, physicalID string
	if err = s.DB.QueryRowContext(ctx, `SELECT id FROM machines WHERE serial_number='SN-1'`).Scan(&machineID); err != nil || machineID != serverID {
		t.Fatalf("machine identity: %s %v", machineID, err)
	}
	if err = s.DB.QueryRowContext(ctx, `SELECT id FROM physical_ports WHERE mac_address='aa:bb:cc:dd:ee:01'`).Scan(&physicalID); err != nil || physicalID != portID {
		t.Fatalf("port identity: %s %v", physicalID, err)
	}
	repeat, err := s.CommitLegacyBOM(ctx, actor, project, batch, data, preview.PreviewToken, "test")
	if err != nil || !repeat.Idempotent || repeat.PackageID != first.PackageID {
		t.Fatalf("idempotency: %+v %v", repeat, err)
	}
	changed := bytes.ReplaceAll(data, []byte("node-1"), []byte("node-2"))
	conflict, err := s.PreviewLegacyBOM(ctx, actor, project, batch, changed)
	if err != nil || conflict.CanCommit {
		t.Fatalf("same batch different hash accepted: %+v %v", conflict, err)
	}
	if _, err = s.CommitLegacyBOM(ctx, actor, project, batch, changed, preview.PreviewToken, "test"); err == nil {
		t.Fatal("old token accepted changed CSV")
	}
	duplicateHost := []byte("SN,hostname,model_name,MAC,port_name,rail\nSN-2,node-2,server-v1,aa:bb:cc:dd:ee:03,p1,0\nSN-3,node-2,server-v1,aa:bb:cc:dd:ee:04,p1,0\n")
	hostPreview, err := s.PreviewLegacyBOM(ctx, actor, project, "BOM-BATCH-P1-20261020-4", duplicateHost)
	if err != nil || hostPreview.CanCommit || !issueFound(&BOMPackage{Issues: hostPreview.Issues}, "hostname_conflict") {
		t.Fatalf("duplicate hostname accepted: %+v %v", hostPreview, err)
	}
	next := "BOM-BATCH-P1-20261020-2"
	nextPreview, err := s.PreviewLegacyBOM(ctx, actor, project, next, data)
	if err != nil || !nextPreview.CanCommit {
		t.Fatalf("next preview: %+v %v", nextPreview, err)
	}
	second, err := s.CommitLegacyBOM(ctx, actor, project, next, data, nextPreview.PreviewToken, "test")
	if err != nil || second.PackageID == first.PackageID {
		t.Fatalf("next commit: %+v %v", second, err)
	}
	var stableID string
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='nic' AND serial='LEGACY-MAC-aabbccddee01'`).Scan(&stableID); err != nil || stableID != nicID {
		t.Fatalf("NIC identity moved: %s %v", stableID, err)
	}
	var switches int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM bom_items WHERE category='switch'`).Scan(&switches); err != nil || switches != 0 {
		t.Fatalf("legacy import created network material: %d %v", switches, err)
	}
	profile := testBOMProfile()
	profileInput := CreateBOMMappingProfileInput{VersionRef: "profile-v1", ServerColumns: profile.ServerColumns, FabricColumns: profile.FabricColumns}
	if _, err = s.CreateBOMMappingProfile(ctx, actor, project, profileInput, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApproveBOMMappingProfile(ctx, actor, project, profileInput.VersionRef, "test"); err != nil {
		t.Fatal(err)
	}
	versions := map[string]string{"server-v1": serverVersion.ID}
	for _, kind := range []string{"nic", "switch", "optic_module"} {
		m, e := s.CreateDeviceModel(ctx, actor, project, CreateDeviceModelInput{Name: kind + "-v1", Kind: kind}, "test")
		if e != nil {
			t.Fatal(e)
		}
		publish := PublishModelVersionInput{HwSpec: map[string]any{"name": kind}}
		if kind == "nic" {
			publish.CapabilityMatrix = map[string]any{"ports": map[string]any{"1": map[string]any{"sub_type": "nic-200g"}}}
		}
		if kind == "switch" {
			publish.CapabilityMatrix = map[string]any{"ports": map[string]any{"1": map[string]any{"speed": "200G", "sub_type": "ethernet-200g"}}}
		}
		v, e := s.PublishModelVersion(ctx, actor, project, m.ID, publish, "test")
		if e != nil {
			t.Fatal(e)
		}
		versions[kind+"-v1"] = v.ID
		if kind == "optic_module" {
			versions["optic-v1"] = v.ID
		}
	}
	standard := bomFixture(t)
	for name, data := range standard {
		if name == "manifest.json" {
			continue
		}
		for before, after := range versions {
			data = bytes.ReplaceAll(data, []byte(before), []byte(after))
		}
		standard[name] = data
	}
	var manifest BOMPackageManifest
	if err = json.Unmarshal(standard["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.ProjectID = project.ID
	manifest.BatchID = "BOM-BATCH-P1-20261020-3"
	manifest.Seq = 3
	standard["manifest.json"], _ = json.Marshal(manifest)
	rehashBOMFixture(t, standard)
	standardPreview, e := s.PreviewBOMPackage(ctx, actor, project, standard)
	if e != nil || !standardPreview.CanCommit {
		t.Fatalf("standard adoption preview: %+v %v", standardPreview, e)
	}
	if _, e = s.CommitBOMPackage(ctx, actor, project, standard, standardPreview.PreviewToken, "test"); e != nil {
		t.Fatal(e)
	}
	var adoptedNIC, adoptedPort string
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='nic' AND serial='NIC-1'`).Scan(&adoptedNIC); err != nil || adoptedNIC != nicID {
		t.Fatalf("real serial did not adopt provisional NIC: %s %v", adoptedNIC, err)
	}
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='port' AND parent_uid=$1 AND port_index='1'`, nicID).Scan(&adoptedPort); err != nil || adoptedPort != portID {
		t.Fatalf("standard port did not adopt provisional port: %s %v", adoptedPort, err)
	}
}
