package platform

import (
	"context"
	"database/sql"
	"testing"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

// Authored for the P2 milestone; uses only a dedicated disposable database.
func TestNetworkTemplateScopeUsesCurrentProjectPublishedModelsPostgres(t *testing.T) {
	s, root, editor, reviewer, p := templateLineageFixture(t)
	ctx := context.Background()
	model, version := RandomID(), RandomID()
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO device_models(id,scope_key,name,kind,created_by,created_at,updated_at) VALUES($1,$2,'Scoped switch','switch',$3,now(),now())`, model, "project:"+p.ID, root.User.ID); e != nil {
		t.Fatal(e)
	}
	input := NetworkTemplateInput{Name: "scoped leaf", SourceRef: "fixture:customer-device-scope", Definition: netbaseline.Template{Source: `{"hostname":"leaf"}`, Variables: map[string]netbaseline.Variable{}, Scope: &netbaseline.TemplateScope{DeviceModelIDs: []string{model}, Roles: []string{"leaf"}, Domains: []string{"base", "ports"}}}}
	_, e := s.CreateNetworkTemplate(ctx, editor, p, input, "test")
	assertErrorCode(t, e, "NETWORK_TEMPLATE_MODEL_UNAVAILABLE")
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO device_model_versions(id,model_id,version,content_hash,published_at) VALUES($1,$2,1,repeat('a',64),now())`, version, model); e != nil {
		t.Fatal(e)
	}
	original, e := s.CreateNetworkTemplate(ctx, editor, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	if original.Definition.Scope == nil || original.Definition.Scope.DeviceModelIDs[0] != model {
		t.Fatal("lost device model classification")
	}
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if e != nil {
		t.Fatal(e)
	}
	if e = validateNetworkTemplateScopeTx(ctx, tx, p.ID, original.Definition.Scope, false); e != nil {
		t.Fatal("read-only scope attempted row lock", e)
	}
	tx.Rollback()
	foreign, e := s.CreateProject(ctx, root, CreateProjectInput{Name: "Foreign template model", Slug: "scope-" + RandomID()[:12]}, RandomID(), "test")
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.CreateNetworkTemplate(ctx, root, foreign, input, "test")
	assertErrorCode(t, e, "NETWORK_TEMPLATE_MODEL_UNAVAILABLE")
	// A rollback must not resurrect a currently retired model through old content.
	if _, e = s.DB.ExecContext(ctx, `UPDATE network_template_versions SET state='released',released_by=$2 WHERE id=$1`, original.ID, reviewer.User.ID); e != nil {
		t.Fatal(e)
	}
	head, e := s.CreateNetworkTemplate(ctx, editor, p, input, "test")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RetireDeviceModel(ctx, root, p, model, "test"); e != nil {
		t.Fatal(e)
	}
	_, e = s.RollbackNetworkTemplate(ctx, editor, p, original.ID, NetworkTemplateRollbackInput{ExpectedHeadID: head.ID, ExpectedHeadHash: head.ContentHash, Reason: "Restore previous scoped configuration"}, "test")
	assertErrorCode(t, e, "NETWORK_TEMPLATE_MODEL_UNAVAILABLE")
	_, e = s.CreateNetworkTemplate(ctx, editor, p, input, "test")
	assertErrorCode(t, e, "NETWORK_TEMPLATE_MODEL_UNAVAILABLE")
}
