package execution

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestOperatorT7CannotUploadPassOrUseUnboundSnapshotPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	issue, e := f.store.CreateIssue(ctx, f.actor, f.project, platform.CreateIssueInput{Title: "Observed topology mismatch", Category: "T7", Source: "topology", Phase: "delivery", Anchors: []platform.IssueAnchor{{Kind: "machine", ID: f.machine}}, Evidence: platform.IssueEvidence{ObservedAt: time.Now().UTC(), SourceVersion: "independent-model-reference", Expected: "slot A", Actual: "slot B"}}, "operator-t7", "test")
	mustAgent(t, e)
	scope, e := f.store.PreviewTopologyRetest(ctx, f.actor, issue.ID)
	mustAgent(t, e)
	if scope.Ready {
		t.Fatal("operator-reported T7 acquired a trusted discovery binding")
	}
	rows, e := f.store.ListTopologyRetests(ctx, f.actor, issue.ID, platform.Pagination{Page: 1, PageSize: 10})
	mustAgent(t, e)
	if rows.Total != 0 {
		t.Fatal("GET fabricated a retest")
	}
	if _, e = f.store.CreateTopologyRetest(ctx, f.actor, issue.ID, platform.TopologyRetestInput{Version: issue.Version, SnapshotID: platform.RandomID(), Reason: "Try unrelated snapshot"}, "untrusted-snapshot", "test"); e == nil {
		t.Fatal("unbound T7 accepted snapshot evidence")
	}
	if _, e = f.store.ActOnIssue(ctx, f.actor, issue.ID, "close", platform.IssueActionInput{Version: issue.Version, Reason: "Operator declares PASS"}, "test"); e == nil {
		t.Fatal("operator text closed T7")
	}
	other, _ := ordinaryTestActor(t, f.store)
	if _, e = f.store.ListTopologyRetests(ctx, other, issue.ID, platform.Pagination{Page: 1, PageSize: 10}); e == nil {
		t.Fatal("retests leaked to another project's nonmember")
	}
	if _, e = f.store.PreviewTopologyRetest(ctx, other, issue.ID); e == nil {
		t.Fatal("scope leaked across membership boundary")
	}
	var count int
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT count(*) FROM topology_issue_retests WHERE issue_id=$1`, issue.ID).Scan(&count))
	if count != 0 {
		t.Fatal("rejected input wrote retest proof")
	}
}

func ordinaryTestActor(t *testing.T, store *platform.Store) (*platform.AuthSession, string) {
	t.Helper()
	ctx := context.Background()
	name := "scoped-user-" + platform.RandomID()[:10]
	password := "isolated-scoped-user-test-password"
	_, e := store.CreateUser(ctx, name, name, password, false, false)
	mustAgent(t, e)
	pre, _, e := store.NewPreSession(ctx)
	mustAgent(t, e)
	_, actor, e := store.Login(ctx, pre, platform.CSRFToken(pre), platform.LoginInput{Username: name, Password: password, Method: "local"}, "198.51.100.187", "scoped-test")
	mustAgent(t, e)
	return actor, name
}
func TestTopologyRetestNeedsCurrentVersionSnapshotAndReason(t *testing.T) {
	store := &platform.Store{}
	for _, in := range []platform.TopologyRetestInput{{}, {Version: 1, SnapshotID: strings.Repeat("a", 32), Reason: "x"}, {Version: 1, SnapshotID: "foreign", Reason: "Valid reason"}} {
		if _, e := store.CreateTopologyRetest(context.Background(), nil, strings.Repeat("b", 32), in, "key", "test"); e == nil {
			t.Fatal("unscoped retest admitted")
		}
	}
}
