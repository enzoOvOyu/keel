package execution

import (
	"context"
	"testing"

	"github.com/enzoOvOyu/keel/internal/assetview"
	"github.com/enzoOvOyu/keel/internal/platform"
)

// Disposable-PG source coverage. No collector, tool, browser or hardware runs.
func TestAssetViewsPrivateSharingCASAndRetainedProjectionPostgres(t *testing.T) {
	f := agentKernel(t)
	other := agentKernel(t)
	other.actor, other.username = ordinaryTestActor(t, f.store)
	ctx := context.Background()
	_, e := f.store.ControlCreateMember(ctx, f.actor, f.project, platform.CreateMemberInput{Username: other.username, Role: "DE"}, "asset-view-test")
	mustAgent(t, e)
	// Membership changes revoke prior sessions; exercise sharing with a fresh login.
	pre, _, e := f.store.NewPreSession(ctx)
	mustAgent(t, e)
	_, other.actor, e = f.store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: other.username, Password: "isolated-scoped-user-test-password", Method: "local"}, "198.51.100.188", "asset-view-test")
	mustAgent(t, e)
	id := platform.RandomID()
	in := platform.AssetViewInput{Name: "Private inventory", Visibility: "personal", Reason: "Initial inventory view", Definition: assetview.Definition{Columns: []assetview.Column{{ID: "host", Title: "Hostname", Source: "asset", Path: "$.hostname"}}, SortColumn: "host", SortDirection: "asc"}}
	saved, e := f.store.SaveAssetView(ctx, f.actor, f.project, id, in, "view-test")
	mustAgent(t, e)
	if saved.Version != 1 {
		t.Fatal(saved)
	}
	repeat, e := f.store.SaveAssetView(ctx, f.actor, f.project, id, in, "view-test")
	mustAgent(t, e)
	if repeat.ID != id || repeat.Version != 1 {
		t.Fatal("ambiguous retry created new revision", repeat)
	}
	if _, e = f.store.GetAssetView(ctx, other.actor, f.project, id); e == nil {
		t.Fatal("team member read another member's private view")
	}
	list, e := f.store.ListAssetViews(ctx, other.actor, f.project, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	for _, v := range list.Items {
		if v.ID == id {
			t.Fatal("private view leaked into list")
		}
	}
	data, e := f.store.AssetViewData(ctx, f.actor, f.project, id, 1, platform.Pagination{Page: 1, PageSize: 20}, false)
	mustAgent(t, e)
	if data.Total != 1 || len(data.Items) != 1 || len(data.Items[0].Cells["host"].Values) != 1 || data.Items[0].Sources["asset"].State != "current" {
		t.Fatal("retained registry projection wrong", data)
	}
	in.ExpectedVersion = 1
	in.Name = "Revised inventory"
	in.Reason = "Rename saved view"
	next, e := f.store.SaveAssetView(ctx, f.actor, f.project, id, in, "view-test")
	mustAgent(t, e)
	if next.Version != 2 {
		t.Fatal(next)
	}
	if _, e = f.store.AssetViewData(ctx, f.actor, f.project, id, 1, platform.Pagination{Page: 1, PageSize: 20}, true); e == nil {
		t.Fatal("CSV evaluated a different revision than selected")
	}
	in.Name = "Concurrent overwrite"
	if _, e = f.store.SaveAssetView(ctx, f.actor, f.project, id, in, "view-test"); e == nil {
		t.Fatal("stale version overwrote current view")
	}
	if _, e = f.store.DB.ExecContext(ctx, `UPDATE asset_view_revisions SET name='forged' WHERE view_id=$1 AND version=1`, id); e == nil {
		t.Fatal("historical view revision mutated")
	}
	in.ExpectedVersion = 2
	in.Visibility = "team"
	if _, e = f.store.SaveAssetView(ctx, f.actor, f.project, id, in, "view-test"); e == nil {
		t.Fatal("private visibility changed implicitly")
	}
	teamID := platform.RandomID()
	in.ExpectedVersion = 0
	shared, e := f.store.SaveAssetView(ctx, f.actor, f.project, teamID, in, "view-test")
	mustAgent(t, e)
	if _, e = f.store.GetAssetView(ctx, other.actor, f.project, teamID); e != nil {
		t.Fatal("team sharing unavailable", e)
	}
	in.ExpectedVersion = shared.Version
	in.Reason = "Unauthorized team edit"
	if _, e = f.store.SaveAssetView(ctx, other.actor, f.project, teamID, in, "view-test"); e == nil {
		t.Fatal("DE modified another owner's team view")
	}
	if _, e = f.store.GetAssetView(ctx, f.actor, other.project, teamID); e == nil {
		t.Fatal("view leaked across projects")
	}
}
