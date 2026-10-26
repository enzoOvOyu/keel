package taskrunner

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

func TestScriptJSONResultContract(t *testing.T) {
	for _, raw := range []string{
		`{"ready":true,"count":3,"items":[null,"value"]}`,
		` { "large_integer": 9007199254740993 } `,
		`{"empty":{},"array":[]}`,
	} {
		got, err := normalizedScriptJSON([]byte(raw))
		if err != nil || !json.Valid([]byte(got)) {
			t.Fatal("valid structured result rejected", raw, got, err)
		}
		if strings.Contains(raw, "9007199254740993") && !strings.Contains(got, "9007199254740993") {
			t.Fatal("structured integer rounded")
		}
	}
	for _, raw := range []string{
		``, `null`, `[]`, `true`, `"object required"`, `{"key":1,"key":2}`,
		`{"key":1,"\u006bey":2}`, `{} {}`, `{}garbage`, `{"number":1e400}`,
		`{"nested":{"same":1,"same":2}}`, string([]byte{'{', '"', 'x', '"', ':', '"', 0xff, '"', '}'}),
		`{"text":"` + strings.Repeat("a", MaxScriptJSONBytes) + `"}`,
		`{"x":` + strings.Repeat("[", 10) + `0` + strings.Repeat("]", 10) + `}`,
		`{"values":[` + strings.Repeat(`0,`, 128) + `0]}`,
	} {
		if _, err := normalizedScriptJSON([]byte(raw)); err == nil {
			t.Fatal("invalid/unbounded JSON accepted", raw)
		}
	}
}

func TestScriptJSONReceiptBindsWholeStdout(t *testing.T) {
	raw := []byte("{\"ready\":true}\n")
	result, err := normalizedScriptJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	now := time.Now().UTC()
	c := scriptTestCommand([]byte("exit 0"), "shell")
	c.ScriptResultFormat = "json"
	o := Observation{Command: c, State: "success", StartedAt: now, FinishedAt: now, Output: map[string]any{
		"process_tree_stopped": true, "process_started": true, "exit_code": 0,
		"script_media_id": c.ScriptMediaID, "script_object_version": c.ScriptObjectVersion, "script_sha256": c.ScriptSHA256, "script_interpreter": c.ScriptInterpreter,
		"stdout_bytes": len(raw), "stdout_complete": true, "stdout_prefix_base64": base64.StdEncoding.EncodeToString(raw), "stdout_sha256": hex.EncodeToString(sum[:]),
		"stderr_bytes": 0, "stderr_complete": true, "stderr_prefix_base64": "", "stderr_sha256": strings.Repeat("0", 64),
		"structured_result_json": result, "structured_stdout_base64": base64.StdEncoding.EncodeToString(raw),
	}}
	if o.Validate(c) != nil {
		t.Fatal("valid JSON receipt rejected")
	}
	for key, changed := range map[string]any{"structured_result_json": `{"ready":false}`, "structured_stdout_base64": base64.StdEncoding.EncodeToString([]byte(`{"ready":false}`)), "stdout_bytes": len(raw) + 1, "stdout_complete": false, "stdout_prefix_base64": "", "stdout_sha256": strings.Repeat("0", 64)} {
		original := o.Output[key]
		o.Output[key] = changed
		if o.Validate(c) == nil {
			t.Fatal("unbound structured result accepted", key)
		}
		o.Output[key] = original
	}
	delete(o.Output, "structured_result_json")
	delete(o.Output, "structured_stdout_base64")
	if o.Validate(c) == nil {
		t.Fatal("JSON success without structured proof")
	}
	o.State = "failed"
	o.Output["reason"] = "script_json_invalid"
	if o.Validate(c) != nil {
		t.Fatal("known invalid JSON failure did not settle")
	}
}

func TestScriptJSONDedicatedProcess(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("helper intentionally rejects root")
	}
	for _, test := range []struct{ name, stdout, state string }{
		{"object", `{"ready":true,"counter":9007199254740993}`, "success"},
		{"duplicate", `{"same":1,"same":2}`, "failed"},
		{"extra-document", `{} {}`, "failed"},
		{"array", `[]`, "failed"},
		{"too-large", `{"text":"` + strings.Repeat("a", MaxScriptJSONBytes) + `"}`, "failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := t.TempDir()
			if err := os.Chmod(state, 0700); err != nil {
				t.Fatal(err)
			}
			body := []byte("printf '%s' '" + strings.ReplaceAll(test.stdout, "'", "'\"'\"'") + "'\n")
			c := scriptTestCommand(body, "shell")
			c.ScriptResultFormat = "json"
			got, err := invokeScriptFixture(t, state, Request{Action: "run", Command: c, Script: body})
			if err != nil || got.State != test.state {
				t.Fatal("structured script result", got, err)
			}
			if test.state == "failed" && got.Output["reason"] != "script_json_invalid" {
				t.Fatal("invalid JSON did not get explicit failure", got)
			}
			// Durable query must retain the exact original JSON contract and data.
			agent := Agent{StateDirectory: state, ProjectID: c.ProjectID, MachineID: c.MachineID}
			queried, err := agent.Handle(context.Background(), Request{Action: "query", Command: c})
			if err != nil || queried.Validate(c) != nil || queried.State != got.State || queried.Output["structured_result_json"] != got.Output["structured_result_json"] {
				t.Fatal("JSON receipt changed after query", queried, err)
			}
		})
	}
}

func TestScriptJSONCommandFreezesResultContract(t *testing.T) {
	command := scriptTestCommand([]byte("exit 0"), "shell")
	text := command.Digest()
	command.ScriptResultFormat = "json"
	if command.Validate() != nil || command.Digest() == text {
		t.Fatal("JSON contract missing from command identity")
	}
	command.ScriptResultFormat = "yaml"
	if command.Validate() == nil {
		t.Fatal("unsupported result contract admitted")
	}
	command = agentCommand("d", 1)
	command.ScriptResultFormat = "json"
	if command.Validate() == nil {
		t.Fatal("read-only command acquired a script output contract")
	}
}
