package platform

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const mediaReadAttempts = 3

type mediaIntegrityFailure struct {
	cause                                            error
	mediaID, objectVersion, expectedSHA, observedSHA string
	expectedSize, observedSize                       int64
}

func (e *mediaIntegrityFailure) Error() string { return e.cause.Error() }
func (e *mediaIntegrityFailure) Unwrap() error { return e.cause }

func mediaIntegrityGate(ctx context.Context, tx *sql.Tx, id string, lock bool) error {
	query := "SELECT quarantined_at IS NOT NULL FROM media_artifact_integrity WHERE media_id=$1"
	if lock {
		query += " FOR SHARE"
	}
	var quarantined bool
	if err := tx.QueryRowContext(ctx, query, id).Scan(&quarantined); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return apiError(503, "MEDIA_INTEGRITY_UNAVAILABLE", "Pinned artifact integrity state is unavailable")
		}
		return err
	}
	if quarantined {
		return apiError(422, "MEDIA_ARTIFACT_QUARANTINED", "Repeated digest failures quarantined this artifact; register and review a new version")
	}
	return nil
}

// RecordMediaIntegrityFailure must run AFTER the enclosing business transaction
// rolls back. Its independent commit must survive the failed approval/download/
// execution and must never wait on the same transaction's lifecycle locks.
// The observation can only be constructed by verified, authorized read paths.
func (s *Store) RecordMediaIntegrityFailure(ctx context.Context, err error, actor, trace string) error {
	var failure *mediaIntegrityFailure
	if !errors.As(err, &failure) {
		return err
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return apiError(503, "MEDIA_INTEGRITY_UNAVAILABLE", "Could not persist artifact quarantine")
	}
	defer tx.Rollback()
	result, e := tx.ExecContext(ctx, `UPDATE media_artifact_integrity SET quarantined_at=$2,reason='digest_mismatch',observed_sha256=$3,observed_size=$4,mismatch_count=3 WHERE media_id=$1 AND quarantined_at IS NULL`, failure.mediaID, time.Now().UTC(), failure.observedSHA, failure.observedSize)
	if e != nil {
		return apiError(503, "MEDIA_INTEGRITY_UNAVAILABLE", "Could not persist artifact quarantine")
	}
	changed, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if changed > 0 {
		var scope string
		if e = tx.QueryRowContext(ctx, "SELECT scope_key FROM media_items WHERE id=$1", failure.mediaID).Scan(&scope); e != nil {
			return e
		}
		if e = appendAudit(ctx, tx, actor, projectIDFromScope(scope), "media.quarantined", failure.mediaID, trace, map[string]any{"reason": "digest_mismatch", "attempts": mediaReadAttempts, "object_version": failure.objectVersion, "expected_sha256": failure.expectedSHA, "observed_sha256": failure.observedSHA, "expected_size": failure.expectedSize, "observed_size": failure.observedSize}); e != nil {
			return e
		}
	} else {
		// Never report quarantine as persisted if its integrity row disappeared.
		var quarantined bool
		if e = tx.QueryRowContext(ctx, "SELECT quarantined_at IS NOT NULL FROM media_artifact_integrity WHERE media_id=$1", failure.mediaID).Scan(&quarantined); e != nil || !quarantined {
			return apiError(503, "MEDIA_INTEGRITY_UNAVAILABLE", "Could not persist artifact quarantine")
		}
	}
	if e = tx.Commit(); e != nil {
		return apiError(503, "MEDIA_INTEGRITY_UNAVAILABLE", "Could not commit artifact quarantine")
	}
	return err
}

func mediaRetryWait(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt) * 100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
