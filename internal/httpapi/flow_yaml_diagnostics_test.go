package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/execution"
	"github.com/gin-gonic/gin"
)

func TestWorkflowYAMLDiagnosticHTTPDoesNotReflectParserText(t *testing.T) {
	_, err := execution.ValidateFlow(execution.FlowValidationInput{YAMLSource: "api_version: keel.host/v1\ninputs: {}\ntasks: customer-secret-not-a-task-array\n"})
	if err == nil {
		t.Fatal("invalid YAML accepted")
	}
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Set("trace_id", "yaml-diagnostic-fixture")
	(&authAPI{}).respondError(ctx, err)
	var body struct {
		Code    string   `json:"code"`
		Details []string `json:"details"`
		Trace   string   `json:"trace_id"`
	}
	if response.Code != 400 || json.Unmarshal(response.Body.Bytes(), &body) != nil || body.Code != "VALIDATION_ERROR" || len(body.Details) != 1 || body.Details[0] != "workflow_yaml_line:3" || body.Trace != "yaml-diagnostic-fixture" {
		t.Fatal("structured coordinate missing", response.Body.String())
	}
	if strings.Contains(response.Body.String(), "customer-secret") {
		t.Fatal("parser text exposed")
	}
}
