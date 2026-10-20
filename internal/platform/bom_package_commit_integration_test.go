package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
)

func TestBOMPackageCommitAtomicIdentityAndIdempotency(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-bom-commit-password"
	if _, err := s.CreateUser(ctx, "bom-commit-admin", "BOM Commit Admin", password, true, true); err != nil {
		t.Fatal(err)
	}
	_, actor := testLogin(t, s, "bom-commit-admin", password, "198.51.100.92")
	project, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "BOM commit", Slug: "p1"}, "new-bom-commit-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	profile := testBOMProfile()
	input := CreateBOMMappingProfileInput{VersionRef: "profile-v1", ServerColumns: profile.ServerColumns, FabricColumns: profile.FabricColumns}
	if _, err = s.CreateBOMMappingProfile(ctx, actor, project, input, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ApproveBOMMappingProfile(ctx, actor, project, input.VersionRef, "test"); err != nil {
		t.Fatal(err)
	}
	modelIDs := map[string]string{}
	for _, kind := range []string{"server", "nic", "switch", "optic_module"} {
		model, createErr := s.CreateDeviceModel(ctx, actor, project, CreateDeviceModelInput{Name: "test-" + kind, Kind: kind}, "test")
		if createErr != nil {
			t.Fatal(createErr)
		}
		publish := PublishModelVersionInput{HwSpec: map[string]any{"name": kind}}
		if kind == "server" {
			publish.ExpectedTopology = map[string]any{"rail_count": 1}
		}
		if kind == "nic" {
			publish.CapabilityMatrix = map[string]any{"ports": map[string]any{"1": map[string]any{"sub_type": "nic-200g"}}}
		}
		if kind == "switch" {
			publish.CapabilityMatrix = map[string]any{"ports": map[string]any{"1": map[string]any{"speed": "200G", "sub_type": "ethernet-200g"}, "2": map[string]any{"speed": "200G", "sub_type": "ethernet-200g"}}}
			publish.HwSpec = map[string]any{"psu_count": 2, "feed_count": 2, "max_power_w": 1000}
			publish.ExpectedTopology = map[string]any{"rail_count": 1}
		}
		version, publishErr := s.PublishModelVersion(ctx, actor, project, model.ID, publish, "test")
		if publishErr != nil {
			t.Fatal(publishErr)
		}
		modelIDs[kind] = version.ID
	}
	files := bomFixture(t)
	replaces := map[string]string{"server-v1": modelIDs["server"], "nic-v1": modelIDs["nic"], "switch-v1": modelIDs["switch"], "optic-v1": modelIDs["optic_module"]}
	for name, data := range files {
		if name == "manifest.json" {
			continue
		}
		for before, after := range replaces {
			data = bytes.ReplaceAll(data, []byte(before), []byte(after))
		}
		files[name] = data
	}
	var manifest BOMPackageManifest
	if err = json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.ProjectID = project.ID
	files["manifest.json"], _ = json.Marshal(manifest)
	rehashBOMFixture(t, files)
	if err = json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	legacyMachineID, legacyPortID := RandomID(), RandomID()
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO machines(id,serial_number,rack,unit_position,created_at,updated_at) VALUES($1,'SN-1','R01','1',now(),now())`, legacyMachineID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO physical_ports(id,machine_id,mac_address,port_name,rail,created_at) VALUES($1,$2,'aa:bb:cc:dd:ee:01','legacy-p1',1,now())`, legacyPortID, legacyMachineID); err != nil {
		t.Fatal(err)
	}
	preview, err := s.PreviewBOMPackage(ctx, actor, project, files)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.CanCommit {
		t.Fatalf("valid package blocked: %+v", preview.Issues)
	}
	engineering := make(map[string][]byte, len(files))
	for name, data := range files {
		engineering[name] = append([]byte(nil), data...)
	}
	engineering["fabric/switches.csv"] = []byte("bom_line_no,sn,hostname,model_ref,role,rack,idx,dc,pod,source_of_record,status,planned_psu_count,planned_feed_count,power_budget_w,max_downlink_uplink_ratio\n4,SW-1,leaf-1," + modelIDs["switch"] + ",leaf,R01,1,DC,P1,design_bom,planned,1,2,900,0.5\n")
	engineering["fabric/switch_ports.csv"] = []byte("bom_line_no,parent_ref,port_index,port_name,port_role,speed,rail,bandwidth_direction\n5,SW-1,1,swp1s0,compute,200G,0,downlink\n8,SW-1,2,swp2,storage,200G,,uplink\n")
	var engineeringManifest BOMPackageManifest
	if err = json.Unmarshal(engineering["manifest.json"], &engineeringManifest); err != nil {
		t.Fatal(err)
	}
	engineeringManifest.BatchID = "BOM-BATCH-P1-20261020-8"
	engineeringManifest.Seq = 8
	engineeringManifest.RowCount = 9
	for i := range engineeringManifest.DomainFiles {
		switch engineeringManifest.DomainFiles[i].File {
		case "fabric/switches.csv":
			engineeringManifest.DomainFiles[i].Columns = []string{"bom_line_no", "sn", "hostname", "model_ref", "role", "rack", "idx", "dc", "pod", "source_of_record", "status", "planned_psu_count", "planned_feed_count", "power_budget_w", "max_downlink_uplink_ratio"}
		case "fabric/switch_ports.csv":
			engineeringManifest.DomainFiles[i].Columns = []string{"bom_line_no", "parent_ref", "port_index", "port_name", "port_role", "speed", "rail", "bandwidth_direction"}
		}
	}
	engineering["manifest.json"], _ = json.Marshal(engineeringManifest)
	rehashBOMFixture(t, engineering)
	checked, checkErr := s.PreviewBOMPackage(ctx, actor, project, engineering)
	if checkErr != nil || checked.CanCommit || !issueFound(&BOMPackage{Issues: checked.Issues}, "switch_power_count_below_model") || !issueFound(&BOMPackage{Issues: checked.Issues}, "switch_power_budget_below_model") || !issueFound(&BOMPackage{Issues: checked.Issues}, "bandwidth_ratio_exceeded") {
		t.Fatalf("network engineering contradictions accepted: %+v %v", checked, checkErr)
	}
	wrongRail := make(map[string][]byte, len(engineering))
	for name, data := range engineering {
		wrongRail[name] = append([]byte(nil), data...)
	}
	wrongRail["fabric/switches.csv"] = bytes.ReplaceAll(wrongRail["fabric/switches.csv"], []byte(",planned,1,2,900,0.5"), []byte(",planned,2,2,1000,2"))
	wrongRail["fabric/switch_ports.csv"] = bytes.ReplaceAll(wrongRail["fabric/switch_ports.csv"], []byte("compute,200G,0,downlink"), []byte("compute,200G,1,downlink"))
	var cableRailManifest BOMPackageManifest
	if err = json.Unmarshal(wrongRail["manifest.json"], &cableRailManifest); err != nil {
		t.Fatal(err)
	}
	cableRailManifest.BatchID = "BOM-BATCH-P1-20261020-9"
	cableRailManifest.Seq = 9
	wrongRail["manifest.json"], _ = json.Marshal(cableRailManifest)
	rehashBOMFixture(t, wrongRail)
	cableRailPreview, cableRailErr := s.PreviewBOMPackage(ctx, actor, project, wrongRail)
	if cableRailErr != nil || cableRailPreview.CanCommit || !issueFound(&BOMPackage{Issues: cableRailPreview.Issues}, "cable_rail_mismatch") {
		t.Fatalf("planned cable rail mismatch accepted: %+v %v", cableRailPreview, cableRailErr)
	}
	baselineInput := CreateNetworkBaselineInput{VersionRef: "engineering-v1", SourceRef: "customer-network-plan-1", EngineeringLimits: NetworkEngineeringLimits{SwitchBandwidthRatioLimits: map[string]float64{"role:leaf": 1.5}}}
	if _, err = s.CreateNetworkBaseline(ctx, actor, project, baselineInput, "test"); err != nil {
		t.Fatal(err)
	}
	withBaseline := make(map[string][]byte, len(engineering))
	for name, data := range engineering {
		withBaseline[name] = append([]byte(nil), data...)
	}
	withBaseline["fabric/switches.csv"] = bytes.ReplaceAll(withBaseline["fabric/switches.csv"], []byte(",planned,1,2,900,0.5"), []byte(",planned,2,2,1000,1.5"))
	var baselineManifest BOMPackageManifest
	if err = json.Unmarshal(withBaseline["manifest.json"], &baselineManifest); err != nil {
		t.Fatal(err)
	}
	baselineManifest.BatchID = "BOM-BATCH-P1-20261020-10"
	baselineManifest.Seq = 10
	baselineManifest.NetworkBaselineRef = "engineering-v1"
	withBaseline["manifest.json"], _ = json.Marshal(baselineManifest)
	rehashBOMFixture(t, withBaseline)
	draftPreview, draftErr := s.PreviewBOMPackage(ctx, actor, project, withBaseline)
	if draftErr != nil || draftPreview.CanCommit || !issueFound(&BOMPackage{Issues: draftPreview.Issues}, "network_baseline_not_approved") {
		t.Fatalf("draft baseline accepted: %+v %v", draftPreview, draftErr)
	}
	if _, err = s.ApproveNetworkBaseline(ctx, actor, project, "engineering-v1", ApproveNetworkBaselineInput{CustomerApprovalRef: "customer-signoff-1", NetworkApprovalRef: "ne-signoff-1"}, "test"); err != nil {
		t.Fatal(err)
	}
	approvedPreview, approvedErr := s.PreviewBOMPackage(ctx, actor, project, withBaseline)
	if approvedErr != nil || !approvedPreview.CanCommit || issueFound(&BOMPackage{Issues: approvedPreview.Issues}, "bandwidth_ratio_limit_approval_unverified") {
		t.Fatalf("approved engineering baseline not accepted: %+v %v", approvedPreview, approvedErr)
	}
	withBaseline["fabric/switches.csv"] = bytes.ReplaceAll(withBaseline["fabric/switches.csv"], []byte(",planned,2,2,1000,1.5"), []byte(",planned,2,2,1000,2"))
	rehashBOMFixture(t, withBaseline)
	loosePreview, looseErr := s.PreviewBOMPackage(ctx, actor, project, withBaseline)
	if looseErr != nil || loosePreview.CanCommit || !issueFound(&BOMPackage{Issues: loosePreview.Issues}, "bandwidth_ratio_limit_exceeds_baseline") {
		t.Fatalf("loose BOM ratio accepted: %+v %v", loosePreview, looseErr)
	}
	first, err := s.CommitBOMPackage(ctx, actor, project, files, preview.PreviewToken, "test")
	if err != nil {
		t.Fatal(err)
	}
	if first.ItemCount != 7 {
		t.Fatalf("item count %d", first.ItemCount)
	}
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO bom_import_issues(id,package_id,severity,file_path,bom_line_no,field,code,created_at) VALUES($1,$2,'WARN','server/servers.csv',1,'hostname','test_warning',now())`, RandomID(), first.PackageID); err != nil {
		t.Fatal(err)
	}
	listed, err := s.ListBOMPackageIssues(ctx, actor, project.ID, first.PackageID, "WARN")
	if err != nil || !issueFound(&BOMPackage{Issues: listed.Issues}, "test_warning") || !issueFound(&BOMPackage{Issues: listed.Issues}, "optic_speed_unverified") {
		t.Fatalf("saved issue query failed: %+v, %v", listed, err)
	}
	if _, err = s.ListBOMPackageIssues(ctx, actor, project.ID, first.PackageID, "invalid"); err == nil {
		t.Fatal("invalid severity accepted")
	}
	if _, err = s.ListBOMPackageIssues(ctx, actor, project.ID, RandomID(), ""); err == nil {
		t.Fatal("missing package accepted")
	}
	var adoptedServer, adoptedPort string
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='server' AND serial='SN-1'`).Scan(&adoptedServer); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='port' AND mac_list @> '["aa:bb:cc:dd:ee:01"]'`).Scan(&adoptedPort); err != nil {
		t.Fatal(err)
	}
	if adoptedServer != legacyMachineID || adoptedPort != legacyPortID {
		t.Fatal("P1 machine/port identities were not preserved")
	}
	badSpeed := make(map[string][]byte, len(files))
	for name, data := range files {
		badSpeed[name] = append([]byte(nil), data...)
	}
	badSpeed["fabric/switch_ports.csv"] = []byte("bom_line_no,parent_ref,port_index,port_name,speed\n5,SW-1,1,swp1s0,400G\n")
	var speedManifest BOMPackageManifest
	if err = json.Unmarshal(badSpeed["manifest.json"], &speedManifest); err != nil {
		t.Fatal(err)
	}
	speedManifest.BatchID = "BOM-BATCH-P1-20261020-4"
	speedManifest.Seq = 4
	for i := range speedManifest.DomainFiles {
		if speedManifest.DomainFiles[i].File == "fabric/switch_ports.csv" {
			speedManifest.DomainFiles[i].Columns = []string{"bom_line_no", "parent_ref", "port_index", "port_name", "speed"}
		}
	}
	badSpeed["manifest.json"], _ = json.Marshal(speedManifest)
	rehashBOMFixture(t, badSpeed)
	speedPreview, speedErr := s.PreviewBOMPackage(ctx, actor, project, badSpeed)
	if speedErr != nil || speedPreview.CanCommit || !issueFound(&BOMPackage{Issues: speedPreview.Issues}, "switch_port_speed_not_supported") {
		t.Fatalf("unsupported switch speed accepted: %+v %v", speedPreview, speedErr)
	}
	badOptic := make(map[string][]byte, len(files))
	for name, data := range files {
		badOptic[name] = append([]byte(nil), data...)
	}
	badOptic["fabric/optic_modules.csv"] = []byte("bom_line_no,parent_ref,serial,sub_type,model_ref,speed\n6,5,OPT-1,DR8," + modelIDs["optic_module"] + ",400G\n")
	var opticManifest BOMPackageManifest
	if err = json.Unmarshal(badOptic["manifest.json"], &opticManifest); err != nil {
		t.Fatal(err)
	}
	opticManifest.BatchID = "BOM-BATCH-P1-20261020-5"
	opticManifest.Seq = 5
	for i := range opticManifest.DomainFiles {
		if opticManifest.DomainFiles[i].File == "fabric/optic_modules.csv" {
			opticManifest.DomainFiles[i].Columns = []string{"bom_line_no", "parent_ref", "serial", "sub_type", "model_ref", "speed"}
		}
	}
	badOptic["manifest.json"], _ = json.Marshal(opticManifest)
	rehashBOMFixture(t, badOptic)
	opticPreview, opticErr := s.PreviewBOMPackage(ctx, actor, project, badOptic)
	if opticErr != nil || opticPreview.CanCommit || !issueFound(&BOMPackage{Issues: opticPreview.Issues}, "optic_port_speed_mismatch") {
		t.Fatalf("mismatched optic speed accepted: %+v %v", opticPreview, opticErr)
	}
	badRail := make(map[string][]byte, len(files))
	for name, data := range files {
		badRail[name] = append([]byte(nil), data...)
	}
	badRail["server/server_ports.csv"] = bytes.Replace(badRail["server/server_ports.csv"], []byte("aa:bb:cc:dd:ee:01,0"), []byte("aa:bb:cc:dd:ee:01,"), 1)
	var railManifest BOMPackageManifest
	if err = json.Unmarshal(badRail["manifest.json"], &railManifest); err != nil {
		t.Fatal(err)
	}
	railManifest.BatchID = "BOM-BATCH-P1-20261020-6"
	railManifest.Seq = 6
	badRail["manifest.json"], _ = json.Marshal(railManifest)
	rehashBOMFixture(t, badRail)
	railPreview, railErr := s.PreviewBOMPackage(ctx, actor, project, badRail)
	if railErr != nil || railPreview.CanCommit || !issueFound(&BOMPackage{Issues: railPreview.Issues}, "compute_rail_missing") {
		t.Fatalf("missing compute rail accepted: %+v %v", railPreview, railErr)
	}

	repeat, err := s.CommitBOMPackage(ctx, actor, project, files, preview.PreviewToken, "test")
	if err != nil {
		t.Fatal(err)
	}
	if !repeat.Idempotent || repeat.PackageID != first.PackageID {
		t.Fatal("duplicate import created another package")
	}
	changed := make(map[string][]byte, len(files))
	for name, data := range files {
		changed[name] = append([]byte(nil), data...)
	}
	changed["server/servers.csv"] = bytes.ReplaceAll(changed["server/servers.csv"], []byte("srv-1"), []byte("srv-2"))
	rehashBOMFixture(t, changed)
	conflict, conflictErr := s.PreviewBOMPackage(ctx, actor, project, changed)
	if conflictErr != nil {
		t.Fatal(conflictErr)
	}
	if conflict.CanCommit || !issueFound(&BOMPackage{Issues: conflict.Issues}, "batch_hash_conflict") {
		t.Fatal("same batch with different hash was not blocked")
	}

	var firstSwitch, firstCable string
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='switch' AND serial='SW-1'`).Scan(&firstSwitch); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='cable'`).Scan(&firstCable); err != nil {
		t.Fatal(err)
	}
	manifest.BatchID = "BOM-BATCH-P1-20261020-2"
	manifest.Seq = 2
	files["manifest.json"], _ = json.Marshal(manifest)
	preview, err = s.PreviewBOMPackage(ctx, actor, project, files)
	if err != nil {
		t.Fatal(err)
	}
	if !preview.CanCommit {
		t.Fatalf("second batch blocked: %+v", preview.Issues)
	}
	second, err := s.CommitBOMPackage(ctx, actor, project, files, preview.PreviewToken, "test")
	if err != nil {
		t.Fatal(err)
	}
	if second.PackageID == first.PackageID {
		t.Fatal("new batch reused package")
	}
	var switchUID, cableUID string
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='switch' AND serial='SW-1'`).Scan(&switchUID); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRowContext(ctx, `SELECT item_uid FROM bom_items WHERE category='cable'`).Scan(&cableUID); err != nil {
		t.Fatal(err)
	}
	if switchUID != firstSwitch || cableUID != firstCable {
		t.Fatal("stable material identity changed in new batch")
	}
	manifest.BatchID = "BOM-BATCH-P1-20261020-3"
	manifest.Seq = 3
	files["manifest.json"], _ = json.Marshal(manifest)
	files["fabric/cables.csv"] = bytes.ReplaceAll(files["fabric/cables.csv"], []byte("7,3,5,"), []byte("7,3,4,"))
	rehashBOMFixture(t, files)
	badPreview, previewErr := s.PreviewBOMPackage(ctx, actor, project, files)
	if previewErr != nil {
		t.Fatal(previewErr)
	}
	if badPreview.CanCommit || badPreview.PreviewToken != "" {
		t.Fatal("invalid cable received a commit token")
	}
	if _, err = s.CommitBOMPackage(ctx, actor, project, files, preview.PreviewToken, "test"); err == nil {
		t.Fatal("invalid cable committed with a token from another package")
	}
	var packages int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM bom_import_packages`).Scan(&packages); err != nil {
		t.Fatal(err)
	}
	if packages != 2 {
		t.Fatalf("failed commit left %d packages", packages)
	}
}
