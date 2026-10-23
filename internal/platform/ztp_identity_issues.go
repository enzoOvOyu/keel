package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

type ZTPIdentityIssue struct {
	ID           string    `json:"id"`
	BatchRef     string    `json:"batch_ref"`
	BootChannel  string    `json:"boot_channel"`
	IdentityKey  string    `json:"identity_key"`
	SerialNumber string    `json:"serial_number"`
	SourceIP     string    `json:"source_ip,omitempty"`
	Reasons      []string  `json:"reasons"`
	EventCount   int       `json:"event_count"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
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
ON CONFLICT(project_id,batch_ref,boot_channel,identity_key) DO UPDATE SET source_ip=excluded.source_ip,reasons=excluded.reasons,event_count=ztp_identity_issues.event_count+1,last_seen_at=excluded.last_seen_at,last_trace_id=excluded.last_trace_id`, RandomID(), projectID, batchRef, channel, identityKey, serial, sourceIP, raw, now, trace)
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
	rows, err := tx.QueryContext(ctx, `SELECT id,batch_ref,boot_channel,identity_key,serial_number,source_ip,reasons,event_count,first_seen_at,last_seen_at FROM ztp_identity_issues WHERE project_id=$1 ORDER BY last_seen_at DESC,id DESC LIMIT 100`, project.ID)
	if err != nil {
		return nil, err
	}
	out := []ZTPIdentityIssue{}
	for rows.Next() {
		var item ZTPIdentityIssue
		var reasons []byte
		if err = rows.Scan(&item.ID, &item.BatchRef, &item.BootChannel, &item.IdentityKey, &item.SerialNumber, &item.SourceIP, &reasons, &item.EventCount, &item.FirstSeenAt, &item.LastSeenAt); err != nil {
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
