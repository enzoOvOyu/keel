package platform

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestProvisionRequestLifecycleAndIsolation(t *testing.T) {
	s, owner, reviewer, project, asset, target, _, _, key, input := provisionFixture(t)
	ctx := context.Background()
	in := CreateProvisionRequestInput{ProvisionPreviewInput: input, Reason: "isolated software installation preparation"}
	first, err := s.CreateProvisionRequest(ctx, owner, project, asset, in, "request-once", "test")
	if err != nil {
		t.Fatal(err)
	}
	if !ProvisionSnapshotValid(first) || first.State != "requested" || first.StateVersion != 1 || !strings.Contains(first.Snapshot.Seed.MetaData, first.ID) {
		t.Fatalf("unbound request: %+v", first)
	}
	again, err := s.CreateProvisionRequest(ctx, owner, project, asset, in, "request-once", "test")
	if err != nil || again.ID != first.ID {
		t.Fatalf("retry duplicated request: %v", err)
	}
	changed := in
	changed.Reason = "different consent"
	if _, err = s.CreateProvisionRequest(ctx, owner, project, asset, changed, "request-once", "test"); err == nil {
		t.Fatal("idempotency key rebound")
	}
	if _, err = s.CreateProvisionRequest(ctx, owner, project, asset, in, "second-active", "test"); err == nil {
		t.Fatal("parallel active preparation accepted")
	}
	action := ProvisionRequestActionInput{ExpectedVersion: 1, Reason: "independent approval"}
	if _, err = s.ChangeProvisionRequest(ctx, owner, project.ID, first.ID, "approve", action, "self-approve", "test"); err == nil {
		t.Fatal("creator approved own request")
	}
	approved, err := s.ChangeProvisionRequest(ctx, reviewer, project.ID, first.ID, "approve", action, "approve-once", "test")
	if err != nil {
		t.Fatal(err)
	}
	if approved.State != "approved" || approved.ApprovedBy == nil || *approved.ApprovedBy != reviewer.User.ID || approved.StateVersion != 2 {
		t.Fatal("approval identity not pinned")
	}
	if _, err = s.ChangeProvisionRequest(ctx, reviewer, project.ID, first.ID, "cancel", action, "stale-cancel", "test"); err == nil {
		t.Fatal("stale state accepted")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.ExecContext(ctx, "UPDATE provision_requests SET snapshot_sha256=$2,state_version=state_version+1 WHERE id=$1", first.ID, strings.Repeat("f", 64))
	tx.Rollback()
	if err == nil {
		t.Fatal("frozen snapshot mutable")
	}
	canceled, err := s.ChangeProvisionRequest(ctx, owner, project.ID, first.ID, "cancel", ProvisionRequestActionInput{ExpectedVersion: 2, Reason: "withdraw consent"}, "cancel-once", "test")
	if err != nil || canceled.State != "canceled" {
		t.Fatalf("cancel failed: %v", err)
	}
	second, err := s.CreateProvisionRequest(ctx, owner, project, asset, in, "fresh-request", "test")
	if err != nil {
		t.Fatal(err)
	}
	if second.ID == first.ID || second.SnapshotSHA256 == first.SnapshotSHA256 {
		t.Fatal("new request reused old installation instance")
	}
	other, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Other preparation", Slug: "other-preparation"}, "other-preparation", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ReadProvisionRequest(ctx, owner, other.ID, second.ID); err == nil {
		t.Fatal("cross project request readable")
	}
	tx, err = s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.VerifyProvisionRequestTx(ctx, tx, owner, approved)
	tx.Rollback()
	if err != nil {
		t.Fatal("approved inputs failed verification", err)
	}
	if _, err = s.RevokeSSHDomainKey(ctx, project.ID, key.ID, key.StateVersion, "withdraw preparation key"); err != nil {
		t.Fatal(err)
	}
	tx, err = s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = ValidateProvisionRequestMetadataTx(ctx, tx, approved, time.Now())
	tx.Rollback()
	if err == nil {
		t.Fatal("revoked frozen key accepted")
	}
	var state, trust string
	var executions int
	if err = s.DB.QueryRowContext(ctx, "SELECT a.state,m.ssh_trust FROM project_assets a JOIN machines m ON m.id=a.machine_id WHERE a.id=$1", asset).Scan(&state, &trust); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_executions").Scan(&executions); err != nil {
		t.Fatal(err)
	}
	if state != target.State || trust == "established" || executions != 0 {
		t.Fatal("request/approval created installation side effects")
	}
}

func TestProvisionRequestReinstallRequiresExactSerial(t *testing.T) {
	s, owner, _, project, asset, target, _, _, _, input := provisionFixture(t)
	ctx := context.Background()
	if _, err := s.DB.ExecContext(ctx, "UPDATE project_assets SET state='provisioned',version=version+1 WHERE id=$1", asset); err != nil {
		t.Fatal(err)
	}
	input.AssetVersion++
	in := CreateProvisionRequestInput{ProvisionPreviewInput: input, Reason: "controlled reinstall"}
	if _, err := s.CreateProvisionRequest(ctx, owner, project, asset, in, "without-intent", "test"); err == nil {
		t.Fatal("existing system admitted without reinstall intent")
	}
	in.Reinstall = true
	in.ConfirmSerial = "wrong"
	if _, err := s.CreateProvisionRequest(ctx, owner, project, asset, in, "wrong-serial", "test"); err == nil {
		t.Fatal("wrong serial admitted")
	}
	in.ConfirmSerial = target.Serial
	if _, err := s.CreateProvisionRequest(ctx, owner, project, asset, in, "exact-serial", "test"); err != nil {
		t.Fatal(err)
	}
}

func TestProvisionDeploymentEngineerPublicKeyScope(t *testing.T) {
	s, owner, _, project, asset, _, _, _, _, input := provisionFixture(t)
	ctx := context.Background()
	const password = "test-only-provision-engineer-password"
	for _, name := range []string{"prep-engineer", "prep-viewer"} {
		if _, err := s.CreateUser(ctx, name, name, password, false, false); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ControlCreateMember(ctx, owner, project.ID, CreateMemberInput{Username: "prep-engineer", Role: "DE"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ControlCreateMember(ctx, owner, project.ID, CreateMemberInput{Username: "prep-viewer", Role: "Viewer"}, "test"); err != nil {
		t.Fatal(err)
	}
	_, engineer := testLogin(t, s, "prep-engineer", password, "198.51.100.185")
	_, viewer := testLogin(t, s, "prep-viewer", password, "198.51.100.186")
	candidates, err := s.ProvisionPublicKeys(ctx, engineer, project.ID, Pagination{Page: 1, PageSize: 20})
	if err != nil || candidates.Total != 1 {
		t.Fatalf("DE cannot choose public installation identity: %v", err)
	}
	if _, err = s.ProjectSSHDomainKeys(ctx, engineer, project.ID, Pagination{Page: 1, PageSize: 20}); err == nil {
		t.Fatal("DE gained SSH credential management access")
	}
	if _, err = s.ProvisionPublicKeys(ctx, viewer, project.ID, Pagination{Page: 1, PageSize: 20}); err == nil {
		t.Fatal("viewer gained deployment public key access")
	}
	request, err := s.CreateProvisionRequest(ctx, engineer, project, asset, CreateProvisionRequestInput{ProvisionPreviewInput: input, Reason: "engineer installation preparation"}, "engineer-request", "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangeProvisionRequest(ctx, engineer, project.ID, request.ID, "approve", ProvisionRequestActionInput{ExpectedVersion: 1, Reason: "must not approve"}, "engineer-approve", "test"); err == nil {
		t.Fatal("DE gained approval authority")
	}
}
