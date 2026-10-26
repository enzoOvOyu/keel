package executionqueue

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestExecutionQueueConfigurationBoundaries(t *testing.T) {
	base := Config{Schema: "keel.execution-queue/v1", Address: "127.0.0.1:16379", Namespace: "fixture"}
	if _, err := options(base, true); err != nil {
		t.Fatal("loopback development fixture rejected", err)
	}
	if _, err := options(base, false); err == nil {
		t.Fatal("production accepted plaintext/anonymous Redis")
	}
	for _, address := range []string{"localhost:16379", "redis:6379", "0.0.0.0:16379", "https://127.0.0.1:16379"} {
		c := base
		c.Address = address
		if _, err := options(c, true); err == nil {
			t.Fatal("plaintext exception escaped literal loopback", address)
		}
	}
	for _, namespace := range []string{"", "../fixture", "bad{slot}", "fixture:secret"} {
		c := base
		c.Namespace = namespace
		if _, err := options(c, true); err == nil {
			t.Fatal("invalid namespace accepted")
		}
	}
	if q, err := Load("", false); q != nil || err != nil {
		t.Fatal("missing opt-in configuration changed DB-only mode")
	}
}

// This is opt-in, actual Redis 7+ protocol evidence. It does not start a service
// or flush a database; every key belongs to a fresh random fixture namespace.
func TestExecutionQueueRedisStreamsRecovery(t *testing.T) {
	address := os.Getenv("KEEL_TEST_REDIS_ADDRESS")
	if address == "" {
		t.Skip("dedicated loopback Redis fixture absent")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || port != "16379" {
		t.Fatal("refusing non-fixture Redis; expected literal loopback:16379")
	}
	var seed [12]byte
	if _, err = rand.Read(seed[:]); err != nil {
		t.Fatal(err)
	}
	opts, err := options(Config{Schema: "keel.execution-queue/v1", Address: address, Namespace: "test_" + hex.EncodeToString(seed[:])}, true)
	if err != nil {
		t.Fatal(err)
	}
	q := newRedis(redis.NewClient(opts), "test_"+hex.EncodeToString(seed[:]))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		q.client.Del(ctx, q.stream, q.epoch)
		q.Close()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	epoch, err := q.Ensure(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := q.Publish(ctx, epoch, `{"reference":"fixture-only"}`)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := q.Read(ctx, "worker-a", 10)
	if err != nil || len(entries) != 1 || entries[0].ID != id {
		t.Fatal("new group delivery failed", entries, err)
	}
	// Simulate a dead consumer without waiting. Redis owns the PEL idle clock.
	if err = q.client.Do(ctx, "XCLAIM", q.stream, Group, "dead-worker", 0, id, "IDLE", 31000).Err(); err != nil {
		t.Fatal(err)
	}
	entries, err = q.Read(ctx, "worker-b", 10)
	if err != nil || len(entries) != 1 || entries[0].ID != id {
		t.Fatal("abandoned PEL entry not recovered", entries, err)
	}
	if err = q.Ack(ctx, epoch, id); err != nil {
		t.Fatal(err)
	}
	if err = q.Ack(ctx, epoch, id); err != nil {
		t.Fatal("duplicate ACK failed", err)
	}
	pending, err := q.client.XPending(ctx, q.stream, Group).Result()
	if err != nil || pending.Count != 0 {
		t.Fatal("ACK did not clear PEL", pending, err)
	}
	// Lose only the stream; retaining the epoch key must still rotate epoch.
	if err = q.client.Del(ctx, q.stream).Err(); err != nil {
		t.Fatal(err)
	}
	next, err := q.Ensure(ctx)
	if err != nil || next == epoch {
		t.Fatal("stream-only loss retained stale epoch", next, err)
	}
	if _, err = q.Publish(ctx, epoch, "stale"); err == nil {
		t.Fatal("stale publisher crossed epoch")
	}
	if err = q.Ack(ctx, epoch, id); err == nil {
		t.Fatal("stale ACK crossed epoch")
	}
	if _, err = q.Publish(ctx, next, "rebuilt"); err != nil {
		t.Fatal("rebuilt stream rejected publisher", err)
	}
}
