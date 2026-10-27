package platform

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

func TestNetworkTemplateCatalogReadSnapshotAndAdmissionPostgres(t *testing.T) {
	s, root, _, _, p := templateLineageFixture(t)
	ctx := context.Background()
	s.MediaObjects = &testMediaObjects{}
	suffix := RandomID()[:12]
	password := "p2-catalog-review-test-password"
	reviewName := "catalog-review-" + suffix
	if _, e := s.CreateUser(ctx, reviewName, reviewName, password, true, false); e != nil {
		t.Fatal(e)
	}
	_, reviewer := testLogin(t, s, reviewName, password, "198.51.100.212")
	for _, in := range []UpsertParamKeyInput{{Key: "fabric.nos.version", ValueType: "version", SourceMediaType: "os"}, {Key: "fabric.profile", ValueType: "enum", Options: []string{"reviewed", "other"}}} {
		if _, e := s.UpsertParamKey(ctx, root, p, in, "test"); e != nil {
			t.Fatal(e)
		}
	}
	media, e := s.IngestMedia(ctx, root, p, RegisterMediaInput{Type: "os", Name: "test-nos-" + suffix, Version: "p2-reviewed", Arch: "amd64", Deps: map[string]any{"compatibility": map[string]any{"source": "fixture:approved-network", "version": "1", "report_ref": "fixture:report", "requires": map[string][]string{"fabric.profile": {"reviewed"}}}}}, bytes.NewBufferString("p2 synthetic NOS bytes"), "test")
	if e != nil {
		t.Fatal(e)
	}
	for _, state := range []string{"verified", "approved"} {
		if _, e = s.TransitionMedia(ctx, reviewer, p, media.ID, state, "test"); e != nil {
			t.Fatal(e)
		}
	}
	model, version := RandomID(), RandomID()
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO device_models(id,scope_key,name,kind,created_by,created_at,updated_at) VALUES($1,$2,$3,'server',$4,now(),now())`, model, "project:"+p.ID, "NOS model "+suffix, root.User.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.ExecContext(ctx, `INSERT INTO device_model_versions(id,model_id,version,hw_spec,content_hash,published_at) VALUES($1,$2,1,'{"cpu_arch":"amd64"}',repeat('a',64),now())`, version, model); e != nil {
		t.Fatal(e)
	}
	definition := netbaseline.Template{Source: `{"version":{{nos|keeljson}},"profile":{{profile|keeljson}}}`, Variables: map[string]netbaseline.Variable{"nos": {Type: "version", Required: true, CatalogKey: "fabric.nos.version"}, "profile": {Type: "string", Required: true, CatalogKey: "fabric.profile"}}}
	values := map[string]any{"nos": map[string]any{"param_key": "fabric.nos.version", "value": "p2-reviewed"}, "profile": map[string]any{"param_key": "fabric.profile", "value": "reviewed"}}
	resolve := func(readonly bool, input map[string]any) (map[string]string, map[string]any, []CatalogSelection, error) {
		t.Helper()
		tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: readonly, Isolation: sql.LevelRepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		c, err := newNetworkVariableCatalogTx(ctx, tx, p.ID, definition, !readonly)
		if err != nil {
			return nil, nil, nil, err
		}
		return c.resolve(ctx, tx, p.ID, definition, input, version, !readonly)
	}
	approved, resolved, selections, e := resolve(true, values)
	if e != nil {
		t.Fatal("read-only catalog attempted row locks or lost compatibility:", e)
	}
	if resolved["nos"] != "p2-reviewed" || approved["fabric.profile"] != "reviewed" || len(selections) != 2 {
		t.Fatalf("unbound catalog selection: %+v", selections)
	}
	found := false
	for _, selection := range selections {
		if selection.Key == "fabric.nos.version" {
			found = selection.Media != nil && selection.Media.ID == media.ID && selection.Media.SHA256 == media.Sha256 && selection.Media.ObjectVersion != ""
		}
	}
	if !found {
		t.Fatal("version lost immutable stored media identity")
	}
	_, _, locked, e := resolve(false, values)
	if e != nil {
		t.Fatal(e)
	}
	left, _ := json.Marshal(selections)
	right, _ := json.Marshal(locked)
	if !bytes.Equal(left, right) {
		t.Fatal("preview and locking admission disagree")
	}
	values["nos"] = "p2-reviewed"
	_, _, _, e = resolve(true, values)
	assertErrorCode(t, e, "NETWORK_CATALOG_REFERENCE_REQUIRED")
	values["nos"] = map[string]any{"param_key": "fabric.nos.version", "value": "p2-reviewed"}
	values["profile"] = map[string]any{"param_key": "fabric.profile", "value": "other"}
	_, _, _, e = resolve(true, values)
	assertErrorCode(t, e, "NETWORK_CATALOG_SELECTION_REJECTED")
	values["profile"] = map[string]any{"param_key": "fabric.profile", "value": "reviewed"}
	if _, e = s.UpsertParamKey(ctx, root, p, UpsertParamKeyInput{Key: "fabric.profile", ValueType: "enum", Options: []string{"other"}}, "test"); e != nil {
		t.Fatal(e)
	}
	_, _, _, e = resolve(false, values)
	assertErrorCode(t, e, "NETWORK_CATALOG_SELECTION_REJECTED")
}
