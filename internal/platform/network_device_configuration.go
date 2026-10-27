package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

type NetworkDeviceConfigSnapshot struct {
	JobID           string               `json:"job_id"`
	JobState        string               `json:"job_state"`
	DeviceState     string               `json:"device_state"`
	TemplateID      string               `json:"template_id"`
	TemplateName    string               `json:"template_name"`
	TemplateVersion int64                `json:"template_version"`
	TemplateState   string               `json:"template_state"`
	BaselineID      string               `json:"baseline_id"`
	Input           NetworkPlanInput     `json:"input"`
	FactsHash       string               `json:"facts_hash"`
	FactsCurrent    bool                 `json:"facts_current"`
	FactsProblem    string               `json:"facts_problem,omitempty"`
	IdentityCurrent bool                 `json:"identity_current"`
	Device          NetworkPlannedDevice `json:"device"`
	Template        netbaseline.Template `json:"template"`
	Intended        json.RawMessage      `json:"intended"`
	// Snapshot is the pre-change backup. It is never called current running.
	BeforeSnapshot     json.RawMessage              `json:"before_snapshot,omitempty"`
	Diff               []netbaseline.Difference     `json:"diff"`
	Prepare            *netbaseline.OperationResult `json:"prepare,omitempty"`
	Apply              *netbaseline.OperationResult `json:"apply,omitempty"`
	Verify             *netbaseline.OperationResult `json:"verify,omitempty"`
	PrepareExecutionID string                       `json:"prepare_execution_id,omitempty"`
	PushExecutionID    string                       `json:"push_execution_id,omitempty"`
	ExecutionState     string                       `json:"execution_state,omitempty"`
	ExecutionCreatedAt *time.Time                   `json:"execution_created_at,omitempty"`
	CreatedAt          time.Time                    `json:"created_at"`
	Golden             NetworkGate                  `json:"golden"`
	GoldenValidUntil   *time.Time                   `json:"golden_valid_until,omitempty"`
}
type NetworkDeviceConfiguration struct {
	Version          string                       `json:"version"`
	ProjectID        string                       `json:"project_id"`
	SwitchID         string                       `json:"switch_id"`
	Serial           string                       `json:"serial"`
	ReadAt           time.Time                    `json:"read_at"`
	LatestProduction *NetworkDeviceConfigSnapshot `json:"latest_production"`
	LatestPlan       *NetworkDeviceConfigSnapshot `json:"latest_plan"`
}

func networkConfigDeviceTx(ctx context.Context, tx *sql.Tx, project, device string) (string, bool, error) {
	var serial, status string
	e := tx.QueryRowContext(ctx, `SELECT coalesce(b.serial,''),b.status FROM bom_items b JOIN switches s ON s.id=b.item_uid WHERE b.item_uid=$2 AND EXISTS(SELECT 1 FROM bom_item_revisions r WHERE r.item_uid=b.item_uid AND r.project_id=$1)`, project, device).Scan(&serial, &status)
	if errors.Is(e, sql.ErrNoRows) {
		return "", false, ErrNotFound
	}
	return serial, status != "retired", e
}
func (s *Store) networkDeviceConfigSnapshotTx(ctx context.Context, tx *sql.Tx, project, device, serial string, active, production bool, now time.Time) (*NetworkDeviceConfigSnapshot, error) {
	query := `SELECT j.id,j.state,d.state,j.template_id,t.name,t.version,t.state,j.baseline_id,j.input,j.facts_hash,d.facts,t.definition,i.configuration,d.snapshot,coalesce(d.diff,'[]'),d.prepare_evidence,d.apply_evidence,d.verify_evidence,coalesce(j.prepare_execution_id,''),coalesce(j.push_execution_id,''),coalesce(x.state,''),x.created_at,j.created_at
 FROM network_push_jobs j JOIN network_push_devices d ON d.job_id=j.id JOIN network_template_versions t ON t.id=j.template_id JOIN network_intended_configs i ON i.id=d.intended_id LEFT JOIN exec_executions x ON x.id=j.push_execution_id
 WHERE j.project_id=$1 AND d.switch_id=$2`
	if production {
		query += ` AND j.push_execution_id IS NOT NULL ORDER BY x.created_at DESC,x.id DESC LIMIT 1`
	} else {
		query += ` ORDER BY j.created_at DESC,j.id DESC LIMIT 1`
	}
	var v NetworkDeviceConfigSnapshot
	var input, facts, definition, diff, prepare, apply, verify, intended, before []byte
	e := tx.QueryRowContext(ctx, query, project, device).Scan(&v.JobID, &v.JobState, &v.DeviceState, &v.TemplateID, &v.TemplateName, &v.TemplateVersion, &v.TemplateState, &v.BaselineID, &input, &v.FactsHash, &facts, &definition, &intended, &before, &diff, &prepare, &apply, &verify, &v.PrepareExecutionID, &v.PushExecutionID, &v.ExecutionState, &v.ExecutionCreatedAt, &v.CreatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	v.Intended = json.RawMessage(intended)
	v.BeforeSnapshot = json.RawMessage(before)
	for _, part := range []struct {
		raw []byte
		out any
	}{{input, &v.Input}, {facts, &v.Device}, {definition, &v.Template}, {diff, &v.Diff}, {prepare, &v.Prepare}, {apply, &v.Apply}, {verify, &v.Verify}} {
		if len(part.raw) > 0 {
			if e = json.Unmarshal(part.raw, part.out); e != nil {
				return nil, e
			}
		}
	}
	v.IdentityCurrent = active && v.Device.ID == device && v.Device.Serial == serial
	// Reuse actual admission facts. This can render current approved templates but
	// performs no collector call, device command, write or auto repair.
	job := &NetworkPushJob{ID: v.JobID, ProjectID: project, TemplateID: v.TemplateID, BaselineID: v.BaselineID, Input: v.Input, FactsHash: v.FactsHash}
	if e = tx.QueryRowContext(ctx, `SELECT config_hash FROM network_push_jobs WHERE id=$1 AND project_id=$2`, v.JobID, project).Scan(&job.ConfigHash); e != nil {
		return nil, e
	}
	if e = s.reviewNetworkPushFactsWithLock(ctx, tx, job, false); e != nil {
		var business *Error
		if !errors.As(e, &business) {
			return nil, e
		}
		v.FactsProblem = business.Code
	} else {
		v.FactsCurrent = true
	}
	if netbaseline.ContentHash(v.Intended) != v.Device.IntendedHash {
		v.FactsCurrent = false
		v.FactsProblem = "NETWORK_INTENDED_SNAPSHOT_MISMATCH"
	}
	v.Golden = networkGoldenReadbackGate(project, v.Device, v.DeviceState, v.ExecutionState, v.Verify, now)
	if !v.IdentityCurrent {
		v.Golden = networkGate(device, "BLOCKED", "current_golden_identity_changed")
	} else if !v.FactsCurrent {
		v.Golden = networkGate(device, "BLOCKED", "current_approved_facts_changed")
	}
	if v.Golden.State == "PASS" {
		expiry := v.Verify.ObservedAt.Add(networkGoldenMaxAge)
		v.GoldenValidUntil = &expiry
	}
	return &v, nil
}
func (s *Store) NetworkDeviceConfiguration(ctx context.Context, a *AuthSession, p *ProjectSummary, device string) (*NetworkDeviceConfiguration, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	serial, active, e := networkConfigDeviceTx(ctx, tx, p.ID, device)
	if e != nil {
		return nil, e
	}
	out := &NetworkDeviceConfiguration{Version: "network-device-configuration/v1", ProjectID: p.ID, SwitchID: device, Serial: serial, ReadAt: time.Now().UTC()}
	out.LatestProduction, e = s.networkDeviceConfigSnapshotTx(ctx, tx, p.ID, device, serial, active, true, out.ReadAt)
	if e != nil {
		return nil, e
	}
	var latestID string
	e = tx.QueryRowContext(ctx, `SELECT j.id FROM network_push_jobs j JOIN network_push_devices d ON d.job_id=j.id WHERE j.project_id=$1 AND d.switch_id=$2 ORDER BY j.created_at DESC,j.id DESC LIMIT 1`, p.ID, device).Scan(&latestID)
	if errors.Is(e, sql.ErrNoRows) {
		return out, nil
	}
	if e != nil {
		return nil, e
	}
	if out.LatestProduction != nil && out.LatestProduction.JobID == latestID {
		out.LatestPlan = out.LatestProduction
	} else {
		out.LatestPlan, e = s.networkDeviceConfigSnapshotTx(ctx, tx, p.ID, device, serial, active, false, out.ReadAt)
	}
	return out, e
}

type NetworkDeviceConfigEvent struct {
	Sequence           int64     `json:"sequence"`
	JobID              string    `json:"job_id"`
	State              string    `json:"state"`
	Scope              string    `json:"scope"`
	TemplateID         string    `json:"template_id"`
	TemplateName       string    `json:"template_name"`
	TemplateVersion    int64     `json:"template_version"`
	BatchRef           string    `json:"batch_ref"`
	PrepareExecutionID string    `json:"prepare_execution_id,omitempty"`
	PushExecutionID    string    `json:"push_execution_id,omitempty"`
	At                 time.Time `json:"at"`
}
type NetworkDeviceConfigEventPage struct {
	ProjectID string                     `json:"project_id"`
	SwitchID  string                     `json:"switch_id"`
	Page      int                        `json:"page"`
	PageSize  int                        `json:"page_size"`
	Total     int                        `json:"total"`
	Items     []NetworkDeviceConfigEvent `json:"items"`
}

func (s *Store) NetworkDeviceConfigurationEvents(ctx context.Context, a *AuthSession, p *ProjectSummary, device string, page, size int) (*NetworkDeviceConfigEventPage, error) {
	if page < 1 || page > 1000000 || size < 1 || size > 100 {
		return nil, apiError(400, "FABRIC_PAGE_INVALID", "Use page >=1 and page_size 1 through 100")
	}
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	if _, _, e = networkConfigDeviceTx(ctx, tx, p.ID, device); e != nil {
		return nil, e
	}
	filter := ` FROM network_push_events e JOIN network_push_jobs j ON j.id=e.job_id JOIN network_push_devices d ON d.job_id=j.id AND d.switch_id=$2 JOIN network_template_versions t ON t.id=j.template_id WHERE j.project_id=$1 AND (NOT(e.metadata ? 'device_id') OR e.metadata->>'device_id'=$2)`
	out := &NetworkDeviceConfigEventPage{ProjectID: p.ID, SwitchID: device, Page: page, PageSize: size, Items: []NetworkDeviceConfigEvent{}}
	if e = tx.QueryRowContext(ctx, "SELECT count(*)"+filter, p.ID, device).Scan(&out.Total); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT e.seq,j.id,e.state,CASE WHEN e.metadata ? 'device_id' THEN 'device' ELSE 'batch' END,t.id,t.name,t.version,j.input->>'batch_ref',coalesce(j.prepare_execution_id,''),coalesce(j.push_execution_id,''),e.created_at`+filter+` ORDER BY e.seq DESC LIMIT $3 OFFSET $4`, p.ID, device, size, (page-1)*size)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v NetworkDeviceConfigEvent
		if e = rows.Scan(&v.Sequence, &v.JobID, &v.State, &v.Scope, &v.TemplateID, &v.TemplateName, &v.TemplateVersion, &v.BatchRef, &v.PrepareExecutionID, &v.PushExecutionID, &v.At); e != nil {
			return nil, e
		}
		out.Items = append(out.Items, v)
	}
	return out, rows.Err()
}
