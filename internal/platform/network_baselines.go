package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type NetworkEngineeringLimits struct {
	SwitchBandwidthRatioLimits map[string]float64 `json:"switch_bandwidth_ratio_limits"`
}

type NetworkBaselineVersion struct {
	ID                  string                   `json:"id"`
	ProjectID           string                   `json:"project_id"`
	VersionRef          string                   `json:"version_ref"`
	State               string                   `json:"state"`
	SourceRef           string                   `json:"source_ref"`
	EngineeringLimits   NetworkEngineeringLimits `json:"engineering_limits"`
	CustomerApprovalRef string                   `json:"customer_approval_ref,omitempty"`
	NetworkApprovalRef  string                   `json:"network_approval_ref,omitempty"`
	CreatedAt           time.Time                `json:"created_at"`
	ApprovedAt          *time.Time               `json:"approved_at,omitempty"`
}

type CreateNetworkBaselineInput struct {
	VersionRef        string                   `json:"version_ref"`
	SourceRef         string                   `json:"source_ref"`
	EngineeringLimits NetworkEngineeringLimits `json:"engineering_limits"`
}

type ApproveNetworkBaselineInput struct {
	CustomerApprovalRef string `json:"customer_approval_ref"`
	NetworkApprovalRef  string `json:"network_approval_ref"`
}

func validateNetworkEngineeringLimits(limits NetworkEngineeringLimits) error {
	if len(limits.SwitchBandwidthRatioLimits) == 0 {
		return apiError(400, "NETWORK_LIMITS_REQUIRED", "At least one switch bandwidth ratio limit is required")
	}
	for key, value := range limits.SwitchBandwidthRatioLimits {
		if len(key) > 160 || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
			return apiError(400, "NETWORK_LIMIT_INVALID", "Invalid switch bandwidth ratio limit")
		}
		if strings.HasPrefix(key, "serial:") {
			if !snPattern.MatchString(strings.TrimPrefix(key, "serial:")) {
				return apiError(400, "NETWORK_LIMIT_INVALID", "Invalid switch serial scope")
			}
		} else if strings.HasPrefix(key, "role:") {
			switch strings.TrimPrefix(key, "role:") {
			case "leaf", "spine", "superspine", "mgmt":
			default:
				return apiError(400, "NETWORK_LIMIT_INVALID", "Invalid switch role scope")
			}
		} else {
			return apiError(400, "NETWORK_LIMIT_INVALID", "Limit scope must be serial:SN or role:role")
		}
	}
	return nil
}

func validNetworkEvidenceRef(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) > 0 && len(value) <= 300 && !strings.ContainsAny(value, "\r\n\x00")
}

func (s *Store) CreateNetworkBaseline(ctx context.Context, a *AuthSession, project *ProjectSummary, input CreateNetworkBaselineInput, trace string) (*NetworkBaselineVersion, error) {
	if !bomProfileVersionPattern.MatchString(input.VersionRef) || !validNetworkEvidenceRef(input.SourceRef) {
		return nil, apiError(400, "VALIDATION_ERROR", "Invalid network baseline version or source reference")
	}
	if err := validateNetworkEngineeringLimits(input.EngineeringLimits); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(input.EngineeringLimits)
	if err != nil {
		return nil, err
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	out := &NetworkBaselineVersion{ID: RandomID(), ProjectID: project.ID, VersionRef: input.VersionRef, State: "draft", SourceRef: input.SourceRef, EngineeringLimits: input.EngineeringLimits, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
	_, err = tx.ExecContext(ctx, `INSERT INTO network_baseline_versions(id,project_id,version_ref,state,source_ref,engineering_limits,created_by,created_at) VALUES($1,$2,$3,'draft',$4,$5,$6,$7)`, out.ID, project.ID, out.VersionRef, out.SourceRef, raw, a.User.ID, out.CreatedAt)
	if err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" {
			return nil, apiError(409, "NETWORK_BASELINE_VERSION_EXISTS", "Network baseline version already exists")
		}
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "network.baseline.create", out.ID, trace, map[string]any{"version_ref": out.VersionRef, "source_ref": out.SourceRef}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ApproveNetworkBaseline(ctx context.Context, a *AuthSession, project *ProjectSummary, versionRef string, input ApproveNetworkBaselineInput, trace string) (*NetworkBaselineVersion, error) {
	if !validNetworkEvidenceRef(input.CustomerApprovalRef) || !validNetworkEvidenceRef(input.NetworkApprovalRef) {
		return nil, apiError(400, "NETWORK_APPROVAL_EVIDENCE_REQUIRED", "Customer and network approval references are required")
	}
	tx, err := s.beginControl(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectAdmin(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	out := &NetworkBaselineVersion{EngineeringLimits: NetworkEngineeringLimits{}}
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT id,state,source_ref,engineering_limits,created_at FROM network_baseline_versions WHERE project_id=$1 AND version_ref=$2 FOR UPDATE`, project.ID, versionRef).Scan(&out.ID, &out.State, &out.SourceRef, &raw, &out.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if out.State != "draft" {
		return nil, apiError(409, "NETWORK_BASELINE_NOT_DRAFT", "Only a draft network baseline can be approved")
	}
	if err = json.Unmarshal(raw, &out.EngineeringLimits); err != nil {
		return nil, err
	}
	out.ProjectID = project.ID
	out.VersionRef = versionRef
	out.State = "approved"
	out.CustomerApprovalRef = input.CustomerApprovalRef
	out.NetworkApprovalRef = input.NetworkApprovalRef
	now := time.Now().UTC().Truncate(time.Microsecond)
	out.ApprovedAt = &now
	_, err = tx.ExecContext(ctx, `UPDATE network_baseline_versions SET state='approved',customer_approval_ref=$2,network_approval_ref=$3,approved_by=$4,approved_at=$5 WHERE id=$1`, out.ID, out.CustomerApprovalRef, out.NetworkApprovalRef, a.User.ID, now)
	if err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project.ID, "network.baseline.approve", out.ID, trace, map[string]any{"version_ref": versionRef, "customer_approval_ref": out.CustomerApprovalRef, "network_approval_ref": out.NetworkApprovalRef}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) GetNetworkBaseline(ctx context.Context, a *AuthSession, project *ProjectSummary, versionRef string) (*NetworkBaselineVersion, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	out := &NetworkBaselineVersion{ProjectID: project.ID, VersionRef: versionRef}
	var raw []byte
	var customer, network sql.NullString
	var approved sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT id,state,source_ref,engineering_limits,customer_approval_ref,network_approval_ref,created_at,approved_at FROM network_baseline_versions WHERE project_id=$1 AND version_ref=$2`, project.ID, versionRef).Scan(&out.ID, &out.State, &out.SourceRef, &raw, &customer, &network, &out.CreatedAt, &approved)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &out.EngineeringLimits); err != nil {
		return nil, err
	}
	out.CustomerApprovalRef = customer.String
	out.NetworkApprovalRef = network.String
	if approved.Valid {
		out.ApprovedAt = &approved.Time
	}
	return out, tx.Commit()
}
