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

func TestArtifactNonstartUsesOriginalLockWithoutCacheOrFence(t *testing.T) {
	for _, operation := range []string{"media.stage", "media.fetch"} {
		t.Run(operation, func(t *testing.T) {
			state := cacheDirectoryFixture(t)
			c := artifactTestCommand([]byte("never-transferred"), "d", 1)
			c.Operation = operation
			a := Agent{StateDirectory: state, ProjectID: c.ProjectID, MachineID: c.MachineID}
			if _, err := a.Handle(context.Background(), Request{Action: "settle", Command: c}); !errors.Is(err, ErrRejected) {
				t.Fatal("live authority settled", err)
			}
			if _, err := os.Stat(filepath.Join(state, c.CommandID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("live proof created state", err)
			}
			c.AuthorizationExpiresAt = time.Now().Add(-time.Second).UnixMicro()
			o, err := a.Handle(context.Background(), Request{Action: "settle", Command: c})
			if err != nil || o.Validate(c) != nil || o.State != "failed" || o.Output["nonstart_proof"] != true || o.Output["content_verified"] != false || o.Output["reason"] != "artifact_authorization_expired_before_start" {
				t.Fatal("no durable non-start proof", o, err)
			}
			if operation == "media.stage" && o.Output["writer_stopped"] != true || operation == "media.fetch" && o.Output["reader_stopped"] != true {
				t.Fatal("wrong transfer direction proof", o)
			}
			if _, err := os.Stat(filepath.Join(state, ".agent-fence.json")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("proof consumed Agent fence", err)
			}
			historical, err := a.Handle(context.Background(), Request{Action: "query", Command: c})
			if err != nil || !historical.FinishedAt.Equal(o.FinishedAt) {
				t.Fatal("proof not durable", err)
			}
			if late, err := a.Handle(context.Background(), Request{Action: "run", Command: c}); err == nil && late.State != "failed" {
				t.Fatal("late command restarted", late)
			}
		})
	}
}
func TestArtifactNonstartCannotOverrideAcceptedOrUncertainState(t *testing.T) {
	for _, status := range []string{"accepted", "running", "locked", "corrupt"} {
		t.Run(status, func(t *testing.T) {
			state := cacheDirectoryFixture(t)
			c := artifactTestCommand([]byte("ambiguous"), "d", 1)
			c.AuthorizationExpiresAt = time.Now().Add(-time.Second).UnixMicro()
			dir := filepath.Join(state, c.CommandID)
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if status == "accepted" || status == "running" {
				if err := writeObservation(dir, Observation{Command: c, State: status, Output: map[string]any{}}); err != nil {
					t.Fatal(err)
				}
			}
			if status == "corrupt" {
				if err := os.WriteFile(filepath.Join(dir, "receipt.json"), []byte("not-json"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if status == "locked" {
				root, err := agentDirectory(dir)
				if err != nil {
					t.Fatal(err)
				}
				defer unix.Close(root)
				lock, err := agentFile(root, "run.lock", true)
				if err != nil {
					t.Fatal(err)
				}
				defer unix.Close(lock)
				if unix.Flock(lock, unix.LOCK_EX|unix.LOCK_NB) != nil {
					t.Fatal("fixture lock failed")
				}
				defer unix.Flock(lock, unix.LOCK_UN)
			}
			o, err := settleArtifactNonstart(context.Background(), state, Request{Action: "settle", Command: c})
			if err == nil && o.Terminal() {
				t.Fatal("uncertain state became non-start proof", status, o)
			}
		})
	}
}
