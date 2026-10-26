package platform

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionfirstboot"
)

// These are historical observations from the existing signed first-boot event,
// not current inventory, a compatibility verdict or a new SSH proof.
type ProvisionInstalledBaseline struct {
	SnapshotSHA256 string                        `json:"snapshot_sha256"`
	Media          []mediaboot.Artifact          `json:"media"`
	Observation    *ProvisionBaselineObservation `json:"observation,omitempty"`
}
type ProvisionBaselineObservation struct {
	EventID      int64                               `json:"event_id"`
	RecordedAt   time.Time                           `json:"recorded_at"`
	ObservedAt   time.Time                           `json:"observed_at"`
	ReportSHA256 string                              `json:"report_sha256"`
	Software     provisionfirstboot.SoftwareBaseline `json:"software"`
}

func readProvisionInstalledBaselineTx(ctx context.Context, tx *sql.Tx, x *ProvisionInstallation) (*ProvisionInstalledBaseline, error) {
	request, err := scanProvisionRequest(tx.QueryRowContext(ctx, "SELECT "+provisionRequestColumns+" FROM provision_requests WHERE project_id=$1 AND id=$2", x.ProjectID, x.RequestID))
	if err != nil {
		return nil, err
	}
	if !ProvisionSnapshotValid(request) || request.SnapshotSHA256 != x.SnapshotSHA256 {
		return nil, ErrProvisionBootDenied
	}
	out := &ProvisionInstalledBaseline{SnapshotSHA256: x.SnapshotSHA256, Media: []mediaboot.Artifact{}}
	for _, artifact := range request.Snapshot.Media.Artifacts {
		if artifact.Role == "rootfs" || artifact.Role == "kernel" {
			out.Media = append(out.Media, artifact)
		}
	}
	var eventID int64
	var recorded time.Time
	var raw []byte
	err = tx.QueryRowContext(ctx, `SELECT id,created_at,payload FROM provision_installation_events WHERE installation_id=$1 AND event_type='first_boot.identity_reported' ORDER BY id LIMIT 1`, x.ID).Scan(&eventID, &recorded, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	var event struct {
		Report       provisionfirstboot.Report `json:"report"`
		ReportSHA256 string                    `json:"report_sha256"`
	}
	if json.Unmarshal(raw, &event) != nil {
		return nil, ErrProvisionBootDenied
	}
	reportRaw, err := json.Marshal(event.Report)
	if err != nil || event.Report.InstallationID != x.ID || event.ReportSHA256 != digest(string(reportRaw)) || event.Report.SoftwareBaseline.Validate() != nil {
		return nil, ErrProvisionBootDenied
	}
	if event.Report.SoftwareBaseline != nil {
		out.Observation = &ProvisionBaselineObservation{EventID: eventID, RecordedAt: recorded, ObservedAt: event.Report.ObservedAt, ReportSHA256: event.ReportSHA256, Software: *event.Report.SoftwareBaseline}
	}
	return out, nil
}
