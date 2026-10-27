package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/enzoOvOyu/keel/internal/fabricmon"
)

type FabricIssueLink struct {
	CableUID       string `json:"cable_uid"`
	IssueID        string `json:"issue_id"`
	Category       string `json:"category"`
	EvidenceOrigin string `json:"evidence_origin"`
}
type FabricIssueObservation struct {
	ID              string            `json:"id"`
	RunID           string            `json:"run_id"`
	PackageID       string            `json:"package_id"`
	EvidenceOrigin  string            `json:"evidence_origin"`
	ObservationHash string            `json:"observation_hash"`
	Finding         fabricmon.Finding `json:"finding"`
	ObservedAt      time.Time         `json:"observed_at"`
	ValidUntil      time.Time         `json:"valid_until"`
	RecordedAt      time.Time         `json:"recorded_at"`
	Stale           bool              `json:"stale"`
}

// Scope fingerprint ignores unrelated switches and includes the BOM, fixed
// predicate and exact endpoint samples. Re-reading an identical observation
// keeps the existing issue/version, while every persisted run keeps its link.
func fabricIssueObservationKey(report *FabricCollectionReport, cable fabricmon.Cable, finding fabricmon.Finding, samples map[string]fabricmon.Snapshot) (string, time.Time, error) {
	observed := report.CollectedAt
	endpoints := []*fabricmon.Snapshot{}
	present := false
	for _, endpoint := range []fabricmon.Endpoint{cable.A, cable.B} {
		sample, ok := samples[endpoint.Serial]
		if !ok {
			endpoints = append(endpoints, nil)
			continue
		}
		present = true
		copySample := sample
		endpoints = append(endpoints, &copySample)
		if sample.CollectedAt.Before(observed) {
			observed = sample.CollectedAt
		}
	}
	if !present {
		return "", time.Time{}, apiError(409, "FABRIC_ISSUE_EVIDENCE_MISSING", "An observed switch failure is required before opening a network issue")
	}

	raw, e := json.Marshal(struct {
		Package, Origin string
		Finding         fabricmon.Finding
		Samples         []*fabricmon.Snapshot
	}{report.PackageID, report.EvidenceSource, finding, endpoints})
	if e != nil {
		return "", time.Time{}, e
	}
	return digest(string(raw)), observed, nil
}

// Called only by the trusted collection producer, inside the transaction that
// persists its typed samples. This is not an alternate browser issue endpoint.
func recordFabricIssueEvidenceTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project string, report *FabricCollectionReport, cables []fabricmon.Cable, samples map[string]fabricmon.Snapshot, trace string) error {
	if report.EvidenceSource != "device" && report.EvidenceSource != "protocol_fixture" {
		return apiError(409, "FABRIC_ISSUE_PROVENANCE_INVALID", "Network issue evidence origin is invalid")
	}
	if !report.ValidUntil.After(time.Now()) {
		return nil
	}
	byID := map[string]fabricmon.Cable{}
	for _, c := range cables {
		byID[c.UID] = c
	}
	for _, finding := range report.Findings {
		// Unknown, unsupported collection, uninstalled host sides and stale data
		// remain pending. Only the evaluator's concrete anomalies create issues.
		if finding.Verdict != "anomaly" {
			continue
		}
		cable, ok := byID[finding.CableUID]
		if !ok || !cable.A.Switch || !cable.B.Switch || finding.A != cable.A.UID || finding.B != cable.B.UID || finding.Source != "switch" || !slices.Contains([]string{"T1", "T2", "T3", "T4", "T6"}, finding.IssueType) {
			return apiError(409, "FABRIC_ISSUE_SCOPE_INVALID", "Network finding does not match its frozen cable scope")
		}
		fingerprint, observed, e := fabricIssueObservationKey(report, cable, finding, samples)
		if e != nil {
			return e
		}
		var id string
		e = tx.QueryRowContext(ctx, `SELECT issue_id FROM fabric_issue_bindings WHERE project_id=$1 AND cable_uid=$2 AND category=$3 AND evidence_origin=$4 FOR UPDATE`, project, cable.UID, finding.IssueType, report.EvidenceSource).Scan(&id)
		fresh := errors.Is(e, sql.ErrNoRows)
		if e != nil && !fresh {
			return e
		}
		var item *Issue
		if fresh {
			expected, e := json.Marshal(map[string]any{"cable_uid": cable.UID, "endpoint_a_uid": cable.A.UID, "endpoint_b_uid": cable.B.UID, "rule_ref": finding.RuleRef})
			if e != nil {
				return e
			}
			actual, e := json.Marshal(finding)
			if e != nil {
				return e
			}
			in := CreateIssueInput{Title: "Network precheck: " + finding.IssueType + " / " + finding.Reason, Category: finding.IssueType, Source: "fabric", Phase: "delivery", Anchors: []IssueAnchor{{Kind: "cable", ID: cable.UID}, {Kind: "port", ID: cable.A.UID}, {Kind: "port", ID: cable.B.UID}}, Evidence: IssueEvidence{ObservedAt: observed, SourceVersion: finding.RuleRef, Expected: string(expected), Actual: string(actual)}}
			if e = validIssueCreate(in); e != nil {
				return e
			}
			for _, anchor := range in.Anchors {
				if e = issueAnchorTx(ctx, tx, project, anchor); e != nil {
					return e
				}
			}
			item, e = insertIssueTx(ctx, tx, a, project, in, "fabric", trace, map[string]any{"run_id": report.ID, "package_id": report.PackageID, "evidence_origin": report.EvidenceSource})
			if e != nil {
				return e
			}
			id = item.ID
			if _, e = tx.ExecContext(ctx, `INSERT INTO fabric_issue_bindings(issue_id,project_id,cable_uid,category,evidence_origin,created_at) VALUES($1,$2,$3,$4,$5,$6)`, id, project, cable.UID, finding.IssueType, report.EvidenceSource, time.Now().UTC()); e != nil {
				return e
			}
		} else {
			item, e = scanIssue(tx.QueryRowContext(ctx, "SELECT "+issueColumns+" FROM issues WHERE id=$1 AND project_id=$2 FOR UPDATE", id, project))
			if e != nil {
				return e
			}
		}
		var seen bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM fabric_issue_observations WHERE issue_id=$1 AND observation_hash=$2)`, id, fingerprint).Scan(&seen); e != nil {
			return e
		}
		findingJSON, e := json.Marshal(finding)
		if e != nil {
			return e
		}
		result, e := tx.ExecContext(ctx, `INSERT INTO fabric_issue_observations(id,issue_id,project_id,evidence_origin,run_id,package_id,observation_hash,finding,observed_at,valid_until,recorded_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(issue_id,run_id) DO NOTHING`, RandomID(), id, project, report.EvidenceSource, report.ID, report.PackageID, fingerprint, findingJSON, observed, report.ValidUntil, time.Now().UTC())
		if e != nil {
			return e
		}
		inserted, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if !fresh && !seen && inserted > 0 {
			state := item.State
			reopened := state == "closed"
			if reopened {
				state = "reopened"
			}
			item, e = scanIssue(tx.QueryRowContext(ctx, `UPDATE issues SET state=$2,version=version+1,updated_at=$3 WHERE id=$1 RETURNING `+issueColumns, id, state, time.Now().UTC()))
			if e != nil {
				return e
			}
			action := "observe"
			if reopened {
				action = "reopen"
			}
			if e = issueEventTx(ctx, tx, a, item, action, trace, map[string]any{"run_id": report.ID, "package_id": report.PackageID, "evidence_origin": report.EvidenceSource, "reason": finding.Reason, "rule_ref": finding.RuleRef}); e != nil {
				return e
			}
		}
	}
	return nil
}
func fabricIssueLinksTx(ctx context.Context, tx *sql.Tx, project, run string) ([]FabricIssueLink, error) {
	out := []FabricIssueLink{}
	rows, e := tx.QueryContext(ctx, `SELECT b.cable_uid,b.issue_id,b.category,b.evidence_origin FROM fabric_issue_bindings b JOIN issues i ON i.id=b.issue_id JOIN fabric_collection_runs run ON run.id=$2 AND run.project_id=b.project_id AND run.report->>'evidence_source'=b.evidence_origin WHERE b.project_id=$1 AND EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.package_id=run.package_id AND r.item_uid=b.cable_uid) AND (i.state<>'closed' OR EXISTS(SELECT 1 FROM fabric_issue_observations o WHERE o.run_id=run.id AND o.issue_id=b.issue_id)) ORDER BY b.cable_uid,b.category,b.issue_id`, project, run)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v FabricIssueLink
		if e = rows.Scan(&v.CableUID, &v.IssueID, &v.Category, &v.EvidenceOrigin); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) FabricIssueObservations(ctx context.Context, a *AuthSession, id string, p Pagination) (Page[FabricIssueObservation], error) {
	out := NewPage[FabricIssueObservation](p)
	if e := p.Validate(); e != nil {
		return out, e
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	var project string
	e = tx.QueryRowContext(ctx, "SELECT project_id FROM issues WHERE id=$1", id).Scan(&project)
	if errors.Is(e, sql.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	if _, e = issueRoleTx(ctx, tx, a, project); e != nil {
		return out, e
	}
	if e = tx.QueryRowContext(ctx, "SELECT count(*) FROM fabric_issue_observations WHERE issue_id=$1", id).Scan(&out.Total); e != nil {
		return out, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT o.id,o.run_id,o.package_id,o.evidence_origin,o.observation_hash,o.finding,o.observed_at,o.valid_until,o.recorded_at,
 (o.valid_until<=now() OR o.package_id IS DISTINCT FROM (SELECT p.id FROM bom_import_packages p WHERE p.project_id=o.project_id AND p.state='committed' AND EXISTS(SELECT 1 FROM bom_item_revisions r JOIN bom_items b ON b.item_uid=r.item_uid WHERE r.package_id=p.id AND b.category='switch') ORDER BY p.committed_at DESC,p.id DESC LIMIT 1))
 FROM fabric_issue_observations o WHERE o.issue_id=$1 ORDER BY o.recorded_at DESC,o.id DESC LIMIT $2 OFFSET $3`, id, p.PageSize, p.Offset())
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v FabricIssueObservation
		var raw []byte
		if e = rows.Scan(&v.ID, &v.RunID, &v.PackageID, &v.EvidenceOrigin, &v.ObservationHash, &raw, &v.ObservedAt, &v.ValidUntil, &v.RecordedAt, &v.Stale); e != nil {
			rows.Close()
			return out, e
		}
		if e = json.Unmarshal(raw, &v.Finding); e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
