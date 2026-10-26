package execution

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type ReceiptProvenance struct {
	Mode   string `json:"mode"`
	Source string `json:"source"`
	Count  int    `json:"count"`
}

type ExecutionReport struct {
	PlannedTaskSlots     int                 `json:"planned_task_slots"`
	UnobservedTaskSlots  int                 `json:"unobserved_task_slots"`
	Version              string              `json:"version"`
	GeneratedAt          time.Time           `json:"generated_at"`
	FrozenAt             time.Time           `json:"frozen_at"`
	Overview             *ExecutionOverview  `json:"overview"`
	Complete             bool                `json:"complete"`
	HardwareAcceptance   bool                `json:"hardware_acceptance"`
	GateEvidence         bool                `json:"gate_evidence"`
	ReceiptCount         int                 `json:"receipt_count"`
	ReceiptProvenance    []ReceiptProvenance `json:"receipt_provenance"`
	QuarantinedResources int                 `json:"quarantined_resources"`
	References           map[string]string   `json:"references"`
	Warnings             []string            `json:"warnings"`
}

// Report aggregates metadata in one repeatable-read snapshot, not the raw bodies
// of every log/receipt. Its bounded shape scales with task/provenance kinds, not
// target count; evidence pages retain their existing scope/permission boundary.
func (s *Service) Report(ctx context.Context, project, id string) (*ExecutionReport, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	x, err := readViewExecution(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	overview, err := overviewTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	out := &ExecutionReport{Version: "execution-evidence-report/v1", GeneratedAt: overview.ObservedAt, FrozenAt: x.Snapshot.FrozenAt, Overview: overview, ReceiptProvenance: []ReceiptProvenance{}, References: map[string]string{}, Warnings: []string{"not_hardware_acceptance", "not_gate_evidence", "receipt_counts_include_attempt_history", "details_require_authorized_paged_reads"}}
	out.PlannedTaskSlots = len(x.Snapshot.Targets) * len(x.Snapshot.Flow.Definition.Tasks)
	observed := 0
	for _, count := range overview.Counts {
		observed += count
	}
	out.UnobservedTaskSlots = max(0, out.PlannedTaskSlots-observed)
	out.Complete = terminal(x.State) && overview.Progress.TotalTargets > 0 && overview.Progress.CompleteTargets == overview.Progress.TotalTargets && overview.Counts["unknown"] == 0
	rows, err := tx.QueryContext(ctx, `SELECT coalesce(nullif(r.payload->'output'->>'mode',''),'unspecified'),coalesce(nullif(r.payload->'output'->>'source',''),'unspecified'),count(*) FROM exec_runner_receipts r JOIN exec_task_attempts a ON a.id=r.attempt_id AND a.command_id=r.command_id AND a.fencing_token=r.fencing_token WHERE a.execution_id=$1 GROUP BY 1,2 ORDER BY 1,2`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var source ReceiptProvenance
		if err = rows.Scan(&source.Mode, &source.Source, &source.Count); err != nil {
			rows.Close()
			return nil, err
		}
		out.ReceiptProvenance = append(out.ReceiptProvenance, source)
		out.ReceiptCount += source.Count
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(DISTINCT l.resource_id) FROM exec_resource_leases l JOIN exec_task_attempts a ON a.id=l.attempt_id AND a.fencing_token=l.fencing_token WHERE a.execution_id=$1 AND a.state='unknown' AND l.state='held'`, id).Scan(&out.QuarantinedResources); err != nil {
		return nil, err
	}
	base := fmt.Sprintf("/api/v1/projects/%s/executions/%s", project, id)
	out.References = map[string]string{"targets": base + "/matrix", "outputs": base + "/outputs", "journal": base + "/journal", "relations": base + "/relations"}
	if !out.Complete {
		out.Warnings = append(out.Warnings, "execution_incomplete")
	}
	if overview.Counts["unknown"] > 0 {
		out.Warnings = append(out.Warnings, "unknown_results_require_reconciliation")
	}
	if out.ReceiptCount == 0 {
		out.Warnings = append(out.Warnings, "no_terminal_receipts")
	}
	return out, tx.Commit()
}
