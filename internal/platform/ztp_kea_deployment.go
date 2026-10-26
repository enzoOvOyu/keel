package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/enzoOvOyu/keel/internal/fabricboot"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type KeaDeploymentInput struct {
	BatchRef      string `json:"batch_ref"`
	PolicyMediaID string `json:"policy_media_id"`
}
type KeaDeploymentSpec struct {
	VendorProfile     *fabricboot.DHCPVendorProfile `json:"vendor_profile,omitempty"`
	SZTPEnvironmentID string                        `json:"sztp_environment_id,omitempty"`
	Canary            *fabricboot.KeaReservation    `json:"canary,omitempty"`
	PlanID            string                        `json:"plan_id"`
	EnvironmentID     string                        `json:"environment_id"`
	ProbeHash         string                        `json:"probe_hash"`
	ControllerKey     string                        `json:"controller_key"`
	Options           fabricboot.DHCPOptions        `json:"options"`
	Bindings          []fabricboot.KeaReservation   `json:"bindings"`
}
type KeaDeployment struct {
	DORA             *fabricboot.DHCPProof `json:"dora,omitempty"`
	VerificationCode string                `json:"verification_code,omitempty"`
	ID               string                `json:"id"`
	ProjectID        string                `json:"project_id"`
	ExecutionID      string                `json:"execution_id,omitempty"`
	State            string                `json:"state"`
	Spec             KeaDeploymentSpec     `json:"spec"`
	SpecHash         string                `json:"spec_hash"`
	BeforeHash       string                `json:"before_hash"`
	CandidateHash    string                `json:"candidate_hash"`
	ActiveHash       string                `json:"active_hash,omitempty"`
	FailureCode      string                `json:"failure_code,omitempty"`
	CreatedAt        time.Time             `json:"created_at"`
}

const keaDeploymentFields = "id,project_id,coalesce(execution_id,''),state,spec,spec_hash,before_hash,candidate_hash,coalesce(active_hash,''),coalesce(failure_code,''),created_at,dora_proof,coalesce(verification_code,'')"

func scanKeaDeployment(row scanner) (*KeaDeployment, error) {
	var out KeaDeployment
	var raw, proof []byte
	e := row.Scan(&out.ID, &out.ProjectID, &out.ExecutionID, &out.State, &raw, &out.SpecHash, &out.BeforeHash, &out.CandidateHash, &out.ActiveHash, &out.FailureCode, &out.CreatedAt, &proof, &out.VerificationCode)
	if e == nil {
		e = json.Unmarshal(raw, &out.Spec)
		if e == nil && len(proof) > 0 {
			e = json.Unmarshal(proof, &out.DORA)
		}
	}
	return &out, e
}
func (s *Store) keaDeploymentFactsTx(ctx context.Context, tx *sql.Tx, a *AuthSession, projectID string, input KeaDeploymentInput, lockMedia bool) (*KeaDeploymentSpec, error) {
	if !bootRouteRef.MatchString(input.BatchRef) || !bootMediaID.MatchString(input.PolicyMediaID) {
		return nil, apiError(400, "VALIDATION_ERROR", "Batch and approved isolation media ID are required")
	}
	if e := requireFabricPermission(ctx, tx, a, projectID, "fabric:plan"); e != nil {
		return nil, e
	}
	_, key, e := s.FabricProbes.DeploymentControl()
	if e != nil {
		return nil, apiError(409, "KEA_DEPLOYMENT_NOT_CONFIGURED", "Server operator has not enabled Kea configuration writes")
	}
	plan, e := currentManagementPlanTx(ctx, tx, projectID, input.BatchRef)
	if e != nil {
		return nil, e
	}
	var reportRaw []byte
	e = tx.QueryRowContext(ctx, `SELECT report FROM ztp_environment_checks WHERE project_id=$1 AND batch_ref=$2 AND boot_channel='239' ORDER BY checked_at DESC,id DESC LIMIT 1`, projectID, input.BatchRef).Scan(&reportRaw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, apiError(409, "ZTP_ENVIRONMENT_REQUIRED", "Run current N-S3 environment checks first")
	}
	if e != nil {
		return nil, e
	}
	var report ZTPEnvironmentReport
	if e = json.Unmarshal(reportRaw, &report); e != nil {
		return nil, e
	}
	if !report.Ready || report.PlanID != plan.ID || report.ConfigHash != s.FabricProbes.Fingerprint || !time.Now().Before(report.ExpiresAt) {
		return nil, apiError(409, "ZTP_ENVIRONMENT_NOT_READY", "Current N-S3 evidence must be ready, unexpired and bound to this plan/configuration")
	}
	origin, e := s.FabricProbes.BootOrigin()
	if e != nil {
		return nil, apiError(409, "ZTP_ENVIRONMENT_NOT_READY", "Dedicated boot endpoint is not configured")
	}
	query := url.Values{"project": {projectID}, "batch": {input.BatchRef}, "stage": {"N-S4"}, "policy": {input.PolicyMediaID}}
	cfg := fabricboot.DHCPOptions{ProjectID: projectID, BatchRef: input.BatchRef, StageRef: "N-S4", ManagementCIDR: plan.Rules.CIDR, Option239URL: origin + "/api/v1/boot/ztp?" + query.Encode(), Option143URIs: []string{origin}, Option60VCI: "cumulus-linux x86_64", Option43: map[string]string{"nos": "cumulus-linux", "version_intent": "5.x"}}
	vendorProfile := s.FabricProbes.DHCPVendorProfile()
	if vendorProfile != nil {
		cfg.Option60VCI = vendorProfile.VCI
		cfg.Option43 = map[string]string{"nos": vendorProfile.NOS, "version_intent": vendorProfile.VersionIntent}
	}
	sztpEnvironmentID := ""
	if sztpOrigin, enabled := s.FabricProbes.SZTPOrigin(); enabled {
		cfg.Option143URIs = []string{sztpOrigin}
		var raw []byte
		readErr := tx.QueryRowContext(ctx, `SELECT report FROM ztp_environment_checks WHERE project_id=$1 AND batch_ref=$2 AND boot_channel='143' ORDER BY checked_at DESC,id DESC LIMIT 1`, projectID, input.BatchRef).Scan(&raw)
		if errors.Is(readErr, sql.ErrNoRows) {
			return nil, apiError(409, "ZTP_SZTP_ENVIRONMENT_REQUIRED", "Run the configured 143 channel environment checks before staging")
		}
		if readErr != nil {
			return nil, readErr
		}
		var sztp ZTPEnvironmentReport
		if readErr = json.Unmarshal(raw, &sztp); readErr != nil {
			return nil, readErr
		}
		if !sztp.Ready || sztp.PlanID != plan.ID || sztp.ConfigHash != s.FabricProbes.Fingerprint || !time.Now().Before(sztp.ExpiresAt) {
			return nil, apiError(409, "ZTP_SZTP_ENVIRONMENT_NOT_READY", "Configured 143 channel needs current ready N-S3 evidence")
		}
		sztpEnvironmentID = sztp.ID
	}
	_, e = approvedZTPManagementPolicyTx(ctx, tx, projectID, input.BatchRef, input.PolicyMediaID, lockMedia)
	if e != nil {
		return nil, e
	}
	out := &KeaDeploymentSpec{VendorProfile: vendorProfile, SZTPEnvironmentID: sztpEnvironmentID, PlanID: plan.ID, EnvironmentID: report.ID, ProbeHash: report.ConfigHash, ControllerKey: key, Options: cfg, Bindings: []fabricboot.KeaReservation{}}
	for _, assignment := range plan.Assignments {
		if assignment.Batch != input.BatchRef {
			continue
		}
		var retired bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM ztp_switch_project_bindings WHERE project_id=$1 AND switch_id=$2 AND mode='device' AND state='retired')`, projectID, assignment.SwitchID).Scan(&retired); e != nil {
			return nil, e
		}
		if retired {
			return nil, apiError(409, "ZTP_RMA_BINDING_RETIRED", "Regenerate the approved management plan after replacement")
		}
		var mac string
		e = tx.QueryRowContext(ctx, `SELECT mac FROM switch_mac_bindings WHERE switch_id=$1 AND kind='mgmt'`, assignment.SwitchID).Scan(&mac)
		if errors.Is(e, sql.ErrNoRows) {
			return nil, apiError(409, "ZTP_MANAGEMENT_MAC_REQUIRED", "Every target switch needs a unique management MAC in SoT")
		}
		if e != nil {
			return nil, e
		}
		out.Bindings = append(out.Bindings, fabricboot.KeaReservation{SwitchID: assignment.SwitchID, MAC: mac, IP: assignment.Address, Hostname: assignment.Hostname})
	}
	if probe := s.FabricProbes.DHCPProbe(); probe != nil {
		prefix, _ := netip.ParsePrefix(plan.Rules.CIDR)
		address, _ := netip.ParseAddr(probe.Address)
		if !prefix.Contains(address) || address == prefix.Addr() || address.String() == plan.Rules.Gateway || address.Next().IsValid() && !prefix.Contains(address.Next()) {
			return nil, apiError(409, "DHCP_CANARY_OUTSIDE_PLAN", "Canary must use a dedicated usable address in the approved management subnet")
		}
		mac, _ := net.ParseMAC(probe.MAC)
		for _, assignment := range plan.Assignments {
			if assignment.Address == probe.Address {
				return nil, apiError(409, "DHCP_CANARY_CONFLICT", "Canary address overlaps a device allocation")
			}
		}
		var exists bool
		if e = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM switch_mac_bindings WHERE lower(mac)=$1)`, strings.ToLower(mac.String())).Scan(&exists); e != nil {
			return nil, e
		}
		if exists {
			return nil, apiError(409, "DHCP_CANARY_CONFLICT", "Canary MAC overlaps SoT device identity")
		}
		out.Canary = &fabricboot.KeaReservation{SwitchID: "dhcp-canary", MAC: mac.String(), IP: probe.Address, Hostname: "keel-canary-" + projectID[:12]}
	}
	return out, nil
}
func keaDeploymentBindings(spec *KeaDeploymentSpec) []fabricboot.KeaReservation {
	out := append([]fabricboot.KeaReservation(nil), spec.Bindings...)
	if spec.Canary != nil {
		out = append(out, *spec.Canary)
	}
	return out
}
func (s *Store) StageKeaDeployment(ctx context.Context, a *AuthSession, p *ProjectSummary, input KeaDeploymentInput, trace string) (*KeaDeployment, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	spec, e := s.keaDeploymentFactsTx(ctx, tx, a, p.ID, input, false)
	tx.Rollback()
	if e != nil {
		return nil, e
	}
	control, _, e := s.FabricProbes.DeploymentControl()
	if e != nil {
		return nil, e
	}
	live, e := control.ReadFullDHCP4(ctx)
	if e != nil {
		return nil, apiError(502, "KEA_READ_FAILED", "Cannot read the configured Kea control service")
	}
	candidate, e := renderKeaCandidate(live.Config, spec)
	if e != nil {
		return nil, apiError(409, "KEA_CANDIDATE_BLOCKED", e.Error())
	}
	if e = control.TestFullDHCP4(ctx, candidate); e != nil {
		return nil, apiError(409, "KEA_CONFIG_TEST_FAILED", "Kea rejected the staged reservation change")
	}
	tx, e = s.beginControl(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	current, e := s.keaDeploymentFactsTx(ctx, tx, a, p.ID, input, true)
	if e != nil {
		return nil, e
	}
	if fabricboot.ManagementPlanDigest(current) != fabricboot.ManagementPlanDigest(spec) {
		return nil, apiError(409, "KEA_STAGE_STALE", "Planning or environment evidence changed while staging")
	}
	now := time.Now().UTC()
	out := &KeaDeployment{ID: RandomID(), ProjectID: p.ID, State: "staged", Spec: *spec, SpecHash: fabricboot.ManagementPlanDigest(spec), BeforeHash: live.Hash, CandidateHash: fabricboot.KeaConfigDigest(candidate), CreatedAt: now}
	raw, _ := json.Marshal(spec)
	_, e = tx.ExecContext(ctx, `INSERT INTO ztp_kea_deployments(id,project_id,state,spec,spec_hash,before_hash,candidate_hash,created_by,created_at,updated_at) VALUES($1,$2,'staged',$3,$4,$5,$6,$7,$8,$8)`, out.ID, p.ID, raw, out.SpecHash, out.BeforeHash, out.CandidateHash, a.User.ID, now)
	if e != nil {
		return nil, e
	}
	if e = appendAudit(ctx, tx, a.User.ID, p.ID, "ztp.kea.stage", out.ID, trace, map[string]any{"plan_id": spec.PlanID, "before_hash": live.Hash, "candidate_hash": out.CandidateHash}); e != nil {
		return nil, e
	}
	return out, tx.Commit()
}

// Called in the existing Execution creation transaction, not a parallel queue.
func (s *Store) QueueKeaDeploymentTx(ctx context.Context, tx *sql.Tx, a *AuthSession, projectID, jobID string) (*KeaDeployment, error) {
	job, e := scanKeaDeployment(tx.QueryRowContext(ctx, "SELECT "+keaDeploymentFields+" FROM ztp_kea_deployments WHERE id=$1 AND project_id=$2 FOR UPDATE", jobID, projectID))
	if errors.Is(e, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if e != nil {
		return nil, e
	}
	if job.State != "staged" {
		return nil, apiError(409, "KEA_DEPLOYMENT_ALREADY_QUEUED", "Deployment is no longer staged")
	}
	facts, e := s.keaDeploymentFactsTx(ctx, tx, a, projectID, KeaDeploymentInput{BatchRef: job.Spec.Options.BatchRef, PolicyMediaID: policyMediaID(job.Spec.Options.Option239URL)}, true)
	if e != nil {
		return nil, e
	}
	if fabricboot.ManagementPlanDigest(facts) != job.SpecHash {
		return nil, apiError(409, "KEA_STAGE_STALE", "Preview facts changed; stage again")
	}
	return job, nil
}
func policyMediaID(raw string) string {
	u, e := url.Parse(raw)
	if e != nil {
		return ""
	}
	return u.Query().Get("policy")
}
func (s *Store) ListKeaDeployments(ctx context.Context, a *AuthSession, p *ProjectSummary) ([]KeaDeployment, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, "SELECT "+keaDeploymentFields+" FROM ztp_kea_deployments WHERE project_id=$1 ORDER BY created_at DESC,id DESC LIMIT 50", p.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []KeaDeployment{}
	for rows.Next() {
		v, e := scanKeaDeployment(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	return out, tx.Commit()
}

// RunKeaDeployment is invoked only by an existing fenced Execution claim. All
// raw Kea configuration (which may contain backend credentials) stays in memory.
func (s *Store) RunKeaDeployment(ctx context.Context, jobID, executionID string) (*fabricboot.KeaApplyResult, error) {
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	job, e := scanKeaDeployment(tx.QueryRowContext(ctx, "SELECT "+keaDeploymentFields+" FROM ztp_kea_deployments WHERE id=$1 AND execution_id=$2 FOR UPDATE", jobID, executionID))
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	if job.State != "queued" {
		tx.Rollback()
		return nil, apiError(409, "KEA_DEPLOYMENT_STATE_INVALID", "Deployment is not queued; no automatic write retry is permitted")
	}
	var userID string
	var enabled, admin bool
	e = tx.QueryRowContext(ctx, `SELECT u.id,u.enabled,u.is_admin FROM exec_executions x JOIN users u ON u.id=x.created_by WHERE x.id=$1 AND x.project_id=$2 AND x.mode='kea' AND x.desired_state='running'`, executionID, job.ProjectID).Scan(&userID, &enabled, &admin)
	if e != nil || !enabled {
		tx.Rollback()
		return nil, ErrForbidden
	}
	actor := &AuthSession{User: UserSummary{ID: userID, IsAdmin: admin}}
	facts, e := s.keaDeploymentFactsTx(ctx, tx, actor, job.ProjectID, KeaDeploymentInput{BatchRef: job.Spec.Options.BatchRef, PolicyMediaID: policyMediaID(job.Spec.Options.Option239URL)}, true)
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	if fabricboot.ManagementPlanDigest(facts) != job.SpecHash {
		tx.Rollback()
		return nil, apiError(409, "KEA_STAGE_STALE", "Deployment evidence changed before dispatch")
	}
	_, e = tx.ExecContext(ctx, `UPDATE ztp_kea_deployments SET state='applying',updated_at=now() WHERE id=$1`, jobID)
	if e != nil {
		tx.Rollback()
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	control, _, e := s.FabricProbes.DeploymentControl()
	if e != nil {
		return nil, e
	}
	live, e := control.ReadFullDHCP4(ctx)
	if e == nil && live.Hash != job.BeforeHash {
		e = apiError(409, "KEA_ACTIVE_HASH_CHANGED", "Kea changed after staging; create a new preview")
	}
	var candidate json.RawMessage
	if e == nil {
		candidate, e = renderKeaCandidate(live.Config, facts)
	}
	if e == nil && fabricboot.KeaConfigDigest(candidate) != job.CandidateHash {
		e = apiError(409, "KEA_CANDIDATE_CHANGED", "Recomputed candidate differs from staged evidence")
	}
	var result *fabricboot.KeaApplyResult
	if e == nil {
		result, e = control.PersistFullDHCP4(ctx, candidate, job.BeforeHash)
	}
	state, code := "config_persisted", ""
	if e != nil {
		state = "blocked"
		code = "kea_prewrite_failed"
		var unknown *fabricboot.KeaMutationUnknown
		if errors.As(e, &unknown) {
			state = "unknown"
			code = "kea_write_outcome_unknown"
		}
	}
	// DHCP is a separate observation after confirmed persistence. Its failure
	// cannot erase the persisted configuration or become an unknown config write.
	var proof *fabricboot.DHCPProof
	verificationCode := "not_configured"
	if e == nil && s.FabricProbes.DHCPProbe() != nil {
		var redirects []string
		if facts.SZTPEnvironmentID != "" {
			redirects = facts.Options.Option143URIs
		}
		var proof *fabricboot.DHCPProof
		var verifyErr error
		if facts.VendorProfile != nil {
			proof, verifyErr = s.FabricProbes.DHCPProbe().VerifyVendorBootstrap(ctx, facts.Options.Option239URL, redirects, *facts.VendorProfile)
		} else {
			proof, verifyErr = s.FabricProbes.DHCPProbe().VerifyBootstrap(ctx, facts.Options.Option239URL, facts.Options.Option60VCI, redirects)
		}
		if verifyErr != nil {
			verificationCode = "dora_failed"
		} else {
			// Check that config did not drift during the UDP exchange.
			readback, readErr := control.ReadFullDHCP4(ctx)
			if readErr != nil || readback.Hash != result.ActiveHash {
				verificationCode = "dora_config_changed"
			} else {
				result.DORA = proof
				verificationCode = "dora_verified"
			}
		}
	}
	proof = resultProof(result)
	finish, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var hash any
	if result != nil {
		hash = result.ActiveHash
	}
	finishTx, writeErr := s.DB.BeginTx(finish, nil)
	if writeErr == nil {
		defer finishTx.Rollback()
		if proof != nil {
			// Admission revocation or source changes while DHCP ran must not open a route.
			var currentEnabled, currentAdmin bool
			var desired string
			checkErr := finishTx.QueryRowContext(finish, `SELECT u.enabled,u.is_admin,x.desired_state FROM exec_executions x JOIN users u ON u.id=x.created_by WHERE x.id=$1 AND u.id=$2 FOR SHARE OF u,x`, executionID, actor.User.ID).Scan(&currentEnabled, &currentAdmin, &desired)
			actor.User.IsAdmin = currentAdmin
			if checkErr == nil && currentEnabled && !currentAdmin {
				var role string
				checkErr = finishTx.QueryRowContext(finish, `SELECT role FROM memberships WHERE project_id=$1 AND user_id=$2 FOR SHARE`, job.ProjectID, actor.User.ID).Scan(&role)
			}
			var current *KeaDeploymentSpec
			if checkErr == nil && currentEnabled && desired == "running" {
				current, checkErr = s.keaDeploymentFactsTx(finish, finishTx, actor, job.ProjectID, KeaDeploymentInput{BatchRef: job.Spec.Options.BatchRef, PolicyMediaID: policyMediaID(job.Spec.Options.Option239URL)}, true)
			} else if checkErr == nil {
				checkErr = ErrForbidden
			}
			if checkErr != nil || fabricboot.ManagementPlanDigest(current) != job.SpecHash {
				proof = nil
				result.DORA = nil
				verificationCode = "dora_admission_changed"
			} else {
				state = "boot_ready"
			}
		}
		var encoded any
		if proof != nil {
			encoded, _ = json.Marshal(proof)
		}
		_, writeErr = finishTx.ExecContext(finish, `UPDATE ztp_kea_deployments SET state=$2,active_hash=$3,failure_code=$4,dora_proof=$5,verification_code=$6,updated_at=now() WHERE id=$1 AND state='applying'`, jobID, state, hash, code, encoded, verificationCode)
		if writeErr == nil {
			writeErr = finishTx.Commit()
		}
	}
	if writeErr != nil {
		return nil, &fabricboot.KeaMutationUnknown{Stage: "job-evidence-persistence"}
	}
	if result != nil {
		result.VerificationCode = verificationCode
	}
	return result, e
}
func resultProof(result *fabricboot.KeaApplyResult) *fabricboot.DHCPProof {
	if result == nil {
		return nil
	}
	return result.DORA
}

type KeaPolicyOption struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

func (s *Store) KeaPolicyOptions(ctx context.Context, a *AuthSession, p *ProjectSummary) ([]KeaPolicyOption, error) {
	tx, e := s.beginControlRead(ctx, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	if e = requireFabricPermission(ctx, tx, a, p.ID, "fabric:read"); e != nil {
		return nil, e
	}
	rows, e := tx.QueryContext(ctx, `SELECT id,name,version FROM media_items WHERE scope_key IN($1,'global') AND type='ztp_script' AND state='approved' ORDER BY name,version,id`, "project:"+p.ID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []KeaPolicyOption{}
	for rows.Next() {
		var v KeaPolicyOption
		if e = rows.Scan(&v.ID, &v.Name, &v.Version); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	return out, tx.Commit()
}

func renderKeaCandidate(live json.RawMessage, spec *KeaDeploymentSpec) (json.RawMessage, error) {
	if spec.VendorProfile != nil {
		var uris []string
		if spec.SZTPEnvironmentID != "" {
			uris = spec.Options.Option143URIs
		}
		return fabricboot.MergeBootstrapVendor(live, spec.Options.ManagementCIDR, spec.Options.Option239URL, spec.Options.ProjectID, keaDeploymentBindings(spec), uris, *spec.VendorProfile)
	}
	if spec.SZTPEnvironmentID != "" {
		return fabricboot.MergeBootstrapDual(live, spec.Options.ManagementCIDR, spec.Options.Option239URL, spec.Options.ProjectID, keaDeploymentBindings(spec), spec.Options.Option143URIs)
	}
	return fabricboot.MergeBootstrap239(live, spec.Options.ManagementCIDR, spec.Options.Option239URL, spec.Options.ProjectID, keaDeploymentBindings(spec))
}
