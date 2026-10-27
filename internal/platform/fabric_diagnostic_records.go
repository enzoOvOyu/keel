package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
)

type FabricDiagnosticRecord struct {
	ID          string                   `json:"id"`
	ContentHash string                   `json:"content_hash"`
	PortScope   string                   `json:"port_scope"`
	RecordedAt  time.Time                `json:"recorded_at"`
	Stale       bool                     `json:"stale"`
	Report      *FabricDiagnosticsReport `json:"report"`
	IssueIDs    []string                 `json:"issue_ids"`
}

func diagnosticRecordHash(out *FabricDiagnosticsReport, port string) (string, error) {
	// A read timestamp or repeated run identifier must not renew unchanged evidence.
	copyReport := *out
	copyReport.RecordID = ""
	copyReport.CollectionID = ""
	copyReport.ValidUntil = time.Time{}
	raw, e := json.Marshal(struct {
		Scope  string
		Report FabricDiagnosticsReport
	}{port, copyReport})
	if e != nil {
		return "", e
	}
	return digest(string(raw)), nil
}
func portDiagnosticIssueIDsTx(ctx context.Context, tx *sql.Tx, record string) ([]string, error) {
	out := []string{}
	rows, e := tx.QueryContext(ctx, "SELECT issue_id FROM fabric_port_issue_observations WHERE record_id=$1 ORDER BY issue_id", record)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func recordPortDiagnosticIssuesTx(ctx context.Context, tx *sql.Tx, a *AuthSession, out *FabricDiagnosticsReport, trace string) error {
	for _, port := range out.Ports {
		if !port.InUse {
			continue
		}
		for _, check := range port.Checks {
			if check.State != "anomaly" {
				continue
			}
			rule, revision, ok := strings.Cut(check.RuleRef, "@")
			if !ok || rule == "" || revision == "" || len(rule) > 128 || len(check.Subtype) > 32 || !slices.Contains([]string{"T2", "T3", "T4", "T6"}, check.IssueType) {
				return apiError(409, "FABRIC_DIAGNOSTIC_RULE_INVALID", "A diagnostic anomaly must have a supported category and versioned rule")
			}
			var portID string
			e := tx.QueryRowContext(ctx, `SELECT p.id FROM physical_ports p JOIN bom_item_revisions r ON r.item_uid=p.id JOIN bom_items b ON b.item_uid=p.id WHERE p.switch_id=$1 AND r.package_id=$2 AND b.category='port' AND b.parent_uid=$1 AND b.status<>'retired' AND coalesce(nullif(b.port_name,''),b.port_index,'')=$3`, out.DeviceID, out.PackageID, port.Port).Scan(&portID)
			if errors.Is(e, sql.ErrNoRows) {
				return apiError(409, "FABRIC_DIAGNOSTIC_PORT_UNMAPPED", "Diagnostic port must resolve to a stable BOM port")
			}
			if e != nil {
				return e
			}
			var issueID string
			e = tx.QueryRowContext(ctx, `SELECT issue_id FROM fabric_port_issue_bindings WHERE project_id=$1 AND port_uid=$2 AND category=$3 AND subtype=$4 AND rule_id=$5 AND evidence_origin=$6 FOR UPDATE`, out.ProjectID, portID, check.IssueType, check.Subtype, rule, out.EvidenceSource).Scan(&issueID)
			fresh := errors.Is(e, sql.ErrNoRows)
			if e != nil && !fresh {
				return e
			}
			checkJSON, e := json.Marshal(check)
			if e != nil {
				return e
			}
			if fresh {
				expected, _ := json.Marshal(map[string]any{"rule_ref": check.RuleRef, "threshold_ref": check.ThresholdRef, "policy_hash": out.PolicyHash, "intent_hash": out.IntentHash})
				in := CreateIssueInput{Title: "Port diagnostic: " + check.IssueType + " / " + port.Port, Category: check.IssueType, Source: "fabric", Phase: "delivery", Anchors: []IssueAnchor{{Kind: "switch", ID: out.DeviceID}, {Kind: "port", ID: portID}}, Evidence: IssueEvidence{ObservedAt: *out.CollectedAt, SourceVersion: check.RuleRef, Expected: string(expected), Actual: string(checkJSON)}}
				if e = validIssueCreate(in); e != nil {
					return e
				}
				for _, anchor := range in.Anchors {
					if e = issueAnchorTx(ctx, tx, out.ProjectID, anchor); e != nil {
						return e
					}
				}
				item, err := insertIssueTx(ctx, tx, a, out.ProjectID, in, "fabric-port-diagnostic", trace, map[string]any{"record_id": out.RecordID, "evidence_origin": out.EvidenceSource, "subtype": check.Subtype})
				if err != nil {
					return err
				}
				issueID = item.ID
				if _, e = tx.ExecContext(ctx, `INSERT INTO fabric_port_issue_bindings(issue_id,project_id,port_uid,category,subtype,rule_id,evidence_origin) VALUES($1,$2,$3,$4,$5,$6,$7)`, issueID, out.ProjectID, portID, check.IssueType, check.Subtype, rule, out.EvidenceSource); e != nil {
					return e
				}
			}
			result, e := tx.ExecContext(ctx, `INSERT INTO fabric_port_issue_observations(id,issue_id,record_id,port_name,check_result) VALUES($1,$2,$3,$4,$5) ON CONFLICT(issue_id,record_id) DO NOTHING`, RandomID(), issueID, out.RecordID, port.Port, checkJSON)
			if e != nil {
				return e
			}
			inserted, e := result.RowsAffected()
			if e != nil {
				return e
			}
			if !fresh && inserted > 0 {
				item, err := scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1 FOR UPDATE", issueID))
				if err != nil {
					return err
				}
				state := item.State
				action := "observe"
				if state == "closed" {
					state = "reopened"
					action = "reopen"
				}
				item, e = scanIssue(tx.QueryRowContext(ctx, `UPDATE issues SET version=version+1,state=$2,updated_at=$3 WHERE id=$1 RETURNING `+issueColumns, issueID, state, time.Now().UTC()))
				if e != nil {
					return e
				}
				if e = issueEventTx(ctx, tx, a, item, action, trace, map[string]any{"record_id": out.RecordID, "reason": check.Reason, "rule_ref": check.RuleRef, "subtype": check.Subtype, "evidence_origin": out.EvidenceSource}); e != nil {
					return e
				}
			}
		}
	}
	return nil
}
func (s *Store) RecordFabricDiagnostics(ctx context.Context, a *AuthSession, p *ProjectSummary, device, port, trace string) (*FabricDiagnosticRecord, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:collect"); e != nil {
		return nil, e
	}
	report, e := s.fabricDiagnosticsTx(ctx, tx, p, device, port)
	if e != nil {
		return nil, e
	}
	if report.CollectionID == "" || report.CollectedAt == nil || !report.ValidUntil.After(time.Now()) || !slices.Contains([]string{"device", "protocol_fixture"}, report.EvidenceSource) {
		return nil, apiError(409, "FABRIC_DIAGNOSTIC_EVIDENCE_REQUIRED", "A current scoped collector snapshot is required")
	}
	hash, e := diagnosticRecordHash(report, port)
	if e != nil {
		return nil, e
	}
	out := &FabricDiagnosticRecord{ID: RandomID(), ContentHash: hash, PortScope: port, RecordedAt: time.Now().UTC().Truncate(time.Microsecond), Report: report, IssueIDs: []string{}}
	var prior []byte
	var expiry time.Time
	e = tx.QueryRowContext(ctx, `SELECT id,report,valid_until,recorded_at FROM fabric_diagnostic_records WHERE project_id=$1 AND device_id=$2 AND port_scope=$3 AND evidence_origin=$4 AND content_hash=$5`, p.ID, device, port, report.EvidenceSource, hash).Scan(&out.ID, &prior, &expiry, &out.RecordedAt)
	if e == nil {
		if e = json.Unmarshal(prior, &out.Report); e != nil {
			return nil, e
		}
		out.Stale = !expiry.After(time.Now())
		out.IssueIDs, e = portDiagnosticIssueIDsTx(ctx, tx, out.ID)
		if e != nil {
			return nil, e
		}
		return out, tx.Commit()
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	report.RecordID = out.ID
	raw, e := json.Marshal(report)
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO fabric_diagnostic_records(id,project_id,collection_id,package_id,device_id,port_scope,evidence_origin,content_hash,report,valid_until,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, out.ID, p.ID, report.CollectionID, report.PackageID, device, port, report.EvidenceSource, hash, raw, report.ValidUntil, out.RecordedAt); e != nil {
		return nil, e
	}
	if e = recordPortDiagnosticIssuesTx(ctx, tx, a, report, trace); e != nil {
		return nil, e
	}
	out.IssueIDs, e = portDiagnosticIssueIDsTx(ctx, tx, out.ID)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "fabric.diagnostics.record", out.ID, trace, map[string]any{"content_hash": hash, "device_id": device, "port_scope": port, "state": report.Gate.State, "issues": len(out.IssueIDs)}); e != nil {
		return nil, e
	}
	out.Stale = !report.ValidUntil.After(time.Now())
	return out, tx.Commit()
}
func (s *Store) FabricPortIssueEvidence(ctx context.Context, a *AuthSession, issue string, p Pagination) (Page[FabricDiagnosticRecord], error) {
	out := NewPage[FabricDiagnosticRecord](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var project string
	e = tx.QueryRowContext(ctx, "SELECT project_id FROM issues WHERE id=$1", issue).Scan(&project)
	if errors.Is(e, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if _, e = issueRoleTx(ctx, tx, a, project); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM fabric_port_issue_observations WHERE issue_id=$1", issue).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT r.id,r.content_hash,r.port_scope,r.recorded_at,r.report,(r.valid_until<=now() OR r.package_id IS DISTINCT FROM (SELECT p.id FROM bom_import_packages p WHERE p.project_id=r.project_id AND p.state='committed' AND EXISTS(SELECT 1 FROM bom_item_revisions rev JOIN bom_items b ON b.item_uid=rev.item_uid WHERE rev.package_id=p.id AND b.category='switch') ORDER BY p.committed_at DESC,p.id DESC LIMIT 1)) FROM fabric_port_issue_observations o JOIN fabric_diagnostic_records r ON r.id=o.record_id WHERE o.issue_id=$1 ORDER BY r.recorded_at DESC,r.id DESC LIMIT $2 OFFSET $3`, issue, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v FabricDiagnosticRecord
		var raw []byte
		if e = rows.Scan(&v.ID, &v.ContentHash, &v.PortScope, &v.RecordedAt, &raw, &v.Stale); e != nil {
			rows.Close()
			return out, e
		}
		if e = json.Unmarshal(raw, &v.Report); e != nil {
			rows.Close()
			return out, e
		}
		v.IssueIDs = []string{issue}
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
