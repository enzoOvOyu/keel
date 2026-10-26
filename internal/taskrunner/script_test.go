package taskrunner

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"
)

func scriptTestCommand(body []byte, interpreter string) TaskCommand {
	sum := sha256.Sum256(body)
	return TaskCommand{APIVersion: APIVersion, ProjectID: strings.Repeat("1", 32), MachineID: strings.Repeat("2", 32), CommandID: strings.Repeat("3", 32), AttemptID: strings.Repeat("4", 32), FencingToken: 1, Operation: "script.run", AuthorizationExpiresAt: time.Now().Add(time.Minute).UnixMicro(), TimeoutMS: 1000, ScriptMediaID: strings.Repeat("5", 32), ScriptObjectVersion: "fixture-object-v1", ScriptSHA256: hex.EncodeToString(sum[:]), ScriptSize: int64(len(body)), ScriptInterpreter: interpreter}
}

func TestScriptCommandBindsApprovedBytes(t *testing.T) {
	body := []byte("printf fixture")
	c := scriptTestCommand(body, "shell")
	if c.Validate() != nil || validateScriptBody(c, body) != nil {
		t.Fatal("valid bounded script rejected")
	}
	if validateScriptBody(c, []byte("printf foreign")) == nil {
		t.Fatal("changed bytes admitted")
	}
	for _, change := range []func(*TaskCommand){
		func(c *TaskCommand) { c.ScriptSize = MaxScriptBytes + 1 },
		func(c *TaskCommand) { c.AuthorizationExpiresAt = 0 },
		func(c *TaskCommand) { c.ScriptMediaID = "" },
		func(c *TaskCommand) { c.ScriptObjectVersion = "\n" },
		func(c *TaskCommand) { c.ScriptSHA256 = strings.Repeat("A", 64) },
		func(c *TaskCommand) { c.ScriptInterpreter = "/bin/sh -c" },
		func(c *TaskCommand) { c.WaitMS = 1 },
		func(c *TaskCommand) { c.PublicKey = "injected" },
		func(c *TaskCommand) { c.Operation = "host.identity" },
	} {
		bad := c
		change(&bad)
		if bad.Validate() == nil {
			t.Fatal("invalid script command admitted", bad)
		}
	}
	originalDigest := c.Digest()
	c.ScriptObjectVersion = "fixture-object-v2"
	if c.Digest() == originalDigest {
		t.Fatal("object version missing from command identity")
	}
}

func TestScriptReceiptRequiresStoppedProcessTree(t *testing.T) {
	now := time.Now().UTC()
	c := scriptTestCommand([]byte("exit 0"), "shell")
	o := Observation{Command: c, State: "success", StartedAt: now, FinishedAt: now, Output: map[string]any{}}
	if o.Validate(c) == nil {
		t.Fatal("success without stopped-process evidence accepted")
	}
	o.State = "failed"
	o.Output = map[string]any{"process_started": false, "process_tree_stopped": true, "reason": "script_process_not_started"}
	if o.Validate(c) != nil {
		t.Fatal("known pre-exec rejection did not settle")
	}
	o.State = "success"
	if o.Validate(c) == nil {
		t.Fatal("pre-exec rejection promoted to success")
	}
}

func TestScriptTransportRequestBodyBoundary(t *testing.T) {
	body := []byte("exit 0\n")
	command := scriptTestCommand(body, "shell")
	for _, action := range []string{"query", "cancel"} {
		if (Request{Action: action, Command: command}).Validate() != nil {
			t.Fatal("historical script request rejected", action)
		}
		if (Request{Action: action, Command: command, Script: body}).Validate() == nil {
			t.Fatal("non-run carried executable bytes", action)
		}
	}
	if (Request{Action: "run", Command: command, Script: body}).Validate() != nil {
		t.Fatal("approved bytes rejected")
	}
	if (Request{Action: "run", Command: command}).Validate() == nil {
		t.Fatal("missing script body accepted")
	}
	if (Request{Action: "run", Command: command, Script: []byte("exit 1\n")}).Validate() == nil {
		t.Fatal("mutated script body accepted")
	}
	command = agentCommand("d", 1)
	if (Request{Action: "run", Command: command, Script: body}).Validate() == nil {
		t.Fatal("read-only command carried script bytes")
	}
}
