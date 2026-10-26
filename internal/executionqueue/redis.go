// Package executionqueue carries public scheduling references only. PostgreSQL
// owns state, deduplication and receipts; Redis is a rebuildable delivery cache.
package executionqueue

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/enzoOvOyu/keel/internal/agentpki"
	"github.com/redis/go-redis/v9"
)

const Group = "execution-scheduler-v1"

var ErrQueue = errors.New("execution queue unavailable or invalid")
var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,47}$`)

type Message struct {
	ID   string
	Body string
}

// Transport must ACK only after the caller's PostgreSQL business transaction
// commits. Epoch fences cache recreation, including stream-only loss.
type Transport interface {
	Ensure(context.Context) (string, error)
	Publish(context.Context, string, string) (string, error)
	Read(context.Context, string, int) ([]Message, error)
	Ack(context.Context, string, string) error
	Close() error
}

type Config struct {
	Schema       string `json:"schema"`
	Address      string `json:"address"`
	Namespace    string `json:"namespace"`
	Username     string `json:"username,omitempty"`
	PasswordFile string `json:"password_file,omitempty"`
	CAFile       string `json:"ca_file,omitempty"`
	ServerName   string `json:"server_name,omitempty"`
	TLS          bool   `json:"tls"`
}

type Redis struct {
	client *redis.Client
	stream string
	epoch  string
	mu     sync.Mutex
	cursor string
}

// Load does not contact Redis. No configuration preserves explicit DB-only mode;
// configuring Redis never silently downgrades to a successful DB-only heartbeat.
func Load(path string, development bool) (*Redis, error) {
	if path == "" {
		return nil, nil
	}
	b, err := agentpki.ReadFile(path, 16384, true)
	if err != nil {
		return nil, ErrQueue
	}
	var c Config
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || d.Decode(new(any)) != io.EOF {
		return nil, ErrQueue
	}
	opts, err := options(c, development)
	if err != nil {
		return nil, err
	}
	return newRedis(redis.NewClient(opts), c.Namespace), nil
}

func options(c Config, development bool) (*redis.Options, error) {
	host, port, err := net.SplitHostPort(c.Address)
	if err != nil || host == "" || port == "" || strings.TrimSpace(c.Address) != c.Address || c.Schema != "keel.execution-queue/v1" || !namespacePattern.MatchString(c.Namespace) {
		return nil, ErrQueue
	}
	// Plaintext and anonymous access are limited to literal loopback fixtures.
	loopback := net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()
	if (!c.TLS || c.PasswordFile == "") && !(development && loopback) {
		return nil, ErrQueue
	}
	if len(c.Username) > 128 || strings.ContainsAny(c.Username, "\r\n\x00") {
		return nil, ErrQueue
	}
	password := ""
	if c.PasswordFile != "" {
		secret, e := agentpki.ReadFile(c.PasswordFile, 4096, true)
		if e != nil {
			return nil, ErrQueue
		}
		password = strings.TrimSuffix(string(secret), "\n")
		if len(password) < 16 || strings.ContainsAny(password, "\r\n\x00") {
			return nil, ErrQueue
		}
	}
	var tlsConfig *tls.Config
	if c.TLS {
		name := c.ServerName
		if name == "" {
			name = host
		}
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12, ServerName: name}
		if c.CAFile != "" {
			pem, e := agentpki.ReadFile(c.CAFile, 1<<20, false)
			if e != nil {
				return nil, ErrQueue
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, ErrQueue
			}
			tlsConfig.RootCAs = pool
		}
	} else if c.CAFile != "" || c.ServerName != "" {
		return nil, ErrQueue
	}
	return &redis.Options{Addr: c.Address, Username: c.Username, Password: password, TLSConfig: tlsConfig,
		Protocol: 2, MaxRetries: -1, DialTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, ContextTimeoutEnabled: true, PoolSize: 4, DisableIdentity: true}, nil
}

func newRedis(client *redis.Client, namespace string) *Redis {
	// Same hash slot for atomic epoch/publish/ACK scripts.
	prefix := "keel:{" + namespace + "}:execution:"
	return &Redis{client: client, stream: prefix + "events", epoch: prefix + "epoch", cursor: "0-0"}
}

var ensureScript = redis.NewScript(`
local made = redis.pcall('XGROUP', 'CREATE', KEYS[1], ARGV[1], '0', 'MKSTREAM')
if type(made) == 'table' and made.err then
  if not string.find(made.err, 'BUSYGROUP', 1, true) then return redis.error_reply('queue group unavailable') end
else
  redis.call('SET', KEYS[2], ARGV[2])
end
local epoch = redis.call('GET', KEYS[2])
if not epoch then
  redis.call('SET', KEYS[2], ARGV[2])
  epoch = ARGV[2]
end
return epoch`)

func (q *Redis) Ensure(ctx context.Context) (string, error) {
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return "", ErrQueue
	}
	epoch, err := ensureScript.Run(ctx, q.client, []string{q.stream, q.epoch}, Group, hex.EncodeToString(seed[:])).Text()
	if err != nil || len(epoch) != 32 {
		return "", ErrQueue
	}
	if _, err := hex.DecodeString(epoch); err != nil {
		return "", ErrQueue
	}
	return epoch, nil
}

var publishScript = redis.NewScript(`
if redis.call('GET', KEYS[2]) ~= ARGV[1] then return redis.error_reply('queue epoch changed') end
return redis.call('XADD', KEYS[1], 'MAXLEN', '~', '20000', '*', 'body', ARGV[2])`)

func (q *Redis) Publish(ctx context.Context, epoch, body string) (string, error) {
	if len(body) > 2048 {
		return "", ErrQueue
	}
	id, err := publishScript.Run(ctx, q.client, []string{q.stream, q.epoch}, epoch, body).Text()
	if err != nil {
		return "", ErrQueue
	}
	return id, nil
}

func (q *Redis) Read(ctx context.Context, consumer string, limit int) ([]Message, error) {
	if len(consumer) < 1 || len(consumer) > 120 || limit < 1 || limit > 1000 {
		return nil, ErrQueue
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	// Scan abandoned PEL entries as well as new messages. Persist the cursor so
	// young pending entries cannot permanently hide older abandoned entries.
	entries, next, err := q.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{Stream: q.stream, Group: Group,
		Consumer: consumer, MinIdle: 30 * time.Second, Start: q.cursor, Count: int64(limit)}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, ErrQueue
	}
	q.cursor = next
	if q.cursor == "" {
		q.cursor = "0-0"
	}
	if len(entries) < limit {
		streams, e := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{Group: Group, Consumer: consumer,
			Streams: []string{q.stream, ">"}, Count: int64(limit - len(entries)), Block: -1}).Result()
		if e != nil && !errors.Is(e, redis.Nil) {
			return nil, ErrQueue
		}
		for _, stream := range streams {
			entries = append(entries, stream.Messages...)
		}
	}
	out := make([]Message, 0, len(entries))
	for _, entry := range entries {
		body, ok := entry.Values["body"].(string)
		if !ok || len(entry.Values) != 1 || len(body) > 2048 {
			body = "" // Durable rejection records never retain attacker payloads.
		}
		out = append(out, Message{ID: entry.ID, Body: body})
	}
	return out, nil
}

var ackScript = redis.NewScript(`
if redis.call('GET', KEYS[2]) ~= ARGV[1] then return redis.error_reply('queue epoch changed') end
return redis.call('XACK', KEYS[1], ARGV[2], ARGV[3])`)

func (q *Redis) Ack(ctx context.Context, epoch, id string) error {
	if _, err := ackScript.Run(ctx, q.client, []string{q.stream, q.epoch}, epoch, Group, id).Result(); err != nil {
		return ErrQueue
	}
	return nil
}

func (q *Redis) Close() error { return q.client.Close() }
