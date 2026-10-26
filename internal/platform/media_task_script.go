package platform

import (
	"encoding/json"

	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

// Script execution metadata is independent of manifest_ref, which identifies
// an upload digest or an external source URL and is never an executable flag.
type MediaTaskScriptProfile struct {
	Version     string `json:"version"`
	Interpreter string `json:"interpreter"`
}

func (p *MediaTaskScriptProfile) Validate() error {
	if p == nil {
		return nil
	}
	if p.Version != "keel.task-script/v1" || (p.Interpreter != "shell" && p.Interpreter != "python") {
		return apiError(400, "TASK_SCRIPT_PROFILE_INVALID", "Task script profile requires keel.task-script/v1 and a fixed shell or python interpreter")
	}
	return nil
}

func validateTaskScriptMediaInput(input RegisterMediaInput) error {
	raw, err := json.Marshal(input.Deps)
	if err != nil {
		return apiError(400, "VALIDATION_ERROR", "Invalid media dependencies")
	}
	var deps catalogDependencies
	if json.Unmarshal(raw, &deps) != nil {
		return apiError(400, "VALIDATION_ERROR", "Invalid media dependencies")
	}
	if value, exists := input.Deps["task_script"]; exists && value == nil {
		return apiError(400, "TASK_SCRIPT_PROFILE_INVALID", "Task script profile cannot be null")
	}
	if deps.TaskScript == nil {
		return nil
	}
	if err = deps.TaskScript.Validate(); err != nil {
		return err
	}
	if input.Type != "tool" || input.Arch != "noarch" || input.OsScope != "linux" || input.HwScope != "" || input.Size < 1 || input.Size > taskrunner.MaxScriptBytes || len(deps.Artifacts) > 0 || deps.Compatibility != nil || deps.OSBoot != nil {
		return apiError(400, "TASK_SCRIPT_PROFILE_SCOPE", "Task scripts require a 1..8192 byte noarch Linux tool without hardware scope, dependencies, compatibility matrices or boot roles")
	}
	return nil
}
