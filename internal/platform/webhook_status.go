package platform

import (
	"context"
	"database/sql"
	"time"
)

type WebhookDeliverySummary struct {
	Format        string     `json:"format"`
	SinkID        string     `json:"sink_id"`
	ExecutionID   string     `json:"execution_id"`
	EventType     string     `json:"event_type"`
	EventID       string     `json:"event_id"`
	State         string     `json:"state"`
	Attempts      int        `json:"attempts"`
	HTTPStatus    *int       `json:"http_status,omitempty"`
	LastError     *string    `json:"last_error,omitempty"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	DeliveredAt   *time.Time `json:"delivered_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}
type WebhookDeliveryPage struct {
	ProjectID string                   `json:"project_id"`
	Total     int                      `json:"total"`
	Limit     int                      `json:"limit"`
	Offset    int                      `json:"offset"`
	Items     []WebhookDeliverySummary `json:"items"`
}

// ReadWebhookDeliveries is an owner maintenance read, not a project-role bypass
// for an HTTP endpoint. Destination URLs, payloads and signing keys stay private.
func (s *Store) ReadWebhookDeliveries(ctx context.Context, project string, limit, offset int) (*WebhookDeliveryPage, error) {
	if !modelIDPattern.MatchString(project) || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return nil, apiError(422, "VALIDATION_ERROR", "Valid project ID and bounded pagination required")
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireDatabaseOwner(ctx, tx); err != nil {
		return nil, err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM projects WHERE id=$1)", project).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	out, err := readWebhookDeliveriesTx(ctx, tx, project, limit, offset)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListProjectWebhookDeliveries uses the same current-session and project Admin
// boundary as audit reads. The owner maintenance entry point stays separate.
func (s *Store) ListProjectWebhookDeliveries(ctx context.Context, a *AuthSession, project string, p Pagination) (Page[WebhookDeliverySummary], error) {
	out := NewPage[WebhookDeliverySummary](p)
	if err := p.Validate(); err != nil {
		return out, err
	}
	if !modelIDPattern.MatchString(project) {
		return out, ErrNotFound
	}
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, project); err != nil {
		return out, err
	}
	rows, err := readWebhookDeliveriesTx(ctx, tx, project, p.PageSize, p.Offset())
	if err != nil {
		return out, err
	}
	out.Items, out.Total = rows.Items, rows.Total
	return out, tx.Commit()
}

func readWebhookDeliveriesTx(ctx context.Context, tx *sql.Tx, project string, limit, offset int) (*WebhookDeliveryPage, error) {
	out := &WebhookDeliveryPage{ProjectID: project, Limit: limit, Offset: offset, Items: []WebhookDeliverySummary{}}
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM notification_webhook_deliveries d JOIN notification_webhook_policies p ON p.sink_id=d.sink_id WHERE p.project_id=$1", project).Scan(&out.Total); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.delivery_format,d.sink_id,o.execution_id,o.event_type,d.event_id,d.state,d.attempts,d.http_status,d.last_error,d.next_attempt_at,d.delivered_at,d.updated_at
 FROM notification_webhook_deliveries d JOIN notification_webhook_policies p ON p.sink_id=d.sink_id
 JOIN exec_outbox o ON o.id=d.event_id
 WHERE p.project_id=$1 ORDER BY d.updated_at DESC,d.sink_id,d.event_id LIMIT $2 OFFSET $3`, project, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item WebhookDeliverySummary
		if err = rows.Scan(&item.Format, &item.SinkID, &item.ExecutionID, &item.EventType, &item.EventID, &item.State, &item.Attempts, &item.HTTPStatus, &item.LastError, &item.NextAttemptAt, &item.DeliveredAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
