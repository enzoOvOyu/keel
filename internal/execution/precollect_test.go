package execution

import (
	"context"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestPrecollectCannotUseGenericHostAdmission(t *testing.T) {
	service := New(nil)
	// Validate before database/transport use; the dedicated domain API is the
	// only place allowed to bind the required independent model/BOM identities.
	if _, e := service.CreateSSH(context.Background(), nil, strings.Repeat("1", 32), SSHCreateInput{Operation: "host.precollect"}, "key", "trace"); e == nil {
		t.Fatal("generic SSH admitted collection")
	}
	yaml := strings.Replace(hostFlowSample, "host.identity", "host.precollect", 1)
	if _, e := ValidateFlow(FlowValidationInput{YAMLSource: yaml}); e == nil {
		t.Fatal("generic host Flow admitted collection")
	}
	service.MediaStore = &platform.Store{}
	if _, e := service.CreatePrecollect(context.Background(), nil, strings.Repeat("1", 32), PrecollectCreateInput{}, "key", "trace"); e == nil {
		t.Fatal("disabled domain admitted collection")
	}
	parent := &Execution{Mode: "ssh", Snapshot: Snapshot{Mode: "ssh", Flow: FlowVersion{Definition: Definition{APIVersion: "keel.precollect/v1"}}}}
	if validNativeHostRerun(parent) {
		t.Fatal("generic rerun bypassed renewed G1 scope preview")
	}
}

// Auth, scope reads and no-evidence behavior use the existing disposable-PG
// fixture. The collector and any hardware tools are deliberately not invoked.
func TestPrecollectMissingPrerequisitesCannotPassPostgres(t *testing.T) {
	f, asset := bmcExecutionFixture(t)
	ctx := context.Background()
	f.store.PrecollectEnabled = true
	scope, e := f.store.PreviewPrecollect(ctx, f.actor, f.project, asset)
	mustAgent(t, e)
	if scope.Ready || scope.Expectation.GPUCount != nil || scope.Expectation.NICCount != nil {
		t.Fatalf("missing independent model became ready: %+v", scope)
	}
	before, e := f.store.CurrentG1(ctx, f.actor, f.project, asset)
	mustAgent(t, e)
	if before.EffectiveVerdict != "BLOCKED" || before.Evidence != nil {
		t.Fatal("missing G1 became pass", before)
	}
	history, e := f.store.ListPrecollect(ctx, f.actor, f.project, asset, platform.Pagination{Page: 1, PageSize: 20})
	mustAgent(t, e)
	if history.Total != 0 || len(history.Items) != 0 {
		t.Fatal("GET fabricated collection history")
	}
	if _, e = f.store.GetPrecollect(ctx, f.actor, f.project, asset, platform.RandomID()); e == nil {
		t.Fatal("unknown request exposed evidence")
	}
	if _, _, e = f.store.PrecollectRaw(ctx, f.actor, f.project, platform.RandomID(), platform.RandomID()); e == nil {
		t.Fatal("foreign asset opened raw storage")
	}
	if _, e = f.store.CurrentG1(ctx, f.actor, platform.RandomID(), asset); e == nil {
		t.Fatal("project/asset relation not checked")
	}
	var state string
	mustAgent(t, f.store.DB.QueryRowContext(ctx, `SELECT state FROM project_assets WHERE id=$1`, asset).Scan(&state))
	if state != "provisioned" {
		t.Fatal("read mutated asset state", state)
	}
}
