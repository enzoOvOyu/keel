//go:build linux

package provisioninitrd

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/enzoOvOyu/keel/internal/provisionboot"
	"github.com/enzoOvOyu/keel/internal/provisionstorage"
	"github.com/enzoOvOyu/keel/internal/provisionwriter"
	"golang.org/x/sys/unix"
)

type liveWriterAuthority struct{ session *WriteSession }

func (a liveWriterAuthority) CheckWrite(ctx context.Context, stage string) (*provisionboot.WriteAuthorization, error) {
	return a.session.Check(ctx, stage)
}

// RunInstallation follows authenticated approval in the same bounded network
// lease. All content and first-boot credentials are ready before the last fresh
// report and one-time permit. It does not reboot or declare first-boot/SSH trust.
func (s *BootSession) RunInstallation(ctx context.Context, input Input, files *Files, preparation *Preparation, source *HTTPFiles) (*Preparation, error) {
	if RequireInitramfs() != nil || preparation == nil || preparation.BootReview == nil || preparation.BootReview.State != "storage_approved" || source == nil || ctx.Err() != nil {
		return preparation, ErrUnavailable
	}
	plan, err := s.ApprovedStoragePlan()
	if err != nil {
		return preparation, err
	}
	stage, err := StageInstallation(ctx, input, files, source, preparation.BootReview.RuntimeInputSHA256)
	if err != nil {
		return preparation, err
	}
	defer stage.Close()
	ids, err := provisionstorage.NewGPTIdentity()
	if err != nil {
		return preparation, ErrUnavailable
	}
	secret, err := s.NewFirstBootSecret(ctx, input, files, stage, ids)
	if err != nil {
		return preparation, err
	}
	defer func() { secret.PrivateKey = "" }()
	encoded, err := json.Marshal(secret)
	if err != nil {
		return preparation, ErrUnavailable
	}
	defer clear(encoded)
	ca, err := readWriterBootCA()
	if err != nil {
		return preparation, err
	}
	writer, err := provisionwriter.NewLinux(ctx, provisionwriter.LinuxInputs{StoragePlan: *plan, GPTIdentity: ids, ManifestSHA256: input.ManifestSHA256, Manifest: files.Manifest, Preflight: files.Preflight, Seed: files.Seed, Runtime: stage.Runtime(), RootFS: stage.RootFSFile(), TargetUKI: stage.TargetUKIFile(), Tools: map[string]*os.File{"unsquashfs": stage.ToolFile("unsquashfs"), "mkfs.ext4": stage.ToolFile("mkfs.ext4"), "mkfs.vfat": stage.ToolFile("mkfs.vfat")}, ExpectedContents: stage.ContentsEvidence(), FirstBootSecret: encoded, BootCA: ca})
	if err != nil {
		return preparation, ErrUnavailable
	}
	// Close succeeds before writes and after an acknowledged verified write.
	// The adapter deliberately refuses Close after Quarantine/unknown.
	defer writer.Close()
	fresh, err := Prepare(ctx, input, files, LinuxHelpers{}, time.Now)
	if err != nil || fresh.Report == nil || fresh.Report.Status != "PASS" || writer.BindFreshReport(fresh.ReportBytes) != nil {
		return preparation, ErrUnavailable
	}
	if storePreparationAt("/run/keel/write-preflight", fresh) != nil {
		return preparation, ErrUnavailable
	}
	request := writer.Request()
	if request.FirstBootBinding == nil || request.FirstBootBinding.SHA256() != secret.Binding.SHA256() {
		return preparation, ErrUnavailable
	}
	if retainWriterJSON("write-request.json", request) != nil {
		return preparation, ErrUnavailable
	}
	preparation.Status = "write_permission_unknown"
	writeSession, err := s.BeginWrite(ctx, request, fresh.ReportBytes, source)
	if err != nil {
		// The permit may have committed remotely even though no local write
		// started. Do not treat loss as permission or retry the mutation.
		writer.Quarantine()
		return preparation, ErrUnavailable
	}
	permit := writeSession.Permit()
	var result provisionwriter.Result
	if retainWriterJSON("write-permit.json", permit) != nil {
		writer.Quarantine()
		result = provisionwriter.Result{Outcome: "unknown", Receipt: unknownWriteReceipt(permit, "local_evidence_unavailable")}
	} else {
		result, err = provisionwriter.Run(ctx, request, &permit, liveWriterAuthority{session: writeSession}, writer)
	}
	if result.Receipt == nil {
		result.Receipt = unknownWriteReceipt(permit, "writer_result_unknown")
	}
	if retainWriterJSON("write-result.json", result) != nil {
		writer.Quarantine()
		result.Outcome = "unknown"
		result.Receipt = unknownWriteReceipt(permit, "local_evidence_unavailable")
	}
	// Network lease cancellation cannot extend authority. A separate bounded
	// reporting context only attempts to retain an authenticated unknown; the
	// server still checks its current lease/source/scope and may reject it.
	reportCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	ack, reportErr := writeSession.Report(reportCtx, *result.Receipt)
	cancel()
	if reportErr != nil || retainWriterJSON("write-ack.json", ack) != nil {
		writer.Quarantine()
		preparation.Status = "installation_write_unknown"
		return preparation, ErrUnavailable
	}
	if result.Outcome != "written" || err != nil {
		writer.Quarantine()
		preparation.Status = "installation_write_unknown"
		return preparation, ErrUnavailable
	}
	preparation.InstallationPerformed = true
	preparation.Status = "awaiting_first_boot"
	if ack.RebootBefore != nil {
		preparation.Status = "reboot_pending"
		// A successful reboot does not return. A missing/expired short grant,
		// error or unexpected return never causes another reboot attempt.
		if writer.Reboot(ctx, ack) != nil {
			preparation.Status = "reboot_unknown"
			return preparation, ErrUnavailable
		}
	}
	return preparation, nil
}

func unknownWriteReceipt(p provisionboot.WritePermit, reason string) *provisionboot.WriteReceipt {
	return &provisionboot.WriteReceipt{PermitID: p.ID, StoragePlanSHA256: p.StoragePlanSHA256, GPTSHA256: p.GPTSHA256, RootFSSHA256: p.RootFSSHA256, TargetUKISHA256: p.TargetUKISHA256, Outcome: "unknown", Reason: reason}
}

func retainWriterJSON(name string, value any) error {
	if RequireInitramfs() != nil {
		return ErrUnavailable
	}
	switch name {
	case "write-request.json", "write-permit.json", "write-result.json", "write-ack.json":
	default:
		return ErrUnavailable
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw) == 0 || len(raw) > 192<<10 {
		return ErrUnavailable
	}
	// /run/keel was created root-only by the earlier preparation phase.
	f, err := os.OpenFile(filepath.Join("/run/keel", name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return ErrUnavailable
	}
	n, writeErr := f.Write(raw)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil || n != len(raw) {
		return ErrUnavailable
	}
	return nil
}

func readWriterBootCA() ([]byte, error) {
	if RequireInitramfs() != nil {
		return nil, ErrUnavailable
	}
	fd, err := unix.Open(BootCAPath, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnavailable
	}
	f := os.NewFile(uintptr(fd), "installer-public-ca")
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&0022 != 0 || st.Size < 1 || st.Size > 1<<20 {
		return nil, ErrUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil || int64(len(raw)) != st.Size {
		return nil, ErrUnavailable
	}
	return raw, nil
}
