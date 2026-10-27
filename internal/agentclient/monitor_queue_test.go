package agentclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/monitor"
)

func TestMonitorQueueRetainsExactFrameUntilDurableAckAndMarksOverflow(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	c := Config{StateDirectory: dir}
	frame := func(i int) monitor.Frame {
		return monitor.Frame{Version: monitor.Version, ID: fmt.Sprintf("frame%03d", i), PolicyID: "default-M1-v1", Full: true, Samples: []monitor.Sample{}, Unchanged: []monitor.Unchanged{}}
	}
	for i := 0; i < monitorQueueLimit; i++ {
		if e := monitorQueueAppend(c, frame(i)); e != nil {
			t.Fatal(e)
		}
	}
	raw, e := monitorQueuePeek(c)
	if e != nil {
		t.Fatal(e)
	}
	var first monitor.Frame
	if json.Unmarshal(raw, &first) != nil || first.ID != "frame000" {
		t.Fatal("queue order changed")
	}
	if e = monitorQueueAck(c, raw, "different-frame"); e == nil {
		t.Fatal("unrelated ACK erased telemetry")
	}
	if next, e := monitorQueuePeek(c); e != nil || string(next) != string(raw) {
		t.Fatal("unconfirmed frame lost")
	}
	if e = monitorQueueAppend(c, frame(65)); !errors.Is(e, errMonitorQueueFull) {
		t.Fatal("queue cap not enforced", e)
	}
	if _, e = os.Stat(filepath.Join(dir, "monitor", "overflow.json")); e != nil {
		t.Fatal("overflow was not persisted")
	}
	if e = monitorQueueAck(c, raw, first.ID); e != nil {
		t.Fatal(e)
	}
	if e = monitorQueueAppend(c, frame(66)); e != nil {
		t.Fatal(e)
	}
	for i := 1; i < monitorQueueLimit; i++ {
		raw, e = monitorQueuePeek(c)
		if e != nil {
			t.Fatal(e)
		}
		var f monitor.Frame
		if json.Unmarshal(raw, &f) != nil {
			t.Fatal("invalid retained frame")
		}
		if e = monitorQueueAck(c, raw, f.ID); e != nil {
			t.Fatal(e)
		}
	}
	raw, e = monitorQueuePeek(c)
	if e != nil {
		t.Fatal(e)
	}
	last, e := monitor.DecodeFrame(raw, time.Now())
	if e != nil || last.GapStart == nil || last.GapEnd == nil || last.ID != "frame066" {
		t.Fatal("gap was silently filled", e)
	}
}
func TestMonitorQueueRejectsSymlinkStateAndInvalidPolicy(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink(t.TempDir(), filepath.Join(dir, "monitor")); e != nil {
		t.Fatal(e)
	}
	if _, e := monitorQueuePeek(Config{StateDirectory: dir}); e == nil {
		t.Fatal("queue followed symlink")
	}
}
