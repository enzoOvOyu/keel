package execution

import (
	"strings"
	"testing"
)

const workbenchYAML = `api_version: keel.sim/v1
inputs:
  profile:
    type: enum
    required: true
    enum: [smoke, full]
    default: smoke
tasks:
  - id: read
    type: sim.read
    retry: 0
    idempotent: true
    timeout_ms: 500
    resources: [machine]
    sim: {outcome: success, duration_ms: 50, seed: 42}
  - id: verify
    type: sim.read
    depends_on: [read]
    retry: 0
    idempotent: true
    timeout_ms: 500
    resources: [machine]
    sim: {outcome: success, duration_ms: 50, seed: 42}
`

func TestFlowWorkbenchUsesExecutableValidation(t *testing.T) {
	out, e := ValidateFlow(FlowValidationInput{YAMLSource: workbenchYAML})
	if e != nil {
		t.Fatal(e)
	}
	if strings.Join(out.TaskOrder, ",") != "read,verify" || out.Parameters["profile"] != "smoke" || out.ContentHash == "" {
		t.Fatalf("unexpected validation %+v", out)
	}
	for _, source := range []string{strings.Replace(workbenchYAML, "type: sim.read", "type: fabric.apply", 1), strings.Replace(workbenchYAML, "depends_on: [read]", "depends_on: [verify]", 1), workbenchYAML + "\n---\nfoo: bar"} {
		if _, e = ValidateFlow(FlowValidationInput{YAMLSource: source}); e == nil {
			t.Fatal("unsupported or unsafe workflow accepted")
		}
	}
	if _, e = ValidateFlow(FlowValidationInput{YAMLSource: workbenchYAML, Parameters: map[string]any{"profile": "unregistered"}}); e == nil {
		t.Fatal("enum bypass")
	}
}
func TestFlowWorkbenchDiffTracksTaskOrderingAndChanges(t *testing.T) {
	v, e := ValidateFlow(FlowValidationInput{YAMLSource: workbenchYAML})
	if e != nil {
		t.Fatal(e)
	}
	if len(DiffFlowDefinitions(v.Definition, v.Definition)) != 0 {
		t.Fatal("identical definitions differ")
	}
	after := v.Definition
	after.Tasks = append([]Task{}, after.Tasks...)
	after.Tasks[0].TimeoutMS = 600
	after.Tasks[0], after.Tasks[1] = after.Tasks[1], after.Tasks[0]
	diff := DiffFlowDefinitions(v.Definition, after)
	if len(diff) != 2 || diff[0].Path != "tasks/read" || diff[1].Path != "task_order" {
		t.Fatalf("diff %+v", diff)
	}
}

func TestFlowWorkbenchRejectsAliasesAndAcceptsUnfilledInputSchema(t *testing.T) {
	source := strings.Replace(workbenchYAML, "resources: [machine]", "resources: &shared [machine]", 1)
	source = strings.Replace(source, "resources: [machine]", "resources: *shared", 1)
	if _, err := ValidateFlow(FlowValidationInput{YAMLSource: source}); err == nil {
		t.Fatal("YAML alias accepted")
	}
	source = strings.Replace(workbenchYAML, "    default: smoke\n", "", 1)
	result, err := ValidateFlow(FlowValidationInput{YAMLSource: source})
	if err != nil {
		t.Fatal("unfilled required input must not prevent definition validation", err)
	}
	if _, err = result.Definition.Parameters(map[string]any{}, true); err == nil {
		t.Fatal("execution may not omit the required input")
	}
}
