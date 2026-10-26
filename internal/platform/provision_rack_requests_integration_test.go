package platform

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func provisionRackFixture(t *testing.T) (*Store, *AuthSession, *AuthSession, *ProjectSummary, CreateProvisionRackRequestsInput) {
	t.Helper()
	s, owner, reviewer, project, asset, target, _, _, _, input := provisionFixture(t)
	ctx := context.Background()
	var model string
	if err := s.DB.QueryRowContext(ctx, "SELECT name FROM device_models WHERE id=(SELECT model_id FROM device_model_versions WHERE id=$1)", target.ModelVersionID).Scan(&model); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, "UPDATE machines SET rack='rack-A' WHERE id=$1", target.MachineID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitBomImport(ctx, owner, project, BomImportInput{FileVersion: "rack-reinstall-v1", Machines: []BomMachineRow{{SerialNumber: "PREVIEW-SERVER-2", Hostname: "preview-node-02", ModelName: model, Rack: "rack-A", Ports: []BomPortRow{{MacAddress: "aa:bb:cc:dd:ee:82", PortName: "p1", Rail: 0}}}}}, "test"); err != nil {
		t.Fatal(err)
	}
	var secondID string
	if err := s.DB.QueryRowContext(ctx, "SELECT id FROM project_assets WHERE project_id=$1 AND id<>$2", project.ID, asset).Scan(&secondID); err != nil {
		t.Fatal(err)
	}
	second, err := s.GetProvisionTarget(ctx, owner, project, secondID)
	if err != nil {
		t.Fatal(err)
	}
	secondInput := input
	secondInput.AssetVersion, secondInput.BootPortID = second.AssetVersion, second.Ports[0].ID
	in, err := canonicalProvisionRackInput(CreateProvisionRackRequestsInput{Rack: "rack-A", Reason: "explicitly selected rack reinstall preparation", Items: []ProvisionRackRequestItem{{AssetID: asset, ProvisionPreviewInput: input, ConfirmSerial: target.Serial}, {AssetID: secondID, ProvisionPreviewInput: secondInput, ConfirmSerial: second.Serial}}})
	if err != nil {
		t.Fatal(err)
	}
	return s, owner, reviewer, project, in
}

func assertRackRequestCount(t *testing.T, s *Store, project string, want int) {
	t.Helper()
	var requests, executions, batches int
	ctx := context.Background()
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM provision_requests WHERE project_id=$1", project).Scan(&requests); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_executions WHERE project_id=$1", project).Scan(&executions); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE project_id=$1 AND action='provision.rack_requests.created'", project).Scan(&batches); err != nil {
		t.Fatal(err)
	}
	if requests != want || executions != 0 || (want == 0 && batches != 0) {
		t.Fatalf("partial or executable side effects: requests=%d batches=%d executions=%d", requests, batches, executions)
	}
}

func TestProvisionRackAtomicRequestsRemainIndividuallyReviewed(t *testing.T) {
	s, owner, reviewer, project, in := provisionRackFixture(t)
	ctx := context.Background()
	batch, err := s.CreateProvisionRackRequests(ctx, owner, project, in, "rack-once", "test")
	if err != nil || batch == nil || len(batch.Requests) != 2 {
		t.Fatalf("rack preparation failed: %v", err)
	}
	if !modelIDPattern.MatchString(batch.BatchID) || batch.Rack != in.Rack || batch.ProjectID != project.ID || len(batch.RequestSHA256) != 64 {
		t.Fatal("batch evidence is not bound")
	}
	for _, request := range batch.Requests {
		if request.State != "requested" || request.StateVersion != 1 || !request.Reinstall || request.ExecutionID != nil || request.ApprovedBy != nil || !ProvisionSnapshotValid(&request) {
			t.Fatal("rack request bypassed the original state machine")
		}
	}
	in.Items[0], in.Items[1] = in.Items[1], in.Items[0]
	again, err := s.CreateProvisionRackRequests(ctx, owner, project, in, "rack-once", "test")
	firstBytes, _ := json.Marshal(batch)
	againBytes, _ := json.Marshal(again)
	if err != nil || string(firstBytes) != string(againBytes) {
		t.Fatal("reordered retry did not preserve the exact batch response", err)
	}
	for _, mutate := range []func(*CreateProvisionRackRequestsInput){
		func(x *CreateProvisionRackRequestsInput) { x.Rack = "rack-B" },
		func(x *CreateProvisionRackRequestsInput) { x.Items[0].AssetVersion++ },
		func(x *CreateProvisionRackRequestsInput) { x.Items[0].ConfirmSerial += "-different" },
		func(x *CreateProvisionRackRequestsInput) { x.Items[0].BootPortID = RandomID() },
		func(x *CreateProvisionRackRequestsInput) { x.Items[0].MediaID = RandomID() },
		func(x *CreateProvisionRackRequestsInput) { x.Items[0].DomainKeyID = RandomID() },
	} {
		copy := in
		copy.Items = append([]ProvisionRackRequestItem(nil), in.Items...)
		mutate(&copy)
		if _, err = s.CreateProvisionRackRequests(ctx, owner, project, copy, "rack-once", "test"); err == nil {
			t.Fatal("batch key rebound to changed consent")
		}
	}
	request := batch.Requests[0]
	action := ProvisionRequestActionInput{ExpectedVersion: 1, Reason: "independent single-machine review"}
	if _, err = s.ChangeProvisionRequest(ctx, owner, project.ID, request.ID, "approve", action, "rack-self-approval", "test"); err == nil {
		t.Fatal("batch creator approved its own request")
	}
	if _, err = s.ChangeProvisionRequest(ctx, reviewer, project.ID, request.ID, "approve", action, "rack-one-approval", "test"); err != nil {
		t.Fatal(err)
	}
	other, err := s.ReadProvisionRequest(ctx, owner, project.ID, batch.Requests[1].ID)
	if err != nil || other.State != "requested" || other.ApprovedBy != nil {
		t.Fatal("approving one machine approved the batch")
	}
	assertRackRequestCount(t, s, project.ID, 2)
}

func TestProvisionRackRollsBackEarlierRequestOnLaterFailure(t *testing.T) {
	s, owner, _, project, in := provisionRackFixture(t)
	ctx := context.Background()
	broken := in
	broken.Items = append([]ProvisionRackRequestItem(nil), in.Items...)
	// Items are sorted. The first shared creation path inserts a request before
	// the second item's media lookup fails; the outer transaction must undo it.
	broken.Items[1].MediaID = RandomID()
	if _, err := s.CreateProvisionRackRequests(ctx, owner, project, broken, "rack-rollback", "test"); err == nil {
		t.Fatal("unknown media admitted")
	}
	assertRackRequestCount(t, s, project.ID, 0)
	var itemAudits int
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE project_id=$1 AND action='provision.request.created'", project.ID).Scan(&itemAudits); err != nil || itemAudits != 0 {
		t.Fatal("rolled-back member evidence escaped the transaction", err)
	}
	// The failed batch must not poison the idempotency key or consume any asset.
	if _, err := s.CreateProvisionRackRequests(ctx, owner, project, in, "rack-rollback", "test"); err != nil {
		t.Fatal(err)
	}
	assertRackRequestCount(t, s, project.ID, 2)
}

func TestProvisionRackRejectsChangedRackVersionSerialAndForeignAsset(t *testing.T) {
	s, owner, _, project, in := provisionRackFixture(t)
	ctx := context.Background()
	for name, mutate := range map[string]func(*CreateProvisionRackRequestsInput){
		"rack":    func(x *CreateProvisionRackRequestsInput) { x.Rack = "RACK-A" },
		"version": func(x *CreateProvisionRackRequestsInput) { x.Items[1].AssetVersion++ },
		"serial":  func(x *CreateProvisionRackRequestsInput) { x.Items[1].ConfirmSerial += "-wrong" },
		"asset":   func(x *CreateProvisionRackRequestsInput) { x.Items[1].AssetID = RandomID() },
	} {
		copy := in
		copy.Items = append([]ProvisionRackRequestItem(nil), in.Items...)
		mutate(&copy)
		if _, err := s.CreateProvisionRackRequests(ctx, owner, project, copy, "bad-"+name, "test"); err == nil {
			t.Fatalf("%s mismatch admitted", name)
		}
		assertRackRequestCount(t, s, project.ID, 0)
	}
	item := in.Items[1]
	if _, err := s.CreateProvisionRequest(ctx, owner, project, item.AssetID, CreateProvisionRequestInput{ProvisionPreviewInput: item.ProvisionPreviewInput, Reason: "existing conflicting single request", Reinstall: true, ConfirmSerial: item.ConfirmSerial}, "single-before-rack", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateProvisionRackRequests(ctx, owner, project, in, "rack-conflict", "test"); err == nil {
		t.Fatal("existing per-machine request ignored")
	}
	assertRackRequestCount(t, s, project.ID, 1)
}

func TestProvisionRackChecksCurrentRoleBeforeIdempotentReplay(t *testing.T) {
	s, owner, _, project, in := provisionRackFixture(t)
	ctx := context.Background()
	const password = "test-only-rack-engineer-password"
	if _, err := s.CreateUser(ctx, "rack-engineer", "Rack engineer", password, false, false); err != nil {
		t.Fatal(err)
	}
	member, err := s.ControlCreateMember(ctx, owner, project.ID, CreateMemberInput{Username: "rack-engineer", Role: "Viewer"}, "test")
	if err != nil {
		t.Fatal(err)
	}
	_, engineer := testLogin(t, s, "rack-engineer", password, "198.51.100.187")
	if _, err = s.CreateProvisionRackRequests(ctx, engineer, project, in, "rack-role", "test"); err == nil {
		t.Fatal("viewer gained rack reinstall preparation authority")
	}
	member, err = s.ControlChangeMember(ctx, owner, project.ID, engineer.User.ID, UpdateMemberInput{Role: "DE", Version: member.Version}, false, "test")
	if err != nil {
		t.Fatal(err)
	}
	_, engineer = testLogin(t, s, "rack-engineer", password, "198.51.100.187")
	if _, err = s.CreateProvisionRackRequests(ctx, engineer, project, in, "rack-role", "test"); err != nil {
		t.Fatal("current engineer cannot prepare a bounded rack selection", err)
	}
	if _, err = s.ControlChangeMember(ctx, owner, project.ID, engineer.User.ID, UpdateMemberInput{Role: "Viewer", Version: member.Version}, false, "test"); err != nil {
		t.Fatal(err)
	}
	_, engineer = testLogin(t, s, "rack-engineer", password, "198.51.100.187")
	if _, err = s.CreateProvisionRackRequests(ctx, engineer, project, in, "rack-role", "test"); err == nil {
		t.Fatal("old successful batch response bypassed current role")
	}
	assertRackRequestCount(t, s, project.ID, 2)
}

func TestProvisionRackOverlappingBatchesShareMachineExclusion(t *testing.T) {
	s, owner, _, project, in := provisionRackFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, key := range []string{"rack-concurrent-a", "rack-concurrent-b"} {
		key := key
		go func() {
			<-start
			_, err := s.CreateProvisionRackRequests(ctx, owner, project, in, key, "test")
			results <- err
		}()
	}
	close(start)
	successes := 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		}
	}
	if successes != 1 || ctx.Err() != nil {
		t.Fatal("overlapping batches duplicated requests or deadlocked")
	}
	assertRackRequestCount(t, s, project.ID, 2)
}
