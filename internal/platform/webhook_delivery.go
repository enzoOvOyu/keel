package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/webhook"
)

type WebhookDispatcher struct {
	DB     *sql.DB
	Config *webhook.Config
}

// Bind verifies each immutable deployment policy before any network delivery.
// Changing project, destination, key or lower time bound requires a new sink ID.
func (d *WebhookDispatcher) Bind(ctx context.Context) error {
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i := range d.Config.Sinks {
		s := &d.Config.Sinks[i]
		types, e := json.Marshal(s.EventTypes)
		if e != nil {
			return e
		}
		inserted, e := tx.ExecContext(ctx, "INSERT INTO notification_webhook_policies(sink_id,project_id,policy_sha256,event_types,not_before,delivery_format) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING", s.ID, s.ProjectID, s.PolicyHash(), types, s.NotBefore, s.DeliveryFormat())
		if e != nil {
			return e
		}
		count, e := inserted.RowsAffected()
		if e != nil {
			return e
		}
		if count == 1 {
			if e = appendAudit(ctx, tx, "", s.ProjectID, "notification.webhook.bound", s.ID, "worker", map[string]any{"policy_sha256": s.PolicyHash(), "key_id": s.KeyID(), "delivery_format": s.DeliveryFormat()}); e != nil {
				return e
			}
		}
		var project, hash string
		var matching bool
		if err = tx.QueryRowContext(ctx, "SELECT project_id,policy_sha256,event_types=$2::jsonb AND not_before=$3 AND delivery_format=$4 FROM notification_webhook_policies WHERE sink_id=$1", s.ID, types, s.NotBefore, s.DeliveryFormat()).Scan(&project, &hash, &matching); err != nil {
			return err
		}
		if project != s.ProjectID || hash != s.PolicyHash() || !matching {
			return errors.New("webhook sink identity was previously bound to a different policy")
		}
	}
	return tx.Commit()
}

// Enqueue consumes a public projection from the same transactionally committed
// outbox as the execution scheduler. It never changes scheduler publication.
func (d *WebhookDispatcher) Enqueue(ctx context.Context, s *webhook.Sink) (int, error) {
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// JSON arrays avoid constructing SQL from configured event-type strings.
	types, _ := json.Marshal(s.EventTypes)
	rows, err := tx.QueryContext(ctx, `WITH events AS(
 SELECT o.id,o.sequence,o.execution_id,o.aggregate_version,o.event_type,o.payload->>'state' AS state,o.created_at,coalesce(o.payload->>'issue_id','') AS issue_id,''::text AS alert_id,''::text AS severity,''::text AS observation_source,e.project_id FROM exec_outbox o JOIN exec_executions e ON e.id=o.execution_id
 UNION ALL SELECT id,sequence,'',aggregate_version,event_type,NULL,created_at,coalesce(issue_id,''),alert_id,severity,source,project_id FROM monitor_notification_outbox
 )SELECT o.id,o.sequence,o.execution_id,o.aggregate_version,o.event_type,o.state,o.created_at,o.issue_id,o.alert_id,o.severity,o.observation_source FROM events o
 WHERE o.project_id=$1 AND o.created_at >= $2 AND o.event_type IN(SELECT jsonb_array_elements_text($3::jsonb))
 AND NOT EXISTS(SELECT 1 FROM notification_webhook_deliveries n WHERE n.sink_id=$4 AND n.event_id=o.id)
 ORDER BY o.sequence LIMIT 100`, s.ProjectID, s.NotBefore, types, s.ID)
	if err != nil {
		return 0, err
	}
	type pending struct {
		id       string
		sequence int64
		body     []byte
	}
	list := []pending{}
	for rows.Next() {
		var id, eid, kind, issueID, alertID, severity, observationSource string
		var sequence, version int64
		var state sql.NullString
		var at time.Time
		if err = rows.Scan(&id, &sequence, &eid, &version, &kind, &state, &at, &issueID, &alertID, &severity, &observationSource); err != nil {
			rows.Close()
			return 0, err
		}
		data := map[string]any{"project_id": s.ProjectID, "execution_id": eid, "aggregate_version": version}
		if state.Valid {
			switch state.String {
			case "created", "scheduled", "dispatching", "running", "pausing", "paused", "cancelling", "reconciling", "success", "failed", "killed", "warning":
				data["state"] = state.String
			}
		}
		subject := "execution/" + eid
		if kind == "issue.escalated" {
			data["issue_id"] = issueID
			data["failed_verifications"] = 2
			subject = "issue/" + issueID
		}
		if kind == "monitor.alert" {
			data["alert_id"] = alertID
			data["severity"] = severity
			data["observation_source"] = observationSource
			data["issue_id"] = issueID
			subject = "alert/" + alertID
		}
		body, e := json.Marshal(map[string]any{"specversion": "1.0", "id": id, "source": "urn:keel:project:" + s.ProjectID, "type": kind, "subject": subject, "time": at.UTC(), "datacontenttype": "application/json", "data": data})
		if e != nil {
			rows.Close()
			return 0, e
		}
		list = append(list, pending{id, sequence, body})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range list {
		result, e := tx.ExecContext(ctx, "INSERT INTO notification_webhook_deliveries(sink_id,event_id,sequence,payload) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING", s.ID, item.id, item.sequence, item.body)
		if e != nil {
			return 0, e
		}
		n, _ := result.RowsAffected()
		count += int(n)
	}
	return count, tx.Commit()
}

// DeliverOne holds a short SQL claim, releases it for HTTP, then records by
// lease-token CAS. Unknown HTTP results retry with the same external event ID.
func (d *WebhookDispatcher) DeliverOne(ctx context.Context, s *webhook.Sink) (bool, error) {
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var id string
	var body []byte
	var attempts int
	err = tx.QueryRowContext(ctx, `SELECT event_id,payload,attempts FROM notification_webhook_deliveries
 WHERE sink_id=$1 AND state IN('pending','delivering') AND next_attempt_at<=now()
 AND (lease_until IS NULL OR lease_until<=now()) ORDER BY sequence LIMIT 1 FOR UPDATE SKIP LOCKED`, s.ID).Scan(&id, &body, &attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if attempts >= 6 {
		_, err = tx.ExecContext(ctx, "UPDATE notification_webhook_deliveries SET state='dead',lease_token=NULL,lease_until=NULL,last_error='attempts_exhausted',updated_at=now() WHERE sink_id=$1 AND event_id=$2", s.ID, id)
		if err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	token := RandomID()
	attempts++
	if _, err = tx.ExecContext(ctx, "UPDATE notification_webhook_deliveries SET state='delivering',attempts=$3,lease_token=$4,lease_until=now()+interval '30 seconds',updated_at=now() WHERE sink_id=$1 AND event_id=$2", s.ID, id, attempts, token); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	callContext, cancel := context.WithTimeout(ctx, 8*time.Second)
	status, retry, sendErr := d.Config.Deliver(callContext, s, id, body)
	cancel()
	// Cancellation leaves a finite claim for recovery instead of writing a false acknowledgement.
	if ctx.Err() != nil {
		return true, ctx.Err()
	}
	state, reason := "delivered", ""
	if sendErr != nil || status < 200 || status >= 300 {
		state = "dead"
		reason = "http_rejected"
		if errors.Is(sendErr, webhook.ErrDeliveryWindowExpired) {
			reason = "event_expired"
		} else if sendErr != nil && !errors.Is(sendErr, webhook.ErrProviderRejected) {
			reason = "transport_unknown"
		}
		if retry && attempts < 6 {
			state = "pending"
		} else if retry {
			reason = "attempts_exhausted"
		}
	}
	delay := 30 * time.Second * time.Duration(1<<uint(attempts-1))
	if delay > 15*time.Minute {
		delay = 15 * time.Minute
	}
	var storedStatus any
	if status >= 100 && status <= 599 {
		storedStatus = status
	}
	result, err := d.DB.ExecContext(ctx, `UPDATE notification_webhook_deliveries SET state=$4::varchar,lease_token=NULL,lease_until=NULL,http_status=$5,last_error=NULLIF($6::varchar,''),
 next_attempt_at=now()+($7::bigint * interval '1 millisecond'),delivered_at=CASE WHEN $4::varchar='delivered' THEN now() ELSE NULL END,updated_at=now()
 WHERE sink_id=$1 AND event_id=$2 AND state='delivering' AND lease_token=$3`, s.ID, id, token, state, storedStatus, reason, delay.Milliseconds())
	if err != nil {
		return true, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return true, err
	}
	if n != 1 {
		return true, errors.New("webhook delivery lease changed")
	}
	return true, nil
}
