package taskrunner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func cacheDirectoryFixture(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("target cache intentionally owned by non-root platform account")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func cachePageObservation(command TaskCommand, output map[string]any, reason string) Observation {
	state := "success"
	if output == nil {
		output = map[string]any{}
		state = "failed"
	}
	output["reason"] = reason
	now := time.Now().UTC()
	return Observation{Command: command, State: state, StartedAt: now, FinishedAt: now, Output: output}
}

func TestScriptOwnedCacheHitAndCorruption(t *testing.T) {
	dir := cacheDirectoryFixture(t)
	ctx := context.Background()
	body := []byte("printf cache-fixture\n")
	c := scriptTestCommand(body, "shell")
	if hit, err := cacheApprovedScript(ctx, dir, c, body); err != nil || hit {
		t.Fatal("initial cache registration", hit, err)
	}
	path := filepath.Join(dir, c.ScriptSHA256)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if hit, err := cacheApprovedScript(ctx, dir, c, body); err != nil || !hit {
		t.Fatal("verified cache hit missed", hit, err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("cache hit replaced object", err)
	}
	query := agentCommand("e", 2)
	query.Operation = "host.cache.inventory"
	output, reason := readMediaCache(ctx, dir, "", "")
	observation := cachePageObservation(query, output, reason)
	if observation.Validate(query) != nil || observation.State != "success" || output["registered_count"] != 1 {
		t.Fatal("cache page not verifiable", observation)
	}
	raw, err := json.Marshal(output)
	if err != nil || strings.Contains(string(raw), c.ScriptMediaID) || strings.Contains(string(raw), c.ScriptObjectVersion) {
		t.Fatal("public cache inventory leaked seed references", string(raw), err)
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = cacheApprovedScript(ctx, dir, c, body); !errors.Is(err, ErrRejected) {
		t.Fatal("changed cache silently overwritten", err)
	}
	if output, reason = readMediaCache(ctx, dir, "", ""); output != nil || reason != "cache_content_mismatch" {
		t.Fatal("corrupt cache reported usable", output, reason)
	}
	current, err := os.ReadFile(path)
	if err != nil || string(current) != "corrupt" {
		t.Fatal("rejection modified cache", err)
	}
}

func TestScriptCacheNeverAdoptsUnownedObjects(t *testing.T) {
	dir := cacheDirectoryFixture(t)
	body := []byte("exit 0\n")
	c := scriptTestCommand(body, "shell")
	path := filepath.Join(dir, c.ScriptSHA256)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cacheApprovedScript(context.Background(), dir, c, body); !errors.Is(err, ErrRejected) {
		t.Fatal("unowned/orphaned content adopted", err)
	}
	after, err := os.Stat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatal("unowned object replaced", err)
	}
	if _, err = os.Stat(filepath.Join(dir, mediaCacheManifest)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("unowned file added to ownership index", err)
	}
}

func TestScriptCachePaginationPinsManifestAndVerifiesEachPage(t *testing.T) {
	dir := cacheDirectoryFixture(t)
	ctx := context.Background()
	for i := 0; i < 17; i++ {
		body := []byte(strings.Repeat("#", i+1) + "\nexit 0\n")
		c := scriptTestCommand(body, "shell")
		if _, err := cacheApprovedScript(ctx, dir, c, body); err != nil {
			t.Fatal(err)
		}
	}
	query := agentCommand("e", 2)
	query.Operation = "host.cache.inventory"
	first, reason := readMediaCache(ctx, dir, "", "")
	if cachePageObservation(query, first, reason).Validate(query) != nil {
		t.Fatal("first cache page invalid")
	}
	cursor, _ := first["next_cursor"].(string)
	digest, _ := first["manifest_sha256"].(string)
	if cursor == "" || digest == "" {
		t.Fatal("continuation identity missing")
	}
	query.CacheCursor, query.CacheManifestSHA256 = cursor, digest
	second, reason := readMediaCache(ctx, dir, cursor, digest)
	if cachePageObservation(query, second, reason).Validate(query) != nil || second["next_cursor"] != "" {
		t.Fatal("last cache page invalid", second)
	}
	body := []byte("# newly registered\nexit 0\n")
	c := scriptTestCommand(body, "shell")
	if _, err := cacheApprovedScript(ctx, dir, c, body); err != nil {
		t.Fatal(err)
	}
	if page, reason := readMediaCache(ctx, dir, cursor, digest); page != nil || reason != "cache_snapshot_changed" {
		t.Fatal("pages silently mixed ownership snapshots", page, reason)
	}
}

func TestScriptCacheLinkAndManifestBoundaries(t *testing.T) {
	dir := cacheDirectoryFixture(t)
	ctx := context.Background()
	body := []byte("exit 0\n")
	c := scriptTestCommand(body, "shell")
	if _, err := cacheApprovedScript(ctx, dir, c, body); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, c.ScriptSHA256)
	outside := filepath.Join(t.TempDir(), "customer")
	if err := os.WriteFile(outside, []byte("customer"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := cacheApprovedScript(ctx, dir, c, body); !errors.Is(err, ErrRejected) {
		t.Fatal("cache symlink followed", err)
	}
	if page, reason := readMediaCache(ctx, dir, "", ""); page != nil || reason != "cache_content_mismatch" {
		t.Fatal("cache link reported usable")
	}
	if actual, err := os.ReadFile(outside); err != nil || string(actual) != "customer" {
		t.Fatal("cache handling touched customer file", err)
	}
	command := agentCommand("d", 3)
	command.CacheCursor = strings.Repeat("a", 64)
	if command.Validate() == nil {
		t.Fatal("non-cache command accepted pagination")
	}
	command.Operation = "host.cache.inventory"
	if command.Validate() == nil {
		t.Fatal("cursor without snapshot hash accepted")
	}
	command.CacheManifestSHA256 = strings.Repeat("b", 64)
	if command.Validate() != nil {
		t.Fatal("valid cache continuation rejected")
	}
}

func TestScriptCacheProbeCopiesOnlyOwnedVerifiedBytes(t *testing.T) {
	dir := cacheDirectoryFixture(t)
	ctx := context.Background()
	body := []byte("printf cache-probe-fixture\n")
	c := scriptTestCommand(body, "shell")
	c.ScriptCacheAllowed = true
	if _, err := cacheApprovedScript(ctx, dir, c, body); err != nil {
		t.Fatal(err)
	}
	probe, err := probeScriptCache(ctx, dir, true, c)
	if err != nil {
		t.Fatal(err)
	}
	if hit, err := ScriptCacheProbeHit(probe, c); err != nil || !hit {
		t.Fatal("owned content not a hit", hit, err)
	}
	copy, err := readCachedScriptBody(ctx, dir, c)
	if err != nil || string(copy) != string(body) {
		t.Fatal("cached bytes differ", err)
	}
	path := filepath.Join(dir, c.ScriptSHA256)
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed after probe\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCachedScriptBody(ctx, dir, c); err == nil {
		t.Fatal("changed content accepted after hit")
	}
	if string(copy) != string(body) {
		t.Fatal("pinned verified copy changed")
	}
	probe, err = probeScriptCache(ctx, dir, true, c)
	if err != nil {
		t.Fatal(err)
	}
	if hit, err := ScriptCacheProbeHit(probe, c); err != nil || hit {
		t.Fatal("corrupt cache promoted to hit", hit, err)
	}
	// Neither a miss nor an opt-out deletes, repairs or takes ownership.
	probe, err = probeScriptCache(ctx, dir, false, c)
	if err != nil {
		t.Fatal(err)
	}
	if hit, err := ScriptCacheProbeHit(probe, c); err != nil || hit {
		t.Fatal("disabled cache reported a hit", hit, err)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != "changed after probe\n" {
		t.Fatal("probe repaired cache", err)
	}
	orphan := scriptTestCommand([]byte("unowned\n"), "shell")
	orphan.ScriptCacheAllowed = true
	if err := os.WriteFile(filepath.Join(dir, orphan.ScriptSHA256), []byte("unowned\n"), 0400); err != nil {
		t.Fatal(err)
	}
	if _, err := readCachedScriptBody(ctx, dir, orphan); err == nil {
		t.Fatal("unregistered object adopted")
	}
}

func TestScriptCacheProbeDoesNotConsumeAgentFenceOrCreateReceipt(t *testing.T) {
	state := cacheDirectoryFixture(t)
	c := scriptTestCommand([]byte("exit 0\n"), "shell")
	c.ScriptCacheAllowed = true
	a := Agent{StateDirectory: state, ProjectID: c.ProjectID, MachineID: c.MachineID}
	// Cache/scripts opt-out is a miss, never task acceptance or script execution.
	o, err := a.Handle(context.Background(), Request{Action: "cache", Command: c})
	if err != nil {
		t.Fatal(err)
	}
	if hit, err := ScriptCacheProbeHit(o, c); err != nil || hit {
		t.Fatal("disabled Agent probe", hit, err)
	}
	entries, err := os.ReadDir(state)
	if err != nil || len(entries) != 0 {
		t.Fatal("probe created execution/fence state", entries, err)
	}
	c.AuthorizationExpiresAt = time.Now().Add(-time.Second).UnixMicro()
	if _, err := a.Handle(context.Background(), Request{Action: "cache", Command: c}); err == nil {
		t.Fatal("expired probe admitted")
	}
}
