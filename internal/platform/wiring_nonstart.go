package platform

import (
	"context"
	"database/sql"
	"github.com/enzoOvOyu/keel/internal/wiring"
)

// RecordWiringNonstart persists a local tombstone atomically against the start
// intent. Existing ambiguous host work is rejected by the database guard.
func (s *Store) RecordWiringNonstart(ctx context.Context, b wiring.ReadbackBinding, code string) error {
	if !s.WiringEnabled {
		return wiring.ErrCaptureRejected
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if e = lockKey(ctx, tx, "wiring-command:"+b.CommandID); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO validation_wiring_nonstarts(command_id,attempt_id,execution_id,fencing_token,code) VALUES($1,$2,$3,$4,$5) ON CONFLICT(command_id) DO NOTHING`, b.CommandID, b.AttemptID, b.ExecutionID, b.FencingToken, code)
	if e != nil {
		return e
	}
	var original string
	if e = tx.QueryRowContext(ctx, `SELECT code FROM validation_wiring_nonstarts WHERE command_id=$1 AND attempt_id=$2 AND execution_id=$3 AND fencing_token=$4`, b.CommandID, b.AttemptID, b.ExecutionID, b.FencingToken).Scan(&original); e != nil {
		return e
	}
	if original != code {
		return apiError(409, "WIRING_EVIDENCE_CONFLICT", "Local nonstart reason is immutable")
	}
	return tx.Commit()
}

// WiringOriginalCommand is internal recovery input, not a public evidence route.
func (s *Store) WiringOriginalCommand(ctx context.Context, b wiring.ReadbackBinding) (*WiringCommand, error) {
	tx, e := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	out, e := loadWiringCommandTx(ctx, tx, b)
	if e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
