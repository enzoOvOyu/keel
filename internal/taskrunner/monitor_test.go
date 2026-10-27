package taskrunner

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/hostmetrics"
	"github.com/enzoOvOyu/keel/internal/monitor"
	"github.com/enzoOvOyu/keel/internal/precollect"
)

func TestMonitorHelperFrozenReadbackNeverRecollectsOrAdmitsPayload(t *testing.T) {
	command := precollectTestCommand()
	command.Operation = "host.monitor"
	command.MonitorNetworkOnly = true
	if command.Validate() != nil {
		t.Fatal("bounded monitor command rejected")
	}
	for _, request := range []Request{{Action: "run", Command: command, Script: []byte("shell")}, {Action: "read", Command: command, ChunkSHA256: strings.Repeat("a", 64), ChunkOffset: 1}, {Action: "read", Command: command, ChunkSHA256: strings.Repeat("a", 64), ChunkOffset: monitor.MaxHostSnapshotBytes}} {
		if request.Validate() == nil {
			t.Fatal("unsafe monitoring payload admitted")
		}
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	wrapper := Wrapper{StateDirectory: dir}
	if _, err := wrapper.Handle(context.Background(), Request{Action: "run", Command: command}); err == nil {
		t.Fatal("disabled helper collected")
	}
	owned := filepath.Join(dir, command.CommandID)
	if err := os.Mkdir(owned, 0700); err != nil {
		t.Fatal(err)
	}
	groups := []string{}
	collect := func(_ context.Context, group string, at time.Time, _ *hostmetrics.CPUCounter) []monitor.Sample {
		groups = append(groups, group)
		key := "link.state"
		if group == "D2" {
			key = "dom.rx_power"
		}
		return []monitor.Sample{{Key: key, Entity: "nic0", Quality: "unknown", ObservedAt: at}}
	}
	output, reason, err := captureMonitorWith(context.Background(), owned, true, collect)
	if err != nil || reason != "monitor_observed" || strings.Join(groups, ",") != "D1,D2" {
		t.Fatal("network-only scope changed", groups, reason, err)
	}
	output["reason"] = reason
	now := time.Now().UTC()
	observation := Observation{Command: command, State: "success", StartedAt: now.Add(-time.Second), FinishedAt: now, Output: output}
	if err = writeObservation(owned, observation); err != nil {
		t.Fatal(err)
	}
	sha := output["monitor_sha256"].(string)
	part, err := wrapper.Handle(context.Background(), Request{Action: "read", Command: command, ChunkSHA256: sha})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(part.Output["chunk"].(string))
	if err != nil || precollect.Hash(raw) != sha {
		t.Fatal("original snapshot changed", err)
	}
	snapshot, err := monitor.DecodeHostSnapshot(raw, time.Now())
	if err != nil || !snapshot.NetworkOnly || len(snapshot.Samples) != 2 {
		t.Fatal("invalid original monitor snapshot", err)
	}
	changed := command
	changed.FencingToken++
	if _, err = wrapper.Handle(context.Background(), Request{Action: "read", Command: changed, ChunkSHA256: sha}); err == nil {
		t.Fatal("foreign fence recovered snapshot")
	}
	command.AuthorizationExpiresAt = 0
	if command.Validate() == nil {
		t.Fatal("unbounded monitor authorization")
	}
}
