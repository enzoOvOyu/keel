package platform

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/wiring"
)

// This checks a fresh real PostgreSQL schema and the frozen-scope predicates.
// It does not claim a physical LLDP session or end-to-end G2 acceptance.
func TestWiringSchemaAndSubsetPredicatesPostgres(t *testing.T) {
	dsn := os.Getenv("KEEL_TEST_AGENT_DATABASE_URL")
	if dsn == "" {
		t.Skip("dedicated disposable database absent")
	}
	u, e := url.Parse(dsn)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Port() != "15433" || !strings.HasPrefix(u.Path, "/keel_test_agent_") {
		t.Fatal("refusing non-disposable database")
	}
	ctx := context.Background()
	s, e := Open(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.CheckMigrations(ctx); e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile("../../sim/fixtures/lldp/expectation.json")
	if e != nil {
		t.Fatal(e)
	}
	var full wiring.Expectation
	if e = json.Unmarshal(raw, &full); e != nil {
		t.Fatal(e)
	}
	selected := full
	selected.Ports = append([]wiring.ExpectedPort{}, full.Ports[1])
	for _, altered := range []bool{false, true} {
		if altered {
			selected.Ports[0].Rail = "unapproved-rail"
		}
		var allowed bool
		if e = s.DB.QueryRowContext(ctx, `SELECT keel_wiring_expectation_within($1::jsonb,$2::jsonb)`, networkJSON(full), networkJSON(selected)).Scan(&allowed); e != nil {
			t.Fatal(e)
		}
		if allowed == altered {
			t.Fatal("database did not preserve exact original port identities")
		}
	}
	// A narrowed port list may never replace the server's frozen diagnostic policy.
	full.HealthPolicy = &wiring.HealthPolicy{Version: wiring.HealthVersion}
	selected = full
	selected.Ports = append([]wiring.ExpectedPort{}, full.Ports[1])
	var allowed bool
	if e = s.DB.QueryRowContext(ctx, `SELECT keel_wiring_expectation_within($1::jsonb,$2::jsonb)`, networkJSON(full), networkJSON(selected)).Scan(&allowed); e != nil {
		t.Fatal(e)
	}
	if !allowed {
		t.Fatal("physical subset lost identical diagnostic policy")
	}
	selected.HealthPolicy = nil
	if e = s.DB.QueryRowContext(ctx, `SELECT keel_wiring_expectation_within($1::jsonb,$2::jsonb)`, networkJSON(full), networkJSON(selected)).Scan(&allowed); e != nil {
		t.Fatal(e)
	}
	if allowed {
		t.Fatal("partial rerun discarded diagnostic policy")
	}
	stamp := "2026-10-27T12:34:56.123456789Z"
	var stored time.Time
	if e = s.DB.QueryRowContext(ctx, `SELECT keel_wiring_original_timestamp($1)`, stamp).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if stored.Nanosecond() != 123456000 {
		t.Fatal("database rounded original nanoseconds instead of projecting binary microseconds")
	}
	var forbidden bool
	if e = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM keel_schema_migrations WHERE version LIKE '%026_fabric_precheck%')`).Scan(&forbidden); e != nil {
		t.Fatal(e)
	}
	if forbidden {
		t.Fatal("excluded original draft was migrated")
	}
}
