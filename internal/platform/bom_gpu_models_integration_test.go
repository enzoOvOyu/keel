package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestBOMPackageGPUInventory(t *testing.T) {
	for _, kind := range []string{"server", "compute-tray"} {
		t.Run(kind, func(t *testing.T) { testBOMPackageGPUInventory(t, kind) })
	}
}

func testBOMPackageGPUInventory(t *testing.T, rootKind string) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-gpu-bom-password"
	if _, err := s.CreateUser(ctx, "gpu-admin", "GPU admin", password, true, true); err != nil {
		t.Fatal(err)
	}
	_, actor := testLogin(t, s, "gpu-admin", password, "198.51.100.93")
	project, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "GPU BOM", Slug: "p1"}, "gpu-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	profile := testBOMProfile()
	if _, err = s.CreateBOMMappingProfile(ctx, actor, project, CreateBOMMappingProfileInput{VersionRef: "profile-v1", ServerColumns: profile.ServerColumns, FabricColumns: profile.FabricColumns}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApproveBOMMappingProfile(ctx, actor, project, "profile-v1", "test"); err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	var serverID string
	for _, kind := range []string{"server", "nic", "switch", "optic_module", "gpu", "other-gpu"} {
		category := kind
		if kind == "server" {
			category = rootKind
		}
		if kind == "other-gpu" {
			category = "gpu"
		}
		model, e := s.CreateDeviceModel(ctx, actor, project, CreateDeviceModelInput{Name: "fixture-" + kind, Kind: category}, "test")
		if e != nil {
			t.Fatal(e)
		}
		in := PublishModelVersionInput{}
		if kind == "server" {
			serverID = model.ID
			in.HwSpec = map[string]any{"gpu_count": 1, "gpu": "fixture-gpu"}
			in.BomExpect = map[string]any{"nic_count": 1, "mac_prefixes": []string{"aa:bb:cc"}}
			in.Slots = []ModelSlot{{SlotNo: 1, PcieSlot: "PCIe-1", ExpectNicModel: "fixture-nic", PanelPort: "p1", DefaultRole: "compute"}}
		}
		if kind == "nic" || kind == "switch" {
			in.CapabilityMatrix = map[string]any{"ports": map[string]any{"1": map[string]any{"sub_type": "fixture-200g", "speed": "200G"}}}
		}
		v, e := s.PublishModelVersion(ctx, actor, project, model.ID, in, "test")
		if e != nil {
			t.Fatal(e)
		}
		ids[kind] = v.ID
	}
	machine := RandomID()
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,rack,unit_position,created_at,updated_at) VALUES($1,'SN-1','R01','1',now(),now())`, machine); err != nil {
		t.Fatal(err)
	}
	makeFiles := func(parent, ref, subtype string, quantity int) map[string][]byte {
		files := bomFixture(t)
		for name, data := range files {
			for kind, id := range ids {
				if kind == "optic_module" {
					kind = "optic"
				}
				data = bytes.ReplaceAll(data, []byte(kind+"-v1"), []byte(id))
			}
			files[name] = data
		}
		for i := 0; i < quantity; i++ {
			files["server/server_components.csv"] = append(files["server/server_components.csv"], []byte(fmt.Sprintf("%d,%s,gpu,%s,%d,GPU-%d,%s,\n", 8+i, parent, subtype, i+1, i+1, ref))...)
		}
		var manifest BOMPackageManifest
		if e := json.Unmarshal(files["manifest.json"], &manifest); e != nil {
			t.Fatal(e)
		}
		manifest.ProjectID = project.ID
		manifest.RowCount = 8 + quantity
		files["manifest.json"], _ = json.Marshal(manifest)
		rehashBOMFixture(t, files)
		return files
	}
	// Compare against a newer independent specification without changing the old package.
	newer, err := s.PublishModelVersion(ctx, actor, project, serverID, PublishModelVersionInput{HwSpec: map[string]any{"gpu_count": 2, "gpu": "fixture-gpu"}}, "test")
	if err != nil {
		t.Fatal(err)
	}
	// SN 20 belongs to line 1, while reference 20 must resolve to the other
	// server on line 20. Hardware grouping must follow the writer's precedence.
	collisionFiles := func() map[string][]byte {
		files := makeFiles("1", ids["gpu"], "fixture-gpu", 1)
		files["server/servers.csv"] = bytes.ReplaceAll(files["server/servers.csv"], []byte("1,SN-1,"), []byte("1,20,"))
		files["server/server_components.csv"] = bytes.ReplaceAll(files["server/server_components.csv"], []byte("2,SN-1,"), []byte("2,1,"))
		files["server/servers.csv"] = append(files["server/servers.csv"], []byte(fmt.Sprintf("20,SN-SECOND,srv-second,%s,R01,2,B300,design_bom\n", newer.ID))...)
		files["server/server_components.csv"] = append(files["server/server_components.csv"], []byte(fmt.Sprintf("21,20,nic,CX8,1,NIC-SECOND,%s,aa:bb:cc:dd:ee:02\n22,20,gpu,fixture-gpu,1,GPU-SECOND-1,%s,\n24,20,gpu,fixture-gpu,2,GPU-SECOND-2,%s,\n", ids["nic"], ids["gpu"], ids["gpu"]))...)
		files["server/server_ports.csv"] = append(files["server/server_ports.csv"], []byte("23,NIC-SECOND,1,compute,aa:bb:cc:dd:ee:02,0\n")...)
		var manifest BOMPackageManifest
		json.Unmarshal(files["manifest.json"], &manifest)
		manifest.RowCount += 5
		files["manifest.json"], _ = json.Marshal(manifest)
		rehashBOMFixture(t, files)
		return files
	}
	if checked, e := s.PreviewBOMPackage(ctx, actor, project, collisionFiles()); e != nil || !checked.CanCommit {
		t.Fatalf("numeric SN/line identity confused %s: %+v %v", rootKind, checked, e)
	}
	badIdentity := collisionFiles()
	badIdentity["server/server_components.csv"] = bytes.ReplaceAll(badIdentity["server/server_components.csv"], []byte("GPU-SECOND-1,"+ids["gpu"]), []byte("GPU-SECOND-1,"+ids["other-gpu"]))
	rehashBOMFixture(t, badIdentity)
	checked, identityErr := s.PreviewBOMPackage(ctx, actor, project, badIdentity)
	identityFound := false
	if checked != nil {
		for _, issue := range checked.Issues {
			if issue.Code == "model_gpu_type_mismatch" && issue.ModelVersionRef == newer.ID && issue.BOMLineNo == 22 {
				identityFound = true
			}
		}
	}
	if identityErr != nil || !identityFound || checked.CanCommit || checked.PreviewToken != "" {
		t.Fatalf("wrong resolved hardware owner: %+v %v", checked, identityErr)
	}
	if _, e := s.CommitBOMPackage(ctx, actor, project, badIdentity, bomPreviewToken(actor, project.ID, badIdentity), "test"); e == nil {
		t.Fatal("wrong resolved owner bypassed commit")
	}
	// A compute tray is still a physical machine, never a rack aggregate.
	for _, kind := range []string{"rack", "switch"} {
		wrong, e := s.CreateDeviceModel(ctx, actor, project, CreateDeviceModelInput{Name: "Invalid-root-" + kind, Kind: kind}, "test")
		if e != nil {
			t.Fatal(e)
		}
		spec := PublishModelVersionInput{}
		if kind == "switch" {
			spec.CapabilityMatrix = map[string]any{"ports": map[string]any{"1": map[string]any{"speed": "200G"}}}
		}
		v, e := s.PublishModelVersion(ctx, actor, project, wrong.ID, spec, "test")
		if e != nil {
			t.Fatal(e)
		}
		files := makeFiles("SN-1", ids["gpu"], "fixture-gpu", 1)
		files["server/servers.csv"] = bytes.ReplaceAll(files["server/servers.csv"], []byte(ids["server"]), []byte(v.ID))
		rehashBOMFixture(t, files)
		checked, e := s.PreviewBOMPackage(ctx, actor, project, files)
		found := false
		if checked != nil {
			for _, issue := range checked.Issues {
				if issue.Code == "model_kind_mismatch" {
					found = true
				}
			}
		}
		if e != nil || !found || checked.CanCommit || checked.PreviewToken != "" {
			t.Fatalf("invalid root %s: %+v %v", kind, checked, e)
		}
		if _, e = s.CommitBOMPackage(ctx, actor, project, files, bomPreviewToken(actor, project.ID, files), "test"); e == nil {
			t.Fatal("invalid root kind committed")
		}
	}
	wide := []byte("SN,hostname,model_name,MAC,port_name,rail\nTRAY-WIDE,tray-wide,fixture-server,aa:bb:cc:dd:ee:09,p1,0\n")
	legacy, e := s.PreviewLegacyBOM(ctx, actor, project, "BOM-BATCH-P1-20261024-1", wide)
	found := false
	if legacy != nil {
		for _, issue := range legacy.Issues {
			if issue.Code == "legacy_gpu_inventory_unverifiable" && issue.ModelVersionRef == newer.ID && issue.BOMLineNo == 2 {
				found = true
			}
		}
	}
	if e != nil || !found || legacy.CanCommit {
		t.Fatalf("wide %s inferred GPU inventory: %+v %v", rootKind, legacy, e)
	}
	for _, parent := range []string{"SN-1", "1", machine} {
		files := makeFiles(parent, ids["gpu"], "user-label-is-not-authority", 1)
		preview, e := s.PreviewBOMPackage(ctx, actor, project, files)
		if e != nil || !preview.CanCommit {
			t.Fatalf("fixed version or parent %s: %+v %v", parent, preview, e)
		}
	}
	for _, tc := range []struct {
		name, ref, subtype, code string
		quantity                 int
		version                  string
	}{
		{"missing", ids["gpu"], "fixture-gpu", "model_gpu_count_mismatch", 0, ""},
		{"extra", ids["gpu"], "fixture-gpu", "model_gpu_count_mismatch", 2, ""},
		{"forged subtype", ids["other-gpu"], "fixture-gpu", "model_gpu_type_mismatch", 1, ""},
		{"new version", ids["gpu"], "fixture-gpu", "model_gpu_count_mismatch", 1, newer.ID},
		{"NIC slot", ids["gpu"], "fixture-gpu", "model_nic_slot_invalid", 1, ""},
		{"MAC prefix", ids["gpu"], "fixture-gpu", "model_mac_prefix_mismatch", 1, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := makeFiles(machine, tc.ref, tc.subtype, tc.quantity)
			if tc.name == "NIC slot" {
				files["server/server_components.csv"] = bytes.ReplaceAll(files["server/server_components.csv"], []byte(",1,NIC-1,"), []byte(",99,NIC-1,"))
			}
			if tc.name == "MAC prefix" {
				for _, path := range []string{"server/server_components.csv", "server/server_ports.csv"} {
					files[path] = bytes.ReplaceAll(files[path], []byte("aa:bb:cc:dd:ee:01"), []byte("00:11:22:dd:ee:01"))
				}
			}
			rehashBOMFixture(t, files)
			if tc.version != "" {
				files["server/servers.csv"] = bytes.ReplaceAll(files["server/servers.csv"], []byte(ids["server"]), []byte(tc.version))
				rehashBOMFixture(t, files)
			}
			preview, e := s.PreviewBOMPackage(ctx, actor, project, files)
			if e != nil || preview.CanCommit || preview.PreviewToken != "" {
				t.Fatalf("GPU mismatch allowed: %+v %v", preview, e)
			}
			found := false
			for _, issue := range preview.Issues {
				if issue.Code == tc.code && issue.Severity == "BLOCK" && issue.ModelVersionRef != "" && issue.Expected != "" && issue.Actual != "" && issue.BOMLineNo > 0 {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing GPU difference: %+v", preview.Issues)
			}
			if _, e = s.CommitBOMPackage(ctx, actor, project, files, bomPreviewToken(actor, project.ID, files), "test"); e == nil {
				t.Fatal("forged token bypassed GPU revalidation")
			}
		})
	}
	var writes int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM bom_import_packages WHERE project_id=$1`, project.ID).Scan(&writes); err != nil || writes != 0 {
		t.Fatalf("blocked GPU writes: %d %v", writes, err)
	}
	// Positive commit persists the real component and exact server version.
	files := collisionFiles()
	preview, err := s.PreviewBOMPackage(ctx, actor, project, files)
	if err != nil || !preview.CanCommit {
		t.Fatalf("valid GPU preview: %+v %v", preview, err)
	}
	if _, err = s.CommitBOMPackage(ctx, actor, project, files, preview.PreviewToken, "test"); err != nil {
		t.Fatal(err)
	}
	// A subsequent standard package may refer to the stable NIC UID instead
	// of its serial or line. Summary comparison must resolve that same owner.
	var nicUID string
	if err = s.DB.QueryRowContext(ctx, "SELECT id FROM components WHERE serial_number='NIC-1'").Scan(&nicUID); err != nil {
		t.Fatal(err)
	}
	uidFiles := collisionFiles()
	uidFiles["server/server_ports.csv"] = bytes.ReplaceAll(uidFiles["server/server_ports.csv"], []byte("3,NIC-1,"), []byte("3,"+nicUID+","))
	var uidManifest BOMPackageManifest
	json.Unmarshal(uidFiles["manifest.json"], &uidManifest)
	uidManifest.Seq = 2
	uidManifest.BatchID = "BOM-BATCH-P1-20261020-2"
	uidFiles["manifest.json"], _ = json.Marshal(uidManifest)
	rehashBOMFixture(t, uidFiles)
	uidPreview, e := s.PreviewBOMPackage(ctx, actor, project, uidFiles)
	if e != nil || !uidPreview.CanCommit {
		t.Fatalf("stable NIC UID lost MAC ownership: %+v %v", uidPreview, e)
	}
	badSummary := map[string][]byte{}
	for name, data := range uidFiles {
		badSummary[name] = append([]byte{}, data...)
	}
	badSummary["server/server_components.csv"] = bytes.ReplaceAll(badSummary["server/server_components.csv"], []byte("aa:bb:cc:dd:ee:01"), []byte("aa:bb:cc:dd:ee:09"))
	rehashBOMFixture(t, badSummary)
	badPreview, e := s.PreviewBOMPackage(ctx, actor, project, badSummary)
	located := false
	if badPreview != nil {
		for _, issue := range badPreview.Issues {
			if issue.Code == "component_port_mac_mismatch" && issue.BOMLineNo == 2 && issue.Expected == "aa:bb:cc:dd:ee:09" && issue.Actual == "aa:bb:cc:dd:ee:01" {
				located = true
			}
		}
	}
	if e != nil || !located || badPreview.CanCommit || badPreview.PreviewToken != "" {
		t.Fatalf("UID summary mismatch escaped: %+v %v", badPreview, e)
	}
	if _, e = s.CommitBOMPackage(ctx, actor, project, badSummary, bomPreviewToken(actor, project.ID, badSummary), "test"); e == nil {
		t.Fatal("UID summary mismatch committed")
	}
	if _, e = s.CommitBOMPackage(ctx, actor, project, uidFiles, uidPreview.PreviewToken, "test"); e != nil {
		t.Fatal(e)
	}
	var preserved string
	if e = s.DB.QueryRowContext(ctx, "SELECT id FROM components WHERE serial_number='NIC-1'").Scan(&preserved); e != nil || preserved != nicUID {
		t.Fatal("UID reimport created a different NIC")
	}
	var secondVersion string
	if err = s.DB.QueryRowContext(ctx, `SELECT a.model_version_id FROM project_assets a JOIN components c ON c.machine_id=a.machine_id WHERE a.project_id=$1 AND c.serial_number='GPU-SECOND-1'`, project.ID).Scan(&secondVersion); err != nil || secondVersion != newer.ID {
		t.Fatalf("second GPU assigned to wrong machine: %s %v", secondVersion, err)
	}
	var actual, version string
	if err = s.DB.QueryRowContext(ctx, `SELECT a.model_version_id,i.model_version_id FROM project_assets a JOIN components c ON c.machine_id=a.machine_id JOIN bom_items i ON i.item_uid=c.id WHERE a.project_id=$1 AND c.serial_number='GPU-1'`, project.ID).Scan(&version, &actual); err != nil || version != ids["server"] || actual != ids["gpu"] {
		t.Fatalf("wrong GPU binding: %s %s %v", version, actual, err)
	}
}
