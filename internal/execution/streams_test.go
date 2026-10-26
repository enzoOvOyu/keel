package execution

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/executionqueue"
	"github.com/enzoOvOyu/keel/internal/platform"
	"github.com/redis/go-redis/v9"
)

type streamFixture struct {
	epoch     string
	published []string
	messages  []executionqueue.Message
	ack       func(string) error
	failure   error
}

func (q *streamFixture) Ensure(context.Context) (string, error) { return q.epoch, q.failure }
func (q *streamFixture) Publish(_ context.Context, _ string, body string) (string, error) {
	q.published = append(q.published, body)
	return "1-0", q.failure
}
func (q *streamFixture) Read(context.Context, string, int) ([]executionqueue.Message, error) {
	return q.messages, q.failure
}
func (q *streamFixture) Ack(_ context.Context, _ string, id string) error {
	if q.ack != nil {
		return q.ack(id)
	}
	return nil
}
func (q *streamFixture) Close() error { return nil }

func streamCreated(t *testing.T, f *agentKernelFixture) (*Execution, streamReference) {
	t.Helper()
	ctx := context.Background()
	flow := publishControlKV(t, f, controlKVSample)
	x, err := f.service.CreateControlFlow(ctx, f.actor, f.project, ControlFlowCreateInput{FlowVersionID: flow.ID}, platform.RandomID(), "streams")
	mustAgent(t, err)
	var id, kind string
	var seq, version int64
	var at time.Time
	err = f.service.DB.QueryRowContext(ctx, `SELECT id,event_type,sequence,aggregate_version,created_at FROM exec_outbox WHERE execution_id=$1 AND event_type='execution.created'`, x.ID).Scan(&id, &kind, &seq, &version, &at)
	mustAgent(t, err)
	return x, reference(id, x.ID, f.project, kind, seq, version, at)
}

func TestExecutionStreamReferenceProjection(t *testing.T) {
	r := reference(strings.Repeat("a", 32), strings.Repeat("b", 32), strings.Repeat("c", 32), "execution.created", 1, 1, time.Now().UTC())
	b, _ := json.Marshal(r)
	decoded, ok := decodeStreamReference(string(b))
	if !ok || !sameStreamReference(r, decoded) {
		t.Fatal("reference did not round-trip")
	}
	for _, body := range []string{string(b) + " {}", strings.Replace(string(b), `"specversion":"1.0"`, `"specversion":"2.0"`, 1), strings.Replace(string(b), `"data":{`, `"data":{"output":"secret",`, 1), strings.Repeat("x", 2049)} {
		if _, ok := decodeStreamReference(body); ok {
			t.Fatal("invalid projection admitted")
		}
	}
}

func TestExecutionStreamsCommitBeforeACKAndDuplicatePostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	x, ref := streamCreated(t, f)
	body, _ := json.Marshal(ref)
	epoch := platform.RandomID()
	queue := &streamFixture{epoch: epoch, messages: []executionqueue.Message{{ID: "1-0", Body: string(body)}}}
	f.service.Queue = queue
	ackLost := true
	queue.ack = func(string) error {
		var state string
		var version int64
		var count int
		mustAgent(t, f.service.DB.QueryRowContext(ctx, `SELECT state,state_version FROM exec_executions WHERE id=$1`, x.ID).Scan(&state, &version))
		mustAgent(t, f.service.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_consumed_events WHERE consumer=$1 AND event_id=$2`, executionqueue.Group, ref.ID).Scan(&count))
		if state != "scheduled" || version != 2 || count != 1 {
			t.Fatal("ACK preceded committed business state/dedup", state, version, count)
		}
		if ackLost {
			return executionqueue.ErrQueue
		}
		return nil
	}
	if _, err := f.service.PumpQueue(ctx, "streams-a", 100); !errors.Is(err, executionqueue.ErrQueue) {
		t.Fatal("lost ACK masked", err)
	}
	ackLost = false
	if n, err := f.service.PumpQueue(ctx, "streams-b", 100); err != nil || n != 1 {
		t.Fatal("duplicate not safely acknowledged", n, err)
	}
	// A rebuilt Redis generation still encounters the same logical DB consumer.
	queue.epoch = platform.RandomID()
	if _, err := f.service.PumpQueue(ctx, "streams-c", 100); err != nil {
		t.Fatal(err)
	}
	for _, body := range queue.published {
		if strings.Contains(body, "delivery.note") || strings.Contains(body, "Ready") {
			t.Fatal("outbox payload leaked to queue")
		}
	}
	var started int
	mustAgent(t, f.service.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_task_attempts WHERE execution_id=$1 AND started_at IS NOT NULL`, x.ID).Scan(&started))
	if started != 0 {
		t.Fatal("stream consumption executed a task")
	}
}

func TestExecutionStreamsRejectAndBusyRecoveryPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	x, ref := streamCreated(t, f)
	epoch := platform.RandomID()
	b, _ := json.Marshal(ref)
	message := executionqueue.Message{ID: "2-0", Body: string(b)}
	tx, err := f.service.DB.BeginTx(ctx, nil)
	mustAgent(t, err)
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `SELECT id FROM exec_executions WHERE id=$1 FOR UPDATE`, x.ID)
	mustAgent(t, err)
	ok, err := f.service.consumeStreamReference(ctx, epoch, message)
	mustAgent(t, err)
	if ok {
		t.Fatal("busy aggregate acknowledged")
	}
	mustAgent(t, tx.Rollback())
	ok, err = f.service.consumeStreamReference(ctx, epoch, message)
	mustAgent(t, err)
	if !ok {
		t.Fatal("recovered aggregate not committed")
	}
	bad := ref
	bad.Data.ProjectID = platform.RandomID()
	other, _ := json.Marshal(bad)
	unknown := ref
	unknown.ID = platform.RandomID()
	missing, _ := json.Marshal(unknown)
	for i, body := range []string{"{}", string(other), string(missing)} {
		id := []string{"3-0", "4-0", "5-0"}[i]
		ok, err = f.service.consumeStreamReference(ctx, epoch, executionqueue.Message{ID: id, Body: body})
		mustAgent(t, err)
		if !ok {
			t.Fatal("poison entry not durably rejected")
		}
		var reason string
		mustAgent(t, f.service.DB.QueryRowContext(ctx, `SELECT reason FROM exec_stream_rejections WHERE stream_epoch=$1 AND stream_id=$2`, epoch, id).Scan(&reason))
		if reason != []string{"invalid_reference", "reference_mismatch", "unknown_event"}[i] {
			t.Fatal("incorrect durable rejection", reason)
		}
	}
}

func TestExecutionStreamsRebuildAndFailureIsolationPostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	x, ref := streamCreated(t, f)
	queue := &streamFixture{epoch: platform.RandomID()}
	f.service.Queue = queue
	// Restrict the fixture to this event without changing publication authority.
	find := func() bool {
		for _, body := range queue.published {
			v, ok := decodeStreamReference(body)
			if ok && v.ID == ref.ID {
				return true
			}
		}
		return false
	}
	untilPublished := func() {
		t.Helper()
		for i := 0; i < 100; i++ {
			mustAgent(t, f.service.publishStreamReferences(ctx, queue.epoch, 1000))
			if find() {
				return
			}
		}
		t.Fatal("fixture event did not reach publication")
	}
	untilPublished()
	queue.published = nil
	queue.epoch = platform.RandomID()
	untilPublished()
	queue.published = nil
	_, err := f.service.DB.ExecContext(ctx, `UPDATE exec_stream_publications SET last_published_at=now()-interval '3 minutes' WHERE event_id=$1`, ref.ID)
	mustAgent(t, err)
	mustAgent(t, f.service.publishStreamReferences(ctx, queue.epoch, 1000))
	if !find() {
		t.Fatal("partial stream loss did not trigger bounded retry")
	}
	queue.failure = executionqueue.ErrQueue
	if _, err = f.service.PumpQueue(ctx, "streams-down", 100); !errors.Is(err, executionqueue.ErrQueue) {
		t.Fatal("configured Redis silently fell back", err)
	}
	var state string
	mustAgent(t, f.service.DB.QueryRowContext(ctx, `SELECT state FROM exec_executions WHERE id=$1`, x.ID).Scan(&state))
	if state != "created" {
		t.Fatal("queue outage changed task state", state)
	}
}

func TestExecutionStreamsRedisPostgresScheduler(t *testing.T) {
	address := os.Getenv("KEEL_TEST_REDIS_ADDRESS")
	if address == "" {
		t.Skip("dedicated loopback Redis fixture absent")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() || port != "16379" {
		t.Fatal("refusing non-fixture Redis")
	}
	f := agentKernel(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	x, _ := streamCreated(t, f)
	namespace := "test_" + platform.RandomID()
	config, _ := json.Marshal(executionqueue.Config{Schema: "keel.execution-queue/v1", Address: address, Namespace: namespace})
	path := filepath.Join(t.TempDir(), "queue.json")
	mustAgent(t, os.WriteFile(path, config, 0600))
	queue, err := executionqueue.Load(path, true)
	mustAgent(t, err)
	defer queue.Close()
	f.service.Queue = queue
	client := redis.NewClient(&redis.Options{Addr: address, Protocol: 2, MaxRetries: -1, ReadTimeout: time.Second, WriteTimeout: time.Second})
	prefix := "keel:{" + namespace + "}:execution:"
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		client.Del(cleanup, prefix+"events", prefix+"epoch")
		client.Close()
	})
	// A disposable shared DB can contain previous fixture outboxes. Drain in
	// bounded batches until this execution is reached, without private Claim.
	for i := 0; i < 1000; i++ {
		_, err = f.service.PumpQueue(ctx, "redis-postgres-fixture", 100)
		mustAgent(t, err)
		var state string
		mustAgent(t, f.service.DB.QueryRowContext(ctx, `SELECT state FROM exec_executions WHERE id=$1`, x.ID).Scan(&state))
		if state == "scheduled" {
			break
		}
		if i == 999 {
			t.Fatal("created fixture did not reach scheduler")
		}
	}
	var version int64
	mustAgent(t, f.service.DB.QueryRowContext(ctx, `SELECT state_version FROM exec_executions WHERE id=$1`, x.ID).Scan(&version))
	if version != 2 {
		t.Fatal("Redis scheduler did not apply exactly one created transition", version)
	}
	// Reconstruct the transport and delete only this fixture's cache. DB dedup
	// must still prevent another transition or any task execution.
	_, err = client.Del(ctx, prefix+"events", prefix+"epoch").Result()
	mustAgent(t, err)
	_, err = f.service.PumpQueue(ctx, "redis-postgres-restarted", 100)
	mustAgent(t, err)
	var started int
	mustAgent(t, f.service.DB.QueryRowContext(ctx, `SELECT count(*) FROM exec_task_attempts WHERE execution_id=$1 AND started_at IS NOT NULL`, x.ID).Scan(&started))
	if started != 0 {
		t.Fatal("Redis rebuilding started an attempt without Claim")
	}
	mustAgent(t, f.service.DB.QueryRowContext(ctx, `SELECT state_version FROM exec_executions WHERE id=$1`, x.ID).Scan(&version))
	if version != 2 {
		t.Fatal("rebuild replayed an aggregate transition")
	}
}

func TestExecutionStreamsHeartbeatDistinguishesQueueModePostgres(t *testing.T) {
	f := agentKernel(t)
	ctx := context.Background()
	id := "queue-health-" + platform.RandomID()
	mustAgent(t, f.service.WorkerHeartbeat(ctx, id, 0))
	mustAgent(t, f.service.WorkerHealthy(ctx, id))
	f.service.Queue = &streamFixture{epoch: platform.RandomID()}
	if err := f.service.WorkerHealthy(ctx, id); err == nil {
		t.Fatal("DB-only heartbeat established Redis queue health")
	}
	mustAgent(t, f.service.WorkerHeartbeat(ctx, id, 0))
	mustAgent(t, f.service.WorkerHealthy(ctx, id))
	f.service.Queue = nil
	if err := f.service.WorkerHealthy(ctx, id); err == nil {
		t.Fatal("Redis heartbeat established DB-only configuration health")
	}
	f.service.Queue = &streamFixture{epoch: platform.RandomID()}
	_, err := f.service.DB.ExecContext(ctx, `UPDATE exec_worker_heartbeats SET observed_at=now()-interval '11 seconds' WHERE id=$1`, id)
	mustAgent(t, err)
	if err := f.service.WorkerHealthy(ctx, id); err == nil {
		t.Fatal("failed queue cycle could retain stale health")
	}
}
