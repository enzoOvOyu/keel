package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/enzoOvOyu/keel/internal/netbaseline"
)

type NetworkPushJob struct {
	ID                 string                  `json:"id"`
	ProjectID          string                  `json:"project_id"`
	TemplateID         string                  `json:"template_id"`
	BaselineID         string                  `json:"baseline_id"`
	Input              NetworkPlanInput        `json:"input"`
	FactsHash          string                  `json:"facts_hash"`
	ConfigHash         string                  `json:"config_hash"`
	State              string                  `json:"state"`
	ReviewHash         string                  `json:"review_hash"`
	Critical           bool                    `json:"critical"`
	CanaryMinSize      int                     `json:"canary_min_size"`
	Concurrency        int                     `json:"concurrency"`
	PrepareExecutionID string                  `json:"prepare_execution_id"`
	PushExecutionID    string                  `json:"push_execution_id"`
	CreatedBy          string                  `json:"created_by"`
	CreatedAt          time.Time               `json:"created_at"`
	Devices            []NetworkPushDevice     `json:"devices"`
	Signatures         []netbaseline.Signature `json:"signatures"`
}
type NetworkPushDevice struct {
	Device      NetworkPlannedDevice         `json:"device"`
	State       string                       `json:"state"`
	RunningHash string                       `json:"running_hash"`
	SnapshotRef string                       `json:"snapshot_ref"`
	Diff        []netbaseline.Difference     `json:"diff"`
	Prepare     *netbaseline.OperationResult `json:"prepare,omitempty"`
	Apply       *netbaseline.OperationResult `json:"apply,omitempty"`
	Verify      *netbaseline.OperationResult `json:"verify,omitempty"`
	Code        string                       `json:"code"`
	Ordinal     int                          `json:"ordinal"`
}

const networkPushFields = `id,project_id,template_id,baseline_id,input,facts_hash,config_hash,state,coalesce(review_hash,''),critical,canary_min_size,push_concurrency,coalesce(prepare_execution_id,''),coalesce(push_execution_id,''),created_by,created_at`

func networkJSON(v any) []byte { raw, _ := json.Marshal(v); return raw }
func loadNetworkPush(ctx context.Context, tx *sql.Tx, project, id string, lock bool) (*NetworkPushJob, error) {
	query := "SELECT " + networkPushFields + " FROM network_push_jobs WHERE id=$1 AND project_id=$2"
	if lock {
		query += " FOR UPDATE"
	}
	j := &NetworkPushJob{Devices: []NetworkPushDevice{}, Signatures: []netbaseline.Signature{}}
	var input []byte
	e := tx.QueryRowContext(ctx, query, id, project).Scan(&j.ID, &j.ProjectID, &j.TemplateID, &j.BaselineID, &input, &j.FactsHash, &j.ConfigHash, &j.State, &j.ReviewHash, &j.Critical, &j.CanaryMinSize, &j.Concurrency, &j.PrepareExecutionID, &j.PushExecutionID, &j.CreatedBy, &j.CreatedAt)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(input, &j.Input); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT facts,state,coalesce(running_hash,''),coalesce(snapshot_ref,''),coalesce(diff,'[]'),prepare_evidence,apply_evidence,verify_evidence,code,ordinal FROM network_push_devices WHERE job_id=$1 ORDER BY ordinal`, id)
	if e != nil {
		return nil, e
	}
	for rows.Next() {
		var d NetworkPushDevice
		var facts, diff, pre, apply, verify []byte
		if e = rows.Scan(&facts, &d.State, &d.RunningHash, &d.SnapshotRef, &diff, &pre, &apply, &verify, &d.Code, &d.Ordinal); e != nil {
			rows.Close()
			return nil, e
		}
		for _, v := range []struct {
			raw []byte
			out any
		}{{facts, &d.Device}, {diff, &d.Diff}, {pre, &d.Prepare}, {apply, &d.Apply}, {verify, &d.Verify}} {
			if len(v.raw) > 0 {
				if e = json.Unmarshal(v.raw, v.out); e != nil {
					rows.Close()
					return nil, e
				}
			}
		}
		j.Devices = append(j.Devices, d)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = tx.QueryContext(ctx, `SELECT user_id,role,review_hash,comment FROM network_push_signatures WHERE job_id=$1 ORDER BY role`, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var v netbaseline.Signature
		if e = rows.Scan(&v.UserID, &v.Role, &v.ReviewHash, &v.Comment); e != nil {
			return nil, e
		}
		j.Signatures = append(j.Signatures, v)
	}
	return j, rows.Err()
}
func pushTransition(ctx context.Context, tx *sql.Tx, j *NetworkPushJob, to string, metadata any) error {
	if !netbaseline.TransitionAllowed(j.State, to) {
		return apiError(409, "NETWORK_PUSH_STATE_CONFLICT", j.State+" cannot transition to "+to)
	}
	if _, e := tx.ExecContext(ctx, `UPDATE network_push_jobs SET state=$2,updated_at=now() WHERE id=$1`, j.ID, to); e != nil {
		return e
	}
	if _, e := tx.ExecContext(ctx, `INSERT INTO network_push_events(job_id,state,metadata,created_at) VALUES($1,$2,$3,now())`, j.ID, to, networkJSON(metadata)); e != nil {
		return e
	}
	j.State = to
	return nil
}
func (s *Store) CreateNetworkPush(ctx context.Context, a *AuthSession, p *ProjectSummary, input NetworkPlanInput, trace string) (*NetworkPushJob, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:plan"); e != nil {
		return nil, e
	}
	template, baseline, devices, e := networkTemplateFactsTx(ctx, tx, p.ID, input)
	if e != nil {
		return nil, e
	}
	if template.State != "released" {
		return nil, apiError(409, "NETWORK_TEMPLATE_RELEASE_REQUIRED", "Use an independently released template")
	}
	if s.FabricProbes == nil {
		return nil, apiError(409, "NETWORK_ADAPTER_NOT_CONFIGURED", "Network adapter is not configured")
	}
	if _, e = s.FabricProbes.NetworkClient(); e != nil {
		return nil, apiError(409, "NETWORK_ADAPTER_NOT_CONFIGURED", e.Error())
	}
	j := &NetworkPushJob{ID: RandomID(), ProjectID: p.ID, TemplateID: template.ID, BaselineID: baseline, Input: input, FactsHash: netbaseline.ContentHash(devices), ConfigHash: s.FabricProbes.Fingerprint, State: "CREATED", CanaryMinSize: 5, Concurrency: 50, CreatedBy: a.User.ID, CreatedAt: time.Now().UTC(), Devices: []NetworkPushDevice{}, Signatures: []netbaseline.Signature{}}
	values, err := catalogDefaultsTx(ctx, tx, p.ID, "fabric.canary_min_size", "fabric.push_concurrency")
	if err != nil {
		return nil, err
	}
	for key, raw := range values {
		value, err := strconv.Atoi(raw)
		maximum := 500
		if key == "fabric.push_concurrency" {
			maximum = 50
		}
		if err != nil || value < 1 || value > maximum {
			return nil, apiError(409, "NETWORK_PUSH_PARAMETER_INVALID", "Invalid canary or concurrency parameter")
		}
		if key == "fabric.canary_min_size" {
			j.CanaryMinSize = value
		} else {
			j.Concurrency = value
		}
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO network_push_jobs(id,project_id,template_id,baseline_id,input,facts_hash,config_hash,state,canary_min_size,push_concurrency,created_by,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,'CREATED',$8,$9,$10,$11,$11)`, j.ID, p.ID, template.ID, baseline, networkJSON(input), j.FactsHash, j.ConfigHash, j.CanaryMinSize, j.Concurrency, a.User.ID, j.CreatedAt)
	if e != nil {
		return nil, e
	}
	for i, d := range devices {
		_, e = tx.ExecContext(ctx, `INSERT INTO network_intended_configs(id,project_id,template_id,switch_id,content_hash,configuration,created_at) VALUES($1,$2,$3,$4,$5,$6,now()) ON CONFLICT(id) DO NOTHING`, d.IntendedID, p.ID, template.ID, d.ID, d.IntendedHash, []byte(d.Intended))
		if e != nil {
			return nil, e
		}
		_, e = tx.ExecContext(ctx, `INSERT INTO network_push_devices(job_id,switch_id,ordinal,intended_id,facts,updated_at) VALUES($1,$2,$3,$4,$5,now())`, j.ID, d.ID, i, d.IntendedID, networkJSON(d))
		if e != nil {
			return nil, e
		}
		j.Devices = append(j.Devices, NetworkPushDevice{Device: d, State: "PENDING", Ordinal: i, Diff: []netbaseline.Difference{}})
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO network_push_events(job_id,state,metadata,created_at) VALUES($1,'CREATED',$2,now())`, j.ID, networkJSON(map[string]any{"facts_hash": j.FactsHash})); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.push.create", j.ID, trace, map[string]any{"template_id": template.ID, "facts_hash": j.FactsHash, "targets": len(devices)}); e != nil {
		return nil, e
	}
	return j, tx.Commit()
}
func (s *Store) ListNetworkPush(ctx context.Context, a *AuthSession, p *ProjectSummary) ([]NetworkPushJob, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id FROM network_push_jobs WHERE project_id=$1 ORDER BY created_at DESC,id DESC LIMIT 30`, p.ID)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	out := []NetworkPushJob{}
	for _, id := range ids {
		j, e := loadNetworkPush(ctx, tx, p.ID, id, false)
		if e != nil {
			return nil, e
		}
		out = append(out, *j)
	}
	return out, nil
}
func (s *Store) reviewNetworkPushFacts(ctx context.Context, tx *sql.Tx, j *NetworkPushJob) error {
	template, base, devices, e := networkTemplateFactsTx(ctx, tx, j.ProjectID, j.Input)
	if e != nil {
		return e
	}
	if template.State != "released" || base != j.BaselineID || netbaseline.ContentHash(devices) != j.FactsHash || s.FabricProbes == nil || s.FabricProbes.Fingerprint != j.ConfigHash {
		return apiError(409, "NETWORK_PUSH_FACTS_CHANGED", "Approved facts, template or adapter changed; create a new PushJob")
	}
	return nil
}
func validNetworkSignatures(ctx context.Context, tx *sql.Tx, j *NetworkPushJob) error {
	for _, signature := range j.Signatures {
		var role string
		var enabled bool
		e := tx.QueryRowContext(ctx, `SELECT u.enabled,m.role FROM users u JOIN memberships m ON m.user_id=u.id AND m.project_id=$2 WHERE u.id=$1 FOR SHARE OF u,m`, signature.UserID, j.ProjectID).Scan(&enabled, &role)
		if errors.Is(e, sql.ErrNoRows) || e == nil && (!enabled || role != signature.Role) {
			return apiError(409, "NETWORK_APPROVER_AUTHORITY_CHANGED", "An approver no longer holds the signed project role")
		}
		if e != nil {
			return e
		}
	}
	return netbaseline.ApprovalQuorum(j.CreatedBy, j.ReviewHash, j.Critical, j.Signatures)
}

// QueueNetworkPushTx runs inside the shared Execution creation transaction.
func (s *Store) QueueNetworkPushTx(ctx context.Context, tx *sql.Tx, a *AuthSession, project, id, phase string) (*NetworkPushJob, error) {
	if e := requireFabricPermission(ctx, tx, a, project, "fabric:plan"); e != nil {
		return nil, e
	}
	j, e := loadNetworkPush(ctx, tx, project, id, true)
	if e != nil {
		return nil, e
	}
	if j.CreatedBy != a.User.ID {
		return nil, apiError(409, "NETWORK_PUSH_EXECUTOR_CHANGED", "The plan owner must queue its execution")
	}
	if e = s.reviewNetworkPushFacts(ctx, tx, j); e != nil {
		return nil, e
	}
	switch phase {
	case "prepare":
		if j.State != "CREATED" && j.State != "BLOCKED" {
			return nil, apiError(409, "NETWORK_PREPARE_NOT_ALLOWED", "Job must be CREATED or BLOCKED")
		}
		if e = pushTransition(ctx, tx, j, "PRECHECKING", map[string]any{"actor": a.User.ID}); e != nil {
			return nil, e
		}
		if _, e = tx.ExecContext(ctx, `UPDATE network_push_devices SET state='PENDING',code='',snapshot=NULL,running_hash=NULL,snapshot_ref=NULL,diff=NULL,prepare_evidence=NULL WHERE job_id=$1`, j.ID); e != nil {
			return nil, e
		}
	case "push":
		if j.State != "APPROVED" {
			return nil, apiError(409, "NETWORK_APPROVAL_REQUIRED", "Complete the required independent approval quorum")
		}
		if e = validNetworkSignatures(ctx, tx, j); e != nil {
			return nil, e
		}
		client, e := s.FabricProbes.NetworkClient()
		if e != nil || !client.WriteEnabled() {
			return nil, apiError(409, "NETWORK_WRITES_DISABLED", "Operator must enable writes for the configured network adapter")
		}
		if e = pushTransition(ctx, tx, j, "EXECUTING", map[string]any{"actor": a.User.ID, "review_hash": j.ReviewHash}); e != nil {
			return nil, e
		}
	default:
		return nil, apiError(400, "NETWORK_PHASE_INVALID", "Use prepare or push")
	}
	return j, nil
}
func (s *Store) DecideNetworkPush(ctx context.Context, a *AuthSession, p *ProjectSummary, id, reviewHash, comment, decision, trace string) (*NetworkPushJob, error) {
	if len(strings.TrimSpace(comment)) < 1 || len(comment) > 2000 {
		return nil, apiError(400, "NETWORK_APPROVAL_COMMENT_REQUIRED", "Provide an approval or rejection comment")
	}
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	role, e := networkRoleTx(ctx, tx, a, p.ID)
	if e != nil {
		return nil, e
	}
	if role != "NE" && role != "DE" && role != "NH" {
		return nil, ErrForbidden
	}
	j, e := loadNetworkPush(ctx, tx, p.ID, id, true)
	if e != nil {
		return nil, e
	}
	if (j.State != "READY" && !(decision == "reject" && j.State == "APPROVED")) || j.CreatedBy == a.User.ID || reviewHash != j.ReviewHash {
		return nil, apiError(409, "NETWORK_APPROVAL_SNAPSHOT_INVALID", "Review the current READY snapshot with an independent account")
	}
	if e = s.reviewNetworkPushFacts(ctx, tx, j); e != nil {
		return nil, e
	}
	if decision == "reject" {
		if e = pushTransition(ctx, tx, j, "REJECTED", map[string]any{"user_id": a.User.ID, "role": role, "comment": comment}); e != nil {
			return nil, e
		}
	} else if decision == "approve" {
		for _, sig := range j.Signatures {
			if sig.UserID == a.User.ID || sig.Role == role {
				return nil, apiError(409, "NETWORK_SIGNATURE_EXISTS", "This account or role has already signed")
			}
		}
		signature := netbaseline.Signature{UserID: a.User.ID, Role: role, ReviewHash: j.ReviewHash, Comment: comment}
		_, e = tx.ExecContext(ctx, `INSERT INTO network_push_signatures(job_id,user_id,role,review_hash,comment,created_at) VALUES($1,$2,$3,$4,$5,now())`, j.ID, a.User.ID, role, j.ReviewHash, comment)
		if e != nil {
			return nil, e
		}
		j.Signatures = append(j.Signatures, signature)
		if netbaseline.ApprovalQuorum(j.CreatedBy, j.ReviewHash, j.Critical, j.Signatures) == nil {
			if e = pushTransition(ctx, tx, j, "APPROVED", map[string]any{"review_hash": j.ReviewHash, "signatures": j.Signatures}); e != nil {
				return nil, e
			}
		}
	} else {
		return nil, apiError(400, "NETWORK_DECISION_INVALID", "Use approve or reject")
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.push."+decision, j.ID, trace, map[string]any{"review_hash": j.ReviewHash, "role": role, "comment": comment}); e != nil {
		return nil, e
	}
	return j, tx.Commit()
}

// NetworkClaimAllowedTx gates canary peers before acquiring the shared SSH lease.
func (s *Store) NetworkClaimAllowedTx(ctx context.Context, tx *sql.Tx, project, id, device, phase string) (bool, error) {
	j, e := loadNetworkPush(ctx, tx, project, id, false)
	if e != nil {
		return false, e
	}
	if phase == "prepare" {
		return j.State == "PRECHECKING", nil
	}
	if j.State != "EXECUTING" && j.State != "VERIFYING" {
		return false, nil
	}
	if e = s.reviewNetworkPushFacts(ctx, tx, j); e != nil {
		return false, nil
	}
	if e = validNetworkSignatures(ctx, tx, j); e != nil {
		return false, nil
	}
	for _, d := range j.Devices {
		if d.Device.ID == device {
			if d.State != "PREPARED" {
				return false, nil
			}
			if len(j.Devices) >= j.CanaryMinSize && d.Ordinal > 0 {
				return j.Devices[0].State == "VERIFIED", nil
			}
			return true, nil
		}
	}
	return false, fmt.Errorf("network target is not in immutable plan")
}

func (s *Store) ArchiveNetworkPush(ctx context.Context, a *AuthSession, p *ProjectSummary, id, trace string) (*NetworkPushJob, error) {
	tx, e := s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:plan"); e != nil {
		return nil, e
	}
	j, e := loadNetworkPush(ctx, tx, p.ID, id, true)
	if e != nil {
		return nil, e
	}
	if e = pushTransition(ctx, tx, j, "ARCHIVED", map[string]any{"actor": a.User.ID}); e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "network.push.archive", j.ID, trace, map[string]any{"review_hash": j.ReviewHash}); e != nil {
		return nil, e
	}
	return j, tx.Commit()
}
