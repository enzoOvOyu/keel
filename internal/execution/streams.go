package execution

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"

	"github.com/enzoOvOyu/keel/internal/executionqueue"
)

// Queue references deliberately omit the original outbox payload: it can contain
// task output. A consumer must compare every field against PostgreSQL authority.
type streamReference struct {
	SpecVersion string              `json:"specversion"`
	Schema      string              `json:"dataschema"`
	Source      string              `json:"source"`
	ID          string              `json:"id"`
	Type        string              `json:"type"`
	Subject     string              `json:"subject"`
	Time        time.Time           `json:"time"`
	Data        streamReferenceData `json:"data"`
}

type streamReferenceData struct {
	ProjectID        string `json:"project_id"`
	Sequence         int64  `json:"sequence"`
	AggregateVersion int64  `json:"aggregate_version"`
}

var streamIDPattern = regexp.MustCompile(`^[0-9]{1,20}-[0-9]{1,20}$`)

func reference(id, eid, project, kind string, sequence, version int64, at time.Time) streamReference {
	return streamReference{SpecVersion: "1.0", Schema: "urn:keel:execution-reference:v1", Source: "keel/execution",
		ID: id, Type: kind, Subject: eid, Time: at.UTC(), Data: streamReferenceData{ProjectID: project, Sequence: sequence, AggregateVersion: version}}
}

func decodeStreamReference(body string) (streamReference, bool) {
	var ref streamReference
	if len(body) == 0 || len(body) > 2048 {
		return ref, false
	}
	d := json.NewDecoder(bytes.NewBufferString(body))
	d.DisallowUnknownFields()
	if d.Decode(&ref) != nil || d.Decode(new(any)) != io.EOF {
		return ref, false
	}
	return ref, ref.SpecVersion == "1.0" && ref.Schema == "urn:keel:execution-reference:v1" && ref.Source == "keel/execution" &&
		opaqueID.MatchString(ref.ID) && opaqueID.MatchString(ref.Subject) && opaqueID.MatchString(ref.Data.ProjectID) &&
		len(ref.Type) > 0 && len(ref.Type) <= 128 && ref.Data.Sequence > 0 && ref.Data.AggregateVersion > 0 && !ref.Time.IsZero()
}

func sameStreamReference(a, b streamReference) bool {
	return a.SpecVersion == b.SpecVersion && a.Schema == b.Schema && a.Source == b.Source && a.ID == b.ID && a.Type == b.Type &&
		a.Subject == b.Subject && a.Time.Equal(b.Time) && a.Data == b.Data
}

// PumpQueue recovers/consumes existing entries, then publishes committed events.
// No Redis call is made under a PostgreSQL transaction/row lock. When configured,
// a Redis failure is returned rather than silently claiming DB-only queue health.
func (s *Service) PumpQueue(ctx context.Context, worker string, limit int) (int, error) {
	if s.Queue == nil {
		return s.PumpOutbox(ctx, worker, limit)
	}
	if len(worker) < 1 || len(worker) > 120 {
		return 0, invalid("invalid consumer")
	}
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	epoch, err := s.Queue.Ensure(ctx)
	if err != nil {
		return 0, err
	}
	if !opaqueID.MatchString(epoch) {
		return 0, executionqueue.ErrQueue
	}
	// Read first so slow publication or a large rebuild cannot starve PEL and
	// messages already published on an earlier cycle. New entries arrive next tick.
	messages, err := s.Queue.Read(ctx, worker, limit)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, message := range messages {
		if !streamIDPattern.MatchString(message.ID) {
			return count, executionqueue.ErrQueue
		}
		committed, e := s.consumeStreamReference(ctx, epoch, message)
		if e != nil {
			return count, e
		}
		if !committed {
			continue
		} // Busy aggregate: leave PEL for recovery.
		if e = s.Queue.Ack(ctx, epoch, message.ID); e != nil {
			return count, e
		}
		count++
	}
	return count, s.publishStreamReferences(ctx, epoch, limit)
}

func (s *Service) publishStreamReferences(ctx context.Context, epoch string, limit int) error {
	// Retry even within the same epoch. Partial stream loss/approximate trimming
	// may lose unconsumed entries without losing the epoch key or consumer group.
	rows, err := s.DB.QueryContext(ctx, `SELECT o.id,o.execution_id,e.project_id,o.event_type,o.sequence,o.aggregate_version,o.created_at
	 FROM exec_outbox o JOIN exec_executions e ON e.id=o.execution_id
	 LEFT JOIN exec_stream_publications p ON p.event_id=o.id
	 WHERE NOT EXISTS(SELECT 1 FROM exec_consumed_events c WHERE c.consumer=$1 AND c.event_id=o.id)
	 AND (p.event_id IS NULL OR p.stream_epoch<>$2 OR p.last_published_at<now()-interval '2 minutes')
	 ORDER BY o.sequence LIMIT $3`, executionqueue.Group, epoch, limit)
	if err != nil {
		return err
	}
	refs := []streamReference{}
	for rows.Next() {
		var id, eid, project, kind string
		var seq, version int64
		var at time.Time
		if err = rows.Scan(&id, &eid, &project, &kind, &seq, &version, &at); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, reference(id, eid, project, kind, seq, version, at))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, ref := range refs {
		body, e := json.Marshal(ref)
		if e != nil {
			return e
		}
		id, e := s.Queue.Publish(ctx, epoch, string(body))
		if e != nil {
			return e
		}
		if !streamIDPattern.MatchString(id) {
			return executionqueue.ErrQueue
		}
		// Unknown XADD outcome or crash before this write yields a safe duplicate.
		_, e = s.DB.ExecContext(ctx, `INSERT INTO exec_stream_publications(event_id,stream_epoch,stream_id,last_published_at)
		 VALUES($1,$2,$3,now()) ON CONFLICT(event_id) DO UPDATE SET stream_epoch=excluded.stream_epoch,stream_id=excluded.stream_id,last_published_at=excluded.last_published_at`, ref.ID, epoch, id)
		if e != nil {
			return e
		}
	}
	return nil
}

// A true return guarantees either business+dedup commit or durable rejection.
// It never issues commands or alters attempts, resource locks or unknown receipts.
func (s *Service) consumeStreamReference(ctx context.Context, epoch string, message executionqueue.Message) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	reject := func(reason string) (bool, error) {
		_, e := tx.ExecContext(ctx, `INSERT INTO exec_stream_rejections(stream_epoch,stream_id,reason) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, epoch, message.ID, reason)
		if e != nil {
			return false, e
		}
		return true, tx.Commit()
	}
	ref, valid := decodeStreamReference(message.Body)
	if !valid {
		return reject("invalid_reference")
	}
	var eid, project, kind string
	var seq, version int64
	var at time.Time
	err = tx.QueryRowContext(ctx, `SELECT o.execution_id,e.project_id,o.event_type,o.sequence,o.aggregate_version,o.created_at
	 FROM exec_outbox o JOIN exec_executions e ON e.id=o.execution_id WHERE o.id=$1`, ref.ID).Scan(&eid, &project, &kind, &seq, &version, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return reject("unknown_event")
	}
	if err != nil {
		return false, err
	}
	if !sameStreamReference(ref, reference(ref.ID, eid, project, kind, seq, version, at)) {
		return reject("reference_mismatch")
	}
	var consumed bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM exec_consumed_events WHERE consumer=$1 AND event_id=$2)`, executionqueue.Group, ref.ID).Scan(&consumed)
	if err != nil {
		return false, err
	}
	if consumed {
		return true, tx.Commit()
	}
	if kind == "execution.created" {
		var currentVersion int64
		var state, desired string
		err = tx.QueryRowContext(ctx, `SELECT state,state_version,desired_state FROM exec_executions WHERE id=$1 FOR UPDATE SKIP LOCKED`, eid).Scan(&state, &currentVersion, &desired)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		// Compare the locked current aggregate version. A delayed created event
		// must not rewind a newer pause/cancel/terminal/reconciling state.
		if state == "created" && desired == "running" && currentVersion >= version {
			_, err = tx.ExecContext(ctx, `UPDATE exec_executions SET state='scheduled',state_version=state_version+1 WHERE id=$1 AND state='created' AND desired_state='running' AND state_version=$2`, eid, currentVersion)
			if err != nil {
				return false, err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO exec_consumed_events(consumer,event_id,handled_at) VALUES($1,$2,now()) ON CONFLICT DO NOTHING`, executionqueue.Group, ref.ID)
	if err != nil {
		return false, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE exec_outbox SET published_at=COALESCE(published_at,now()) WHERE id=$1`, ref.ID)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}
