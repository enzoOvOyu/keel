package taskrunner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentinventory"
)

func TestAgentCacheHeartbeatObservesIndexWithoutClaimingContentVerified(t *testing.T) {
	dir := cacheDirectoryFixture(t)
	ctx := context.Background()
	now := time.Now()
	body := []byte("printf ownership-observation\n")
	c := scriptTestCommand(body, "shell")
	if _, err := cacheApprovedScript(ctx, dir, c, body); err != nil {
		t.Fatal(err)
	}
	// A damaged file remains a declared index entry, never a verified hit.
	path := filepath.Join(dir, c.ScriptSHA256)
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupt\n"), 0600); err != nil {
		t.Fatal(err)
	}
	frame := collectMediaCacheIndex(ctx, dir, now)
	if frame.Validate(now) != nil || frame.State != "ready" || len(frame.Entries) != 1 || frame.Entries[0].SHA256 != c.ScriptSHA256 {
		t.Fatal("ownership index not observed", frame)
	}
	raw, err := json.Marshal(frame)
	if err != nil || len(raw) > agentinventory.MaxCacheFrameBytes || strings.Contains(string(raw), c.ScriptMediaID) || strings.Contains(string(raw), c.ScriptObjectVersion) {
		t.Fatal("private seed reference leaked", err)
	}
	output, reason := readMediaCache(ctx, dir, "", frame.ManifestSHA256)
	if output != nil || reason != "cache_content_mismatch" {
		t.Fatal("heartbeat index became content proof", output, reason)
	}
	if err := os.WriteFile(filepath.Join(dir, mediaCacheManifest), []byte(`{"version":"foreign"}`), 0600); err != nil {
		t.Fatal(err)
	}
	failed := collectMediaCacheIndex(ctx, dir, now)
	if failed.State != "unavailable" || failed.FailureCode != "cache_index_unavailable" || len(failed.Entries) != 0 || failed.Validate(now) != nil {
		t.Fatal("corrupt index reported partial success", failed)
	}
}
