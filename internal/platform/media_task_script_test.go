package platform

import (
	"strings"
	"testing"
)

func TestTaskScriptMediaRegistrationProfile(t *testing.T) {
	valid := func() RegisterMediaInput {
		return RegisterMediaInput{Type: "tool", Name: "approved-script", Version: "v1", Sha256: strings.Repeat("a", 64), Size: 10, Arch: "noarch", OsScope: "linux", ManifestRef: "https://media.example.invalid/script", Deps: map[string]any{"task_script": MediaTaskScriptProfile{Version: "keel.task-script/v1", Interpreter: "shell"}}}
	}
	if err := validateMediaInput(valid()); err != nil {
		t.Fatal("valid script profile rejected", err)
	}
	for _, change := range []func(*RegisterMediaInput){
		func(in *RegisterMediaInput) { in.Type = "driver" },
		func(in *RegisterMediaInput) { in.Arch = "amd64" },
		func(in *RegisterMediaInput) { in.OsScope = "other" },
		func(in *RegisterMediaInput) { in.HwScope = "model-scope" },
		func(in *RegisterMediaInput) { in.Size = 0 },
		func(in *RegisterMediaInput) { in.Size = 8193 },
		func(in *RegisterMediaInput) { in.Deps["task_script"] = nil },
		func(in *RegisterMediaInput) {
			in.Deps["task_script"] = map[string]any{"version": "keel.task-script/v1", "interpreter": "/bin/sh"}
		},
		func(in *RegisterMediaInput) {
			in.Deps["task_script"] = map[string]any{"version": "keel.task-script/v2", "interpreter": "shell"}
		},
		func(in *RegisterMediaInput) {
			in.Deps["task_script"] = map[string]any{"version": "keel.task-script/v1", "interpreter": "shell", "arguments": []string{"-c"}}
		},
		func(in *RegisterMediaInput) {
			in.Deps["artifacts"] = []map[string]any{{"media_id": strings.Repeat("b", 32), "sha256": strings.Repeat("c", 64)}}
		},
		func(in *RegisterMediaInput) { in.Deps["compatibility"] = map[string]any{} },
		func(in *RegisterMediaInput) { in.Deps["os_boot"] = map[string]any{} },
	} {
		in := valid()
		change(&in)
		if validateMediaInput(in) == nil {
			t.Fatal("invalid/inapplicable executable profile accepted", in)
		}
	}
	// A source/manifest string by itself never creates executable authority.
	legacy := valid()
	legacy.ManifestRef = "keel.task-script/shell/v1"
	legacy.Deps = map[string]any{}
	if err := validateMediaInput(legacy); err != nil {
		t.Fatal("ordinary non-executable manifest rejected", err)
	}
}
