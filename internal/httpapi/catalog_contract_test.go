package httpapi

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

// These schemas are emitted by both the platform catalog and frozen execution
// views. Clients must see the same allowed rules and immutable artifact fields.
func TestSharedCatalogOpenAPISchemas(t *testing.T) {
	load := func(name string) map[string]any {
		t.Helper()
		content, err := os.ReadFile(filepath.Join("..", "..", "api", name))
		if err != nil {
			t.Fatal(err)
		}
		var document struct {
			Components struct {
				Schemas map[string]any `yaml:"schemas"`
			} `yaml:"components"`
		}
		if err = yaml.Unmarshal(content, &document); err != nil {
			t.Fatal(err)
		}
		return document.Components.Schemas
	}
	platform, execution := load("openapi.yaml"), load("execution.openapi.yaml")
	for _, name := range []string{"CompatibilityMatrix", "CatalogDependencies", "MediaArtifactDependency", "MediaDependencies", "CatalogMedia", "CatalogSelection"} {
		t.Run(name, func(t *testing.T) {
			if platform[name] == nil || execution[name] == nil {
				t.Fatalf("shared schema %s missing from an API contract", name)
			}
			if !reflect.DeepEqual(platform[name], execution[name]) {
				t.Errorf("shared schema %s differs between platform and execution OpenAPI; synchronize its fields, constraints and required keys", name)
			}
		})
	}
}
