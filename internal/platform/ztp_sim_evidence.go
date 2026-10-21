package platform

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"

	"github.com/enzoOvOyu/keel/internal/fabricboot"
)

// activeZTPSimOfferTx rechecks the committed mock plan on each simulated
// boundary. The offer may change after a session was created; a stale
// simulation must stop rather than claim it fetched the old boot artifact.
func activeZTPSimOfferTx(ctx context.Context, tx *sql.Tx, projectID, batchRef string) (*fabricboot.DHCPOptions, string, error) {
	var raw []byte
	var activeHash string
	err := tx.QueryRowContext(ctx, `SELECT active_config,coalesce(active_hash,'') FROM ztp_mock_kea_plans WHERE project_id=$1 AND batch_ref=$2 AND stage_ref='N-S4' FOR SHARE`, projectID, batchRef).Scan(&raw, &activeHash)
	if errors.Is(err, sql.ErrNoRows) || len(raw) == 0 {
		return nil, "", apiError(409, "ZTP_KEA_NO_ACTIVE_PLAN", "No committed mock Kea offer")
	}
	if err != nil {
		return nil, "", err
	}
	var cfg fabricboot.DHCPOptions
	if err = json.Unmarshal(raw, &cfg); err != nil {
		return nil, "", err
	}
	if cfg.ProjectID != projectID || cfg.BatchRef != batchRef || cfg.StageRef != "N-S4" {
		return nil, "", apiError(409, "ZTP_KEA_ACTIVE_INVALID", "Active mock Kea scope changed")
	}
	mock := fabricboot.NewMockKea()
	computed, validationErr := mock.Stage(cfg)
	if validationErr != nil || computed != activeHash {
		return nil, "", apiError(409, "ZTP_KEA_ACTIVE_INVALID", "Active mock Kea offer failed validation")
	}
	return &cfg, activeHash, nil
}

func requireSimDHCPHashTx(ctx context.Context, tx *sql.Tx, sessionID string, attempt int, activeHash string) error {
	var evidence string
	err := tx.QueryRowContext(ctx, `SELECT coalesce(evidence_ref,'') FROM ztp_session_events WHERE session_id=$1 AND attempt=$2 AND event_code='sim.dhcp' ORDER BY seq DESC LIMIT 1`, sessionID, attempt).Scan(&evidence)
	if errors.Is(err, sql.ErrNoRows) || evidence != "mock-kea:"+activeHash {
		return apiError(409, "ZTP_KEA_OFFER_CHANGED", "Active mock Kea offer changed since DHCP")
	}
	return err
}

func approvedZTPSimArtifactTx(ctx context.Context, tx *sql.Tx, projectID string, cfg *fabricboot.DHCPOptions, channel string) (string, error) {
	bootURL, err := url.Parse(cfg.Option239URL)
	if err != nil {
		return "", err
	}
	if channel != "143" && bootURL.Path != "/api/v1/boot/ztp" {
		return "", apiError(409, "ZTP_BOOT_URL_INVALID", "Active Cumulus URL does not reach the boot endpoint")
	}
	mediaID := bootURL.Query().Get("policy")
	if !bootMediaID.MatchString(mediaID) {
		return "", apiError(409, "ZTP_SCRIPT_NOT_APPROVED", "Approved isolation script is required")
	}
	var digest string
	var size int64
	err = tx.QueryRowContext(ctx, `SELECT sha256,size FROM media_items WHERE id=$1 AND scope_key IN($2,'global') AND type='ztp_script' AND state='approved' FOR SHARE`, mediaID, "project:"+projectID).Scan(&digest, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return "", apiError(409, "ZTP_SCRIPT_NOT_APPROVED", "Approved isolation script is required")
	}
	if err != nil {
		return "", err
	}
	if digest != fabricboot.IsolationScriptDigest() || size != int64(len(fabricboot.IsolationScriptV1)) {
		return "", apiError(409, "ZTP_SCRIPT_DIGEST_MISMATCH", "Approved isolation script digest or size differs")
	}
	if channel == "143" {
		cms, encodeErr := fabricboot.UnsignedIsolationOnboarding(false)
		if encodeErr != nil {
			return "", encodeErr
		}
		sum := sha256.Sum256(cms)
		return "mock-sztp143:" + hex.EncodeToString(sum[:]), nil
	}
	return "mock-boot239:" + digest, nil
}
