package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/enzoOvOyu/keel/internal/webhook"
)

func TestWebhookDurableOutboxDeliveryAndIsolation(t *testing.T) {
	s := integrationStore(t)
	ctx := context.Background()
	const password = "test-only-notification-password"
	if _, err := s.CreateUser(ctx, "notification-owner", "Owner", password, true, true); err != nil {
		t.Fatal(err)
	}
	_, owner := testLogin(t, s, "notification-owner", password, "198.51.100.190")
	project, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Webhook delivery", Slug: "webhook-delivery"}, "notification-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateProject(ctx, owner, CreateProjectInput{Name: "Other notification project", Slug: "webhook-other"}, "notification-other-project", "test")
	if err != nil {
		t.Fatal(err)
	}
	// Source fixtures exercise committed outbox consumption, not hardware work.
	seedExecution := func(projectID string) string {
		t.Helper()
		flow, version, execution := RandomID(), RandomID(), RandomID()
		_, e := s.DB.ExecContext(ctx, `INSERT INTO exec_flows(id,project_id,name,created_by,created_at,updated_at)VALUES($1,$2,$1,$3,now(),now())`, flow, projectID, owner.User.ID)
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.DB.ExecContext(ctx, `INSERT INTO exec_flow_versions(id,flow_id,project_id,version,definition,content_hash,created_by,created_at) VALUES($1,$2,$3,1,'{}',$4,$5,now())`, version, flow, projectID, strings.Repeat("a", 64), owner.User.ID)
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.DB.ExecContext(ctx, `INSERT INTO exec_executions(id,project_id,flow_version_id,parameters,snapshot,target_set_hash,concurrency,created_by,created_at)VALUES($1,$2,$3,'{}','{}',$4,1,$5,now())`, execution, projectID, version, strings.Repeat("b", 64), owner.User.ID)
		if e != nil {
			t.Fatal(e)
		}
		return execution
	}
	execution := seedExecution(project.ID)
	foreignExecution := seedExecution(other.ID)
	seedEvent := func(execution, kind, payload string) string {
		t.Helper()
		id := RandomID()
		_, e := s.DB.ExecContext(ctx, "INSERT INTO exec_outbox(id,execution_id,aggregate_version,event_type,payload,created_at)VALUES($1,$2,1,$3,$4,now())", id, execution, kind, payload)
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	eventID := seedEvent(execution, "execution.state_changed", `{"state":"failed","untrusted_metadata":"must-not-leave-platform"}`)
	seedEvent(foreignExecution, "execution.state_changed", `{"state":"failed"}`)
	seedEvent(execution, "attempt.receipt_applied", `{"result":"failed"}`)
	key := bytes.Repeat([]byte{0x71}, 32)
	var status atomic.Int64
	status.Store(503)
	var calls atomic.Int64
	var bodies sync.Map
	var block atomic.Bool
	started, release := make(chan struct{}, 1), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		body, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
		}
		if e = webhook.Verify(key, r.Header.Get("X-Keel-Timestamp"), r.Header.Get("X-Keel-Event-ID"), r.Header.Get("X-Keel-Signature"), body, time.Now()); e != nil {
			t.Error(e)
		}
		var event struct {
			ID, Source string
			Data       struct {
				ProjectID string `json:"project_id"`
			}
		}
		if e = json.Unmarshal(body, &event); e != nil || event.Data.ProjectID != project.ID || event.Source != "urn:keel:project:"+project.ID || bytes.Contains(body, []byte("must-not-leave-platform")) {
			t.Error("project/private-payload boundary failed")
		}
		if prior, ok := bodies.LoadOrStore(event.ID, string(body)); ok && prior != string(body) {
			t.Error("retry changed immutable event payload")
		}
		if block.Load() {
			select {
			case started <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer server.Close()
	directory := t.TempDir()
	keyPath := filepath.Join(directory, "hmac-key")
	if err = os.WriteFile(keyPath, key, 0600); err != nil {
		t.Fatal(err)
	}
	policy := webhook.Config{Schema: webhook.Schema, AllowedOrigins: []string{server.URL}, Sinks: []webhook.Sink{{ID: "project-failure", ProjectID: project.ID, NotBefore: time.Now().UTC().Add(-time.Hour), URL: server.URL + "/events", SigningKeyFile: keyPath, EventTypes: []string{"execution.state_changed"}}}}
	load := func(config webhook.Config) *webhook.Config {
		t.Helper()
		raw, _ := json.Marshal(config)
		path := filepath.Join(directory, RandomID()+".json")
		if e := os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
		c, e := webhook.Load(path, true)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(c.Close)
		return c
	}
	config := load(policy)
	dispatcher := &WebhookDispatcher{DB: s.DB, Config: config}
	sink := &config.Sinks[0]
	if err = dispatcher.Bind(ctx); err != nil {
		t.Fatal(err)
	}
	if err = dispatcher.Bind(ctx); err != nil {
		t.Fatal("idempotent binding failed", err)
	}
	var auditCount int
	if err = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM audits WHERE action='notification.webhook.bound'").Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatal("policy binding audit is not atomic/idempotent", err, auditCount)
	}
	if n, e := dispatcher.Enqueue(ctx, sink); e != nil || n != 1 {
		t.Fatal("project/event filtering", n, e)
	}

	var sourceSequence int64
	if err = s.DB.QueryRowContext(ctx, "SELECT sequence FROM exec_outbox WHERE id=$1", eventID).Scan(&sourceSequence); err != nil {
		t.Fatal(err)
	}
	for name, payload := range map[string]string{"extra-metadata": `{"secret":"must not be signed"}`, "forged-project": `{"data":{"project_id":"` + other.ID + `"}}`} {
		if _, e := s.DB.ExecContext(ctx, "INSERT INTO notification_webhook_deliveries(sink_id,event_id,sequence,payload)VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", sink.ID, eventID, sourceSequence, payload); e == nil {
			t.Fatal("database accepted invalid public projection", name)
		}
	}
	if _, e := s.DB.ExecContext(ctx, "UPDATE notification_webhook_deliveries SET state='delivered',http_status=204,delivered_at=now() WHERE event_id=$1", eventID); e == nil {
		t.Fatal("unclaimed event was marked delivered")
	}
	if n, e := dispatcher.Enqueue(ctx, sink); e != nil || n != 0 {
		t.Fatal("outbox scan duplicated deliveries", n, e)
	}
	var published int
	if err = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM exec_outbox WHERE published_at IS NOT NULL").Scan(&published); err != nil || published != 0 {
		t.Fatal("notification stole scheduler outbox", err)
	}
	assertState := func(id, want string, attempts int) {
		t.Helper()
		var state string
		var count int
		if e := s.DB.QueryRowContext(ctx, "SELECT state,attempts FROM notification_webhook_deliveries WHERE sink_id=$1 AND event_id=$2", sink.ID, id).Scan(&state, &count); e != nil || state != want || count != attempts {
			t.Fatalf("state=%s/%d expected=%s/%d error=%v", state, count, want, attempts, e)
		}
	}
	due := func(id string) {
		t.Helper()
		if _, e := s.DB.ExecContext(ctx, "UPDATE notification_webhook_deliveries SET next_attempt_at=now()-interval '1 second' WHERE sink_id=$1 AND event_id=$2", sink.ID, id); e != nil {
			t.Fatal(e)
		}
	}
	deliver := func() {
		t.Helper()
		if claimed, e := dispatcher.DeliverOne(ctx, sink); e != nil || !claimed {
			t.Fatal("delivery did not complete", claimed, e)
		}
	}
	deliver()
	assertState(eventID, "pending", 1)
	if claimed, e := dispatcher.DeliverOne(ctx, sink); e != nil || claimed {
		t.Fatal("bounded retry delay was bypassed", claimed, e)
	}
	status.Store(204)
	due(eventID)
	deliver()
	assertState(eventID, "delivered", 2)
	if claimed, e := dispatcher.DeliverOne(ctx, sink); e != nil || claimed {
		t.Fatal("successful event replayed", claimed, e)
	}
	if calls.Load() != 2 {
		t.Fatal("unexpected HTTP sends", calls.Load())
	}
	if _, err = s.DB.ExecContext(ctx, "UPDATE notification_webhook_deliveries SET payload='{}' WHERE event_id=$1", eventID); err == nil {
		t.Fatal("terminal evidence was mutable")
	}
	if _, err = s.DB.ExecContext(ctx, "DELETE FROM notification_webhook_deliveries WHERE event_id=$1", eventID); err == nil {
		t.Fatal("terminal evidence was deletable")
	}
	changed := policy
	changed.Sinks = append([]webhook.Sink{}, policy.Sinks...)
	changed.Sinks[0].URL += "/other"
	changedDispatcher := &WebhookDispatcher{DB: s.DB, Config: load(changed)}
	if changedDispatcher.Bind(ctx) == nil {
		t.Fatal("same sink ID was silently retargeted")
	}
	// Expired claim recovers without a false delivered marker; original ID stays stable.
	recovery := seedEvent(execution, "execution.state_changed", `{"state":"warning"}`)
	if n, e := dispatcher.Enqueue(ctx, sink); e != nil || n != 1 {
		t.Fatal("new event enqueue failed", n, e)
	}
	if _, err = s.DB.ExecContext(ctx, "UPDATE notification_webhook_deliveries SET state='delivering',attempts=1,lease_token=$3,lease_until=now()-interval '1 second' WHERE sink_id=$1 AND event_id=$2", sink.ID, recovery, RandomID()); err != nil {
		t.Fatal(err)
	}
	deliver()
	assertState(recovery, "delivered", 2)
	// Simultaneous consumers cannot claim an active delivery, even while HTTP waits.
	concurrent := seedEvent(execution, "execution.state_changed", `{"state":"success"}`)
	if n, e := dispatcher.Enqueue(ctx, sink); e != nil || n != 1 {
		t.Fatal("new event enqueue failed", n, e)
	}
	block.Store(true)
	finished := make(chan error, 1)
	go func() { _, e := dispatcher.DeliverOne(ctx, sink); finished <- e }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("receiver did not observe the actual request")
	}
	if claimed, e := dispatcher.DeliverOne(ctx, sink); e != nil || claimed {
		t.Error("active claim was duplicated", claimed, e)
	}
	close(release)
	if e := <-finished; e != nil {
		t.Fatal(e)
	}
	block.Store(false)
	assertState(concurrent, "delivered", 1)
	exhausted := seedEvent(execution, "execution.state_changed", `{"state":"failed"}`)
	if n, e := dispatcher.Enqueue(ctx, sink); e != nil || n != 1 {
		t.Fatal("new event enqueue failed", n, e)
	}
	status.Store(503)
	for attempt := 1; attempt <= 6; attempt++ {
		if attempt > 1 {
			due(exhausted)
		}
		deliver()
		want := "pending"
		if attempt == 6 {
			want = "dead"
		}
		assertState(exhausted, want, attempt)
	}
	if claimed, e := dispatcher.DeliverOne(ctx, sink); e != nil || claimed {
		t.Fatal("exhausted event retried again", claimed, e)
	}
	rejected := seedEvent(execution, "execution.state_changed", `{"state":"failed"}`)
	if n, e := dispatcher.Enqueue(ctx, sink); e != nil || n != 1 {
		t.Fatal("new event enqueue failed", n, e)
	}
	status.Store(403)
	deliver()
	assertState(rejected, "dead", 1)
	page, err := s.ReadWebhookDeliveries(ctx, project.ID, 2, 0)
	if err != nil || page.Total != 5 || len(page.Items) != 2 {
		t.Fatal("bounded owner status page", page, err)
	}
	empty, err := s.ReadWebhookDeliveries(ctx, other.ID, 10, 0)
	if err != nil || empty.Total != 0 {
		t.Fatal("status leaked across projects", empty, err)
	}
	t.Run("project_status_current_authority_and_redaction", func(t *testing.T) {
		user, e := s.ControlCreateUser(ctx, owner, CreateUserInput{Username: "notification-project-admin", DisplayName: "Project Admin", Password: password}, "notification-reader")
		if e != nil {
			t.Fatal(e)
		}
		_, reader := testLogin(t, s, user.Username, password, "198.51.100.191")
		_, e = s.ListProjectWebhookDeliveries(ctx, reader, project.ID, Pagination{1, 2})
		assertErrorCode(t, e, "NOT_FOUND")
		member, e := s.ControlCreateMember(ctx, owner, project.ID, CreateMemberInput{Username: user.Username, Role: "Viewer"}, "notification-member")
		if e != nil {
			t.Fatal(e)
		}
		_, reader = testLogin(t, s, user.Username, password, "198.51.100.191")
		_, e = s.ListProjectWebhookDeliveries(ctx, reader, project.ID, Pagination{1, 2})
		assertErrorCode(t, e, "FORBIDDEN")
		member, e = s.ControlChangeMember(ctx, owner, project.ID, user.ID, UpdateMemberInput{Role: "Admin", Version: member.Version}, false, "notification-promote")
		if e != nil {
			t.Fatal(e)
		}
		// Refresh after the authorized membership update; subsequent reads must
		// still consult current membership, never this session's cached roles.
		_, reader = testLogin(t, s, user.Username, password, "198.51.100.191")
		visible, e := s.ListProjectWebhookDeliveries(ctx, reader, project.ID, Pagination{1, 2})
		if e != nil || visible.Total != 5 || len(visible.Items) != 2 || visible.Page != 1 || visible.PageSize != 2 {
			t.Fatal("authorized project status", visible, e)
		}
		for _, item := range visible.Items {
			if item.ExecutionID != execution || item.EventType != "execution.state_changed" {
				t.Fatal("wrong source event projection", item)
			}
		}
		raw, e := json.Marshal(visible)
		if e != nil || bytes.Contains(raw, []byte(server.URL)) || bytes.Contains(raw, []byte("untrusted_metadata")) || bytes.Contains(raw, []byte("payload")) || bytes.Contains(raw, key) {
			t.Fatal("status exposed private delivery data", e)
		}
		next, e := s.ListProjectWebhookDeliveries(ctx, reader, project.ID, Pagination{2, 2})
		if e != nil || len(next.Items) != 2 || next.Items[0].EventID == visible.Items[0].EventID {
			t.Fatal("status pagination", next, e)
		}
		_, e = s.ListProjectWebhookDeliveries(ctx, reader, other.ID, Pagination{1, 20})
		assertErrorCode(t, e, "NOT_FOUND")
		empty, e := s.ListProjectWebhookDeliveries(ctx, owner, other.ID, Pagination{1, 20})
		if e != nil || empty.Total != 0 || len(empty.Items) != 0 {
			t.Fatal("foreign-project delivery leaked", empty, e)
		}
		_, e = s.ListProjectWebhookDeliveries(ctx, owner, project.ID, Pagination{0, 20})
		assertErrorCode(t, e, "VALIDATION_ERROR")
		_, e = s.ControlChangeMember(ctx, owner, project.ID, user.ID, UpdateMemberInput{Role: "Viewer", Version: member.Version}, false, "notification-demote")
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.ListProjectWebhookDeliveries(ctx, reader, project.ID, Pagination{1, 2})
		assertErrorCode(t, e, "UNAUTHENTICATED")
		_, reader = testLogin(t, s, user.Username, password, "198.51.100.191")
		_, e = s.ListProjectWebhookDeliveries(ctx, reader, project.ID, Pagination{1, 2})
		assertErrorCode(t, e, "FORBIDDEN")
		if e = s.Logout(ctx, reader, "notification-reader-logout"); e != nil {
			t.Fatal(e)
		}
		_, e = s.ListProjectWebhookDeliveries(ctx, reader, project.ID, Pagination{1, 2})
		assertErrorCode(t, e, "UNAUTHENTICATED")
	})
	if binary := os.Getenv("KEEL_TEST_P1_ADMIN_BIN"); binary != "" {
		command := exec.CommandContext(ctx, binary, "webhook-delivery-status", "--project", project.ID, "--limit", "2")
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "KEEL_") {
				command.Env = append(command.Env, value)
			}
		}
		command.Env = append(command.Env, "KEEL_DATABASE_URL="+os.Getenv("KEEL_TEST_DATABASE_URL"))
		raw, e := command.Output()
		if e != nil {
			t.Fatal("actual webhook status CLI failed", e)
		}
		var actual WebhookDeliveryPage
		if e = json.Unmarshal(raw, &actual); e != nil || actual.ProjectID != project.ID || actual.Total != 5 || len(actual.Items) != 2 || bytes.Contains(raw, []byte(server.URL)) || bytes.Contains(raw, []byte("payload")) {
			t.Fatal("actual status CLI metadata boundary", e)
		}
		t.Log("P1_LIFECYCLE_WEBHOOK_CLI_OK")
	}
	role := "keel_test_webhook_reader_" + RandomID()[:12]
	if _, err = s.DB.ExecContext(ctx, "CREATE ROLE "+role+"; GRANT USAGE ON SCHEMA public TO "+role+"; GRANT SELECT ON notification_webhook_deliveries,notification_webhook_policies TO "+role); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = s.DB.ExecContext(context.Background(), "DROP OWNED BY "+role+"; DROP ROLE "+role) })
	parsed, err := url.Parse(os.Getenv("KEEL_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("options", "-crole="+role)
	parsed.RawQuery = query.Encode()
	restricted, err := Open(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer restricted.Close()
	if _, err = restricted.ReadWebhookDeliveries(ctx, project.ID, 10, 0); err == nil {
		t.Fatal("non-owner bypassed status maintenance boundary")
	}

	t.Run("provider_adapters_share_durable_project_outbox", func(t *testing.T) {
		for _, format := range []string{"resend", "slack"} {
			t.Run(format, func(t *testing.T) {
				var calls atomic.Int64
				hmacKey := bytes.Repeat([]byte{0x62}, 32)
				provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls.Add(1)
					body, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
					if err != nil {
						t.Error(err)
					}
					if err = webhook.Verify(hmacKey, r.Header.Get("X-Keel-Timestamp"), r.Header.Get("X-Keel-Event-ID"), r.Header.Get("X-Keel-Signature"), body, time.Now()); err != nil {
						t.Error("provider wire signature", err)
					}
					if bytes.Contains(body, []byte("private_metadata")) || bytes.Contains(body, []byte("private_token")) || !bytes.Contains(body, []byte(project.ID)) {
						t.Error("provider public projection boundary")
					}
					if format == "resend" {
						if r.Header.Get("Authorization") != "Bearer re_private_token_1234567890" || r.Header.Get("Idempotency-Key") == "" {
							t.Error("mail provider authority")
						}
						_, _ = io.WriteString(w, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`)
					} else {
						_, _ = io.WriteString(w, "ok")
					}
				}))
				defer provider.Close()
				dir := t.TempDir()
				keyFile, tokenFile := filepath.Join(dir, "signing"), filepath.Join(dir, "provider")
				if err := os.WriteFile(keyFile, hmacKey, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(tokenFile, []byte("re_private_token_1234567890"), 0600); err != nil {
					t.Fatal(err)
				}
				var lower time.Time
				if err := s.DB.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&lower); err != nil {
					t.Fatal(err)
				}
				sink := webhook.Sink{ID: "provider-" + format, ProjectID: project.ID, NotBefore: lower, EventTypes: []string{"execution.authorization_blocked"}, Format: format, SigningKeyFile: keyFile}
				if format == "resend" {
					sink.URL = provider.URL + "/emails"
					sink.APIKeyFile = tokenFile
					sink.Email = &webhook.EmailDestination{From: "keel@example.com", To: []string{"ops@example.com"}}
				} else {
					sink.URL = provider.URL + "/services/T1/B1/fixture"
				}
				configFile := filepath.Join(dir, "config.json")
				settings := webhook.Config{Schema: webhook.Schema, AllowedOrigins: []string{provider.URL}, Sinks: []webhook.Sink{sink}}
				writeConfig := func() *webhook.Config {
					t.Helper()
					raw, err := json.Marshal(settings)
					if err != nil {
						t.Fatal(err)
					}
					if err = os.WriteFile(configFile, raw, 0600); err != nil {
						t.Fatal(err)
					}
					loaded, err := webhook.Load(configFile, true)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(loaded.Close)
					return loaded
				}
				config := writeConfig()
				dispatcher := &WebhookDispatcher{DB: s.DB, Config: config}
				if err := dispatcher.Bind(ctx); err != nil {
					t.Fatal(err)
				}
				expectedEvent := seedEvent(execution, "execution.authorization_blocked", `{"state":"failed","private_metadata":"never-forward"}`)
				seedEvent(foreignExecution, "execution.authorization_blocked", `{"state":"failed"}`)
				if n, err := dispatcher.Enqueue(ctx, &config.Sinks[0]); err != nil || n != 1 {
					t.Fatal("provider enqueue isolation", n, err)
				}
				if sent, err := dispatcher.DeliverOne(ctx, &config.Sinks[0]); err != nil || !sent {
					t.Fatal("provider dispatch", sent, err)
				}
				if sent, err := dispatcher.DeliverOne(ctx, &config.Sinks[0]); err != nil || sent || calls.Load() != 1 {
					t.Fatal("terminal provider event replayed", sent, err)
				}
				var state, recordedFormat string
				var attempts, status int
				if err := s.DB.QueryRowContext(ctx, `SELECT d.state,d.attempts,d.http_status,p.delivery_format FROM notification_webhook_deliveries d JOIN notification_webhook_policies p ON p.sink_id=d.sink_id WHERE d.sink_id=$1 AND d.event_id=$2`, sink.ID, expectedEvent).Scan(&state, &attempts, &status, &recordedFormat); err != nil || state != "delivered" || attempts != 1 || status != 200 || recordedFormat != format {
					t.Fatal("provider durable acknowledgement", state, attempts, status, recordedFormat, err)
				}
				if format == "resend" {
					settings.Sinks[0].Email.To = []string{"another@example.com"}
				} else {
					settings.Sinks[0].URL = provider.URL + "/services/T1/B2/different"
				}
				changed := &WebhookDispatcher{DB: s.DB, Config: writeConfig()}
				if err := changed.Bind(ctx); err == nil {
					t.Fatal("provider destination changed under an existing immutable sink ID")
				}
			})
		}
	})

}
