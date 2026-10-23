package platform

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// ZTPCredentialIssue is a simulated reachability observation, not a Vault or device probe.
type ZTPCredentialIssue struct {
	ID                 string    `json:"id"`
	SessionID          string    `json:"session_id"`
	SwitchID           string    `json:"switch_id"`
	CredentialBatchRef string    `json:"credential_batch_ref"`
	Verdict            string    `json:"verdict"`
	Attempt            int       `json:"attempt"`
	OccurredAt         time.Time `json:"occurred_at"`
}

func simSwitchCredentialRef(ctx context.Context, tx *sql.Tx, projectID, switchID string) (string, error) {
	var ref sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT r.row_snapshot->'values'->>'credential_batch_ref' FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid WHERE r.item_uid=$1 AND b.category='switch' AND r.project_id=$2 ORDER BY r.recorded_at DESC,r.package_id DESC LIMIT 1`, switchID, projectID).Scan(&ref)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return ref.String, nil
}

func simCredentialRegistered(ctx context.Context, tx *sql.Tx, projectID, ref string) (bool, error) {
	var scope string
	err := tx.QueryRowContext(ctx, `SELECT scope FROM bom_credential_batches WHERE project_id=$1 AND credential_batch_ref=$2`, projectID, ref).Scan(&scope)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return scope == "switch", err
}

func insertZTPCredentialIssueTx(ctx context.Context, tx *sql.Tx, projectID string, session *ZTPSessionDetail, ref, verdict string, now time.Time) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ztp_credential_issues(id,project_id,session_id,switch_id,credential_batch_ref,verdict,attempt,occurred_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, RandomID(), projectID, session.ID, session.SwitchID, ref, verdict, session.Attempt, now)
	return err
}

func (s *Store) ListZTPCredentialIssues(ctx context.Context, a *AuthSession, project *ProjectSummary) ([]ZTPCredentialIssue, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,session_id,switch_id,credential_batch_ref,verdict,attempt,occurred_at FROM ztp_credential_issues WHERE project_id=$1 ORDER BY occurred_at DESC,id DESC LIMIT 100`, project.ID)
	if err != nil {
		return nil, err
	}
	out := []ZTPCredentialIssue{}
	for rows.Next() {
		var issue ZTPCredentialIssue
		if err = rows.Scan(&issue.ID, &issue.SessionID, &issue.SwitchID, &issue.CredentialBatchRef, &issue.Verdict, &issue.Attempt, &issue.OccurredAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, issue)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
