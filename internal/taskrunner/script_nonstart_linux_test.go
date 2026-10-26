package taskrunner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestAgentScriptNonstartProofRequiresExpiredAuthorityAndNoAcceptance(t *testing.T) {
	state := cacheDirectoryFixture(t)
	body := []byte("printf must-not-run\n")
	c := scriptTestCommand(body, "shell")
	w := Wrapper{StateDirectory: state}
	if _, err := w.Handle(context.Background(), Request{Action: "settle", Command: c}); !errors.Is(err, ErrRejected) {
		t.Fatal("unexpired authority settled", err)
	}
	if _, err := os.Stat(filepath.Join(state, c.CommandID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("live settlement created state", err)
	}
	c.AuthorizationExpiresAt = time.Now().Add(-time.Second).UnixMicro()
	a := Agent{StateDirectory: state, ProjectID: c.ProjectID, MachineID: c.MachineID}
	// Target script opt-out does not block proving an expired dispatch never ran.
	o, err := a.Handle(context.Background(), Request{Action: "settle", Command: c})
	if err != nil || o.Validate(c) != nil || o.State != "failed" || o.Output["process_started"] != false || o.Output["nonstart_proof"] != true {
		t.Fatal("expired missing dispatch not proved", o, err)
	}
	if _, err := os.Stat(filepath.Join(state, ".agent-fence.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("proof consumed Agent execution fence", err)
	}
	query, err := a.Handle(context.Background(), Request{Action: "query", Command: c})
	if err != nil || !query.FinishedAt.Equal(o.FinishedAt) {
		t.Fatal("proof not durable", query, err)
	}
	late, err := invokeScriptFixture(t, state, Request{Action: "run", Command: c, Script: body})
	if err != nil || late.State != "failed" || !late.FinishedAt.Equal(o.FinishedAt) {
		t.Fatal("late body run replaced original proof", late, err)
	}
}

func TestAgentScriptNonstartProofPreservesAcceptedRunningAndLockedUnknown(t *testing.T) {
	for _, stage := range []string{"accepted", "running", "locked", "corrupt"} {
		t.Run(stage, func(t *testing.T) {
			state := cacheDirectoryFixture(t)
			c := scriptTestCommand([]byte("exit 0\n"), "shell")
			c.AuthorizationExpiresAt = time.Now().Add(-time.Second).UnixMicro()
			dir := filepath.Join(state, c.CommandID)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if stage == "accepted" || stage == "running" {
				o := Observation{Command: c, State: stage, Output: map[string]any{}}
				if stage == "running" {
					o.StartedAt = time.Now().Add(-2 * time.Second)
				}
				if err := writeObservation(dir, o); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "corrupt" {
				if err := os.WriteFile(filepath.Join(dir, "receipt.json"), []byte("incomplete receipt"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "locked" {
				fd, err := unix.Open(filepath.Join(dir, "run.lock"), unix.O_CREAT|unix.O_WRONLY|unix.O_CLOEXEC, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer unix.Close(fd)
				if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			o, err := (Wrapper{StateDirectory: state}).Handle(context.Background(), Request{Action: "settle", Command: c})
			if stage == "accepted" || stage == "running" {
				if err != nil || o.State != stage || o.Terminal() {
					t.Fatal("acceptance/running inferred stopped", o, err)
				}
			} else if !errors.Is(err, ErrUnknown) {
				t.Fatal("ambiguous state inferred never started", o, err)
			}
		})
	}
}

func TestAgentScriptNonstartMetadataBoundary(t *testing.T) {
	c := scriptTestCommand([]byte("exit 0\n"), "shell")
	if (Request{Action: "settle", Command: c}).Validate() != nil {
		t.Fatal("valid script metadata rejected")
	}
	if (Request{Action: "settle", Command: c, Script: []byte("exit 0\n")}).Validate() == nil || (Request{Action: "settle", Command: c, UseCachedScript: true}).Validate() == nil {
		t.Fatal("non-start proof carried executable input")
	}
	c.Operation, c.ScriptMediaID, c.ScriptObjectVersion, c.ScriptSHA256, c.ScriptSize, c.ScriptInterpreter, c.AuthorizationExpiresAt = "host.identity", "", "", "", 0, "", 0
	if (Request{Action: "settle", Command: c}).Validate() == nil {
		t.Fatal("read-only task synthesized non-start proof")
	}
}
