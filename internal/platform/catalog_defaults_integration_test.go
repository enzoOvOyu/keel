package platform

import (
	"context"
	"database/sql"
	"testing"
)

func TestCatalogDefaultConsumersRespectScopeAndRetirement(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "isolated-catalog-consumer-password"
	if _, e := s.CreateUser(ctx, "catalog-consumer", "Catalog Consumer", password, true, true); e != nil {
		t.Fatal(e)
	}
	_, actor := testLogin(t, s, "catalog-consumer", password, "198.51.100.119")
	p, e := s.CreateProject(ctx, actor, CreateProjectInput{Name: "Consumer scope", Slug: "consumer-scope"}, "consumer-scope", "test")
	if e != nil {
		t.Fatal(e)
	}
	put := func(project *ProjectSummary, key, value string) *ParamKeySummary {
		t.Helper()
		out, err := s.UpsertParamKey(ctx, actor, project, UpsertParamKeyInput{Key: key, ValueType: "string", DefaultValue: value}, "test")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	read := func(fn func(*sql.Tx) error) error {
		t.Helper()
		tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		return fn(tx)
	}
	// No declaration preserves the documented missing-policy/unknown behavior.
	if e = read(func(tx *sql.Tx) error {
		limits, err := fabricDDMLimitsTx(ctx, tx, p.ID)
		if err == nil && len(limits) != 0 {
			t.Fatal("missing DDM thresholds were invented")
		}
		interval, err2 := fabricDDMIntervalTx(ctx, tx, p.ID)
		if err2 != nil || interval != nil {
			t.Fatalf("missing interval: %v %v", interval, err2)
		}
		return err
	}); e != nil {
		t.Fatal(e)
	}
	cases := []struct {
		key, global, project string
		check                func(*sql.Tx) error
	}{
		{"fabric.ddm.temperature_c.warn_max", "malformed-global", "70", func(tx *sql.Tx) error {
			out, err := fabricDDMLimitsTx(ctx, tx, p.ID)
			if err == nil && (out["temperature_c"].WarnMax == nil || *out["temperature_c"].WarnMax != 70) {
				t.Fatal("DDM scope override not honored")
			}
			return err
		}},
		{"fabric.ddm_interval", "malformed-global", "30s", func(tx *sql.Tx) error {
			out, err := fabricDDMIntervalTx(ctx, tx, p.ID)
			if err == nil && (out == nil || *out != 30) {
				t.Fatal("interval scope override not honored")
			}
			return err
		}},
		{"fabric.precheck.counter_window_seconds", "malformed-global", "60", func(tx *sql.Tx) error {
			out, err := fabricCounterPolicyTx(ctx, tx, p.ID)
			if err == nil && (out.WindowSeconds == nil || *out.WindowSeconds != 60) {
				t.Fatal("counter scope override not honored")
			}
			return err
		}},
		{"fabric.health_score.weights", `{"LL-01":90,"LL-02":80}`, `{"LL-01":7}`, func(tx *sql.Tx) error {
			out, err := networkRuleDefaultsTx(ctx, tx, p.ID)
			if err == nil {
				for _, rule := range out {
					if rule.ID == "LL-01" && rule.Weight != 7 || rule.ID == "LL-02" && rule.Weight == 80 {
						t.Fatal("project weight definition was merged with the overridden global definition")
					}
				}
			}
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			global := put(nil, c.key, c.global)
			local := put(p, c.key, c.project)
			if e := read(c.check); e != nil {
				t.Fatalf("valid project override rejected: %v", e)
			}
			if _, e := s.RetireParamKey(ctx, actor, nil, global.ID, "test"); e != nil {
				t.Fatal(e)
			}
			if e := read(c.check); e != nil {
				t.Fatalf("retired shadowed global policy blocked valid project definition: %v", e)
			}
			if _, e := s.RetireParamKey(ctx, actor, p, local.ID, "test"); e != nil {
				t.Fatal(e)
			}
			assertErrorCode(t, read(c.check), "PARAM_KEY_RETIRED")
		})
	}
	global := put(nil, "fabric.push_concurrency", "12")
	put(p, "unrelated.setting", "unused")
	if e := read(func(tx *sql.Tx) error {
		out, err := catalogDefaultsTx(ctx, tx, p.ID, "fabric.push_concurrency")
		if err == nil && out["fabric.push_concurrency"] != "12" {
			t.Fatal("global inheritance missing")
		}
		return err
	}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.RetireParamKey(ctx, actor, nil, global.ID, "test"); e != nil {
		t.Fatal(e)
	}
	assertErrorCode(t, read(func(tx *sql.Tx) error {
		_, err := catalogDefaultsTx(ctx, tx, p.ID, "fabric.push_concurrency")
		return err
	}), "PARAM_KEY_RETIRED")
	if e = read(func(tx *sql.Tx) error {
		out, err := catalogDefaultsTx(ctx, tx, RandomID(), "unrelated.setting")
		if err == nil && len(out) != 0 {
			t.Fatal("another project definition leaked into the consumer")
		}
		return err
	}); e != nil {
		t.Fatal(e)
	}
}
