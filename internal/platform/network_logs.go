package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricmon"
	"strings"
	"time"
)

type NetworkLogReport struct {
	Version        string                 `json:"version"`
	ProjectID      string                 `json:"project_id"`
	DeviceID       string                 `json:"device_id"`
	Source         string                 `json:"source"`
	EvidenceSource string                 `json:"evidence_source"`
	SampleRef      string                 `json:"sample_ref"`
	WindowHours    int                    `json:"window_hours"`
	WindowStart    time.Time              `json:"window_start"`
	CollectedAt    *time.Time             `json:"collected_at"`
	ValidUntil     time.Time              `json:"valid_until"`
	Gate           NetworkGate            `json:"gate"`
	Findings       []fabricmon.LogFinding `json:"findings"`
	Entries        []fabricmon.LogEntry   `json:"entries"`
	TotalEntries   int                    `json:"total_entries"`
	Page           int                    `json:"page"`
	PageSize       int                    `json:"page_size"`
	Warnings       []string               `json:"warnings"`
}

func (s *Store) NetworkLogs(ctx context.Context, a *AuthSession, p *ProjectSummary, device string, hours, page, size int, level, keyword, format, trace string) (*NetworkLogReport, error) {
	if format != "" && format != "json" && format != "csv" {
		return nil, apiError(400, "FABRIC_LOG_FORMAT_INVALID", "Use JSON or CSV export")
	}

	if hours != 6 && hours != 24 && hours != 48 && hours != 72 {
		return nil, apiError(400, "FABRIC_LOG_WINDOW_INVALID", "Use 6, 24, 48 or 72 hours")
	}
	if page < 1 || page > 1000000 || size < 1 || size > 1000 || len(keyword) > 256 {
		return nil, apiError(400, "FABRIC_LOG_FILTER_INVALID", "Invalid log query bounds")
	}
	switch level {
	case "", "debug", "info", "notice", "warning", "error", "critical", "alert", "emergency":
	default:
		return nil, apiError(400, "FABRIC_LOG_LEVEL_INVALID", "Unknown normalized log level")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	_, devices, _, e := fabricInventoryTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	found := false
	for _, d := range devices {
		if d.ID == device {
			found = true
			break
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	now := time.Now().UTC()
	out := &NetworkLogReport{Version: "network-logs/v1", ProjectID: p.ID, DeviceID: device, Source: "switch", WindowHours: hours, WindowStart: now.Add(-time.Duration(hours) * time.Hour), ValidUntil: now.Add(30 * time.Second), Gate: networkGate("logs", "BLOCKED", "log_evidence_unknown"), Findings: []fabricmon.LogFinding{}, Entries: []fabricmon.LogEntry{}, Warnings: []string{}, Page: page, PageSize: size}
	collection, e := latestFabricCollectionTx(ctx, tx, p.ID)
	if e != nil {
		return nil, e
	}
	if collection != nil {
		out.EvidenceSource = collection.EvidenceSource
		if collection.ValidUntil.Before(out.ValidUntil) {
			out.ValidUntil = collection.ValidUntil
		}
		if !collection.Stale {
			var raw []byte
			var sample fabricmon.Snapshot
			e = tx.QueryRowContext(ctx, `SELECT content_hash,sample FROM fabric_switch_samples WHERE run_id=$1 AND switch_id=$2`, collection.ID, device).Scan(&out.SampleRef, &raw)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
			if e == nil {
				if e = json.Unmarshal(raw, &sample); e != nil {
					return nil, e
				}
				out.CollectedAt = &sample.CollectedAt
				// Coverage is relative to the collector boundary, with a five-minute freshness bound.
				out.WindowStart = sample.CollectedAt.Add(-time.Duration(hours) * time.Hour)
				if sample.Capabilities["logs"] && sample.LogWindowStart != nil && !sample.LogWindowStart.After(out.WindowStart) {
					out.Gate = networkGate("logs", "PASS", "")
					out.Gate.EvidenceRefs = []string{out.SampleRef}
				}
				entries := []fabricmon.LogEntry{}
				for _, entry := range sample.Logs {
					if entry.At.Before(out.WindowStart) || level != "" && entry.Level != level || keyword != "" && !strings.Contains(strings.ToLower(entry.Message+" "+entry.Raw+" "+entry.Process), strings.ToLower(keyword)) {
						continue
					}
					entries = append(entries, entry)
				}
				rules, e := fabricmon.DefaultLogRules()
				if e != nil {
					return nil, e
				}
				out.Findings = fabricmon.AnalyzeLogs(entries, rules, out.WindowStart, "", "")
				out.TotalEntries = len(entries)
				if format != "" {
					out.Entries = entries
					out.Page = 1
					out.PageSize = len(entries)
				}
				start := (page - 1) * size
				end := page * size
				if end > len(entries) {
					end = len(entries)
				}
				if format == "" && start < len(entries) {
					out.Entries = entries[start:end]
				}
				for _, finding := range out.Findings {
					state := "WARN"
					if finding.Severity == "critical" {
						state = "FAIL"
					}
					mergeNetworkGate(&out.Gate, networkGate(finding.RuleRef, state, "log_rule_findings_present"))
				}
			}
		} else {
			out.Warnings = append(out.Warnings, "evidence_stale")
		}
	}
	action := "fabric.logs.read"
	if format != "" {
		action = "fabric.logs.export"
	}

	if e = appendAudit(ctx, tx, a.User.ID, p.ID, action, device, trace, map[string]any{"window_hours": hours, "page": page, "page_size": size, "sample_ref": out.SampleRef, "state": out.Gate.State}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}
