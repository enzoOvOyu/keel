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

// Type generation does not reject a misspelled named authentication scheme.
// Keep operation security references tied to the actual declared cookie schemes.
func TestOpenAPISecuritySchemeReferences(t *testing.T) {
	for _, name := range []string{"openapi.yaml", "execution.openapi.yaml"} {
		t.Run(name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("..", "..", "api", name))
			if err != nil {
				t.Fatal(err)
			}
			var document struct {
				Components struct {
					SecuritySchemes map[string]any `yaml:"securitySchemes"`
				} `yaml:"components"`
				Paths map[string]map[string]yaml.Node `yaml:"paths"`
			}
			if err = yaml.Unmarshal(content, &document); err != nil {
				t.Fatal(err)
			}
			methods := map[string]bool{"get": true, "put": true, "post": true, "delete": true, "options": true, "head": true, "patch": true, "trace": true}
			for path, items := range document.Paths {
				for method, node := range items {
					if !methods[method] {
						continue
					}
					var operation struct {
						Security []map[string][]string `yaml:"security"`
					}
					if err = node.Decode(&operation); err != nil {
						t.Fatalf("%s %s: %v", method, path, err)
					}
					for _, requirement := range operation.Security {
						for scheme := range requirement {
							if document.Components.SecuritySchemes[scheme] == nil {
								t.Errorf("%s %s references undeclared security scheme %s", method, path, scheme)
							}
						}
					}
				}
			}
		})
	}
}
