package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type ZTPIdentityIssueResolveInput struct {
	Reason string `json:"reason"`
}

type ZTPIdentityIssue struct {
	ID               string     `json:"id"`
	BatchRef         string     `json:"batch_ref"`
	BootChannel      string     `json:"boot_channel"`
	IdentityKey      string     `json:"identity_key"`
	SerialNumber     string     `json:"serial_number"`
	SourceIP         string     `json:"source_ip,omitempty"`
	Reasons          []string   `json:"reasons"`
	EventCount       int        `json:"event_count"`
	State            string     `json:"state"`
	ResolutionReason string     `json:"resolution_reason,omitempty"`
	ResolvedAt       *time.Time `json:"resolved_at,omitempty"`
	FirstSeenAt      time.Time  `json:"first_seen_at"`
	LastSeenAt       time.Time  `json:"last_seen_at"`
}

func ztpIssueIdentityKey(claim ZTPIdentityInput) string {
	if claim.Serial != "" {
		return "sn:" + claim.Serial
	}
	for _, entry := range []struct{ kind, raw string }{{"mgmt", claim.MgmtMAC}, {"base", claim.BaseMAC}} {
		if mac, ok := normalizeMAC(entry.raw); ok {
			return entry.kind + ":" + mac
		}
	}
	for _, raw := range claim.OtherMACs {
		if mac, ok := normalizeMAC(raw); ok {
			return "other:" + mac
		}
	}
	return "unknown:" + strings.TrimSpace(claim.Serial)
}

func upsertZTPIdentityIssueTx(ctx context.Context, tx *sql.Tx, projectID, batchRef, channel, identityKey, serial, sourceIP, trace string, reasons []string) error {
	raw, err := json.Marshal(reasons)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = tx.ExecContext(ctx, `INSERT INTO ztp_identity_issues(id,project_id,batch_ref,boot_channel,identity_key,serial_number,source_ip,reasons,event_count,first_seen_at,last_seen_at,last_trace_id)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,1,$9,$9,$10)
ON CONFLICT(project_id,batch_ref,boot_channel,identity_key) DO UPDATE SET source_ip=excluded.source_ip,reasons=excluded.reasons,event_count=ztp_identity_issues.event_count+1,last_seen_at=excluded.last_seen_at,last_trace_id=excluded.last_trace_id,state='open',resolved_by=NULL,resolved_at=NULL,resolution_reason=NULL`, RandomID(), projectID, batchRef, channel, identityKey, serial, sourceIP, raw, now, trace)
	return err
}

func (s *Store) ListZTPIdentityIssues(ctx context.Context, a *AuthSession, project *ProjectSummary) ([]ZTPIdentityIssue, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,batch_ref,boot_channel,identity_key,serial_number,source_ip,reasons,event_count,state,coalesce(resolution_reason,''),resolved_at,first_seen_at,last_seen_at FROM ztp_identity_issues WHERE project_id=$1 ORDER BY last_seen_at DESC,id DESC LIMIT 100`, project.ID)
	if err != nil {
		return nil, err
	}
	out := []ZTPIdentityIssue{}
	for rows.Next() {
		var item ZTPIdentityIssue
		var reasons []byte
		if err = rows.Scan(&item.ID, &item.BatchRef, &item.BootChannel, &item.IdentityKey, &item.SerialNumber, &item.SourceIP, &reasons, &item.EventCount, &item.State, &item.ResolutionReason, &item.ResolvedAt, &item.FirstSeenAt, &item.LastSeenAt); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(reasons, &item.Reasons); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func (s *Store) ResolveZTPIdentityIssue(ctx context.Context, a *AuthSession, project *ProjectSummary, issueID, reason, trace string) (*ZTPIdentityIssue, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 300 || strings.ContainsAny(reason, "\r\n\x00") {
		return nil, apiError(400, "ZTP_ISSUE_REASON_INVALID", "Resolution reason is required")
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	var state string
	err = tx.QueryRowContext(ctx, `SELECT state FROM ztp_identity_issues WHERE id=$1 AND project_id=$2 FOR UPDATE`, issueID, project.ID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if state != "open" {
		return nil, apiError(409, "ZTP_ISSUE_ALREADY_RESOLVED", "Identity issue is already resolved")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = tx.ExecContext(ctx, `UPDATE ztp_identity_issues SET state='resolved',resolved_by=$2,resolved_at=$3,resolution_reason=$4 WHERE id=$1`, issueID, a.User.ID, now, reason)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "ztp.identity_issue.resolve", issueID, trace, map[string]any{"reason": reason}); err != nil {
		return nil, err
	}
	out := &ZTPIdentityIssue{ID: issueID}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT batch_ref,boot_channel,identity_key,serial_number,source_ip,reasons,event_count,state,coalesce(resolution_reason,''),resolved_at,first_seen_at,last_seen_at FROM ztp_identity_issues WHERE id=$1`, issueID).Scan(&out.BatchRef, &out.BootChannel, &out.IdentityKey, &out.SerialNumber, &out.SourceIP, &raw, &out.EventCount, &out.State, &out.ResolutionReason, &out.ResolvedAt, &out.FirstSeenAt, &out.LastSeenAt)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.Reasons); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
