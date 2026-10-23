package platform

import (
	"context"
	"database/sql"
	"errors"
	"net/netip"
	"regexp"
	"strings"
	"time"
)

var ztpSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// SZTPProgress is an authenticated device claim. Even bootstrap-complete is
// only an observation; independent reachability and config checks are needed
// before any switch or FabricReadiness record can become managed.
type SZTPProgress struct {
	Type           string `json:"progress_type"`
	Message        string `json:"message,omitempty"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}

type ZTPDeviceObservation struct {
	ID             string    `json:"id"`
	BatchRef       string    `json:"batch_ref"`
	SwitchID       string    `json:"switch_id,omitempty"`
	SerialNumber   string    `json:"serial_number"`
	SourceIP       string    `json:"source_ip"`
	BootChannel    string    `json:"boot_channel"`
	EventType      string    `json:"event_type"`
	ProgressType   string    `json:"progress_type,omitempty"`
	Message        string    `json:"message,omitempty"`
	OfferHash      string    `json:"offer_hash"`
	EvidenceSHA256 string    `json:"evidence_sha256,omitempty"`
	TraceID        string    `json:"trace_id"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// RecordZTP143Progress binds the report to a recent get-bootstrapping-data
// call from the same IDevID serial, TCP address, project, batch and active
// offer. It never projects a claimed completion to a managed state.
func (s *Store) RecordZTP143Progress(ctx context.Context, certSerial, sourceIP, requestHost, trace string, progress SZTPProgress) error {
	if !snPattern.MatchString(certSerial) || strings.TrimSpace(certSerial) != certSerial || requestHost == "" || !validSZTPProgress(progress.Type) || len(progress.Message) > 1000 || strings.ContainsRune(progress.Message, '\x00') || !ztpSHA256Pattern.MatchString(progress.EvidenceSHA256) {
		return apiError(400, "SZTP_PROGRESS_INVALID", "Invalid device progress report")
	}
	source, err := netip.ParseAddr(sourceIP)
	if err != nil || !source.Is4() {
		return ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	plan, err := s.activeSZTPPlanTx(ctx, tx, source, requestHost)
	if err != nil {
		return err
	}
	var priorSwitch sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT switch_id FROM ztp_device_observations WHERE project_id=$1 AND batch_ref=$2 AND serial_number=$3 AND source_ip=$4 AND boot_channel='143' AND event_type='data-served' AND offer_hash=$5 AND occurred_at>now()-interval '30 minutes' ORDER BY occurred_at DESC LIMIT 1 FOR SHARE`, plan.projectID, plan.batchRef, certSerial, sourceIP, plan.hash).Scan(&priorSwitch)
	if errors.Is(err, sql.ErrNoRows) {
		return apiError(409, "SZTP_PROGRESS_NO_BOOTSTRAP", "No recent bootstrap data was served to this device")
	}
	if err != nil {
		return err
	}
	verdict, err := resolveZTPIdentityTx(ctx, tx, plan.projectID, ZTPIdentityInput{Serial: certSerial})
	if err != nil {
		return err
	}
	if verdict.Verdict == "conflict" || verdict.SwitchID != priorSwitch.String {
		return apiError(409, "SZTP_PROGRESS_IDENTITY_CHANGED", "Device identity changed since bootstrap delivery")
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = tx.ExecContext(ctx, `INSERT INTO ztp_device_observations(id,project_id,batch_ref,switch_id,serial_number,source_ip,boot_channel,event_type,progress_type,message,offer_hash,evidence_sha256,trace_id,occurred_at) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,'143','report',$7,NULLIF($8,''),$9,$10,$11,$12)`, RandomID(), plan.projectID, plan.batchRef, verdict.SwitchID, certSerial, sourceIP, progress.Type, progress.Message, plan.hash, progress.EvidenceSHA256, trace, now)
	if err != nil {
		return err
	}
	if err = appendAudit(ctx, tx, "", plan.projectID, "ztp.boot143.progress", plan.batchRef, trace, map[string]any{"serial": certSerial, "source_ip": sourceIP, "progress_type": progress.Type, "evidence_sha256": progress.EvidenceSHA256}); err != nil {
		return err
	}
	return tx.Commit()
}

func validSZTPProgress(value string) bool {
	switch value {
	case "bootstrap-initiated", "parsing-initiated", "parsing-warning", "parsing-error", "parsing-complete", "boot-image-initiated", "boot-image-warning", "boot-image-error", "boot-image-mismatch", "boot-image-installed-rebooting", "boot-image-complete", "pre-script-initiated", "pre-script-warning", "pre-script-error", "pre-script-complete", "config-initiated", "config-warning", "config-error", "config-complete", "post-script-initiated", "post-script-warning", "post-script-error", "post-script-complete", "bootstrap-warning", "bootstrap-error", "bootstrap-complete", "informational":
		return true
	}
	return false
}

func (s *Store) ListZTPDeviceObservations(ctx context.Context, a *AuthSession, project *ProjectSummary) ([]ZTPDeviceObservation, error) {
	tx, err := s.beginControlRead(ctx, a)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = requireProjectMember(ctx, tx, a, project.ID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,batch_ref,coalesce(switch_id,''),serial_number,source_ip,boot_channel,event_type,coalesce(progress_type,''),coalesce(message,''),offer_hash,coalesce(evidence_sha256,''),trace_id,occurred_at FROM ztp_device_observations WHERE project_id=$1 ORDER BY occurred_at DESC,id DESC LIMIT 100`, project.ID)
	if err != nil {
		return nil, err
	}
	out := []ZTPDeviceObservation{}
	for rows.Next() {
		var item ZTPDeviceObservation
		if err = rows.Scan(&item.ID, &item.BatchRef, &item.SwitchID, &item.SerialNumber, &item.SourceIP, &item.BootChannel, &item.EventType, &item.ProgressType, &item.Message, &item.OfferHash, &item.EvidenceSHA256, &item.TraceID, &item.OccurredAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return out, tx.Commit()
}
