package taskrunner

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// The subreaper must never run inside the test coordinator: it is scoped to
// this single dedicated helper subprocess, as in the installed SSH helper.
func TestScriptDedicatedHelper(t *testing.T) {
	if os.Getenv("KEEL_SCRIPT_FIXTURE_HELPER") != "1" {
		return
	}
	if PrepareScriptProcess() != nil {
		os.Exit(2)
	}
	if (Wrapper{StateDirectory: os.Getenv("KEEL_SCRIPT_FIXTURE_STATE"), ScriptProcessIsolation: true}).Serve(context.Background(), os.Stdin, os.Stdout) != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

func invokeScriptFixture(t *testing.T, state string, request Request) (Observation, error) {
	t.Helper()
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestScriptDedicatedHelper$")
	cmd.Env = []string{"KEEL_SCRIPT_FIXTURE_HELPER=1", "KEEL_SCRIPT_FIXTURE_STATE=" + state, "PATH=/usr/bin:/bin"}
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.Output()
	if err != nil {
		return Observation{}, err
	}
	var result Observation
	err = json.Unmarshal(out, &result)
	if err == nil {
		err = result.Validate(request.Command)
	}
	return result, err
}

func TestApprovedScriptDedicatedProcessProtocol(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("helper intentionally rejects root")
	}
	scenarios := []struct {
		name, body, interpreter, state string
		timeout                        int
	}{
		{"success", "printf fixture", "shell", "success", 1000},
		{"nonzero", "exit 7", "shell", "failed", 1000},
		{"timeout", "sleep 20 & wait", "shell", "failed", 100},
		{"python", "print('fixture')", "python", "success", 1000},
		{"output-limit", "import sys\nsys.stdout.write('x'*2000000)", "python", "failed", 1000},
		{"detached-descendant", "import os,time,sys\npid=os.fork()\nif pid==0:\n os.setsid(); time.sleep(20); os._exit(0)\nprint(pid,flush=True)\nsys.exit(0)", "python", "success", 1000},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			if scenario.interpreter == "python" {
				if _, err := os.Stat("/usr/bin/python3"); err != nil {
					t.Skip("fixed python interpreter absent")
				}
			}
			state := t.TempDir()
			if err := os.Chmod(state, 0700); err != nil {
				t.Fatal(err)
			}
			body := []byte(scenario.body)
			c := scriptTestCommand(body, scenario.interpreter)
			c.TimeoutMS = scenario.timeout
			result, err := invokeScriptFixture(t, state, Request{Action: "run", Command: c, Script: body})
			if err != nil || result.State != scenario.state || result.Output["process_tree_stopped"] != true {
				t.Fatalf("result=%+v error=%v", result, err)
			}
			if scenario.name == "detached-descendant" {
				prefix, _ := base64.StdEncoding.DecodeString(result.Output["stdout_prefix_base64"].(string))
				pid, err := strconv.Atoi(strings.TrimSpace(string(prefix)))
				if err != nil || unix.Kill(pid, 0) != unix.ESRCH {
					t.Fatal("escaped descendant remains alive", pid, err)
				}
			}
			replay, err := invokeScriptFixture(t, state, Request{Action: "run", Command: c, Script: body})
			first, _ := json.Marshal(result)
			second, _ := json.Marshal(replay)
			if err != nil || !bytes.Equal(first, second) {
				t.Fatal("run replay did not return original immutable receipt", err)
			}
			query, err := invokeScriptFixture(t, state, Request{Action: "query", Command: c})
			third, _ := json.Marshal(query)
			if err != nil || !bytes.Equal(first, third) {
				t.Fatal("query changed receipt", err)
			}
		})
	}
}

func TestScriptBodyMismatchHasNoDurableAcceptance(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("helper intentionally rejects root")
	}
	state := t.TempDir()
	os.Chmod(state, 0700)
	c := scriptTestCommand([]byte("printf expected"), "shell")
	if _, err := invokeScriptFixture(t, state, Request{Action: "run", Command: c, Script: []byte("printf foreign")}); err == nil {
		t.Fatal("changed body accepted")
	}
	if _, err := os.Stat(filepath.Join(state, c.CommandID)); !os.IsNotExist(err) {
		t.Fatal("rejected input created command state", err)
	}
}
