package provisioninitrd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/enzoOvOyu/keel/internal/mediaboot"
	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionpreflight"
)

const PreparationSchema = "keel.initrd-preparation/v1"

type HelperRunner interface {
	Check(context.Context, mediaboot.Plan, string) error
	Run(context.Context, []byte, mediaboot.Artifact, string) ([]byte, int, error)
}
type Preparation struct {
	Schema                string                            `json:"schema"`
	ManifestSHA256        string                            `json:"manifest_sha256"`
	ReportSHA256          string                            `json:"report_sha256"`
	Status                string                            `json:"status"`
	Report                *provisionpreflight.Report        `json:"report"`
	InstallationPerformed bool                              `json:"installation_performed"`
	BootAcknowledgment    *provisionboot.BootAcknowledgment `json:"boot_acknowledgment,omitempty"`
	BootCallbackStatus    string                            `json:"boot_callback_status,omitempty"`
	BootReview            *provisionboot.ReviewStatus       `json:"boot_review,omitempty"`
	// Exact verified report bytes are retained separately for evidence and a
	// subsequent independently approved storage plan; never hash a re-encoding.
	ReportBytes []byte `json:"-"`
}

// Prepare never writes a disk, submits a callback or changes controller state.
// It invokes only the pinned read-only preflight adapter. A PASS is waiting for
// independent storage approval, not permission to install or claim SSH trust.
func Prepare(ctx context.Context, input Input, files *Files, runner HelperRunner, clock func() time.Time) (*Preparation, error) {
	if files == nil || runner == nil || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	if clock == nil {
		clock = time.Now
	}
	manifest, spec, err := provisionboot.VerifyBootFiles(files.Manifest, input.ManifestSHA256, files.Preflight, files.Seed, clock())
	if err != nil || manifest.GrantID != input.GrantID || manifest.MAC != input.BootMAC {
		return nil, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 100*time.Second)
	defer cancel()
	if err = runner.Check(ctx, manifest.Media, manifest.Arch); err != nil {
		return nil, ErrUnavailable
	}
	var preflight mediaboot.Artifact
	for _, artifact := range manifest.Media.Artifacts {
		if artifact.Role == "preflight" {
			preflight = artifact
		}
	}
	if preflight.MediaID == "" || ctx.Err() != nil || spec.Validate(clock()) != nil {
		return nil, ErrUnavailable
	}
	raw, exit, err := runner.Run(ctx, files.Preflight, preflight, manifest.Arch)
	if err != nil || ctx.Err() != nil || (exit != 0 && exit != 1) {
		return nil, ErrUnavailable
	}
	report, err := provisionpreflight.EvaluateReport(*spec, raw, clock())
	if err != nil || (exit == 0) != (report.Status == "PASS") {
		return nil, ErrUnavailable
	}
	status := "blocked"
	if report.Status == "FAIL" {
		status = "preflight_failed"
	} else if report.Status == "PASS" {
		status = "ready_for_storage_review"
	}
	if ctx.Err() != nil || spec.Validate(clock()) != nil {
		return nil, ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	return &Preparation{Schema: PreparationSchema, ManifestSHA256: input.ManifestSHA256, ReportSHA256: hex.EncodeToString(sum[:]), Status: status, Report: report, InstallationPerformed: false, ReportBytes: append([]byte(nil), raw...)}, nil
}
