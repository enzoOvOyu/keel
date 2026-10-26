package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Seed completed SOFTWARE session facts to isolate lineage persistence. This
// does not validate ZTP transport, real replacement or hardware management.
func TestZTPIdentityHistoryPreservesPhysicalIdentityPostgres(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	suffix := RandomID()[:8]
	password := "test-only-ztp-history-password"
	user, err := s.CreateUser(ctx, "ztp-history-"+suffix, "History fixture", password, true, true)
	if err != nil {
		t.Fatal(err)
	}
	_, actor := testLogin(t, s, user.Username, password, "198.51.100.192")
	project, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "History", Slug: "history-" + suffix}, "history-project-"+suffix, "test")
	if err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now().UTC().Add(-time.Hour)
	old, newID := RandomID(), RandomID()
	oldSN, newSN := "OLD-"+suffix, "NEW-"+suffix
	pkg := RandomID()
	exec(`INSERT INTO bom_import_packages(id,project_id,bom_import_batch_id,schema_version,profile_version_ref,content_hash,declared_by,manifest,row_count,state,uploaded_by,committed_at) VALUES($1,$2,'history-fixture','bom-import/v1','v1',$3,'design_institute','{}',2,'committed',$4,$5)`, pkg, project.ID, "sha256:"+strings.Repeat("a", 64), actor.User.ID, start)
	for i, item := range []struct{ id, serial string }{{old, oldSN}, {newID, newSN}} {
		exec(`INSERT INTO bom_items(item_uid,category,serial,position,source_of_record,first_project_id,created_at,updated_at) VALUES($1,'switch',$2,'{"dc":"d1","rack":"r1","idx":"1"}','design_bom',$3,$4,$4)`, item.id, item.serial, project.ID, start)
		exec(`INSERT INTO bom_item_revisions(package_id,item_uid,bom_line_no,project_id,row_snapshot,recorded_at) VALUES($1,$2,$3,$4,'{}',$5)`, pkg, item.id, i+1, project.ID, start)
		mac := "02:00:00:00:00:01"
		if i == 1 {
			mac = "02:00:00:00:00:02"
		}

		exec(`INSERT INTO switches(id,serial_number,hostname,created_at,updated_at) VALUES($1,$2,$2,$3,$3)`, item.id, item.serial, start)
		exec(`INSERT INTO switch_mac_bindings(switch_id,kind,mac,package_id,updated_at) VALUES($1,'mgmt',$2,$3,$4)`, item.id, mac, pkg, start)
	}
	seed := func(sw, serial, mode string, offset time.Duration) string {
		t.Helper()
		id := RandomID()
		claim, _ := json.Marshal(ZTPIdentityInput{Serial: serial})
		resolution, _ := json.Marshal(ZTPIdentityResolution{Verdict: "matched", SwitchID: sw, IdentityPriority: "sn", Reasons: []string{}})
		began := start.Add(offset)
		finished := began.Add(time.Second)
		exec(`INSERT INTO ztp_sessions(id,project_id,switch_id,batch_ref,mode,boot_channel,state,state_version,attempt,retry_max,identity_claim,identity_resolution,created_by,started_at,updated_at,managed_at) VALUES($1,$2,$3,'fixture',$4,'239','managed',1,1,3,$5,$6,$7,$8,$9,$9)`, id, project.ID, sw, mode, claim, resolution, actor.User.ID, began, finished)
		return id
	}
	first := seed(old, oldSN, "sim", 0)
	reinstalled := seed(old, oldSN, "sim", time.Minute)

	request := ZTPIdentityHistoryInput{Kind: "reinstall", FromSessionID: first, ToSessionID: reinstalled, FromExpectedVersion: 1, ToExpectedVersion: 1, Reason: "confirmed software reinstall lineage", Confirmed: true}
	record, err := s.RecordZTPIdentityHistory(ctx, actor, project, request, "history-test")
	if err != nil {
		t.Fatal(err)
	}
	if record.Source.SwitchID != record.Target.SwitchID || record.Mode != "sim" || record.DeclarationSource != "operator_declared" {
		t.Fatal("wrong physical/evidence relationship", record)
	}
	repeated, err := s.RecordZTPIdentityHistory(ctx, actor, project, request, "history-test")
	if err != nil || repeated.ID != record.ID {
		t.Fatal("identical retry created another history", repeated, err)
	}
	var version int64
	if err = s.DB.QueryRowContext(ctx, `SELECT state_version FROM ztp_sessions WHERE id=$1`, reinstalled).Scan(&version); err != nil || version != 2 {
		t.Fatal("lineage event not versioned once", version, err)
	}
	rmaInput := ZTPReplacementInput{FromSessionID: reinstalled, FromExpectedVersion: version, ToSwitchID: newID, Reason: "approved replacement of the original physical device", Confirmed: true}
	replacementRequest, err := s.CreateZTPReplacement(ctx, actor, project, rmaInput, "rma-test")
	if err != nil {
		t.Fatal(err)
	}
	approvalInput := ZTPReplacementApproval{ExpectedVersion: replacementRequest.StateVersion, SnapshotSHA256: replacementRequest.SnapshotSHA256, Confirmed: true}
	repeatedRequest, err := s.CreateZTPReplacement(ctx, actor, project, rmaInput, "rma-test")
	if err != nil || repeatedRequest.ID != replacementRequest.ID {
		t.Fatal("request replay duplicated approval scope", err)
	}
	rejected, err := s.RejectZTPReplacement(ctx, actor, project, replacementRequest.ID, approvalInput, "rma-test")
	if err != nil || rejected.State != "rejected" {
		t.Fatal("request rejection failed", err)
	}
	replacementRequest, err = s.CreateZTPReplacement(ctx, actor, project, rmaInput, "rma-test")
	if err != nil || replacementRequest.ID == rejected.ID {
		t.Fatal("rejected snapshot cannot be reviewed as a new request", err)
	}
	approvalInput = ZTPReplacementApproval{ExpectedVersion: replacementRequest.StateVersion, SnapshotSHA256: replacementRequest.SnapshotSHA256, Confirmed: true}
	if _, err = s.ApproveZTPReplacement(ctx, actor, project, replacementRequest.ID, approvalInput, "rma-test"); err == nil {
		t.Fatal("self approval accepted")
	}
	approverUser, err := s.CreateUser(ctx, "rma-approver-"+suffix, "RMA approver", password, true, false)
	if err != nil {
		t.Fatal(err)
	}
	_, approver := testLogin(t, s, approverUser.Username, password, "198.51.100.193")
	txPending, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	pendingIdentity, err := resolveZTPIdentityTx(ctx, txPending, project.ID, ZTPIdentityInput{Serial: newSN}, "sim")
	txPending.Rollback()
	if err != nil || pendingIdentity.Verdict != "conflict" {
		t.Fatal("pending replacement boot was not blocked", err)
	}
	exec(`UPDATE switch_mac_bindings SET mac='02:00:00:00:00:03' WHERE switch_id=$1 AND kind='mgmt'`, newID)
	if _, err = s.ApproveZTPReplacement(ctx, approver, project, replacementRequest.ID, approvalInput, "rma-test"); err == nil {
		t.Fatal("changed BOM MAC approved against stale snapshot")
	}
	exec(`UPDATE switch_mac_bindings SET mac='02:00:00:00:00:02' WHERE switch_id=$1 AND kind='mgmt'`, newID)
	approved, err := s.ApproveZTPReplacement(ctx, approver, project, replacementRequest.ID, approvalInput, "rma-test")
	if err != nil {
		t.Fatal(err)
	}
	if approved.ApprovedBy == approved.CreatedBy || approved.State != "approved" {
		t.Fatal("independent approval missing")
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE ztp_replacements SET reason='overwritten' WHERE id=$1`, approved.ID); err == nil {
		t.Fatal("approved replacement snapshot was mutable")
	}
	repeatedApproval, err := s.ApproveZTPReplacement(ctx, approver, project, replacementRequest.ID, approvalInput, "rma-test")
	if err != nil || repeatedApproval.StateVersion != approved.StateVersion {
		t.Fatal("approval replay changed binding", err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	simIdentity, err := resolveZTPIdentityTx(ctx, tx, project.ID, ZTPIdentityInput{Serial: oldSN}, "sim")
	if err != nil {
		t.Fatal(err)
	}
	realIdentity, err := resolveZTPIdentityTx(ctx, tx, project.ID, ZTPIdentityInput{Serial: oldSN})
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if simIdentity.Verdict != "conflict" || realIdentity.Verdict != "matched" {
		t.Fatal("sim retirement modified real binding or failed to retire", simIdentity, realIdentity)
	}
	if _, err = s.RejectZTPReplacement(ctx, approver, project, approved.ID, approvalInput, "rma-test"); err == nil {
		t.Fatal("rejection resurrected an approved retired binding")
	}
	start = time.Now().UTC()
	replacement := seed(newID, newSN, "sim", time.Millisecond)
	rma := ZTPIdentityHistoryInput{ReplacementID: approved.ID, Kind: "rma", FromSessionID: reinstalled, ToSessionID: replacement, FromExpectedVersion: version, ToExpectedVersion: 1, Reason: "replacement is a separate BOM identity", Confirmed: true}
	replaced, err := s.RecordZTPIdentityHistory(ctx, actor, project, rma, "history-test")
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Source.SwitchID == replaced.Target.SwitchID || replaced.Source.Serial != oldSN || replaced.Target.Serial != newSN {
		t.Fatal("RMA overwrote physical lineage", replaced)
	}
	var retainedSN string
	if err = s.DB.QueryRowContext(ctx, `SELECT serial_number FROM switches WHERE id=$1`, old).Scan(&retainedSN); err != nil || retainedSN != oldSN {
		t.Fatal("old physical identity was modified", retainedSN, err)
	}
	if _, err = s.DB.ExecContext(ctx, `UPDATE ztp_identity_history SET reason='overwritten' WHERE id=$1`, record.ID); err == nil {
		t.Fatal("immutable lineage was edited")
	}
	if _, err = s.DB.ExecContext(ctx, `DELETE FROM ztp_identity_history WHERE id=$1`, record.ID); err == nil {
		t.Fatal("immutable lineage deleted")
	}
	for _, change := range []func(*ZTPIdentityHistoryInput){func(r *ZTPIdentityHistoryInput) { r.Reason = "different predecessor declaration" }, func(r *ZTPIdentityHistoryInput) { r.Kind = "rma" }, func(r *ZTPIdentityHistoryInput) { r.Confirmed = false }} {
		bad := request
		change(&bad)
		if _, err = s.RecordZTPIdentityHistory(ctx, actor, project, bad, "history-test"); err == nil {
			t.Fatal("history conflict accepted")
		}
	}
	mixed := seed(newID, newSN, "device", 3*time.Minute)
	bad := ZTPIdentityHistoryInput{Kind: "rma", FromSessionID: first, ToSessionID: mixed, FromExpectedVersion: 1, ToExpectedVersion: 1, Reason: "must not mix evidence modes", Confirmed: true}
	if _, err = s.RecordZTPIdentityHistory(ctx, actor, project, bad, "history-test"); err == nil {
		t.Fatal("simulation mixed with device history")
	}
	later := seed(old, oldSN, "sim", 4*time.Minute)
	bad = ZTPIdentityHistoryInput{Kind: "rma", FromSessionID: first, ToSessionID: later, FromExpectedVersion: 1, ToExpectedVersion: 1, Reason: "same physical UID is not replacement", Confirmed: true}
	if _, err = s.RecordZTPIdentityHistory(ctx, actor, project, bad, "history-test"); err == nil {
		t.Fatal("same physical identity called RMA")
	}
	bad.Kind = "reinstall"
	bad.FromSessionID = later
	bad.ToSessionID = first
	if _, err = s.RecordZTPIdentityHistory(ctx, actor, project, bad, "history-test"); err == nil {
		t.Fatal("backwards history accepted")
	}
	list, err := s.ListZTPIdentityHistory(ctx, actor, project)
	if err != nil || len(list) != 2 {
		t.Fatal("lineage list not scoped and stable", len(list), err)
	}
	other, err := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Other history", Slug: "history-other-" + suffix}, "history-other-"+suffix, "test")
	if err != nil {
		t.Fatal(err)
	}
	otherList, err := s.ListZTPIdentityHistory(ctx, actor, other)
	if err != nil || len(otherList) != 0 {
		t.Fatal("history leaked across project")
	}
	if _, err = s.RecordZTPIdentityHistory(ctx, actor, other, request, "history-test"); err == nil {
		t.Fatal("foreign sessions accepted")
	}
}
