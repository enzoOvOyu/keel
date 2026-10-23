package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"net/url"
	"strings"
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
	identityPackage := make(map[string][]byte, len(files))
	for name, data := range files {
		identityPackage[name] = append([]byte(nil), data...)
	}
	identityPackage["fabric/switches.csv"] = []byte("bom_line_no,sn,hostname,model_ref,role,rack,idx,dc,pod,source_of_record,status,mgmt_mac,base_mac,other_macs\n4,SW-1,leaf-1," + modelIDs["switch"] + ",leaf,R01,1,DC,P1,design_bom,planned,aa:bb:cc:dd:ff:01,aa:bb:cc:dd:ff:02,aa:bb:cc:dd:ff:03\n")
	var identityManifest BOMPackageManifest
	if err = json.Unmarshal(identityPackage["manifest.json"], &identityManifest); err != nil {
		t.Fatal(err)
	}
	identityManifest.BatchID = "BOM-BATCH-P1-20261020-11"
	identityManifest.Seq = 11
	for i := range identityManifest.DomainFiles {
		if identityManifest.DomainFiles[i].File == "fabric/switches.csv" {
			identityManifest.DomainFiles[i].Columns = []string{"bom_line_no", "sn", "hostname", "model_ref", "role", "rack", "idx", "dc", "pod", "source_of_record", "status", "mgmt_mac", "base_mac", "other_macs"}
		}
	}
	identityPackage["manifest.json"], _ = json.Marshal(identityManifest)
	rehashBOMFixture(t, identityPackage)
	identityPreview, identityErr := s.PreviewBOMPackage(ctx, actor, project, identityPackage)
	if identityErr != nil || !identityPreview.CanCommit {
		t.Fatalf("valid switch identity blocked: %+v %v", identityPreview, identityErr)
	}
	if _, err = s.CommitBOMPackage(ctx, actor, project, identityPackage, identityPreview.PreviewToken, "test"); err != nil {
		t.Fatal(err)
	}
	var bindingCount int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM switch_mac_bindings b JOIN switches sw ON sw.id=b.switch_id WHERE sw.serial_number='SW-1'`).Scan(&bindingCount); err != nil || bindingCount != 3 {
		t.Fatalf("switch MAC bindings: %d %v", bindingCount, err)
	}
	matchedIdentity, resolveErr := s.ResolveZTPIdentity(ctx, actor, project, ZTPIdentityInput{Serial: "SW-1", MgmtMAC: "AA-BB-CC-DD-FF-01", BaseMAC: "aa:bb:cc:dd:ff:02"})
	if resolveErr != nil || matchedIdentity.Verdict != "matched" || matchedIdentity.IdentityPriority != "sn" {
		t.Fatalf("ZTP SN resolution: %+v %v", matchedIdentity, resolveErr)
	}
	macIdentity, resolveErr := s.ResolveZTPIdentity(ctx, actor, project, ZTPIdentityInput{MgmtMAC: "aa:bb:cc:dd:ff:01"})
	if resolveErr != nil || macIdentity.Verdict != "matched" || macIdentity.SwitchID != matchedIdentity.SwitchID || macIdentity.IdentityPriority != "mgmt" {
		t.Fatalf("ZTP MAC resolution: %+v %v", macIdentity, resolveErr)
	}
	conflictingSN, resolveErr := s.ResolveZTPIdentity(ctx, actor, project, ZTPIdentityInput{Serial: "SW-RMA", MgmtMAC: "aa:bb:cc:dd:ff:01"})
	if resolveErr != nil || conflictingSN.Verdict != "conflict" {
		t.Fatalf("RMA serial conflict not isolated: %+v %v", conflictingSN, resolveErr)
	}
	mismatchedMAC, resolveErr := s.ResolveZTPIdentity(ctx, actor, project, ZTPIdentityInput{Serial: "SW-1", MgmtMAC: "aa:bb:cc:dd:ff:09"})
	if resolveErr != nil || mismatchedMAC.Verdict != "conflict" {
		t.Fatalf("MAC conflict not isolated: %+v %v", mismatchedMAC, resolveErr)
	}
	unknownIdentity, resolveErr := s.ResolveZTPIdentity(ctx, actor, project, ZTPIdentityInput{Serial: "SW-UNKNOWN"})
	if resolveErr != nil || unknownIdentity.Verdict != "isolated" {
		t.Fatalf("unknown identity not isolated: %+v %v", unknownIdentity, resolveErr)
	}
	sim, simErr := s.CreateZTPSimSession(ctx, actor, project, ZTPSimSessionInput{BatchRef: "sim-batch-1", BootChannel: "239", Identity: ZTPIdentityInput{Serial: "SW-1", MgmtMAC: "aa:bb:cc:dd:ff:01"}}, "test")
	if simErr != nil || sim.Mode != "sim" || sim.State != "powered" || len(sim.Events) != 1 {
		t.Fatalf("simulation session: %+v %v", sim, simErr)
	}
	dora := &fabricboot.DORARequest{Interface: "eth0", LinkIP: "192.0.2.1", VendorClass: "cumulus-linux x86_64"}
	if _, simErr = s.AdvanceZTPSimSession(ctx, actor, project, sim.ID, ZTPSessionAction{Action: "advance", ExpectedVersion: sim.StateVersion, EvidenceRef: "unverified"}, "test"); simErr == nil {
		t.Fatal("DHCP advanced without DORA request")
	}
	if _, simErr = s.AdvanceZTPSimSession(ctx, actor, project, sim.ID, ZTPSessionAction{Action: "advance", ExpectedVersion: sim.StateVersion, DORA: dora}, "test"); simErr == nil {
		t.Fatal("DHCP advanced without active Kea plan")
	}
	if _, err = s.CreateUser(ctx, "bom-ztp-reviewer", "BOM ZTP Reviewer", password, true, false); err != nil {
		t.Fatal(err)
	}
	_, reviewer := testLogin(t, s, "bom-ztp-reviewer", password, "198.51.100.93")
	scriptMedia, simErr := s.RegisterMedia(ctx, actor, project, RegisterMediaInput{Type: "ztp_script", Name: "BOM isolation", Version: "isolation-v1", Sha256: fabricboot.IsolationScriptDigest(), Size: int64(len(fabricboot.IsolationScriptV1)), Arch: "noarch", OsScope: "cumulus-linux-5.x", ManifestRef: "generated:isolation-v1"}, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	if _, simErr = s.TransitionMedia(ctx, actor, project, scriptMedia.ID, "verified", "test"); simErr != nil {
		t.Fatal(simErr)
	}
	if _, simErr = s.TransitionMedia(ctx, reviewer, project, scriptMedia.ID, "approved", "test"); simErr != nil {
		t.Fatal(simErr)
	}
	keaCfg := fabricboot.DHCPOptions{BatchRef: "sim-batch-1", StageRef: "N-S4", ManagementCIDR: "192.0.2.0/24", Option239URL: fmt.Sprintf("https://boot.example.invalid/api/v1/boot/ztp?project=%s&batch=sim-batch-1&stage=N-S4&policy=%s", project.ID, scriptMedia.ID), Option143URIs: []string{"https://boot.example.invalid"}, Option60VCI: "cumulus-linux x86_64", Option43: map[string]string{"nos": "cumulus-linux", "version_intent": "5.x"}}
	keaPlan, simErr := s.StageZTPMockKeaPlan(ctx, actor, project, keaCfg, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	_, simErr = s.CommitZTPMockKeaPlan(ctx, actor, project, ZTPMockKeaCommitInput{BatchRef: keaCfg.BatchRef, StageRef: keaCfg.StageRef, StagedHash: keaPlan.StagedHash, ExpectedVersion: keaPlan.Version}, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	parsedBootURL, simErr := url.Parse(keaCfg.Option239URL)
	if simErr != nil {
		t.Fatal(simErr)
	}
	goodClaim := ZTP239Claim{Serial: "SW-1", MgmtMAC: "aa:bb:cc:dd:ff:01", BaseMAC: "aa:bb:cc:dd:ff:02", ProductName: "SN5610", Version: "5.0.0", Build: "5.0.0"}
	served, simErr := s.ServeZTP239(ctx, project.ID, "sim-batch-1", "N-S4", scriptMedia.ID, "192.0.2.8", "https", "boot.example.invalid", parsedBootURL.Path, parsedBootURL.RawQuery, "test", goodClaim)
	if simErr != nil || served.IdentityVerdict != "matched" || served.Body != fabricboot.IsolationScriptV1 {
		t.Fatalf("known switch bootstrap: %+v %v", served, simErr)
	}
	conflictClaim := goodClaim
	conflictClaim.Serial = "SW-RMA"
	if _, simErr = s.ServeZTP239(ctx, project.ID, "sim-batch-1", "N-S4", scriptMedia.ID, "192.0.2.8", "https", "boot.example.invalid", parsedBootURL.Path, parsedBootURL.RawQuery, "test", conflictClaim); simErr == nil {
		t.Fatal("conflicting switch received bootstrap script")
	}
	if _, simErr = s.ServeZTP239(ctx, project.ID, "sim-batch-1", "N-S4", scriptMedia.ID, "192.0.2.8", "https", "boot.example.invalid", parsedBootURL.Path, parsedBootURL.RawQuery, "test", conflictClaim); simErr == nil {
		t.Fatal("repeated conflicting switch received bootstrap script")
	}
	issues, issueErr := s.ListZTPIdentityIssues(ctx, actor, project)
	if issueErr != nil || len(issues) != 1 || issues[0].BootChannel != "239" || issues[0].EventCount != 2 || len(issues[0].Reasons) == 0 {
		t.Fatalf("device conflict issue: %+v %v", issues, issueErr)
	}
	frontPanel := *dora
	frontPanel.Interface = "swp1"
	if _, simErr = s.AdvanceZTPSimSession(ctx, actor, project, sim.ID, ZTPSessionAction{Action: "advance", ExpectedVersion: sim.StateVersion, DORA: &frontPanel}, "test"); simErr == nil {
		t.Fatal("front-panel DORA advanced session")
	}
	for _, stage := range []string{"dhcp", "booturl", "identity", "lookup", "fetch", "config", "restart", "managed"} {
		action := ZTPSessionAction{Action: "advance", ExpectedVersion: sim.StateVersion, EvidenceRef: "sim-evidence-" + stage}
		if stage == "dhcp" {
			action.DORA = dora
		}
		sim, simErr = s.AdvanceZTPSimSession(ctx, actor, project, sim.ID, action, "test")
		if simErr != nil || sim.State != stage {
			t.Fatalf("stage %s: %+v %v", stage, sim, simErr)
		}
	}
	if sim.Mode != "sim" || len(sim.Events) != 9 || sim.ManagedAt == nil || sim.Events[1].EvidenceRef != "mock-kea:"+keaPlan.StagedHash {
		t.Fatalf("simulation timeline: %+v", sim)
	}
	if _, simErr = s.AdvanceZTPSimSession(ctx, actor, project, sim.ID, ZTPSessionAction{Action: "advance", ExpectedVersion: 1, EvidenceRef: "stale"}, "test"); simErr == nil {
		t.Fatal("stale ZTP state version accepted")
	}
	conflictSim, simErr := s.CreateZTPSimSession(ctx, actor, project, ZTPSimSessionInput{BatchRef: "sim-batch-1", BootChannel: "143", Identity: ZTPIdentityInput{Serial: "SW-RMA", MgmtMAC: "aa:bb:cc:dd:ff:01"}}, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	for _, stage := range []string{"dhcp", "booturl", "identity"} {
		action := ZTPSessionAction{Action: "advance", ExpectedVersion: conflictSim.StateVersion, EvidenceRef: "sim-evidence-" + stage}
		if stage == "dhcp" {
			action.DORA = dora
		}
		conflictSim, simErr = s.AdvanceZTPSimSession(ctx, actor, project, conflictSim.ID, action, "test")
		if simErr != nil {
			t.Fatal(simErr)
		}
	}
	conflictSim, simErr = s.AdvanceZTPSimSession(ctx, actor, project, conflictSim.ID, ZTPSessionAction{Action: "advance", ExpectedVersion: conflictSim.StateVersion, EvidenceRef: "sim-evidence-lookup"}, "test")
	if simErr != nil || conflictSim.State != "conflict" || len(conflictSim.Events) != 6 {
		t.Fatalf("conflict timeline: %+v %v", conflictSim, simErr)
	}
	issues, issueErr = s.ListZTPIdentityIssues(ctx, actor, project)
	if issueErr != nil || len(issues) != 2 {
		t.Fatalf("simulation conflict issue: %+v %v", issues, issueErr)
	}
	failedSim, simErr := s.CreateZTPSimSession(ctx, actor, project, ZTPSimSessionInput{BatchRef: "sim-batch-1", BootChannel: "239", Identity: ZTPIdentityInput{Serial: "SW-1"}}, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	failedSim, simErr = s.AdvanceZTPSimSession(ctx, actor, project, failedSim.ID, ZTPSessionAction{Action: "fail", ExpectedVersion: failedSim.StateVersion, Phenomenon: "DHCP timeout", Cause: "no offer", NextAction: "check Kea"}, "test")
	if simErr != nil || failedSim.State != "failed" || failedSim.NextRetryAt == nil {
		t.Fatalf("failure classification: %+v %v", failedSim, simErr)
	}
	if _, simErr = s.AdvanceZTPSimSession(ctx, actor, project, failedSim.ID, ZTPSessionAction{Action: "retry", ExpectedVersion: failedSim.StateVersion}, "test"); simErr == nil {
		t.Fatal("ZTP retry ignored backoff")
	}
	if _, simErr = s.DB.ExecContext(ctx, `UPDATE ztp_sessions SET next_retry_at=now()-interval '1 second' WHERE id=$1`, failedSim.ID); simErr != nil {
		t.Fatal(simErr)
	}
	failedSim, simErr = s.AdvanceZTPSimSession(ctx, actor, project, failedSim.ID, ZTPSessionAction{Action: "retry", ExpectedVersion: failedSim.StateVersion}, "test")
	if simErr != nil || failedSim.State != "powered" || failedSim.Attempt != 2 || failedSim.NextRetryAt != nil || len(failedSim.Events) != 3 {
		t.Fatalf("retry after backoff: %+v %v", failedSim, simErr)
	}
	loadedSim, simErr := s.GetZTPSession(ctx, actor, project, sim.ID)
	if simErr != nil || loadedSim.State != "managed" || len(loadedSim.Events) != 9 {
		t.Fatalf("persisted timeline: %+v %v", loadedSim, simErr)
	}
	listedSims, simErr := s.ListZTPSimSessions(ctx, actor, project)
	if simErr != nil || len(listedSims) != 3 {
		t.Fatalf("simulation board list: %+v %v", listedSims, simErr)
	}

	conflicting := make(map[string][]byte, len(identityPackage))
	for name, data := range identityPackage {
		conflicting[name] = append([]byte(nil), data...)
	}
	conflicting["fabric/switches.csv"] = bytes.ReplaceAll(conflicting["fabric/switches.csv"], []byte("aa:bb:cc:dd:ff:01"), []byte("aa:bb:cc:dd:ee:01"))
	var conflictIdentityManifest BOMPackageManifest
	if err = json.Unmarshal(conflicting["manifest.json"], &conflictIdentityManifest); err != nil {
		t.Fatal(err)
	}
	conflictIdentityManifest.BatchID = "BOM-BATCH-P1-20261020-12"
	conflictIdentityManifest.Seq = 12
	conflicting["manifest.json"], _ = json.Marshal(conflictIdentityManifest)
	rehashBOMFixture(t, conflicting)
	conflictIdentityPreview, conflictIdentityErr := s.PreviewBOMPackage(ctx, actor, project, conflicting)
	if conflictIdentityErr != nil || conflictIdentityPreview.CanCommit || !issueFound(&BOMPackage{Issues: conflictIdentityPreview.Issues}, "switch_identity_mac_conflict") {
		t.Fatalf("switch/server MAC collision accepted: %+v %v", conflictIdentityPreview, conflictIdentityErr)
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
	var retainedMACs int
	if err = s.DB.QueryRowContext(ctx, `SELECT jsonb_array_length(mac_list) FROM bom_items WHERE item_uid=$1`, switchUID).Scan(&retainedMACs); err != nil || retainedMACs != 3 {
		t.Fatalf("omitted identity columns cleared current MACs: %d %v", retainedMACs, err)
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
	if packages != 3 {
		t.Fatalf("failed commit left %d packages", packages)
	}

	// An identity captured when a session starts cannot authorize a later lookup.
	stale, simErr := s.CreateZTPSimSession(ctx, actor, project, ZTPSimSessionInput{BatchRef: "sim-batch-1", BootChannel: "239", Identity: ZTPIdentityInput{Serial: "SW-1", MgmtMAC: "aa:bb:cc:dd:ff:01"}}, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	for _, stage := range []string{"dhcp", "booturl", "identity"} {
		action := ZTPSessionAction{Action: "advance", ExpectedVersion: stale.StateVersion, EvidenceRef: "manual-" + stage}
		if stage == "dhcp" {
			action.DORA = dora
		}
		stale, simErr = s.AdvanceZTPSimSession(ctx, actor, project, stale.ID, action, "test")
		if simErr != nil {
			t.Fatal(simErr)
		}
	}
	if _, simErr = s.DB.ExecContext(ctx, `UPDATE switch_mac_bindings SET mac='aa:bb:cc:dd:ff:99' WHERE switch_id=$1 AND kind='mgmt'`, firstSwitch); simErr != nil {
		t.Fatal(simErr)
	}
	stale, simErr = s.AdvanceZTPSimSession(ctx, actor, project, stale.ID, ZTPSessionAction{Action: "advance", ExpectedVersion: stale.StateVersion, EvidenceRef: "caller-claimed-match"}, "test")
	if simErr != nil || stale.State != "conflict" || stale.IdentityResolution.Verdict != "conflict" || stale.Events[len(stale.Events)-2].EvidenceRef != "sot-lookup:conflict" {
		t.Fatalf("stale SoT lookup was trusted: %+v %v", stale, simErr)
	}
	if _, simErr = s.DB.ExecContext(ctx, `UPDATE switch_mac_bindings SET mac='aa:bb:cc:dd:ff:01' WHERE switch_id=$1 AND kind='mgmt'`, firstSwitch); simErr != nil {
		t.Fatal(simErr)
	}

	// DHCP evidence is bound to one active offer, even if an operator commits
	// a new plan while the simulated device is booting.
	changedOffer, simErr := s.CreateZTPSimSession(ctx, actor, project, ZTPSimSessionInput{BatchRef: "sim-batch-1", BootChannel: "239", Identity: ZTPIdentityInput{Serial: "SW-1", MgmtMAC: "aa:bb:cc:dd:ff:01"}}, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	changedOffer, simErr = s.AdvanceZTPSimSession(ctx, actor, project, changedOffer.ID, ZTPSessionAction{Action: "advance", ExpectedVersion: changedOffer.StateVersion, DORA: dora}, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	keaCfg.Option43["version_intent"] = "5.1"
	replacement, simErr := s.StageZTPMockKeaPlan(ctx, actor, project, keaCfg, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	if _, simErr = s.CommitZTPMockKeaPlan(ctx, actor, project, ZTPMockKeaCommitInput{BatchRef: keaCfg.BatchRef, StageRef: keaCfg.StageRef, StagedHash: replacement.StagedHash, ExpectedVersion: replacement.Version}, "test"); simErr != nil {
		t.Fatal(simErr)
	}
	if _, simErr = s.AdvanceZTPSimSession(ctx, actor, project, changedOffer.ID, ZTPSessionAction{Action: "advance", ExpectedVersion: changedOffer.StateVersion, EvidenceRef: "caller-claimed-url"}, "test"); simErr == nil {
		t.Fatal("changed active offer accepted for boot URL")
	}

	// Unknown IDevID reaches only the approved isolation artifact, never a
	// simulated managed state or production configuration.
	unknown143, simErr := s.CreateZTPSimSession(ctx, actor, project, ZTPSimSessionInput{BatchRef: "sim-batch-1", BootChannel: "143", Identity: ZTPIdentityInput{Serial: "UNKNOWN-SW-143"}}, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	for _, stage := range []string{"dhcp", "booturl", "identity", "lookup"} {
		action := ZTPSessionAction{Action: "advance", ExpectedVersion: unknown143.StateVersion, EvidenceRef: "manual-" + stage}
		if stage == "dhcp" {
			action.DORA = dora
		}
		unknown143, simErr = s.AdvanceZTPSimSession(ctx, actor, project, unknown143.ID, action, "test")
		if simErr != nil {
			t.Fatalf("unknown IDevID %s: %v", stage, simErr)
		}
	}
	if unknown143.State != "isolated" || unknown143.IdentityResolution.Verdict != "isolated" || len(unknown143.Events) != 7 || !strings.HasPrefix(unknown143.Events[5].EvidenceRef, "mock-sztp143:") || unknown143.ManagedAt != nil {
		t.Fatalf("unknown device did not receive isolation artifact: %+v", unknown143)
	}

	// Approval is checked again when fetching, rather than inferred from an
	// earlier approved plan or a caller supplied evidence string.
	retired, simErr := s.CreateZTPSimSession(ctx, actor, project, ZTPSimSessionInput{BatchRef: "sim-batch-1", BootChannel: "239", Identity: ZTPIdentityInput{Serial: "SW-1", MgmtMAC: "aa:bb:cc:dd:ff:01"}}, "test")
	if simErr != nil {
		t.Fatal(simErr)
	}
	for _, stage := range []string{"dhcp", "booturl", "identity", "lookup"} {
		action := ZTPSessionAction{Action: "advance", ExpectedVersion: retired.StateVersion, EvidenceRef: "manual-" + stage}
		if stage == "dhcp" {
			action.DORA = dora
		}
		retired, simErr = s.AdvanceZTPSimSession(ctx, actor, project, retired.ID, action, "test")
		if simErr != nil {
			t.Fatalf("retirement setup %s: %v", stage, simErr)
		}
	}
	if _, simErr = s.TransitionMedia(ctx, reviewer, project, scriptMedia.ID, "retired", "test"); simErr != nil {
		t.Fatal(simErr)
	}
	if _, simErr = s.AdvanceZTPSimSession(ctx, actor, project, retired.ID, ZTPSessionAction{Action: "advance", ExpectedVersion: retired.StateVersion, EvidenceRef: "caller-claimed-fetch"}, "test"); simErr == nil {
		t.Fatal("retired isolation script accepted for simulated fetch")
	}
}
