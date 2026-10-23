package netbaseline

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNetworkSandboxAndJSONBinding(t *testing.T) {
	template := Template{Source: `{"hostname":{{ device.hostname|keeljson }},"asn":{{ asn|keeljson }},"description":{{ description|keeljson }}}`, Variables: map[string]Variable{"asn": {Type: "integer", Required: true}, "description": {Type: "string"}}}
	rendered, e := template.Render(context.Background(), map[string]any{"asn": 4200000001, "description": "quoted \" data </script>"}, map[string]any{"hostname": "leaf-01"})
	if e != nil {
		t.Fatal(e)
	}
	var object map[string]any
	if json.Unmarshal(rendered, &object) != nil || object["hostname"] != "leaf-01" || object["description"] != "quoted \" data </script>" {
		t.Fatal("unsafe or incorrect JSON binding")
	}
	if _, e = template.Render(context.Background(), map[string]any{}, nil); e == nil {
		t.Fatal("missing required ASN accepted")
	}
	for _, source := range []string{`{% include "/etc/passwd" %}`, `{% extends "/etc/passwd" %}`, `{{ value|ljust:999999999 }}`, `{% macro recurse() %}{% endmacro %}`} {
		if e = (Template{Source: source}).Validate(); e == nil {
			t.Fatal("unsafe template accepted", source)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = template.Render(ctx, nil, nil); e == nil {
		t.Fatal("canceled render accepted")
	}
	nested := Template{Source: `{% for a in values %}{% for b in values %}{% for c in values %}x{% endfor %}{% endfor %}{% endfor %}`}
	if nested.Validate() == nil {
		t.Fatal("unbounded nested loops accepted")
	}
}
func TestStructuredNetworkDiff(t *testing.T) {
	before := json.RawMessage(`{"qos":{"pfc":true},"description":"old","interfaces":{"swp1":{"mtu":1500}},"nullable":null}`)
	after := json.RawMessage(`{"qos":{"pfc":false},"description":"new","interfaces":{"swp1":{"mtu":9216}}}`)
	differences, e := StructuredDiff(before, after)
	if e != nil || len(differences) != 4 {
		t.Fatal(differences, e)
	}
	seen := map[string]Difference{}
	for _, d := range differences {
		seen[d.Path] = d
	}
	if seen["/qos/pfc"].Severity != "Critical" || seen["/interfaces/swp1/mtu"].Severity != "Major" || seen["/description"].Severity != "Minor" || seen["/nullable"].AfterPresent {
		t.Fatal("incorrect structural/presence grading", differences)
	}
	if _, e = StructuredDiff(json.RawMessage(`{} {}`), json.RawMessage(`{}`)); e == nil {
		t.Fatal("trailing JSON accepted")
	}
}
