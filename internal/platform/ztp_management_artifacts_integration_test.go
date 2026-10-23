package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"strings"
	"testing"
	"time"
)

func TestZTPManagementArtifactsAndDeviceProjection(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	pw := "isolated-ztp-management-password"
	user, e := s.CreateUser(ctx, "ztp-minimum-admin", "Admin", pw, true, true)
	if e != nil {
		t.Fatal(e)
	}
	_, actor := testLogin(t, s, user.Username, pw, "198.51.100.172")
	p, e := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Minimum management", Slug: "ztp-minimum"}, "ztp-minimum", "test")
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	pkg, profile, sw := RandomID(), RandomID(), RandomID()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := s.DB.ExecContext(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec(`INSERT INTO bom_mapping_profiles(id,project_id,version_ref,state,created_by,created_at) VALUES($1,$2,'minimum-v1','approved',$3,$4)`, profile, p.ID, user.ID, now)
	exec(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,'ztp-minimum-fixture','bom-import/v1','minimum-v1',$3,'design_institute','{}',1,'committed',$4,$5)`, pkg, p.ID, "sha256:"+strings.Repeat("a", 64), user.ID, now)
	exec(`INSERT INTO bom_items(item_uid,category,serial,position,pod,role,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'switch','MINIMUM-SW','{"dc":"dc1","rack":"r1","idx":"1"}','p1','leaf','design_bom',$2,$3,$3)`, sw, p.ID, now)
	exec(`INSERT INTO switches(id,serial_number,hostname,created_at,updated_at) VALUES($1,'MINIMUM-SW','minimum',$2,$2)`, sw, now)
	exec(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,1,$3,'{"values":{"credential_batch_ref":"switch-cred-v1"}}',$4)`, pkg, sw, p.ID, now)
	exec(`INSERT INTO bom_credential_batches(project_id,credential_batch_ref,scope,rotation_policy,vault_path,owner,package_id,updated_at) VALUES($1,'switch-cred-v1','switch','poc','secret/switch/one','fixture',$2,$3)`, p.ID, pkg, now)
	rules := &fabricboot.ManagementRules{CIDR: "192.0.2.0/24", FirstAddress: "192.0.2.10", Gateway: "192.0.2.1", BatchPrefix: "minimum", BatchBy: "rack"}
	_, e = s.CreateNetworkBaseline(ctx, actor, p, CreateNetworkBaselineInput{VersionRef: "minimum-v1", SourceRef: "fixture:approved-minimum", EngineeringLimits: NetworkEngineeringLimits{SwitchBandwidthRatioLimits: map[string]float64{"role:leaf": 2}, Management: rules}}, "test")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.ApproveNetworkBaseline(ctx, actor, p, "minimum-v1", ApproveNetworkBaselineInput{CustomerApprovalRef: "customer:minimum", NetworkApprovalRef: "network:minimum"}, "test")
	if e != nil {
		t.Fatal(e)
	}
	plan, e := s.PreviewZTPManagementPlan(ctx, actor, p, ZTPManagementPlanInput{BaselineRef: "minimum-v1"})
	if e != nil {
		t.Fatal(e)
	}
	plan, e = s.CommitZTPManagementPlan(ctx, actor, p, ZTPManagementPlanInput{BaselineRef: plan.BaselineRef, ExpectedHash: plan.ContentHash}, "test")
	if e != nil {
		t.Fatal(e)
	}
	batch := plan.Assignments[0].Batch
	var before, after int
	s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audits`).Scan(&before)
	preview, e := s.PreviewZTPManagementArtifacts(ctx, actor, p, batch)
	if e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audits`).Scan(&after)
	if before != after {
		t.Fatal("artifact preview wrote an audit")
	}
	if preview.SHA256 != digest(preview.Content) || preview.Size != len(preview.Content) || len(preview.Scripts) != 1 || preview.Manifest.Scripts[0].Body != "" || preview.Manifest.IsolationSHA256 != fabricboot.IsolationScriptDigest() {
		t.Fatal("manifest and content hashes disagree")
	}
	if !strings.Contains(preview.Scripts[0].Body, "nv set system hostname dc1-p1-leaf-r1-1") || strings.Contains(preview.Scripts[0].Body, "switch-cred-v1") {
		t.Fatal("personalization or secret boundary")
	}
	media := RandomID()
	exec(`INSERT INTO media_items(id,scope_key,type,name,version,sha256,size,arch,state,uploaded_by,approved_by,created_at,updated_at) VALUES($1,$2,'ztp_script','minimum-manifest','v1',$3,$4,'noarch','approved',$5,$5,$6,$6)`, media, "project:"+p.ID, preview.SHA256, preview.Size, user.ID, now)
	tx, _ := s.DB.BeginTx(ctx, nil)
	approved, e := approvedZTPManagementPolicyTx(ctx, tx, p.ID, batch, media, true)
	if e != nil || approved.SHA256 != preview.SHA256 {
		t.Fatal("approved manifest rejected", e)
	}
	tx.Rollback()
	exec(`UPDATE bom_item_revisions SET row_snapshot='{"values":{"credential_batch_ref":"switch-cred-v2"}}' WHERE package_id=$1 AND item_uid=$2`, pkg, sw)
	exec(`INSERT INTO bom_credential_batches(project_id,credential_batch_ref,scope,rotation_policy,vault_path,owner,package_id,updated_at) VALUES($1,'switch-cred-v2','switch','poc','secret/switch/two','fixture',$2,$3)`, p.ID, pkg, now)
	tx, _ = s.DB.BeginTx(ctx, nil)
	_, e = approvedZTPManagementPolicyTx(ctx, tx, p.ID, batch, media, true)
	tx.Rollback()
	assertErrorCode(t, e, "ZTP_SCRIPT_DIGEST_MISMATCH")
	request := fabricboot.ManageabilityRequest{Version: "ztp-manageability-request/v1", ID: RandomID(), ProjectID: p.ID, SwitchID: sw, Serial: "MINIMUM-SW", BatchRef: batch, CredentialBatchRef: "switch-cred-v1", Hostname: plan.Assignments[0].Hostname, Address: plan.Assignments[0].Address, PrefixBits: 24, Gateway: rules.Gateway, ManagementMAC: "02:00:00:00:00:01"}
	raw, _ := json.Marshal(request)
	sum := sha256.Sum256(raw)
	proof := &fabricboot.ManageabilityProof{Version: "ztp-manageability/v1", RequestSHA256: hex.EncodeToString(sum[:]), ProjectID: p.ID, SwitchID: sw, Serial: request.Serial, CredentialBatchRef: request.CredentialBatchRef, Source: "protocol_fixture", Code: "ready", At: now}
	facts := &ztpManageabilityFacts{Request: request, RetryMax: 3, Observation: ZTPDeviceObservation{ID: RandomID(), OccurredAt: now, BootChannel: "239"}}
	result := &ZTPManageabilityCheck{ID: RandomID(), Code: "ready", EvidenceSource: "protocol_fixture", Attempt: 1, CheckedAt: now, Proof: proof}
	tx, _ = s.DB.BeginTx(ctx, nil)
	_, e = s.projectZTPManageabilityTx(ctx, tx, facts, result)
	tx.Rollback()
	assertErrorCode(t, e, "ZTP_DEVICE_EVIDENCE_REQUIRED")
	var count int
	s.DB.QueryRowContext(ctx, `SELECT count(*) FROM ztp_sessions WHERE mode='device'`).Scan(&count)
	if count != 0 {
		t.Fatal("fixture created real device session")
	}
	// An authoritative negative credential receipt creates a failure, never managed.
	proof.Source = "device"
	proof.Code = "credential_expired"
	result.Code = proof.Code
	result.EvidenceSource = proof.Source
	retry := now.Add(10 * time.Second)
	result.NextRetryAt = &retry
	tx, _ = s.DB.BeginTx(ctx, nil)
	id, e := s.projectZTPManageabilityTx(ctx, tx, facts, result)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	var state string
	var next sql.NullTime
	e = s.DB.QueryRowContext(ctx, `SELECT state,next_retry_at FROM ztp_sessions WHERE id=$1`, id).Scan(&state, &next)
	if e != nil || state != "failed" || !next.Valid {
		t.Fatal("expired credential not classified for retry", e)
	}
	// Full authoritative proof drives nine states only after independent checks.
	proof.Code = "ready"
	proof.Hostname = request.Hostname
	proof.Address = request.Address
	proof.PrefixBits = 24
	proof.Gateway = request.Gateway
	proof.MgmtVRF = "mgmt"
	proof.HostKeySHA256 = strings.Repeat("a", 64)
	proof.HostKeyTrustRef = "trusted:host-key"
	proof.SSHAuthenticated = true
	proof.CredentialPurpose = "ztp-management-read"
	proof.ConfigImported = true
	proof.ServicesRestarted = true
	proof.FrontPanelIsolated = true
	proof.DHCPDORAVerified = true
	proof.DHCPClientMAC = request.ManagementMAC
	proof.DHCPLeaseIP = request.Address
	proof.DHCPProofSHA256 = strings.Repeat("b", 64)
	proof.EvidenceRefs = []string{strings.Repeat("c", 64)}
	result.Code = "ready"
	result.Attempt = 2
	result.NextRetryAt = nil
	tx, _ = s.DB.BeginTx(ctx, nil)
	_, e = s.projectZTPManageabilityTx(ctx, tx, facts, result)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	detail, e := s.GetZTPDeviceSession(ctx, actor, p, id)
	if e != nil || detail.State != "managed" || detail.Mode != "device" || detail.ManagedAt == nil || len(detail.Events) != 11 {
		t.Fatal("verified device timeline did not converge", e)
	}
	rows, e := s.DB.QueryContext(ctx, ztpDeviceCandidateQuery, 16, strings.Repeat("a", 64), fabricboot.IsolationScriptDigest())
	if e != nil {
		t.Fatal(e)
	}
	rows.Close()
	// The read guard blocks the same row used by the shared worker's write claims.
	exec(`INSERT INTO exec_resources(id) VALUES($1) ON CONFLICT DO NOTHING`, "ssh:switch:"+sw)
	guard, e := s.guardZTPManagementRead(ctx, sw)
	if e != nil {
		t.Fatal(e)
	}
	busy, _ := s.DB.BeginTx(ctx, nil)
	_, e = busy.ExecContext(ctx, `SELECT id FROM exec_resources WHERE id=$1 FOR UPDATE NOWAIT`, "ssh:switch:"+sw)
	busy.Rollback()
	guard.Rollback()
	if e == nil {
		t.Fatal("management read did not block a concurrent write claim")
	}
}
