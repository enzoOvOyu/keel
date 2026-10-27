package netbaseline

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Exact bounds and catalog-only versions are authored for the P2 milestone.
func TestNetworkSchemaRejectsRoundedAndUnboundedNumbers(t *testing.T) {
	low, high := json.Number("9007199254740993"), json.Number("9007199254740995")
	tpl := Template{Source: `{"asn":{{n|keeljson}}}`, Variables: map[string]Variable{"n": {Type: "integer", Required: true, Minimum: &low, Maximum: &high}}}
	for _, tc := range []struct {
		value   string
		allowed bool
	}{{"9007199254740992", false}, {"9007199254740993", true}, {"9007199254740995", true}, {"9007199254740996", false}} {
		_, e := tpl.Render(context.Background(), map[string]any{"n": json.Number(tc.value)}, nil)
		if (e == nil) != tc.allowed {
			t.Fatalf("exact value %s allowed=%v: %v", tc.value, tc.allowed, e)
		}
	}
	bomb := json.Number("1e999999999")
	tpl.Variables["n"] = Variable{Type: "number", Minimum: &bomb}
	if tpl.Validate() == nil {
		t.Fatal("unbounded numeric exponent accepted")
	}
	invalid := json.Number("1/2")
	tpl.Variables["n"] = Variable{Type: "number", Minimum: &invalid}
	if tpl.Validate() == nil {
		t.Fatal("non-JSON numeric bound accepted")
	}
	tpl.Variables["n"] = Variable{Type: "integer", Minimum: &high, Maximum: &low}
	if tpl.Validate() == nil {
		t.Fatal("inverted range accepted")
	}
}
func TestNetworkSchemaStringBoundsAndCatalogReferences(t *testing.T) {
	two := 2
	tpl := Template{Source: `{"description":{{label|keeljson}},"version":{{nos|keeljson}}}`, Variables: map[string]Variable{"label": {Type: "string", Required: true, MinLength: &two, MaxLength: &two, Pattern: `^[\p{Han}]+$`}, "nos": {Type: "version", Required: true, CatalogKey: "fabric.nos.version"}}}
	values := map[string]any{"label": "交换", "nos": map[string]any{"param_key": "fabric.nos.version", "value": "2026.1"}}
	if _, e := tpl.Render(context.Background(), values, nil); e == nil {
		t.Fatal("unresolved catalog reference accepted")
	}
	approved := map[string]string{"fabric.nos.version": "2026.1"}
	rendered, e := tpl.RenderWithCatalog(context.Background(), values, nil, approved)
	if e != nil || !strings.Contains(string(rendered), `"version":"2026.1"`) {
		t.Fatal(string(rendered), e)
	}
	for _, bad := range []any{"2026.1", map[string]any{"param_key": "other.version", "value": "2026.1"}, map[string]any{"param_key": "fabric.nos.version", "value": "unapproved"}, map[string]any{"param_key": "fabric.nos.version", "value": "2026.1", "file": "/tmp/bypass"}} {
		values["nos"] = bad
		if _, e = tpl.RenderWithCatalog(context.Background(), values, nil, approved); e == nil {
			t.Fatalf("catalog bypass accepted: %#v", bad)
		}
	}
	values["nos"] = map[string]any{"param_key": "fabric.nos.version", "value": "2026.1"}
	for _, bad := range []string{"交", "交换机", "ab"} {
		values["label"] = bad
		if _, e = tpl.RenderWithCatalog(context.Background(), values, nil, approved); e == nil {
			t.Fatalf("string constraint accepted: %s", bad)
		}
	}
	tpl.Variables["nos"] = Variable{Type: "version"}
	if tpl.Validate() == nil {
		t.Fatal("version without catalog key accepted")
	}
}
