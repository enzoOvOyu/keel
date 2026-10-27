package assetview

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBoundedJSONPathInventoryProjection(t *testing.T) {
	root, e := Decode([]byte(`{"resources":[{"data":{"BiosVersion":"fixture-v1","@odata.type":"#ComputerSystem.v1_0_0.ComputerSystem"}},{"data":{"BiosVersion":"fixture-v2"}}]}`))
	if e != nil {
		t.Fatal(e)
	}
	cases := map[string]int{`$.resources[*].data.BiosVersion`: 2, `$.resources[0].data["@odata.type"]`: 1, `$['resources'][1]['data'].BiosVersion`: 1, `$.resources[0].data.NotObserved`: 0}
	for path, count := range cases {
		values, e := Project(root, path)
		if e != nil || len(values) != count {
			t.Fatalf("%s: %+v %v", path, values, e)
		}
	}
	for _, path := range []string{`$..BiosVersion`, `$.resources[?(@.data)]`, `$.resources[0:20]`, `$.resources[-1]`, `$.resources[*].data.*`, `$[4096]`, `$['unclosed]`, `$.a` + strings.Repeat(".b", 16)} {
		if _, e := Project(root, path); e == nil {
			t.Fatal("unbounded/executable expression admitted", path)
		}
	}
	if _, e := Decode([]byte(`{} {}`)); e == nil {
		t.Fatal("trailing document admitted")
	}
	huge := map[string]any{"items": make([]any, 33)}
	if _, e := Project(huge, `$.items[*]`); e != ErrLimit {
		t.Fatal("partial wildcard values returned", e)
	}
	if _, e := Project(map[string]any{"large": strings.Repeat("x", 4097)}, `$.large`); e != ErrLimit {
		t.Fatal("unbounded cell admitted")
	}
}
func TestCatalogColorsDoNotInventCompliance(t *testing.T) {
	column := Column{ID: "bios", Title: "BIOS", Source: "redfish", Path: "$.bios", Color: &ColorRule{BaselineKey: "firmware.bios", Operator: "ne", Tone: "danger"}}
	baseline := &Baseline{ID: "catalog-id", Key: "firmware.bios", Version: "catalog-revision", Value: "v1", State: "active"}
	root := map[string]any{"bios": "v2"}
	mismatch := Evaluate(column, root, "current", baseline)
	if mismatch.Tone != "danger" || mismatch.Baseline.Version != baseline.Version {
		t.Fatal("catalog mismatch lost provenance", mismatch)
	}
	for _, state := range []string{"stale", "simulated", "partial", "source_unknown"} {
		if got := Evaluate(column, root, state, baseline); got.Tone != "unknown" {
			t.Fatal("untrusted source got comparison color", state, got)
		}
	}
	if got := Evaluate(column, root, "current", nil); got.Tone != "unknown" {
		t.Fatal("missing baseline guessed", got)
	}
	baseline.State = "retired"
	if got := Evaluate(column, root, "current", baseline); got.Tone != "unknown" {
		t.Fatal("retired baseline guessed", got)
	}
	baseline.State = "active"
	if got := Evaluate(column, map[string]any{}, "current", baseline); got.State != "missing" || len(got.Values) != 0 {
		t.Fatal("missing value fabricated", got)
	}
	column.Color.Operator = "eq"
	column.Color.Tone = "success"
	if got := Evaluate(column, map[string]any{"bios": "v1"}, "current", baseline); got.Tone != "success" {
		t.Fatal(got)
	}
}
func TestDefinitionAndGlobalOrdering(t *testing.T) {
	d := Definition{Columns: []Column{{ID: "count", Title: "Count", Source: "asset", Path: "$.count"}, {ID: "rack", Title: "Rack", Source: "asset", Path: "$.rack"}}, SortColumn: "count", SortDirection: "asc", GroupColumn: "rack"}
	if e := d.Validate(); e != nil {
		t.Fatal(e)
	}
	rows := []Row{{ID: "b", Kind: "server", Cells: map[string]Cell{"count": {Values: []any{json.Number("10")}}, "rack": {Values: []any{"r1"}}}}, {ID: "a", Kind: "server", Cells: map[string]Cell{"count": {Values: []any{json.Number("2")}}, "rack": {Values: []any{"r1"}}}}, {ID: "c", Kind: "switch", Cells: map[string]Cell{"count": {Values: []any{}}, "rack": {Values: []any{"r1"}}}}}
	Sort(rows, d)
	if rows[0].ID != "a" || rows[1].ID != "b" || rows[2].ID != "c" || rows[0].Group != "r1" {
		t.Fatal("numeric/global grouping wrong", rows)
	}
	d.SortDirection = "desc"
	Sort(rows, d)
	if rows[0].ID != "b" || rows[2].ID != "c" {
		t.Fatal("missing must remain last", rows)
	}
	d.Columns = append(d.Columns, d.Columns[0])
	if d.Validate() == nil {
		t.Fatal("duplicate column IDs admitted")
	}
}
