package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func agentFixture(t *testing.T) Agent {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("Agent intentionally rejects root")
	}
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	return Agent{StateDirectory: dir, ProjectID: strings.Repeat("a", 32), MachineID: strings.Repeat("b", 32)}
}
func agentCommand(id string, fence int64) TaskCommand {
	return TaskCommand{APIVersion: APIVersion, ProjectID: strings.Repeat("a", 32), MachineID: strings.Repeat("b", 32), CommandID: strings.Repeat(id, 32), AttemptID: strings.Repeat("c", 32), FencingToken: fence, Operation: "host.identity", TimeoutMS: 1000}
}
func awaitAgentReceipt(t *testing.T, path string) Observation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		o, e := readObservation(path)
		if e == nil && o.State == "running" {
			return o
		}
		if time.Now().After(deadline) {
			t.Fatal("Agent did not persist running receipt", e)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
func TestAgentLocalDurableIdentityAndFence(t *testing.T) {
	a := agentFixture(t)
	first := agentCommand("1", 10)
	original, e := a.Handle(context.Background(), Request{Action: "run", Command: first})
	if e != nil || original.State != "success" || original.Output["hostname"] == "" {
		t.Fatal(original, e)
	}
	raw, _ := json.Marshal(original)
	again, e := a.Handle(context.Background(), Request{Action: "run", Command: first})
	repeat, _ := json.Marshal(again)
	if e != nil || string(raw) != string(repeat) {
		t.Fatal("run retry changed durable receipt", e)
	}
	second := agentCommand("2", 20)
	if _, e = a.Handle(context.Background(), Request{Action: "run", Command: second}); e != nil {
		t.Fatal(e)
	}
	for _, command := range []TaskCommand{agentCommand("3", 19), agentCommand("4", 20)} {
		if _, e = a.Handle(context.Background(), Request{Action: "run", Command: command}); !errors.Is(e, ErrRejected) {
			t.Fatal("stale or reused fence admitted", e)
		}
	}
	// Reopening Agent after a later fence may still retrieve the exact old proof.
	reopened := Agent{StateDirectory: a.StateDirectory, ProjectID: a.ProjectID, MachineID: a.MachineID}
	for _, action := range []string{"query", "cancel", "run"} {
		o, e := reopened.Handle(context.Background(), Request{Action: action, Command: first})
		b, _ := json.Marshal(o)
		if e != nil || string(b) != string(raw) {
			t.Fatal("old receipt was lost or reexecuted", action, e)
		}
	}
	changed := first
	changed.TimeoutMS++
	if _, e = a.Handle(context.Background(), Request{Action: "query", Command: changed}); !errors.Is(e, ErrRejected) {
		t.Fatal("changed command matched receipt", e)
	}
	// Even deliberate loss of old local proof cannot authorize first execution
	// with a fence lower than a newer command's durable high-water mark.
	receipt := filepath.Join(a.StateDirectory, first.CommandID, "receipt.json")
	if e = os.Remove(receipt); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Handle(context.Background(), Request{Action: "run", Command: first}); !errors.Is(e, ErrRejected) {
		t.Fatal("lost receipt let stale command execute", e)
	}
	if _, e = os.Stat(receipt); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("stale command recreated lost receipt", e)
	}
	latest := filepath.Join(a.StateDirectory, second.CommandID, "receipt.json")
	if e = os.Remove(latest); e != nil {
		t.Fatal(e)
	}
	if _, e = a.Handle(context.Background(), Request{Action: "run", Command: second}); !errors.Is(e, ErrUnknown) {
		t.Fatal("matching fence retried missing latest proof", e)
	}
	if _, e = os.Stat(latest); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("matching fence recreated lost receipt", e)
	}
}
func TestAgentLocalBindingAndAllowlist(t *testing.T) {
	a := agentFixture(t)
	valid := agentCommand("1", 1)
	wrongProject := valid
	wrongProject.ProjectID = strings.Repeat("d", 32)
	wrongMachine := valid
	wrongMachine.MachineID = strings.Repeat("d", 32)
	trust := valid
	trust.Operation = "trust.verify"
	shell := valid
	shell.Operation = "shell"
	for _, command := range []TaskCommand{wrongProject, wrongMachine, trust, shell} {
		if _, e := a.Handle(context.Background(), Request{Action: "run", Command: command}); !errors.Is(e, ErrRejected) {
			t.Fatal("forbidden command admitted", e)
		}
	}
	files, e := os.ReadDir(a.StateDirectory)
	if e != nil || len(files) != 0 {
		t.Fatal("rejected request touched durable state", e)
	}
}
func TestAgentLocalConcurrentRunCancellationAndContextStop(t *testing.T) {
	a := agentFixture(t)
	command := agentCommand("1", 1)
	command.Operation = "host.wait"
	command.WaitMS = 1000
	command.TimeoutMS = 2000
	type result struct {
		o Observation
		e error
	}
	done := make(chan result, 1)
	go func() {
		o, e := a.Handle(context.Background(), Request{Action: "run", Command: command})
		done <- result{o, e}
	}()
	awaitAgentReceipt(t, filepath.Join(a.StateDirectory, command.CommandID, "receipt.json"))
	if _, e := a.Handle(context.Background(), Request{Action: "run", Command: agentCommand("2", 2)}); !errors.Is(e, ErrUnknown) {
		t.Fatal("concurrent machine operation admitted", e)
	}
	if _, e := a.Handle(context.Background(), Request{Action: "cancel", Command: command}); e != nil {
		t.Fatal(e)
	}
	completed := <-done
	if completed.e != nil || completed.o.State != "cancelled" {
		t.Fatal("cooperative cancellation did not persist stop proof", completed)
	}
	work, cancel := context.WithCancel(context.Background())
	defer cancel()
	command.CommandID = strings.Repeat("3", 32)
	command.FencingToken = 3
	go func() { o, e := a.Handle(work, Request{Action: "run", Command: command}); done <- result{o, e} }()
	awaitAgentReceipt(t, filepath.Join(a.StateDirectory, command.CommandID, "receipt.json"))
	cancel()
	completed = <-done
	if completed.e != nil || completed.o.State != "failed" || completed.o.Output["reason"] != "deadline_or_process_stop_confirmed" {
		t.Fatal("stream cancellation had no durable terminal proof", completed)
	}
	o, e := a.Handle(context.Background(), Request{Action: "query", Command: command})
	if e != nil || o.FinishedAt != completed.o.FinishedAt {
		t.Fatal("terminal proof not persistent", e)
	}
}
func TestAgentLocalCrashNeverReruns(t *testing.T) {
	const child = "KEEL_AGENT_LOCAL_CRASH_CHILD"
	if path := os.Getenv(child); path != "" {
		a := Agent{StateDirectory: path, ProjectID: strings.Repeat("a", 32), MachineID: strings.Repeat("b", 32)}
		command := agentCommand("5", 5)
		command.Operation = "host.wait"
		command.WaitMS = 30000
		command.TimeoutMS = 30000
		_, e := a.Handle(context.Background(), Request{Action: "run", Command: command})
		if e != nil {
			os.Exit(4)
		}
		os.Exit(0)
	}
	a := agentFixture(t)
	binary, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	process := exec.Command(binary, "-test.run=^TestAgentLocalCrashNeverReruns$")
	process.Env = append(os.Environ(), child+"="+a.StateDirectory)
	if e = process.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if process.ProcessState == nil {
			_ = process.Process.Kill()
			_ = process.Wait()
		}
	}()
	command := agentCommand("5", 5)
	command.Operation = "host.wait"
	command.WaitMS = 30000
	command.TimeoutMS = 30000
	before := awaitAgentReceipt(t, filepath.Join(a.StateDirectory, command.CommandID, "receipt.json"))
	if _, err := a.Handle(context.Background(), Request{Action: "run", Command: agentCommand("6", 6)}); !errors.Is(err, ErrUnknown) {
		t.Fatal("second process bypassed global run lock", err)
	}
	if e = process.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	_ = process.Wait()
	reopened := Agent{StateDirectory: a.StateDirectory, ProjectID: a.ProjectID, MachineID: a.MachineID}
	started := time.Now()
	after, e := reopened.Handle(context.Background(), Request{Action: "run", Command: command})
	if e != nil || after.State != "running" || after.StartedAt != before.StartedAt || time.Since(started) > time.Second {
		t.Fatal("crashed accepted operation restarted or manufactured completion", e)
	}
}
func createAgentFIFO(path string) error { return unix.Mkfifo(path, 0600) }

func TestAgentLocalProtectedPaths(t *testing.T) {
	t.Run("ancestor_link", func(t *testing.T) {
		a := agentFixture(t)
		parent := t.TempDir()
		link := filepath.Join(parent, "alias")
		if e := os.Symlink(a.StateDirectory, link); e != nil {
			t.Fatal(e)
		}
		a.StateDirectory = link
		if _, e := a.Handle(context.Background(), Request{Action: "run", Command: agentCommand("1", 1)}); !errors.Is(e, ErrRejected) {
			t.Fatal("linked state accepted", e)
		}
		if _, e := PrepareAgentState(link); e == nil {
			t.Fatal("linked parent prepared")
		}
		if _, e := os.Stat(filepath.Join(a.StateDirectory, "tasks")); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("link followed during state preparation", e)
		}
	})
	for _, kind := range []string{"symlink", "hardlink", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			a := agentFixture(t)
			command := agentCommand("1", 1)
			dir := filepath.Join(a.StateDirectory, command.CommandID)
			if e := os.Mkdir(dir, 0700); e != nil {
				t.Fatal(e)
			}
			target := filepath.Join(a.StateDirectory, "external")
			if e := os.WriteFile(target, []byte("preserve"), 0600); e != nil {
				t.Fatal(e)
			}
			receipt := filepath.Join(dir, "receipt.json")
			var e error
			switch kind {
			case "symlink":
				e = os.Symlink(target, receipt)
			case "hardlink":
				e = os.Link(target, receipt)
			case "fifo":
				e = createAgentFIFO(receipt)
			}
			if e != nil {
				t.Fatal(e)
			}
			start := time.Now()
			if _, e = a.Handle(context.Background(), Request{Action: "run", Command: command}); !errors.Is(e, ErrRejected) || time.Since(start) > time.Second {
				t.Fatal("unsafe receipt path accepted or blocked", e)
			}
			raw, _ := os.ReadFile(target)
			if string(raw) != "preserve" {
				t.Fatal("external file changed")
			}
		})
	}
	t.Run("corrupted_fence", func(t *testing.T) {
		a := agentFixture(t)
		if e := os.WriteFile(filepath.Join(a.StateDirectory, ".agent-fence.json"), []byte(`{"broken":true}`), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e := a.Handle(context.Background(), Request{Action: "run", Command: agentCommand("1", 1)}); !errors.Is(e, ErrUnknown) {
			t.Fatal("corrupt fence reset", e)
		}
	})
}

func TestAgentLocalPrepareAndAncestorCustody(t *testing.T) {
	a := agentFixture(t)
	path, e := PrepareAgentState(a.StateDirectory)
	if e != nil {
		t.Fatal(e)
	}
	if again, e := PrepareAgentState(a.StateDirectory); e != nil || again != path {
		t.Fatal("private tasks directory not reusable", e)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0700 {
		t.Fatal("task directory mode", e)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if e = os.Symlink(a.StateDirectory, alias); e != nil {
		t.Fatal(e)
	}
	a.StateDirectory = filepath.Join(alias, "tasks")
	if _, e = a.Handle(context.Background(), Request{Action: "run", Command: agentCommand("1", 1)}); !errors.Is(e, ErrRejected) {
		t.Fatal("ancestor symlink was followed", e)
	}
	if files, e := os.ReadDir(path); e != nil || len(files) != 0 {
		t.Fatal("ancestor link caused write", e)
	}
}

func TestAgentScriptOptOutAndHistoricalQueries(t *testing.T) {
	a := agentFixture(t)
	body := []byte("exit 0\n")
	command := scriptTestCommand(body, "shell")
	command.ProjectID, command.MachineID = a.ProjectID, a.MachineID
	if _, err := a.Handle(context.Background(), Request{Action: "run", Command: command, Script: body}); !errors.Is(err, ErrRejected) {
		t.Fatal("default-off Agent script admitted", err)
	}
	if _, err := os.Lstat(filepath.Join(a.StateDirectory, ".agent-fence.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("rejected script consumed Agent fence", err)
	}
	// Persist a running proof as if a dedicated helper disconnected. Opting out
	// must leave that proof queryable and must never restart the script.
	dir := filepath.Join(a.StateDirectory, command.CommandID)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	observation := Observation{Command: command, State: "running", StartedAt: now, Output: map[string]any{}}
	raw, err := json.Marshal(observation)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "receipt.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	got, err := a.Handle(context.Background(), Request{Action: "query", Command: command})
	if err != nil || got.State != "running" || got.Command != command {
		t.Fatal("opt-out hid original script receipt", got, err)
	}
	a.ScriptsEnabled = true
	got, err = a.Handle(context.Background(), Request{Action: "run", Command: command, Script: body})
	if err != nil || got.State != "running" {
		t.Fatal("existing script proof caused helper replay", got, err)
	}
}
