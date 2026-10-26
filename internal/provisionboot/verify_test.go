package provisionboot

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBootFilesVerifyExactFrozenInputs(t *testing.T) {
	scope, g, plan, spec, seed, now := renderFixture()
	bundle, err := RenderBootBundle(scope, g, plan, spec, seed, now)
	if err != nil {
		t.Fatal(err)
	}
	manifest, preflight, err := VerifyBootFiles(bundle.Manifest, bytesDigest(bundle.Manifest), bundle.Preflight, seed, now)
	if err != nil || manifest.GrantID != g.ID || preflight.FencingToken != g.FencingToken || preflight.Expected.Serial != spec.Expected.Serial {
		t.Fatalf("unbound boot files: %+v %+v %v", manifest, preflight, err)
	}
	if _, _, err := VerifyBootFiles(bundle.Manifest, bytesDigest(bundle.Manifest), bundle.Preflight, seed, g.ExpiresAt.Add(time.Second)); err == nil {
		t.Fatal("expired public files admitted")
	}
	if _, _, err := VerifyBootFiles(bundle.Manifest, strings.Repeat("f", 64), bundle.Preflight, seed, now); err == nil {
		t.Fatal("manifest supplied its own trust anchor")
	}
	for _, field := range []string{"user-data", "meta-data", "preflight"} {
		t.Run(field, func(t *testing.T) {
			copy := seed
			rawSpec := bundle.Preflight
			switch field {
			case "user-data":
				copy.UserData += "#changed"
			case "meta-data":
				copy.MetaData += "#changed"
			case "preflight":
				rawSpec = append(append([]byte{}, rawSpec...), ' ')
			}
			if _, _, err := VerifyBootFiles(bundle.Manifest, bytesDigest(bundle.Manifest), rawSpec, copy, now); err == nil {
				t.Fatal("modified frozen bytes accepted")
			}
		})
	}
}
func TestBootFilesRejectInternallyInconsistentOrAmbiguousManifest(t *testing.T) {
	for name, edit := range map[string]func(*BootManifest){
		"changed_command":          func(m *BootManifest) { m.CommandID = strings.Repeat("f", 32) },
		"changed_fence":            func(m *BootManifest) { m.FencingToken++ },
		"changed_MAC":              func(m *BootManifest) { m.MAC = "02:00:00:00:00:01" },
		"missing_recipe":           func(m *BootManifest) { m.Media.Profile.Recipe = nil },
		"foreign_project_artifact": func(m *BootManifest) { m.Media.Artifacts[0].ScopeKey = "project:" + strings.Repeat("f", 32) },
		"changed_helper_hash":      func(m *BootManifest) { m.Media.Profile.Recipe.SSHHelperSHA256 = strings.Repeat("b", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			scope, g, plan, spec, seed, now := renderFixture()
			b, err := RenderBootBundle(scope, g, plan, spec, seed, now)
			if err != nil {
				t.Fatal(err)
			}
			var m BootManifest
			json.Unmarshal(b.Manifest, &m)
			edit(&m)
			raw, _ := json.Marshal(m)
			if _, _, err := VerifyBootFiles(raw, bytesDigest(raw), b.Preflight, seed, now); err == nil {
				t.Fatal("internal binding was not checked")
			}
		})
	}
	scope, g, plan, spec, seed, now := renderFixture()
	b, _ := RenderBootBundle(scope, g, plan, spec, seed, now)
	for _, raw := range [][]byte{append(append([]byte{}, b.Manifest...), b.Manifest...), []byte(strings.Replace(string(b.Manifest), `"schema":`, `"Schema":`, 1)), []byte(strings.Replace(string(b.Manifest), `"schema":`, `"unknown":true,"schema":`, 1)), []byte(strings.Replace(string(b.Manifest), `"schema":`, `"schema":"ignored","schema":`, 1))} {
		if _, _, err := VerifyBootFiles(raw, bytesDigest(raw), b.Preflight, seed, now); err == nil {
			t.Fatal("ambiguous/unknown manifest accepted")
		}
	}
}
