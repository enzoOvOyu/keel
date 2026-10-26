package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/netip"
	"strconv"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
)

// Requests are immutable installation events, serialized under the existing
// installation lock. Binding to the current report sequence consumes a request
// exactly once when the next signed report advances that sequence.
func pendingProvisionRefreshTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation) (string, error) {
	if x.State != "preflight_ready" || x.LatestReportSHA256 == nil {
		return "", nil
	}
	var id string
	err := tx.QueryRowContext(ctx, `SELECT payload->>'refresh_id' FROM provision_installation_events WHERE installation_id=$1 AND event_type='preflight.refresh_requested' AND payload->>'sequence'=$2 AND payload->>'report_sha256'=$3 ORDER BY id DESC LIMIT 1`, x.ID, strconv.FormatInt(x.CallbackSequence, 10), *x.LatestReportSHA256).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !modelIDPattern.MatchString(id) {
		return "", ErrProvisionBootDenied
	}
	return id, nil
}

type ProvisionPreflightRefresh struct {
	ID             string `json:"id"`
	InstallationID string `json:"installation_id"`
	Sequence       int64  `json:"sequence"`
}

func (s *Store) RequestProvisionPreflightRefresh(ctx context.Context, a *AuthSession, project, id string, in ProvisionRequestActionInput, trace string) (*ProvisionPreflightRefresh, error) {
	if !s.ProvisionInstallEnabled {
		return nil, apiError(503, "PROVISION_INSTALL_DISABLED", "Installation runtime disabled")
	}
	if in.ExpectedVersion < 1 || !validProvisionReason(in.Reason) {
		return nil, apiError(400, "PROVISION_PREFLIGHT_REFRESH_INVALID", "Exact version and reason required")
	}
	tx, err := s.beginPinnedRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProvisionAuthority(ctx, tx, a, project, false); err != nil {
		return nil, err
	}
	x, _, _, _, err := installationMutationTx(ctx, tx, project, id)
	if err != nil {
		return nil, err
	}
	if x.State != "preflight_ready" || x.StateVersion != in.ExpectedVersion || x.StoragePlan != nil || x.LatestReportSHA256 == nil || x.CallbackSequence < 2 || x.CallbackSequence >= 33 {
		return nil, apiError(409, "PROVISION_STORAGE_STATE_CHANGED", "Refresh requires current pre-selection state and remaining report allowance")
	}
	pending, err := pendingProvisionRefreshTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	if pending != "" {
		return nil, apiError(409, "PROVISION_PREFLIGHT_REFRESH_PENDING", "A refresh already awaits the same boot identity")
	}
	out := &ProvisionPreflightRefresh{ID: RandomID(), InstallationID: x.ID, Sequence: x.CallbackSequence}
	if err = appendProvisionEventTx(ctx, tx, x, "preflight.refresh_requested", map[string]any{"refresh_id": out.ID, "sequence": out.Sequence, "report_sha256": *x.LatestReportSHA256, "requested_by": a.User.ID}); err != nil {
		return nil, err
	}
	if err = appendAudit(ctx, tx, a.User.ID, project, "provision.preflight.refresh_requested", id, trace, map[string]any{"refresh_id": out.ID, "reason": in.Reason}); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

var _ provisionboot.ReviewAuthority = (*ProvisionBootAuthority)(nil)

func (a *ProvisionBootAuthority) ReviewBoot(ctx context.Context, scope provisionboot.Scope, grant string, peer netip.Addr, in provisionboot.ReviewSubmission) (*provisionboot.ReviewStatus, error) {
	if a == nil || a.Store == nil || a.Store.DB == nil || !scope.CheckPeer(peer) || !modelIDPattern.MatchString(in.Query.Nonce) {
		return nil, ErrProvisionBootDenied
	}
	tx, err := a.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	live, err := a.liveTx(ctx, tx, scope, grant, peer)
	if err != nil {
		return nil, err
	}
	x := live.installation
	// liveTx holds the domain SHARE lock after execution/attempt admission.
	// Read only small authentication/evidence identifiers, never report bodies.
	if err = tx.QueryRowContext(ctx, `SELECT boot_id,boot_public_key,callback_sequence,latest_report_sha256,storage_plan_sha256 FROM provision_installations WHERE id=$1`, x.ID).Scan(&x.BootID, &x.BootPublicKey, &x.CallbackSequence, &x.LatestReportSHA256, &x.StoragePlanSHA256); err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(in.Query)
	if x.BootID == nil || x.LatestReportSHA256 == nil || x.CallbackSequence < 2 || provisionboot.VerifyBootProof(x.BootPublicKey, x.ID, *x.BootID, x.CallbackSequence, "review", payload, in.Proof) != nil {
		return nil, ErrProvisionBootDenied
	}
	refresh, err := pendingProvisionRefreshTx(ctx, tx, x)
	if err != nil {
		return nil, err
	}
	out := &provisionboot.ReviewStatus{InstallationID: x.ID, BootID: *x.BootID, Sequence: x.CallbackSequence, Nonce: in.Query.Nonce, State: x.State, ExpiresAt: x.ExpiresAt, ReportSHA256: *x.LatestReportSHA256, RefreshID: refresh}
	runtime, err := provisionRuntimeEvidenceTx(ctx, tx, x.ID)
	if err != nil {
		return nil, err
	}
	out.RuntimeInputSHA256 = runtime.ConfigSHA
	if x.StoragePlanSHA256 != nil {
		out.StoragePlanSHA256 = *x.StoragePlanSHA256
	}
	if x.State == "storage_approved" {
		var raw []byte
		if err = tx.QueryRowContext(ctx, "SELECT storage_plan FROM provision_installations WHERE id=$1", x.ID).Scan(&raw); err != nil {
			return nil, err
		}
		out.StoragePlan = &provisionstorage.Plan{}
		if json.Unmarshal(raw, out.StoragePlan) != nil {
			return nil, ErrProvisionBootDenied
		}
	}
	if provisionboot.ValidateReviewStatus(out, in.Proof, in.Query.Nonce, time.Now()) != nil {
		return nil, ErrProvisionBootDenied
	}
	return out, tx.Commit()
}
