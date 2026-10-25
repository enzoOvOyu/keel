package platform

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
)

func TestModelPublicationPreservesAndSeals(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-model-review-password"
	if _, e := s.CreateUser(ctx, "model-review", "Model review", password, true, true); e != nil {
		t.Fatal(e)
	}
	_, a := testLogin(t, s, "model-review", password, "198.51.100.90")
	p, e := s.CreateProject(ctx, a, CreateProjectInput{Name: "Model review", Slug: "model-review"}, "model-review-project", "test")
	if e != nil {
		t.Fatal(e)
	}
	in := ImportModelScanInput{ModelName: "Reviewed server", SourceHost: "fixture-node", HwSpec: map[string]any{"kind": "server", "cpu_arch": "amd64"}, ExpectedTopology: map[string]any{"source": "independent-fixture"}, CapabilityMatrix: map[string]any{"feature": "fixture"}, PanelLayout: map[string]any{"source": "panel-fixture"}, BomExpect: map[string]any{"nic_count": float64(2)}, DefaultRolePlan: map[string]any{"management": "slot-2"}, Slots: []ModelSlot{{SlotNo: 2, PcieSlot: "PCIe-2", ExpectNicModel: "fixture", PanelPort: "2", DefaultRole: "management"}, {SlotNo: 1, PcieSlot: "PCIe-1", ExpectNicModel: "fixture", PanelPort: "1", DefaultRole: "compute"}}}
	in.PanelLayout = map[string]any{"source": "independent-panel-fixture", "layout_v1": map[string]any{"rows": 1, "columns": 2, "ports": []any{map[string]any{"id": "p1", "label": "NIC 1", "row": 1, "column": 1, "slot_no": 1}, map[string]any{"id": "p2", "label": "NIC 2", "row": 1, "column": 2, "slot_no": 2}}}}
	d, e := s.ImportModelScan(ctx, a, p, in, "test")
	if e != nil {
		t.Fatal(e)
	}
	// Invalid slot identities must not create an automatic model, a draft,
	// or a published version, in either project or global scope.
	for _, scope := range []*ProjectSummary{p, nil} {
		name := "Invalid slot identity"
		if scope == nil {
			name += " global"
		}
		for _, location := range []string{"PCIe-2", "2", " PCIe-1 "} {
			invalid := in
			invalid.ModelName = name
			invalid.Slots = append([]ModelSlot{}, in.Slots...)
			invalid.Slots[1].PcieSlot = location
			if _, err := s.ImportModelScan(ctx, a, scope, invalid, "test"); err == nil {
				t.Fatal("ambiguous scan created a draft")
			} else {
				assertErrorCode(t, err, "VALIDATION_ERROR")
			}
			if _, err := s.PublishModelVersion(ctx, a, p, d.ModelID, invalid.payload(), "test"); err == nil {
				t.Fatal("ambiguous slots published")
			} else {
				assertErrorCode(t, err, "VALIDATION_ERROR")
			}
		}
		var count int
		if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM device_models WHERE name=$1", name).Scan(&count); err != nil || count != 0 {
			t.Fatalf("invalid scan left model identity: %d %v", count, err)
		}
	}
	for _, table := range []string{"device_model_versions", "device_model_drafts"} {
		var count int
		want := 0
		if table == "device_model_drafts" {
			want = 1
		}
		if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM "+table+" WHERE model_id=$1", d.ModelID).Scan(&count); err != nil || count != want {
			t.Fatalf("invalid slots modified %s: %d %v", table, count, err)
		}
	}
	for _, scope := range []*ProjectSummary{p, nil} {
		invalid := in
		invalid.ModelName = "Invalid panel identity"
		invalid.PanelLayout = map[string]any{"layout_v1": map[string]any{"rows": 1, "columns": 1, "ports": []any{map[string]any{"id": "p1", "label": "Missing slot", "row": 1, "column": 1, "slot_no": 99}}}}
		if _, err := s.ImportModelScan(ctx, a, scope, invalid, "test"); err == nil {
			t.Fatal("invalid panel created a draft")
		} else {
			assertErrorCode(t, err, "VALIDATION_ERROR")
		}
		if _, err := s.PublishModelVersion(ctx, a, p, d.ModelID, invalid.payload(), "test"); err == nil {
			t.Fatal("invalid panel published")
		} else {
			assertErrorCode(t, err, "VALIDATION_ERROR")
		}
	}
	var invalidPanels int
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM device_models WHERE name='Invalid panel identity'").Scan(&invalidPanels); err != nil || invalidPanels != 0 {
		t.Fatalf("invalid panel left identity: %d %v", invalidPanels, err)
	}
	again := in
	again.Slots = []ModelSlot{in.Slots[1], in.Slots[0]}
	reuse, e := s.ImportModelScan(ctx, a, p, again, "test")
	if e != nil || reuse.ID != d.ID || len(reuse.Slots) != 2 || string(reuse.CapabilityMatrix) == "{}" {
		t.Fatalf("draft reuse lost metadata %+v %v", reuse, e)
	}
	// Preserve a legacy hash in storage: semantic dedup must still find the draft.
	if _, e = s.DB.ExecContext(ctx, "UPDATE device_model_drafts SET content_hash=$2 WHERE id=$1", d.ID, "legacy-hash-before-canonicalization"); e != nil {
		t.Fatal(e)
	}
	reuse, e = s.ImportModelScan(ctx, a, p, in, "test")
	if e != nil || reuse.ID != d.ID {
		t.Fatalf("legacy draft lost %v", e)
	}
	expected := int64(0)
	payload := in.payload()
	payload.ExpectedVersion = &expected
	payload.SourceDraftID = d.ID
	v, e := s.PublishModelVersion(ctx, a, p, d.ModelID, payload, "test")
	if e != nil {
		t.Fatal(e)
	}
	if v.ContentHash != d.ContentHash || len(v.Slots) != 2 {
		t.Fatal("publication lost draft content")
	}
	if _, e = s.PublishModelVersion(ctx, a, p, d.ModelID, payload, "test"); e == nil {
		t.Fatal("stale publish created another version")
	} else {
		assertErrorCode(t, e, "MODEL_VERSION_CONFLICT")
	}
	if _, e = s.ImportModelScan(ctx, a, p, in, "test"); e == nil {
		t.Fatal("published scan did not match")
	} else {
		assertErrorCode(t, e, "ALREADY_CURRENT")
	}
	machine, asset := RandomID(), RandomID()
	if _, e = s.DB.ExecContext(ctx, "INSERT INTO machines(id,serial_number,created_at,updated_at) VALUES($1,'MODEL-FIXTURE-1',now(),now())", machine); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, "INSERT INTO project_assets(id,project_id,machine_id,model_id,model_version_id,hostname,created_at,updated_at) VALUES($1,$2,$3,$4,$5,'model-fixture',now(),now())", asset, p.ID, machine, d.ModelID, v.ID); e != nil {
		t.Fatal(e)
	}
	// Immutability applies to unreferenced versions too, including late slot appends.
	for _, q := range []string{`UPDATE device_model_versions SET hw_spec='{}' WHERE id=$1`, `DELETE FROM device_model_versions WHERE id=$1`, `UPDATE device_model_slots SET panel_port='changed' WHERE model_version_id=$1`, `DELETE FROM device_model_slots WHERE model_version_id=$1`, `INSERT INTO device_model_slots(id,model_version_id,slot_no,pcie_slot,expect_nic_model,panel_port,default_role) VALUES('late-slot-test',$1,3,'3','fixture','3','compute')`} {
		if _, e = s.DB.ExecContext(ctx, q, v.ID); e == nil {
			t.Fatalf("published content changed: %s", q)
		}
	}
	// Two editors at the same observed version yield exactly one successor.
	expected = 1
	payload.SourceDraftID = ""
	payload.PanelLayout = map[string]any{"layout_v1": map[string]any{"rows": 1, "columns": 1, "ports": []any{map[string]any{"id": "new-p1", "label": "New layout", "row": 1, "column": 1, "slot_no": 1}}}}
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.PublishModelVersion(ctx, a, p, d.ModelID, payload, "test")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else {
			assertErrorCode(t, err, "MODEL_VERSION_CONFLICT")
			conflict++
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal("concurrent editors did not conflict")
	}
	versions, e := s.ListModelVersions(ctx, a, p, d.ModelID, Pagination{Page: 1, PageSize: 20})
	if e != nil || len(versions.Items) != 2 {
		t.Fatalf("versions %+v %v", versions, e)
	}
	original := versions.Items[1]
	if original.ID != v.ID || original.ContentHash != v.ContentHash || len(original.Slots) != 2 {
		t.Fatal("old version changed")
	}
	other, e := s.CreateDeviceModel(ctx, a, p, CreateDeviceModelInput{Name: "Other model", Kind: "server"}, "test")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DeleteModelDraft(ctx, a, p, other.ID, d.ID, "test"); e == nil {
		t.Fatal("wrong model path discarded draft")
	}
	global, e := s.CreateDeviceModel(ctx, a, nil, CreateDeviceModelInput{Name: "Global model", Kind: "server"}, "test")
	if e != nil {
		t.Fatal(e)
	}
	globalDraft, e := s.ImportModelScan(ctx, a, nil, ImportModelScanInput{ModelName: global.Name, SourceHost: "global-draft-private-source"}, "test")
	if e != nil {
		t.Fatal(e)
	}
	hidden, e := s.ListModelDrafts(ctx, a, p, global.ID)
	if e != nil || len(hidden) != 0 {
		t.Fatal("project read unpublished global drafts")
	}
	if globalDraft.ModelID != global.ID {
		t.Fatal("global identity changed")
	}
	if _, e = s.PublishModelVersion(ctx, a, p, global.ID, PublishModelVersionInput{}, "test"); e == nil {
		t.Fatal("project modified global model")
	}
	if _, e = s.RetireDeviceModel(ctx, a, p, global.ID, "test"); e == nil {
		t.Fatal("project retired global model")
	}
	if _, e = s.RetireDeviceModel(ctx, a, p, d.ModelID, "test"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ImportModelScan(ctx, a, p, in, "test"); e == nil {
		t.Fatal("scan resurrected retired model")
	} else {
		assertErrorCode(t, e, "MODEL_RETIRED")
	}
	var bound string
	if e = s.DB.QueryRowContext(ctx, "SELECT model_version_id FROM project_assets WHERE id=$1", asset).Scan(&bound); e != nil || bound != v.ID {
		t.Fatal("new publication or retirement silently changed asset binding")
	}
	if _, e = s.DB.ExecContext(ctx, "DELETE FROM device_models WHERE id=$1", d.ModelID); e == nil {
		t.Fatal("referenced model was deleted")
	}
	// Hash equality remains interpretable after JSON round trips, not just Go maps.
	var roundTrip ImportModelScanInput
	raw, _ := json.Marshal(in)
	json.Unmarshal(raw, &roundTrip)
	hash, _ := modelContentHash(roundTrip.payload())
	if hash != v.ContentHash {
		t.Fatal("JSON round trip changed model identity")
	}
}
