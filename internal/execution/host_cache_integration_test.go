package execution

import (
	"context"
	"strings"
	"testing"

	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/enzoOvOyu/keel/internal/taskrunner"
)

func TestAgentCacheInventoryAdmissionAndFrozenCursor(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.service.MediaStore = f.store
	in := SSHCreateInput{MachineIDs: []string{f.machine}, Operation: "host.cache.inventory", TimeoutMS: 5000, CacheCursor: strings.Repeat("a", 64), CacheManifestSHA256: strings.Repeat("b", 64)}
	if _, err := f.service.CreateAgent(ctx, f.actor, f.project, in, platform.RandomID(), "cache-test"); err == nil {
		t.Fatal("cache query admitted without control-plane opt-in")
	}
	f.store.TaskCacheInventoryEnabled = true
	key := platform.RandomID()
	x, err := f.service.CreateAgent(ctx, f.actor, f.project, in, key, "cache-test")
	mustAgent(t, err)
	replay, err := f.service.CreateAgent(ctx, f.actor, f.project, in, key, "cache-test")
	mustAgent(t, err)
	if replay.ID != x.ID || x.Parameters["cache_cursor"] != in.CacheCursor || x.Parameters["cache_manifest_sha256"] != in.CacheManifestSHA256 {
		t.Fatal("cache query parameters not immutable")
	}
	claim := f.claim(t, x.ID)
	if claim == nil {
		t.Fatal("cache query not claimed")
	}
	command := sshCommand(claim)
	if command.Validate() != nil || command.CacheCursor != in.CacheCursor || command.CacheManifestSHA256 != in.CacheManifestSHA256 {
		t.Fatal("cache cursor missing from command", command)
	}
	mustAgent(t, f.store.AgentRequestAllowed(ctx, taskrunner.Request{Action: "run", Command: command}, f.credential))
	altered := command
	altered.CacheCursor = strings.Repeat("c", 64)
	if err = f.store.AgentRequestAllowed(ctx, taskrunner.Request{Action: "run", Command: altered}, f.credential); err == nil {
		t.Fatal("transport changed frozen cache cursor")
	}
	f.store.TaskCacheInventoryEnabled = false
	if err = f.store.AgentRequestAllowed(ctx, taskrunner.Request{Action: "run", Command: command}, f.credential); err == nil {
		t.Fatal("opt-out still admitted new cache queries")
	}
	mustAgent(t, f.store.AgentRequestAllowed(ctx, taskrunner.Request{Action: "query", Command: command}, f.credential))
}

func TestAgentArtifactInventoryAdmissionAndFrozenCursor(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	f.service.MediaStore = f.store
	in := SSHCreateInput{MachineIDs: []string{f.machine}, Operation: "host.artifact.inventory", TimeoutMS: 5000, CacheCursor: strings.Repeat("a", 64), CacheManifestSHA256: strings.Repeat("b", 64)}
	if _, err := f.service.CreateAgent(ctx, f.actor, f.project, in, platform.RandomID(), "cache-test"); err == nil {
		t.Fatal("cache query admitted without control-plane opt-in")
	}
	f.store.ArtifactTransfersEnabled = true
	key := platform.RandomID()
	x, err := f.service.CreateAgent(ctx, f.actor, f.project, in, key, "cache-test")
	mustAgent(t, err)
	replay, err := f.service.CreateAgent(ctx, f.actor, f.project, in, key, "cache-test")
	mustAgent(t, err)
	if replay.ID != x.ID || x.Parameters["cache_cursor"] != in.CacheCursor || x.Parameters["cache_manifest_sha256"] != in.CacheManifestSHA256 {
		t.Fatal("cache query parameters not immutable")
	}
	claim := f.claim(t, x.ID)
	if claim == nil {
		t.Fatal("cache query not claimed")
	}
	command := sshCommand(claim)
	if command.Validate() != nil || command.CacheCursor != in.CacheCursor || command.CacheManifestSHA256 != in.CacheManifestSHA256 {
		t.Fatal("cache cursor missing from command", command)
	}
	mustAgent(t, f.store.AgentRequestAllowed(ctx, taskrunner.Request{Action: "run", Command: command}, f.credential))
	altered := command
	altered.CacheCursor = strings.Repeat("c", 64)
	if err = f.store.AgentRequestAllowed(ctx, taskrunner.Request{Action: "run", Command: altered}, f.credential); err == nil {
		t.Fatal("transport changed frozen cache cursor")
	}
	f.store.ArtifactTransfersEnabled = false
	if err = f.store.AgentRequestAllowed(ctx, taskrunner.Request{Action: "run", Command: command}, f.credential); err == nil {
		t.Fatal("opt-out still admitted new cache queries")
	}
	mustAgent(t, f.store.AgentRequestAllowed(ctx, taskrunner.Request{Action: "query", Command: command}, f.credential))
}
