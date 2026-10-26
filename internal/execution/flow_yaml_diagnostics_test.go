package execution

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
)

func TestFlowYAMLSourceCoordinatesWithoutReflectingContent(t *testing.T) {
	const secret = "private_customer_password_should_not_echo"
	cases := []struct{ name, source string }{
		{"unknown field", strings.Replace(workbenchYAML, "    type: sim.read", "    "+secret+": value\n    type: sim.read", 1)},
		{"wrong scalar type", strings.Replace(workbenchYAML, "timeout_ms: 500", "timeout_ms: "+secret, 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateFlow(FlowValidationInput{YAMLSource: tc.source})
			var problem *platform.Error
			if !errors.As(err, &problem) || problem.Status != 400 || problem.Code != "VALIDATION_ERROR" || len(problem.Details) != 1 {
				t.Fatal("source coordinate missing", err)
			}
			// Locate the physical source line independently; edits above are not fixed
			// to the fixture's current blank-line layout.
			want := 0
			for i, line := range strings.Split(tc.source, "\n") {
				if strings.Contains(line, secret) {
					want = i + 1
					break
				}
			}
			if problem.Details[0] != "workflow_yaml_line:"+strconv.Itoa(want) {
				t.Fatal("wrong coordinate", problem.Details, want)
			}
			if strings.Contains(problem.Message, secret) || strings.Contains(strings.Join(problem.Details, " "), secret) {
				t.Fatal("YAML parser content reflected")
			}
		})
	}
	alias := strings.Replace(workbenchYAML, "resources: [machine]", "resources: &owned [machine]", 1)
	alias = strings.Replace(alias, "resources: [machine]", "resources: *owned", 1)
	for _, source := range []string{alias, workbenchYAML + "\n---\nprivate: value\n", strings.Replace(workbenchYAML, "tasks:", "tasks: [\n", 1)} {
		_, err := ValidateFlow(FlowValidationInput{YAMLSource: source})
		var problem *platform.Error
		if !errors.As(err, &problem) || len(problem.Details) != 1 {
			t.Fatal("malformed YAML has no bounded coordinate", err)
		}
	}
	// Ordinary semantic errors still use stable domain messages; unrelated
	// JSON errors must not acquire fabricated YAML coordinates.
	if _, err := (PublishInput{Definition: &Definition{APIVersion: "unknown"}}).Parse(); err == nil {
		t.Fatal("invalid definition accepted")
	}
}
