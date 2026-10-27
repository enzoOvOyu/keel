package netbaseline

import (
	"strings"
	"testing"
)

func TestTemplateScopeRejectsUnknownOrAmbiguousDimensions(t *testing.T) {
	id := strings.Repeat("a", 32)
	scope := TemplateScope{DeviceModelIDs: []string{id}, Roles: []string{"leaf"}, Domains: []string{"base", "bgp_evpn"}}
	if e := scope.Validate(); e != nil {
		t.Fatal(e)
	}
	if !scope.Matches(id, "leaf") || scope.Matches(id, "spine") || scope.Matches(strings.Repeat("b", 32), "leaf") {
		t.Fatal("model and role scope must both match")
	}
	invalid := []TemplateScope{{Roles: scope.Roles, Domains: scope.Domains}, {DeviceModelIDs: []string{id, id}, Roles: scope.Roles, Domains: scope.Domains}, {DeviceModelIDs: scope.DeviceModelIDs, Roles: []string{"leaf", "leaf"}, Domains: scope.Domains}, {DeviceModelIDs: scope.DeviceModelIDs, Roles: []string{"server"}, Domains: scope.Domains}, {DeviceModelIDs: scope.DeviceModelIDs, Roles: scope.Roles, Domains: []string{"shell"}}}
	for _, bad := range invalid {
		if e := bad.Validate(); e == nil {
			t.Fatalf("ambiguous scope accepted: %+v", bad)
		}
	}
	original := TemplateScope{DeviceModelIDs: []string{strings.Repeat("b", 32), id}, Roles: []string{"spine", "leaf"}, Domains: []string{"ports", "base"}}
	canonical := original.Canonical()
	if canonical.DeviceModelIDs[0] != id || canonical.Roles[0] != "leaf" || canonical.Domains[0] != "base" || original.Roles[0] != "spine" {
		t.Fatal("canonical scope modified caller or did not sort dimensions")
	}
}
func TestUnscopedTemplateSerializationPreservesExistingHash(t *testing.T) {
	old := struct {
		Source    string              `json:"source"`
		Variables map[string]Variable `json:"variables"`
	}{Source: `{"hostname":"leaf"}`, Variables: map[string]Variable{}}
	current := Template{Source: old.Source, Variables: old.Variables}
	if ContentHash(old) != ContentHash(current) {
		t.Fatal("adding optional scope rewrote immutable legacy definitions")
	}
	current.Scope = &TemplateScope{DeviceModelIDs: []string{strings.Repeat("a", 32)}, Roles: []string{"leaf"}, Domains: []string{"base"}}
	if ContentHash(old) == ContentHash(current) {
		t.Fatal("template scope omitted from release identity")
	}
}
