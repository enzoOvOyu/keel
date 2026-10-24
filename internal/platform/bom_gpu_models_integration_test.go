package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
)

func TestBOMPackageGPUInventory(t *testing.T) {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := makeFiles(machine, tc.ref, tc.subtype, tc.quantity)
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
	files := makeFiles(machine, ids["gpu"], "untrusted-label", 1)
	preview, err := s.PreviewBOMPackage(ctx, actor, project, files)
	if err != nil || !preview.CanCommit {
		t.Fatalf("valid GPU preview: %+v %v", preview, err)
	}
	if _, err = s.CommitBOMPackage(ctx, actor, project, files, preview.PreviewToken, "test"); err != nil {
		t.Fatal(err)
	}
	var actual, version string
	if err = s.DB.QueryRowContext(ctx, `SELECT a.model_version_id,i.model_version_id FROM project_assets a JOIN components c ON c.machine_id=a.machine_id JOIN bom_items i ON i.item_uid=c.id WHERE a.project_id=$1 AND c.serial_number='GPU-1'`, project.ID).Scan(&version, &actual); err != nil || version != ids["server"] || actual != ids["gpu"] {
		t.Fatalf("wrong GPU binding: %s %s %v", version, actual, err)
	}
}
