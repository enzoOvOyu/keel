package platform

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func bomFixture(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{
		"server/servers.csv":           []byte("bom_line_no,sn,hostname,model_ref,rack,u,sub_type,source_of_record\n1,SN-1,srv-1,server-v1,R01,1,B300,design_bom\n"),
		"server/server_components.csv": []byte("bom_line_no,parent_sn,category,sub_type,slot,serial,model_ref,mac\n2,SN-1,nic,CX8,1,NIC-1,nic-v1,aa:bb:cc:dd:ee:01\n"),
		"server/server_ports.csv":      []byte("bom_line_no,parent_ref,port_index,port_role,mac\n3,NIC-1,1,compute,aa:bb:cc:dd:ee:01\n"),
		"fabric/switches.csv":          []byte("bom_line_no,sn,hostname,model_ref,role,rack,idx,dc,pod,source_of_record,status\n4,SW-1,leaf-1,switch-v1,leaf,R01,1,DC,P1,design_bom,planned\n"),
		"fabric/switch_ports.csv":      []byte("bom_line_no,parent_ref,port_index,port_name\n5,SW-1,1,swp1s0\n"),
		"fabric/optic_modules.csv":     []byte("bom_line_no,parent_ref,serial,sub_type,model_ref\n6,5,OPT-1,DR8,optic-v1\n"),
		"fabric/cables.csv":            []byte("bom_line_no,endpoint_a,endpoint_b,cable_type,sub_type\n7,3,5,DAC,DAC200\n"),
		"address_plan.csv":             []byte("scope,object_ref,pool\nmgmt,SW-1,pool-1\n"),
	}
	decl := []BOMPackageFile{}
	for name, typ := range requiredBOMFiles {
		header := strings.TrimSpace(strings.SplitN(string(files[name]), "\n", 2)[0])
		decl = append(decl, BOMPackageFile{Domain: typ.domain, File: name, Entity: typ.entity, Columns: strings.Split(header, ",")})
	}
	decl = append(decl, BOMPackageFile{Domain: "address_plan", File: "address_plan.csv", Entity: "address_plan", Columns: []string{"scope", "object_ref", "pool"}})
	manifest := BOMPackageManifest{SchemaVersion: "bom-import/v1", BatchID: "BOM-BATCH-P1-20261020-1", ProjectCode: "P1", ProjectID: "proj-1", BatchDate: "2026-10-20", Seq: 1, DeclaredBy: "design_institute", ProfileVersionRef: "profile-v1", DomainFiles: decl, RowCount: 8, ContentHash: BOMContentHash(files)}
	raw, e := json.Marshal(manifest)
	if e != nil {
		t.Fatal(e)
	}
	files["manifest.json"] = raw
	return files
}
func issueFound(p *BOMPackage, code string) bool {
	for _, i := range p.Issues {
		if i.Code == code {
			return true
		}
	}
	return false
}
func TestBOMPackageHashAndRequiredFiles(t *testing.T) {
	files := bomFixture(t)
	p, e := ValidateBOMPackage(files)
	if e != nil {
		t.Fatal(e)
	}
	for _, i := range p.Issues {
		if i.Severity == "BLOCK" {
			t.Fatalf("valid fixture blocked: %+v", i)
		}
	}
	files["address_plan.csv"] = append(files["address_plan.csv"], []byte("mgmt,SN-1,pool-2\n")...)
	p, e = ValidateBOMPackage(files)
	if e != nil {
		t.Fatal(e)
	}
	if !issueFound(p, "hash_mismatch") {
		t.Fatal("optional declared file missing from hash")
	}
	delete(files, "fabric/cables.csv")
	p, e = ValidateBOMPackage(files)
	if e != nil {
		t.Fatal(e)
	}
	if !issueFound(p, "missing_declared_file") {
		t.Fatal("missing standard file accepted")
	}
}
func TestBOMPackageRejectsUndeclaredAndDuplicateLines(t *testing.T) {
	files := bomFixture(t)
	files["extra.csv"] = []byte("x\ny\n")
	p, e := ValidateBOMPackage(files)
	if e != nil {
		t.Fatal(e)
	}
	if !issueFound(p, "undeclared_file") {
		t.Fatal("undeclared file accepted")
	}
	delete(files, "extra.csv")
	files["fabric/cables.csv"] = []byte("bom_line_no,endpoint_a,endpoint_b,cable_type,sub_type\n3,3,5,DAC,DAC200\n")
	p, e = ValidateBOMPackage(files)
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, i := range p.Issues {
		if strings.HasPrefix(i.Code, "duplicate_line_number") {
			found = true
		}
	}
	if !found {
		t.Fatal("duplicate bom_line_no accepted")
	}
}
func TestBOMPackageRejectsBOMAndSecrets(t *testing.T) {
	files := bomFixture(t)
	files["server/servers.csv"] = append([]byte{0xef, 0xbb, 0xbf}, files["server/servers.csv"]...)
	p, e := ValidateBOMPackage(files)
	if e != nil {
		t.Fatal(e)
	}
	if !issueFound(p, "invalid_utf8_or_bom") {
		t.Fatal("UTF-8 BOM accepted in standard package")
	}
	files = bomFixture(t)
	files["credential_batches.csv"] = []byte("password\nsecret\n")
	p, e = ValidateBOMPackage(files)
	if e != nil {
		t.Fatal(e)
	}
	if !issueFound(p, "undeclared_file") {
		t.Fatal("unlisted credential file accepted")
	}
}

func TestBOMArchiveExtraction(t *testing.T) {
	files := bomFixture(t)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, e := zw.Create("bom-import-package/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(data); e != nil {
			t.Fatal(e)
		}
	}
	if e := zw.Close(); e != nil {
		t.Fatal(e)
	}
	decoded, e := ReadBOMPackageArchive("sample.zip", buf.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	p, e := ValidateBOMPackage(decoded)
	if e != nil {
		t.Fatal(e)
	}
	for _, issue := range p.Issues {
		if issue.Severity == "BLOCK" {
			t.Fatalf("valid archive blocked: %+v", issue)
		}
	}
	var bad bytes.Buffer
	writer := zip.NewWriter(&bad)
	w, e := writer.Create("../manifest.json")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = w.Write(files["manifest.json"]); e != nil {
		t.Fatal(e)
	}
	if e = writer.Close(); e != nil {
		t.Fatal(e)
	}
	if _, e = ReadBOMPackageArchive("bad.zip", bad.Bytes()); e == nil {
		t.Fatal("path traversal accepted")
	}
}

func testBOMProfile() *BOMMappingProfile {
	p := &BOMMappingProfile{ServerColumns: map[string]map[string]string{}, FabricColumns: map[string]map[string]string{}}
	for name, kind := range requiredBOMFiles {
		if kind.domain == "server" {
			p.ServerColumns[name] = map[string]string{}
		} else {
			p.FabricColumns[name] = map[string]string{}
		}
	}
	return p
}
func rehashBOMFixture(t *testing.T, files map[string][]byte) {
	t.Helper()
	var m BOMPackageManifest
	if e := json.Unmarshal(files["manifest.json"], &m); e != nil {
		t.Fatal(e)
	}
	m.ContentHash = BOMContentHash(files)
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	files["manifest.json"] = raw
}
func TestBOMProfileMappingAndAmbiguousEndpoint(t *testing.T) {
	files := bomFixture(t)
	files["server/server_ports.csv"] = []byte("bom_line_no,parent_ref,port_index,port_role,hardware_mac,port_name\n3,NIC-1,1,compute,aa:bb:cc:dd:ee:01,dup\n")
	files["fabric/switch_ports.csv"] = []byte("bom_line_no,parent_ref,port_index,port_name\n5,SW-1,1,dup\n")
	files["fabric/cables.csv"] = []byte("bom_line_no,endpoint_a,endpoint_b,cable_type,sub_type\n7,dup,5,DAC,DAC200\n")
	var m BOMPackageManifest
	if e := json.Unmarshal(files["manifest.json"], &m); e != nil {
		t.Fatal(e)
	}
	for i := range m.DomainFiles {
		if m.DomainFiles[i].File == "server/server_ports.csv" {
			m.DomainFiles[i].Columns = []string{"bom_line_no", "parent_ref", "port_index", "port_role", "hardware_mac", "port_name"}
		}
	}
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	files["manifest.json"] = raw
	rehashBOMFixture(t, files)
	pkg, e := ValidateBOMPackage(files)
	if e != nil {
		t.Fatal(e)
	}
	profile := testBOMProfile()
	profile.ServerColumns["server/server_ports.csv"]["hardware_mac"] = "mac"
	if e = validateBOMMappingProfile(CreateBOMMappingProfileInput{VersionRef: "profile-v1", ServerColumns: profile.ServerColumns, FabricColumns: profile.FabricColumns}); e != nil {
		t.Fatal(e)
	}
	ApplyBOMMappingProfile(pkg, profile)
	if got := pkg.Rows["server/server_ports.csv"][0].Values["mac"]; got != "aa:bb:cc:dd:ee:01" {
		t.Fatalf("alias not applied: %q", got)
	}
	if !issueFound(pkg, "ambiguous_reference") {
		t.Fatal("duplicate port display name accepted as cable endpoint")
	}
}

func TestBOMMappedLineNumberAndManifestIdentity(t *testing.T) {
	files := bomFixture(t)
	files["fabric/cables.csv"] = []byte("Line,endpoint_a,endpoint_b,cable_type,sub_type\n7,3,5,DAC,DAC200\n")
	var m BOMPackageManifest
	if e := json.Unmarshal(files["manifest.json"], &m); e != nil {
		t.Fatal(e)
	}
	for i := range m.DomainFiles {
		if m.DomainFiles[i].File == "fabric/cables.csv" {
			m.DomainFiles[i].Columns[0] = "Line"
		}
	}
	files["manifest.json"], _ = json.Marshal(m)
	rehashBOMFixture(t, files)
	pkg, e := ValidateBOMPackage(files)
	if e != nil {
		t.Fatal(e)
	}
	profile := testBOMProfile()
	profile.FabricColumns["fabric/cables.csv"]["Line"] = "bom_line_no"
	ApplyBOMMappingProfile(pkg, profile)
	for _, issue := range pkg.Issues {
		if issue.Severity == "BLOCK" {
			t.Fatalf("valid mapped line blocked: %+v", issue)
		}
	}
	if pkg.Rows["fabric/cables.csv"][0].Line != 7 {
		t.Fatal("mapped line number not retained")
	}
	m.BatchDate = "2026-10-19"
	files["manifest.json"], _ = json.Marshal(m)
	pkg, e = ValidateBOMPackage(files)
	if e != nil {
		t.Fatal(e)
	}
	if !issueFound(pkg, "batch_metadata_mismatch") {
		t.Fatal("batch/date mismatch accepted")
	}
}

func TestBOMMappingUnknownColumnWarns(t *testing.T) {
	files := bomFixture(t)
	files["server/servers.csv"] = []byte("bom_line_no,sn,hostname,model_ref,rack,u,sub_type,source_of_record,LegacyNote\n1,SN-1,srv-1,server-v1,R01,1,B300,design_bom,check-me\n")
	var manifest BOMPackageManifest
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	for i := range manifest.DomainFiles {
		if manifest.DomainFiles[i].File == "server/servers.csv" {
			manifest.DomainFiles[i].Columns = append(manifest.DomainFiles[i].Columns, "LegacyNote")
		}
	}
	files["manifest.json"], _ = json.Marshal(manifest)
	rehashBOMFixture(t, files)
	pkg, err := ValidateBOMPackage(files)
	if err != nil {
		t.Fatal(err)
	}
	ApplyBOMMappingProfile(pkg, testBOMProfile())
	if !issueFound(pkg, "unmapped_column") {
		t.Fatal("unknown column silently ignored")
	}
}
